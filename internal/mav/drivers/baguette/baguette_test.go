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
		drivers.CapErase,
	} {
		if !caps.Has(want) {
			t.Errorf("expected baguette to provide %s on sim", want)
		}
	}
	// Must NOT include capabilities baguette's CLI doesn't expose.
	for _, banned := range []drivers.Capability{
		drivers.CapRotate,
		drivers.CapW3CActions,
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

func TestProbeRunsListSanity(t *testing.T) {
	exec := newFake()
	d := New(exec)
	report := d.Probe(context.Background(), exec)
	if report.State != drivers.HealthOK {
		t.Fatalf("expected OK, got %s (%s)", report.State, report.Detail)
	}
	if len(exec.calls) != 1 || exec.calls[0] != "baguette list --json" {
		t.Fatalf("expected single `baguette list --json` call, got %v", exec.calls)
	}
}

func TestTapBuildsCoordArgsWithWidthHeight(t *testing.T) {
	exec := newFake()
	d := New(exec)
	_, err := d.Tap(context.Background(), simTarget(), drivers.TapSpec{X: 120, Y: 340})
	if err != nil {
		t.Fatal(err)
	}
	got := exec.calls[0]
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
	got := exec.calls[0]
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

func TestPinchUsesSpreadModel(t *testing.T) {
	exec := newFake()
	d := New(exec)
	err := d.Pinch(context.Background(), simTarget(), drivers.PinchSpec{
		X: 200, Y: 400, Scale: 2.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := exec.calls[0]
	for _, want := range []string{
		"baguette pinch",
		"--cx 200 --cy 400",
		"--startSpread 120.0 --endSpread 240.0",
		"--width 402 --height 874",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
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
	got := exec.calls[0]
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

func TestEraseSendsBackspaceKeys(t *testing.T) {
	exec := newFake()
	d := New(exec)
	if err := d.Erase(context.Background(), simTarget(), drivers.TextSpec{Text: "hola", Focused: true}); err != nil {
		t.Fatal(err)
	}
	if len(exec.calls) != 32 {
		t.Fatalf("expected 32 backspaces, got %d", len(exec.calls))
	}
	want := "baguette key --udid " + simUDID + " --code Backspace"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
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
