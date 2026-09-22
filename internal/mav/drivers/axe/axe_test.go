package axe

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type fakeExec struct {
	tools    map[string]bool
	commands []string
	result   drivers.ExecResult
}

func (f *fakeExec) LookPath(name string) (string, error) {
	if f.tools[name] {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("missing")
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) drivers.ExecResult {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	return f.result
}

func (f *fakeExec) Start(context.Context, string, string, ...string) (int, error) { return 0, nil }

func TestTapBuildsAXeIDCommand(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"axe": true}}
	d := New(exec)
	_, err := d.Tap(context.Background(), drivers.Target{UDID: "SIM-1"}, drivers.TapSpec{Selector: drivers.ElementSelector{ID: "settings"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "axe tap --tap-style physical --udid SIM-1 --id settings"
	if exec.commands[0] != want {
		t.Fatalf("command=%q want=%q", exec.commands[0], want)
	}
}

// The regression for a tap that reported success and did nothing. AXe's
// default style routes a non-toggle target through FBSimulator's `tapAt`,
// which drops the touch under load and still exits 0: measured 6 of 12
// (then 0 of 8) taps landing with the default, against 10 of 10 with
// `physical`. Every selector shape has to carry the flag -- a tap that
// silently evaporates is indistinguishable from one that worked, so a
// path that loses it would go unnoticed for as long as this one did.
func TestEverySemanticTapPinsThePhysicalTapStyle(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec drivers.TapSpec
	}{
		{"by id", drivers.TapSpec{Selector: drivers.ElementSelector{ID: "settings"}}},
		{"by text", drivers.TapSpec{Selector: drivers.ElementSelector{Text: "Accesibilidad"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec := &fakeExec{tools: map[string]bool{"axe": true}}
			d := New(exec)
			if _, err := d.Tap(context.Background(), drivers.Target{UDID: "SIM-1"}, tc.spec); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(exec.commands[0], "--tap-style physical") {
				t.Fatalf("command=%q must pin --tap-style physical", exec.commands[0])
			}
		})
	}
}

func TestTreeReturnsRawJSON(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"axe": true}, result: drivers.ExecResult{Stdout: `{"AXLabel":"Home"}`}}
	d := New(exec)
	got, err := d.Tree(context.Background(), drivers.Target{}, drivers.TreeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.JSON) != `{"AXLabel":"Home"}` {
		t.Fatalf("json=%s", got.JSON)
	}
}

func TestEraseSendsOneBackspacePerDeletion(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"axe": true}}
	d := New(exec)
	if err := d.Erase(context.Background(), drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"}, drivers.TextSpec{Deletions: 3, Focused: true}); err != nil {
		t.Fatal(err)
	}
	if len(exec.commands) != 3 {
		t.Fatalf("expected 3 deletions, got %d: %v", len(exec.commands), exec.commands)
	}
	// HID usage 42, keyboard page: the same usage baguette names "Backspace"
	// and fails to deliver. Sent as a bare keycode because that is the only
	// shape `axe key` takes.
	if exec.commands[0] != "axe key 42 --udid SIM-1" {
		t.Fatalf("command=%q", exec.commands[0])
	}
}

func TestEraseIsSimulatorOnly(t *testing.T) {
	d := New(&fakeExec{tools: map[string]bool{"axe": true}})
	if !d.Provides(drivers.Target{Kind: drivers.KindSim}).Has(drivers.CapErase) {
		t.Fatal("axe must serve erase on a simulator")
	}
	// On a device AXe reads the tree and drives no keyboard, so declaring
	// erase there would route `ui erase` to a driver that cannot do it.
	if d.Provides(drivers.Target{Kind: drivers.KindDevice}).Has(drivers.CapErase) {
		t.Fatal("axe must not claim erase on a physical device")
	}
}
