package baguette

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// fakeExec captures every baguette invocation as a sorted "args" string and
// canned stdout/code per command key. Sufficient for unit tests of the driver.
type fakeExec struct {
	tools     map[string]bool
	calls     []string                  // each is "name arg1 arg2 ..."
	responses map[string]drivers.ExecResult // key is "name arg1 arg2 ..."
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

func TestProvidesIncludesMultitouchOnSim(t *testing.T) {
	d := New(newFake())
	caps := d.Provides(simTarget())
	for _, want := range []drivers.Capability{
		drivers.CapPinch, drivers.CapRotate, drivers.CapTwoFingerPan,
		drivers.CapHideKeyboard, drivers.CapErase, drivers.CapTreeSystem,
		drivers.CapHardwareBtn, drivers.CapW3CActions,
	} {
		if !caps.Has(want) {
			t.Errorf("expected baguette to provide %s on sim", want)
		}
	}
}

func TestProbeMissing(t *testing.T) {
	exec := newFake()
	exec.tools = map[string]bool{} // baguette absent
	d := New(exec)
	report := d.Probe(context.Background(), exec)
	if report.State != drivers.HealthMissing {
		t.Fatalf("expected Missing, got %s", report.State)
	}
}

func TestProbeRunsSanityCommand(t *testing.T) {
	exec := newFake()
	d := New(exec)
	report := d.Probe(context.Background(), exec)
	if report.State != drivers.HealthOK {
		t.Fatalf("expected OK, got %s (%s)", report.State, report.Detail)
	}
	if len(exec.calls) != 1 || exec.calls[0] != "baguette probe" {
		t.Fatalf("expected single `baguette probe` call, got %v", exec.calls)
	}
}

func TestProbeDegradedWhenSanityFails(t *testing.T) {
	exec := newFake()
	exec.responses["baguette probe"] = drivers.ExecResult{
		Err:    fmt.Errorf("exit 1"),
		Stderr: "SimulatorKit symbol not found\n",
	}
	d := New(exec)
	report := d.Probe(context.Background(), exec)
	if report.State != drivers.HealthDegraded {
		t.Fatalf("expected Degraded, got %s", report.State)
	}
}

func TestTapBuildsCoordArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	_, err := d.Tap(context.Background(), simTarget(), drivers.TapSpec{X: 120, Y: 340})
	if err != nil {
		t.Fatal(err)
	}
	want := "baguette --udid " + simUDID + " tap --x 120 --y 340"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestTapBuildsSemanticArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	_, err := d.Tap(context.Background(), simTarget(), drivers.TapSpec{
		Selector: drivers.ElementSelector{ID: "submit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exec.calls[0], "--id submit") {
		t.Fatalf("expected --id submit, got %q", exec.calls[0])
	}
	if strings.Contains(exec.calls[0], "--x") {
		t.Fatalf("did not expect coord flags, got %q", exec.calls[0])
	}
}

func TestPinchBuildsArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	err := d.Pinch(context.Background(), simTarget(), drivers.PinchSpec{
		X: 200, Y: 400, Scale: 1.5, DurationMs: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "baguette --udid " + simUDID + " pinch --x 200 --y 400 --scale 1.5 --duration 500"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestRotateBuildsArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	err := d.Rotate(context.Background(), simTarget(), drivers.RotateSpec{
		X: 100, Y: 200, Degrees: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "baguette --udid " + simUDID + " rotate --x 100 --y 200 --degrees 90"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestTwoFingerPanBuildsArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	err := d.TwoFingerPan(context.Background(), simTarget(), drivers.TwoFingerPanSpec{
		X: 100, Y: 100, PanX: 50, PanY: -20, HoldMs: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "baguette --udid " + simUDID + " two-finger-pan --x 100 --y 100 --pan-x 50 --pan-y -20 --hold 200"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestHideKeyboardBuildsArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	if err := d.HideKeyboard(context.Background(), simTarget()); err != nil {
		t.Fatal(err)
	}
	want := "baguette --udid " + simUDID + " hide-keyboard"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestTypeBuildsArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	if err := d.Type(context.Background(), simTarget(), drivers.TextSpec{Text: "hola", Focused: true}); err != nil {
		t.Fatal(err)
	}
	want := "baguette --udid " + simUDID + " type --text hola --focused"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestEraseBuildsArgs(t *testing.T) {
	exec := newFake()
	d := New(exec)
	if err := d.Erase(context.Background(), simTarget(), drivers.TextSpec{Focused: true}); err != nil {
		t.Fatal(err)
	}
	want := "baguette --udid " + simUDID + " erase --focused"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestPressButton(t *testing.T) {
	exec := newFake()
	d := New(exec)
	if err := d.PressButton(context.Background(), simTarget(), drivers.BtnHome); err != nil {
		t.Fatal(err)
	}
	want := "baguette --udid " + simUDID + " button home"
	if exec.calls[0] != want {
		t.Fatalf("got=%q want=%q", exec.calls[0], want)
	}
}

func TestPressButtonRejectsUnknown(t *testing.T) {
	exec := newFake()
	d := New(exec)
	err := d.PressButton(context.Background(), simTarget(), drivers.HardwareButton("noisy-stick"))
	if err == nil {
		t.Fatal("expected error on unknown button")
	}
}

func TestTreeReturnsJSON(t *testing.T) {
	exec := newFake()
	exec.responses["baguette --udid "+simUDID+" tree --json"] = drivers.ExecResult{
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

func TestW3CActionsWritesTempFile(t *testing.T) {
	exec := newFake()
	d := New(exec)
	if err := d.W3CActions(context.Background(), simTarget(), []byte(`{"actions":[]}`)); err != nil {
		t.Fatal(err)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("expected one call, got %d", len(exec.calls))
	}
	if !strings.Contains(exec.calls[0], "actions --file ") {
		t.Fatalf("expected actions --file ..., got %q", exec.calls[0])
	}
}

func TestErrorOnNonZeroExit(t *testing.T) {
	exec := newFake()
	exec.responses["baguette --udid "+simUDID+" hide-keyboard"] = drivers.ExecResult{
		Code:   2,
		Stderr: "keyboard not visible\n",
	}
	d := New(exec)
	err := d.HideKeyboard(context.Background(), simTarget())
	if err == nil || !strings.Contains(err.Error(), "exit 2") {
		t.Fatalf("expected exit 2 error, got %v", err)
	}
}
