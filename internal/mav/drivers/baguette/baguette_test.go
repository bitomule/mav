package baguette

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// fakeExec captures every baguette invocation as a single "args" string and
// returns canned responses per command key.
type fakeExec struct {
	tools     map[string]bool
	calls     []string
	responses map[string]drivers.ExecResult
}

func (f *fakeExec) LookPath(name string) (string, error) {
	if f.tools[name] {
		return "/usr/local/bin/" + name, nil
	}
	return "", fmt.Errorf("not on PATH")
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) drivers.ExecResult {
	key := name
	for _, a := range args {
		key += " " + a
	}
	f.calls = append(f.calls, key)
	if r, ok := f.responses[key]; ok {
		return r
	}
	return drivers.ExecResult{}
}

func (f *fakeExec) Start(_ context.Context, _ string, _ string, _ ...string) (int, error) {
	return 0, nil
}

func newFake() *fakeExec {
	return &fakeExec{
		tools:     map[string]bool{"baguette": true},
		responses: map[string]drivers.ExecResult{},
	}
}

const simUDID = "ABCDEF01-2345-6789-ABCD-EF0123456789"

func simTarget() drivers.Target { return drivers.Target{Kind: drivers.KindSim, UDID: simUDID} }
func devTarget() drivers.Target { return drivers.Target{Kind: drivers.KindDevice, UDID: simUDID} }

func TestProvidesEmptyOnDevice(t *testing.T) {
	d := New(newFake())
	if len(d.Provides(devTarget())) != 0 {
		t.Fatalf("expected empty caps on device, got %v", d.Provides(devTarget()))
	}
}

func TestProvidesAdvertisesSupportedCapsOnly(t *testing.T) {
	d := New(newFake())
	caps := d.Provides(simTarget())
	// Must include capabilities the new driver actually serves.
	for _, want := range []drivers.Capability{
		drivers.CapPinch,
		drivers.CapTwoFingerPan,
		drivers.CapHardwareBtn,
		drivers.CapScreenshot,
		drivers.CapType,
		drivers.CapTap,
		drivers.CapCoordTap,
		drivers.CapSwipe,
		drivers.CapTreeSystem,
		drivers.CapHideKeyboard,
	} {
		if !caps.Has(want) {
			t.Errorf("expected baguette to provide %s on sim", want)
		}
	}
	// Must NOT include capabilities baguette's CLI doesn't expose.
	for _, banned := range []drivers.Capability{
		drivers.CapRotate,
		drivers.CapW3CActions,
		drivers.CapErase,
	} {
		if caps.Has(banned) {
			t.Errorf("did not expect baguette to advertise %s — CLI does not expose it", banned)
		}
	}
}

func TestProbeMissing(t *testing.T) {
	exec := newFake()
	exec.tools = map[string]bool{}
	d := New(exec)
	if got := d.Probe(context.Background(), exec); got.State != drivers.HealthMissing {
		t.Fatalf("expected Missing, got %s", got.State)
	}
}

func TestProbeAcceptsTheMinimumVersion(t *testing.T) {
	exec := newFake()
	exec.responses["baguette --version"] = drivers.ExecResult{Stdout: "0.2.1\n"}
	d := New(exec)
	report := d.Probe(context.Background(), exec)
	if report.State != drivers.HealthOK {
		t.Fatalf("expected OK, got %s (%s)", report.State, report.Detail)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("expected a single `baguette --version` call, got %v", exec.calls)
	}
}

// 0.1.97 is what this machine had when Device Hub shipped: its gestures acked
// and landed nowhere, and it could not fold an iPhone Duo.
func TestProbeRefusesABaguetteOlderThanTheFloor(t *testing.T) {
	exec := newFake()
	exec.responses["baguette --version"] = drivers.ExecResult{Stdout: "0.1.97\n"}
	d := New(exec)
	report := d.Probe(context.Background(), exec)
	if report.IsHealthy() {
		t.Fatalf("expected baguette 0.1.97 to be refused, got %s", report.State)
	}
	if report.Next != "brew upgrade baguette" || !strings.Contains(report.Detail, "0.2.1") {
		t.Fatalf("expected the floor and the upgrade command, got detail=%q next=%q", report.Detail, report.Next)
	}
}

