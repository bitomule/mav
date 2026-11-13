package axe

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type fakeExec struct {
	tools    map[string]bool
	commands []string
	argv     [][]string
	result   drivers.ExecResult
	handle   func(string, []string) drivers.ExecResult
	inputs   []string
}

func (f *fakeExec) LookPath(name string) (string, error) {
	if f.tools[name] {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("missing")
}
func (f *fakeExec) Run(_ context.Context, name string, args ...string) drivers.ExecResult {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	f.argv = append(f.argv, append([]string{}, args...))
	if f.handle != nil {
		return f.handle(name, args)
	}
	if f.result.Stdout == "" && f.result.Err == nil && f.result.Code == 0 {
		return drivers.ExecResult{Stdout: okOutput(`{}`)}
	}
	return f.result
}
func (f *fakeExec) RunInput(ctx context.Context, input, name string, args ...string) drivers.ExecResult {
	f.inputs = append(f.inputs, input)
	return f.Run(ctx, name, args...)
}
func (f *fakeExec) Start(context.Context, string, string, ...string) (int, error) { return 0, nil }
func okOutput(result string) string {
	return `{"schema":"axe.output.command-result","schemaVersion":"1","didError":false,"data":{"result":` + result + `}}`
}
func fake() *fakeExec { return &fakeExec{tools: map[string]bool{"axe": true}} }
func TestTapPreservesAllSelectorAndTimingFields(t *testing.T) {
	f := fake()
	d := New(f)
	_, err := d.Tap(context.Background(), drivers.Target{UDID: "SIM-1"}, drivers.TapSpec{Selector: drivers.ElementSelector{Value: "75"}, ElementType: "Slider", Duration: 400, Count: 2, Touches: 2, PreDelayMs: 10, PostDelayMs: 20, WaitTimeout: 3, PollInterval: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--output", "json", "tap", "--tap-style", "physical", "--value", "75", "--element-type", "Slider", "--hold", "0.4", "--count", "2", "--touches", "2", "--pre-delay", "0.01", "--post-delay", "0.02", "--wait-timeout", "3", "--poll-interval", "0.1", "--verbose", "--udid", "SIM-1"}
	if !reflect.DeepEqual(f.argv[0], want) {
		t.Fatalf("got %q want %q", f.argv[0], want)
	}
}
func TestEverySemanticTapPinsThePhysicalTapStyle(t *testing.T) {
	for _, s := range []drivers.ElementSelector{{ID: "settings"}, {Text: "Accesibilidad"}, {Value: "1"}} {
		f := fake()
		if _, err := New(f).Tap(context.Background(), drivers.Target{}, drivers.TapSpec{Selector: s}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(f.commands[0], "--tap-style physical") {
			t.Fatal(f.commands)
		}
	}
}
func TestTreeUnwrapsEnvelopeAndSelectedRoots(t *testing.T) {
	for _, payload := range []string{`[{"AXLabel":"Home"}]`, `{"ax":{"roots":[{"AXLabel":"Home"}]},"outcome":"enqueuedAndDrained","cleanup":"released","enqueuedEventCount":0}`} {
		f := fake()
		f.result.Stdout = okOutput(payload)
		got, err := New(f).Tree(context.Background(), drivers.Target{}, drivers.TreeSpec{})
		if err != nil {
			t.Fatal(err)
		}
		if string(got.JSON) != `[{"AXLabel":"Home"}]` {
			t.Fatal(string(got.JSON))
		}
	}
}
func TestTreeAtPointKeepsPointInNativeUnits(t *testing.T) {
	f := fake()
	f.result.Stdout = okOutput(`{"AXLabel":"Home"}`)
	got, err := New(f).TreeAtPoint(context.Background(), drivers.Target{}, 200, 400)
	if err != nil || string(got.JSON) != `{"AXLabel":"Home"}` {
		t.Fatal(got, err)
	}
	if !strings.Contains(f.commands[0], "--point 200,400") {
		t.Fatal(f.commands)
	}
}
func TestEraseUsesOneOrderedKeyboardSessionOnHardware(t *testing.T) {
	f := fake()
	err := New(f).Erase(context.Background(), drivers.Target{Kind: drivers.KindDevice, UDID: "DEVICE-1"}, drivers.TextSpec{Deletions: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "key-sequence --keycodes 42,42,42 --delay 0") {
		t.Fatal(f.commands)
	}
}
func TestEraseStopsWithoutReplayingAfterPartialInput(t *testing.T) {
	f := fake()
	f.result = drivers.ExecResult{Stdout: `{"schema":"axe.output.command-result","schemaVersion":"1","didError":true,"data":{"result":{"outcome":"failedNoReplay","cleanup":"released","enqueuedEventCount":2}},"error":{"code":"inputFailed","message":"stopped"}}`, Err: errors.New("exit 1")}
	err := New(f).Erase(context.Background(), drivers.Target{}, drivers.TextSpec{Deletions: 205})
	var de *drivers.DriverError
	if !errors.As(err, &de) || de.Delivery != drivers.DeliverySent || len(f.commands) != 1 {
		t.Fatalf("%v %v", err, f.commands)
	}
}
func TestProbeRequiresMinimumVersionAndRunsOnce(t *testing.T) {
	for _, tc := range []struct {
		version string
		healthy bool
	}{{"v2.0.4", true}, {"2.1.0", true}, {"v1.5.0", false}, {"v2.0.3", false}, {"nonsense", false}, {"", false}} {
		t.Run(tc.version, func(t *testing.T) {
			f := fake()
			f.handle = func(string, []string) drivers.ExecResult { return drivers.ExecResult{Stdout: tc.version} }
			d := New(f)
			a := d.Probe(context.Background(), f)
			b := d.Probe(context.Background(), f)
			if a.IsHealthy() != tc.healthy || b.IsHealthy() != tc.healthy || len(f.commands) != 1 {
				t.Fatal(a, b, f.commands)
			}
			if !tc.healthy && !strings.Contains(a.Next, MinimumVersion) {
				t.Fatal(a)
			}
		})
	}
}
func TestProbeReportsMissingAndKilledBinary(t *testing.T) {
	f := fake()
	f.tools = nil
	if New(f).Probe(context.Background(), f).State != drivers.HealthMissing {
		t.Fatal("not missing")
	}
	f = fake()
	f.result = drivers.ExecResult{Err: errors.New("signal: killed")}
	health := New(f).Probe(context.Background(), f)
	if health.State != drivers.HealthBroken || !strings.Contains(health.Detail, "signal: killed") {
		t.Fatal(health)
	}
}
func TestTypedOutputPreservesEvidenceAndUnknownDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		delivery     drivers.InputDelivery
		mutation     bool
	}{
		{"typed partial", `{"schema":"axe.output.command-result","schemaVersion":"1","didError":true,"data":{"result":{"outcome":"failedNoReplay","enqueuedEventCount":2,"cleanup":"released"}},"error":{"code":"inputFailed","message":"stopped"}}`, drivers.DeliverySent, true},
		{"before input", `{"schema":"axe.output.command-result","schemaVersion":"1","didError":true,"data":{"result":{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"notNeeded"}},"error":{"code":"targetNotFound","message":"missing"}}`, drivers.DeliveryNotSent, true},
		{"quarantine", `{"schema":"axe.output.command-result","schemaVersion":"1","didError":true,"data":{"result":{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"uncertain","quarantine":{"persisted":true}}},"error":{"code":"cleanupUncertain","message":"blocked"}}`, drivers.DeliveryUncertain, true},
		{"lost response", "", drivers.DeliveryUncertain, true},
		{"raw legacy JSON", `{"AXLabel":"Home"}`, drivers.DeliveryNotSent, false},
		{"invalid schema", `{"schema":"other","schemaVersion":"1","didError":false,"data":{}}`, drivers.DeliveryUncertain, true},
		{"truncated", `{"schema":`, drivers.DeliveryUncertain, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeOutput(drivers.ExecResult{Stdout: tc.output, Err: errors.New("failed")}, tc.mutation)
			var typed *drivers.DriverError
			if !errors.As(err, &typed) || typed.Delivery != tc.delivery {
				t.Fatalf("%#v %v", typed, err)
			}
		})
	}
}
func TestBatchDeliveryIncludesEarlierCompletedSteps(t *testing.T) {
	raw := `{"schema":"axe.output.command-result","schemaVersion":"1","didError":true,"data":{"results":[{"outcome":"enqueuedAndDrained","enqueuedEventCount":2,"cleanup":"released"},{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"notNeeded"}]},"error":{"code":"targetNotFound","message":"missing"}}`
	_, err := decodeOutput(drivers.ExecResult{Stdout: raw}, true)
	var typed *drivers.DriverError
	if !errors.As(err, &typed) || typed.Delivery != drivers.DeliverySent || typed.Receipt.EnqueuedEventCount != 2 {
		t.Fatalf("%#v", typed)
	}
}
func TestSelectedContextAndFocusRequirements(t *testing.T) {
	f := fake()
	id := uint32(1)
	target := drivers.Target{InputContext: &drivers.InputContext{Selected: true, ScreenID: &id, ScreenUUID: "SCREEN-UUID", ExpectedPID: 123}}
	_, err := New(f).Key(context.Background(), target, drivers.KeySpec{Code: 42, FocusID: "search"})
	if err != nil {
		t.Fatal(err)
	}
	command := f.commands[0]
	for _, part := range []string{"--input-mode selected", "--screen-id 1", "--screen-uuid SCREEN-UUID", "--expected-pid 123", "--focus-id search"} {
		if !strings.Contains(command, part) {
			t.Fatal(command)
		}
	}
	f = fake()
	if _, err := New(f).Key(context.Background(), target, drivers.KeySpec{Code: 42}); err == nil || len(f.commands) != 0 {
		t.Fatal(err, f.commands)
	}
}
func TestTypeUsesStdinWithoutArgumentInjection(t *testing.T) {
	f := fake()
	text := "--agent\nhello 'world'"
	err := New(f).Type(context.Background(), drivers.Target{}, drivers.TextSpec{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.inputs, []string{text}) || strings.Contains(f.commands[0], text) || !strings.Contains(f.commands[0], "--stdin") {
		t.Fatal(f.commands, f.inputs)
	}
}
func TestTargetCapabilityMatrix(t *testing.T) {
	d := New(fake())
	if !d.Provides(drivers.Target{Kind: drivers.KindDevice}).Has(drivers.CapErase) {
		t.Fatal("hardware erase missing")
	}
	if d.Provides(drivers.Target{Kind: drivers.KindDevice}).Has(drivers.CapSlider) {
		t.Fatal("hardware slider advertised")
	}
	if len(d.Provides(drivers.Target{Kind: drivers.KindMac})) != 0 {
		t.Fatal("mac advertised")
	}
}
func TestNewNativeOperationsBuildTypedArguments(t *testing.T) {
	for _, tc := range []struct {
		name     string
		call     func(*Driver) (drivers.NativeResult, error)
		contains string
	}{
		{"keys", func(d *Driver) (drivers.NativeResult, error) {
			return d.KeyCombo(context.Background(), drivers.Target{}, drivers.KeyComboSpec{Modifiers: []int{227, 225}, Key: 4})
		}, "key-combo --modifiers 227,225 --key 4"},
		{"touch", func(d *Driver) (drivers.NativeResult, error) {
			return d.Touch(context.Background(), drivers.Target{}, drivers.TouchSpec{X: 10, Y: 20, Down: true, Up: true})
		}, "touch -x 10 -y 20 --down --up"},
		{"draw", func(d *Driver) (drivers.NativeResult, error) {
			return d.Draw(context.Background(), drivers.Target{}, drivers.DrawSpec{Points: []drivers.PathPoint{{X: 1, Y: 2}, {X: 3, Y: 4}}, Close: true})
		}, "draw 1,2 3,4 --close"},
		{"settings", func(d *Driver) (drivers.NativeResult, error) {
			return d.Settings(context.Background(), drivers.Target{}, drivers.SettingsSpec{Appearance: "dark"})
		}, "settings set --appearance dark"},
		{"settings read", func(d *Driver) (drivers.NativeResult, error) {
			return d.Settings(context.Background(), drivers.Target{}, drivers.SettingsSpec{})
		}, "settings show"},
		{"location", func(d *Driver) (drivers.NativeResult, error) {
			lat, lon := 1.5, -0.1
			return d.Location(context.Background(), drivers.Target{}, drivers.LocationSpec{Action: "set", Latitude: &lat, Longitude: &lon})
		}, "settings location set --latitude 1.5 --longitude -0.1"},
		{"discovery", func(d *Driver) (drivers.NativeResult, error) { return d.Discover(context.Background()) }, "list-devices"},
		{"readiness", func(d *Driver) (drivers.NativeResult, error) {
			return d.Readiness(context.Background(), drivers.Target{UDID: "DEVICE-1"})
		}, "doctor --udid DEVICE-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fake()
			if _, err := tc.call(New(f)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(f.commands[0], tc.contains) {
				t.Fatal(f.commands)
			}
		})
	}
}
func TestInvalidLaterBatchStepSendsNothing(t *testing.T) {
	f := fake()
	d := New(f)
	_, err := d.Batch(context.Background(), drivers.Target{}, drivers.BatchSpec{Steps: []drivers.BatchStep{{Tap: &drivers.TapSpec{X: 1, Y: 2}}, {Key: &drivers.KeySpec{Code: 256}}}})
	var typed *drivers.DriverError
	if !errors.As(err, &typed) || typed.Delivery != drivers.DeliveryNotSent || len(f.commands) != 0 {
		t.Fatal(err, f.commands)
	}
}
func TestBatchQuotesTypedUserValues(t *testing.T) {
	f := fake()
	_, err := New(f).Batch(context.Background(), drivers.Target{}, drivers.BatchSpec{Steps: []drivers.BatchStep{{Tap: &drivers.TapSpec{Selector: drivers.ElementSelector{Text: "Save 'item'"}}}, {Type: &drivers.TextSpec{Text: "hello --agent"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 {
		t.Fatal(f.commands)
	}
	steps := []string{}
	for i, a := range f.argv[0] {
		if a == "--step" {
			steps = append(steps, f.argv[0][i+1])
		}
	}
	if len(steps) != 2 || !strings.Contains(steps[0], `'Save '"'"'item'"'"''`) || !strings.Contains(steps[1], `'hello --agent'`) {
		t.Fatalf("%q", steps)
	}
}
func TestUnsupportedHardwareOperationNeverInvokesTool(t *testing.T) {
	f := fake()
	d := New(f)
	percent := 50.0
	for _, call := range []func() error{
		func() error {
			_, e := d.Slider(context.Background(), drivers.Target{Kind: drivers.KindDevice}, drivers.SliderSpec{Selector: drivers.ElementSelector{ID: "volume"}, Percent: &percent})
			return e
		},
		func() error {
			_, e := d.Touch(context.Background(), drivers.Target{Kind: drivers.KindDevice}, drivers.TouchSpec{X: 1, Y: 2, Down: true})
			return e
		},
		func() error {
			_, e := d.Button(context.Background(), drivers.Target{Kind: drivers.KindDevice}, drivers.ButtonSpec{Button: drivers.BtnLock})
			return e
		},
	} {
		if call() == nil {
			t.Fatal("unsupported action accepted")
		}
	}
	if len(f.commands) != 0 {
		t.Fatal(f.commands)
	}
}
func TestHingeReturnsOnlyConfirmedAngle(t *testing.T) {
	f := fake()
	f.result.Stdout = okOutput(`{"hinge":{"degrees":90}}`)
	got, err := New(f).Hinge(context.Background(), drivers.Target{}, drivers.HingeSpec{})
	if err != nil || got.Angle == nil || *got.Angle != 90 {
		t.Fatal(got, err)
	}
}
func TestValidEnvelopesRemainJSON(t *testing.T) {
	if !json.Valid([]byte(okOutput(`[]`))) {
		t.Fatal("fixture")
	}
}

func TestSemanticPressSelectsSimulatorRouteWithoutExplicitStyle(t *testing.T) {
	for _, kind := range []drivers.TargetKind{drivers.KindSim, drivers.KindDevice} {
		f := fake()
		_, err := New(f).Tap(context.Background(), drivers.Target{Kind: kind}, drivers.TapSpec{Selector: drivers.ElementSelector{ID: "toggle"}, SemanticPress: true})
		if err != nil {
			t.Fatal(err)
		}
		command := f.commands[0]
		if strings.Contains(command, "--tap-style") || !strings.Contains(command, "--semantic-press") {
			t.Fatal(command)
		}
		if (kind == drivers.KindSim) != strings.Contains(command, "--input-mode selected") {
			t.Fatal(command)
		}
	}
}
func TestUnsupportedTypeCharactersAndLimitsSendNothing(t *testing.T) {
	for _, text := range []string{"café", strings.Repeat("a", 257)} {
		f := fake()
		err := New(f).Type(context.Background(), drivers.Target{Kind: drivers.KindDevice}, drivers.TextSpec{Text: text})
		if err == nil || len(f.commands) != 0 {
			t.Fatal(err, f.commands)
		}
	}
}
func TestSwipeAndDragPreserveNativeTimingControls(t *testing.T) {
	f := fake()
	d := New(f)
	err := d.Swipe(context.Background(), drivers.Target{}, drivers.SwipeSpec{StartX: 1, StartY: 2, EndX: 3, EndY: 4, DurationMs: 600, PreDelayMs: 20, PostDelayMs: 30, Delta: 5, UntilID: "finish", MaxSwipes: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"--duration 0.6", "--pre-delay 0.02", "--post-delay 0.03", "--delta 5", "--until-id finish", "--max-swipes 3"} {
		if !strings.Contains(f.commands[0], part) {
			t.Fatal(f.commands)
		}
	}
	err = d.Drag(context.Background(), drivers.Target{}, drivers.DragSpec{StartX: 1, StartY: 2, EndX: 3, EndY: 4, DurationMs: 600, PressMs: 700, Steps: 80, PreDelayMs: 20, PostDelayMs: 30})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"--hold 0.7", "--steps 80", "--pre-delay 0.02", "--post-delay 0.03"} {
		if !strings.Contains(f.commands[1], part) {
			t.Fatal(f.commands)
		}
	}
}

func TestEraseLaterRefusalPreservesEarlierChunkDelivery(t *testing.T) {
	f := fake()
	calls := 0
	f.handle = func(string, []string) drivers.ExecResult {
		calls++
		if calls == 1 {
			return drivers.ExecResult{Stdout: okOutput(`{"outcome":"enqueuedAndDrained","enqueuedEventCount":200,"cleanup":"released"}`)}
		}
		return drivers.ExecResult{Stdout: `{"schema":"axe.output.command-result","schemaVersion":"1","didError":true,"data":{"result":{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"notNeeded"}},"error":{"code":"targetNotFound","message":"missing"}}`}
	}
	err := New(f).Erase(context.Background(), drivers.Target{}, drivers.TextSpec{Deletions: 205})
	var typed *drivers.DriverError
	if !errors.As(err, &typed) || typed.Delivery != drivers.DeliverySent || calls != 2 || !strings.Contains(string(typed.Details), `"completedDeletions":100`) {
		t.Fatalf("%v calls=%d", err, calls)
	}
}

func TestHingePreservesMavNamedPoses(t *testing.T) {
	for pose, want := range map[string]string{"closed": "0", "open": "130", "flat": "180"} {
		f := fake()
		f.result.Stdout = okOutput(`{"hinge":{"degrees":130}}`)
		_, err := New(f).Hinge(context.Background(), drivers.Target{}, drivers.HingeSpec{Pose: pose})
		if err != nil || !strings.Contains(f.commands[0], "hinge set "+want) {
			t.Fatal(pose, err, f.commands)
		}
	}
}