func TestTapBuildsCoordArgsWithWidthHeight(t *testing.T) {
	exec := newFake()
	d := New(exec)
	_, err := d.Tap(context.Background(), simTarget(), drivers.TapSpec{X: 120, Y: 340})
	if err != nil {
		t.Fatal(err)
	}
	got := exec.calls[len(exec.calls)-1]
	for _, want := range []string{
		"baguette tap",
		"--udid " + simUDID,
		"--x 120 --y 340",
		"--width 402 --height 874",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}

func TestTapRejectsSemanticSelector(t *testing.T) {
	d := New(newFake())
	_, err := d.Tap(context.Background(), simTarget(), drivers.TapSpec{
		Selector: drivers.ElementSelector{ID: "submit"},
	})
	if err == nil {
		t.Fatal("expected error: semantic taps go via AXe")
	}
}

// baguette spells the swipe endpoints with hyphens. This test used to pin the
// camelCase spelling -- and pass, because the fake executor accepts anything.
// The real binary answers "Missing expected argument '--start-x'", which
// nobody saw: AXe is canonical for CapSwipe and always won the route, so this
// code path was unreachable until a rotated simulator started routing around
// AXe. Revert the flag names and this test fails.
func TestSwipeUsesTheHyphenatedEndpointFlags(t *testing.T) {
	exec := newFake()
	d := New(exec)
	err := d.Swipe(context.Background(), simTarget(), drivers.SwipeSpec{
		StartX: 100, StartY: 200, EndX: 300, EndY: 400,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := exec.calls[len(exec.calls)-1]
	for _, want := range []string{
		"baguette swipe",
		"--start-x 100 --start-y 200",
		"--end-x 300 --end-y 400",
		"--width 402 --height 874",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
	if strings.Contains(got, "--startX") || strings.Contains(got, "--endX") {
		t.Errorf("the camelCase spelling baguette rejects is still being sent: %q", got)
	}
}

// Driver.Drag routes through the same `baguette swipe` subcommand as
// Driver.Swipe and must spell the endpoints the same way. It used to send
// the camelCase flags baguette rejects; this pins the hyphenated form.
func TestDragUsesTheHyphenatedEndpointFlags(t *testing.T) {
	exec := newFake()
	d := New(exec)
	err := d.Drag(context.Background(), simTarget(), drivers.DragSpec{
		StartX: 100, StartY: 200, EndX: 300, EndY: 400,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := exec.calls[len(exec.calls)-1]
	for _, want := range []string{
		"baguette swipe",
		"--start-x 100 --start-y 200",
		"--end-x 300 --end-y 400",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
	if strings.Contains(got, "--startX") || strings.Contains(got, "--endX") {
		t.Errorf("the camelCase spelling baguette rejects is still being sent: %q", got)
	}
}

// The camelCase spelling this test used to pin is what baguette answers with
// "Missing expected argument '--start-spread'": every `mav ui pinch` failed
// with exit 64, measured on 2026-10-01 against baguette 0.1.97 and 0.2.1.
func TestPinchUsesTheHyphenatedSpreadFlags(t *testing.T) {
	exec := newFake()
	d := New(exec)
	err := d.Pinch(context.Background(), simTarget(), drivers.PinchSpec{
		X: 200, Y: 400, Scale: 2.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := exec.calls[len(exec.calls)-1]
	for _, want := range []string{
		"baguette pinch",
		"--cx 200 --cy 400",
		"--start-spread 120.0 --end-spread 240.0",
		"--width 402 --height 874",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
	if strings.Contains(got, "--startSpread") || strings.Contains(got, "--endSpread") {
		t.Errorf("the camelCase spelling baguette rejects is still being sent: %q", got)
	}
}

const duoCoverLayout = `{"composite":{"height":715,"width":532},"identifier":"phone15","screen":{"height":678,"width":466,"x":35,"y":23}}`

// baguette divides every point by width/height, so the size has to be the lit
// panel's. iPhone Duo's cover is 466×678; sending iPhone 17 Pro's 402×874 put
// a tap at y=339 on y=263.
func TestGesturesUseTheLitPanelSizeFromChromeLayout(t *testing.T) {
	exec := newFake()
	exec.responses["baguette chrome layout --udid "+simUDID] = drivers.ExecResult{Stdout: duoCoverLayout}
	d := New(exec)
	if _, err := d.Tap(context.Background(), simTarget(), drivers.TapSpec{X: 233, Y: 339}); err != nil {
		t.Fatal(err)
	}
	if err := d.Swipe(context.Background(), simTarget(), drivers.SwipeSpec{StartX: 1, StartY: 2, EndX: 3, EndY: 4}); err != nil {
		t.Fatal(err)
	}
	layouts := 0
	for _, call := range exec.calls {
		if strings.HasPrefix(call, "baguette chrome layout") {
			layouts++
			continue
		}
		if !strings.Contains(call, "--width 466 --height 678") {
			t.Errorf("expected the cover's 466×678 in %q", call)
		}
	}
	if layouts != 1 {
		t.Errorf("expected one chrome layout read for two gestures, got %d", layouts)
	}
}

func TestForgetScreenSizeReadsTheLayoutAgain(t *testing.T) {
	exec := newFake()
	key := "baguette chrome layout --udid " + simUDID
	exec.responses[key] = drivers.ExecResult{Stdout: duoCoverLayout}
	d := New(exec)
	if _, err := d.Tap(context.Background(), simTarget(), drivers.TapSpec{X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	exec.responses[key] = drivers.ExecResult{Stdout: `{"screen":{"height":951,"width":669}}`}
	d.ForgetScreenSize(simUDID)
	if _, err := d.Tap(context.Background(), simTarget(), drivers.TapSpec{X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	if got := exec.calls[len(exec.calls)-1]; !strings.Contains(got, "--width 669 --height 951") {
		t.Errorf("expected the unfolded panel's 669×951 after forgetting, got %q", got)
	}
}

func TestPinchRejectsZeroScale(t *testing.T) {
	d := New(newFake())
	if err := d.Pinch(context.Background(), simTarget(), drivers.PinchSpec{Scale: 0}); err == nil {
		t.Fatal("expected error on Scale=0")
	}
}

func TestTwoFingerPanBuildsPanArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	err := d.TwoFingerPan(context.Background(), simTarget(), drivers.TwoFingerPanSpec{
		X: 200, Y: 400, PanX: 80, PanY: -40,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := exec.calls[len(exec.calls)-1]
	for _, want := range []string{
		"baguette pan",
		"--x1 140 --y1 400",
		"--x2 260 --y2 400",
		"--dx 80 --dy -40",
		"--width 402 --height 874",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}

func TestTypeBuildsArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	if err := d.Type(context.Background(), simTarget(), drivers.TextSpec{Text: "hola"}); err != nil {
		t.Fatal(err)
	}
	want := "baguette type --udid " + simUDID + " --text hola"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestPressButtonMapsVolumeNames(t *testing.T) {
	for _, c := range []struct {
		btn  drivers.HardwareButton
		want string
	}{
		{drivers.BtnHome, "home"},
		{drivers.BtnLock, "lock"},
		{drivers.BtnVolumeUp, "volumeUp"},
		{drivers.BtnVolumeDown, "volumeDown"},
	} {
		exec := newFake()
		d := New(exec)
		if err := d.PressButton(context.Background(), simTarget(), c.btn); err != nil {
			t.Fatalf("%s: %v", c.btn, err)
		}
		if !strings.Contains(exec.calls[0], "--button "+c.want) {
			t.Errorf("button=%s: expected `--button %s`, got %q", c.btn, c.want, exec.calls[0])
		}
	}
}

func TestPressButtonRejectsUnknown(t *testing.T) {
	d := New(newFake())
	if err := d.PressButton(context.Background(), simTarget(), drivers.HardwareButton("noisy")); err == nil {
		t.Fatal("expected error on unknown button")
	}
}

func TestTreeRunsDescribeUI(t *testing.T) {
	exec := newFake()
	exec.responses["baguette describe-ui --udid "+simUDID] = drivers.ExecResult{
		Stdout: `[{"id":"root"}]`,
	}
	d := New(exec)
	tree, err := d.Tree(context.Background(), simTarget(), drivers.TreeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if string(tree.JSON) != `[{"id":"root"}]` {
		t.Fatalf("unexpected tree: %s", string(tree.JSON))
	}
}

func TestScreenshotRequiresOutPath(t *testing.T) {
	d := New(newFake())
	if err := d.Screenshot(context.Background(), simTarget(), drivers.ScreenshotSpec{}); err == nil {
		t.Fatal("expected error when OutPath empty")
	}
}

func TestScreenshotBuildsArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	if err := d.Screenshot(context.Background(), simTarget(), drivers.ScreenshotSpec{OutPath: "/tmp/out.png"}); err != nil {
		t.Fatal(err)
	}
	want := "baguette screenshot --udid " + simUDID + " --output /tmp/out.png"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

// baguette must not offer to erase. Its HID keyboard delivers nothing into a
// focused simulator field (measured 2026-09-22: `baguette key --code
// Backspace` twice against value="a12345" left it at "a12345", while `axe key
// 42` took it to "a1234" then "a123"), so claiming the capability only bought
// callers an ok for work that did not happen.
func TestDoesNotClaimErase(t *testing.T) {
	d := New(newFake())
	if d.Provides(simTarget()).Has(drivers.CapErase) {
		t.Fatal("baguette must not declare CapErase: its HID keyboard delivers no keystroke")
	}
	if _, ok := any(d).(drivers.EraseDriver); ok {
		t.Fatal("baguette must not implement EraseDriver")
	}
}

func TestHideKeyboardSendsEscape(t *testing.T) {
	exec := newFake()
	d := New(exec)
	if err := d.HideKeyboard(context.Background(), simTarget()); err != nil {
		t.Fatal(err)
	}
	want := "baguette key --udid " + simUDID + " --code Escape"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestRotateAndW3CReturnUnsupported(t *testing.T) {
	d := New(newFake())
	if err := d.Rotate(context.Background(), simTarget(), drivers.RotateSpec{}); err == nil {
		t.Error("Rotate should return unsupported error")
	}
	if err := d.W3CActions(context.Background(), simTarget(), []byte("{}")); err == nil {
		t.Error("W3CActions should return unsupported error")
	}
}

func TestErrorOnNonZeroExit(t *testing.T) {
	exec := newFake()
	exec.responses["baguette type --udid "+simUDID+" --text x"] = drivers.ExecResult{
		Code:   2,
		Stderr: "type failed\n",
	}
	d := New(exec)
	if err := d.Type(context.Background(), simTarget(), drivers.TextSpec{Text: "x"}); err == nil || !strings.Contains(err.Error(), "exit 2") {
		t.Fatalf("expected exit 2 error, got %v", err)
	}
}
