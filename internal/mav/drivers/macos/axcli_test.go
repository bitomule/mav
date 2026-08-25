package macos

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// TestAxcliCoversWhatCuaCannotReach: cua-driver is the canonical one, but
// it resolves the window through `list_windows`, which only enumerates
// layer 0. A floating UI, panel, HUD, popover, onboarding, is invisible to
// it. axcli targets by `--app` and needs no window id, so it reaches
// exactly those windows: that is why it stays in the mix even though cua
// covers the same capabilities.
func TestAxcliCoversWhatCuaCannotReach(t *testing.T) {
	ax := NewAxcli(&fakeExec{})
	cua := NewCua(&fakeExec{})
	mac := macTarget()

	caps := ax.Provides(mac)
	if !caps.Has(drivers.CapSemanticTap) || !caps.Has(drivers.CapType) {
		t.Fatalf("axcli must serve input: %v", caps)
	}
	if !cua.Provides(mac).Has(drivers.CapSemanticTap) {
		t.Fatal("cua is the canonical input provider")
	}
	if caps.Has(drivers.CapScreenshot) {
		t.Fatal("axcli's capture returned the desktop without saying so; it is not declared")
	}
	// Behind cua-driver: cg-pid synthesizes a mouse event and there are
	// SwiftUI buttons that accept it without reacting, while cua-driver's
	// AXPress does press them. It stays cheap because when cua cannot
	// resolve the window it is the only path.
	if ax.Cost(drivers.CapSemanticTap, mac) <= cua.Cost(drivers.CapSemanticTap, mac) {
		t.Fatal("cua-driver is the canonical input provider; axcli goes behind")
	}
	// Typing is NOT background-safe even here: `fill` activates the app
	// before typing, in the code and with no flag to avoid it. It is
	// declared expensive so it stays on record that it is not free, even if
	// today it competes with nobody.
	if ax.Cost(drivers.CapType, mac) == 0 {
		t.Fatal("typing activates the app: it cannot be advertised as the canonical path")
	}
}

// TestAxcliRefusesCoordinateTaps: `mouse click` is global, ignores --app,
// moves the real cursor and fires on whatever window is on top. Accepting
// it here would be selling as background-safe something that is not.
func TestAxcliRefusesCoordinateTaps(t *testing.T) {
	f := &fakeExec{tools: map[string]bool{"axcli": true}}
	_, err := NewAxcli(f).Tap(context.Background(), macTarget(), drivers.TapSpec{X: 10, Y: 20})
	if err == nil || !strings.Contains(err.Error(), "background-safe") {
		t.Fatalf("a coordinate tap is not background-safe and must be refused: %v", err)
	}
	if len(f.commands) != 0 {
		t.Fatalf("nothing must be executed: %v", f.commands)
	}
}

func TestAxcliTapAsksForPIDDeliveryExplicitly(t *testing.T) {
	f := &fakeExec{tools: map[string]bool{"axcli": true}}
	if _, err := NewAxcli(f).Tap(context.Background(), macTarget(), drivers.TapSpec{
		Selector: drivers.ElementSelector{ID: "saveButton"},
	}); err != nil {
		t.Fatal(err)
	}
	// --app goes AFTER the subcommand in 0.1.0, not before: it is a
	// subcommand flag, not a global one. Verified against the binary the
	// formula installs.
	// --strategy cg-pid explicit: it is the entire property axcli is in the
	// mix for. Without it, its click activates the app and clicks by
	// coordinates, which is what opened the user's mail in one trial.
	if len(f.commands) != 1 || !strings.HasPrefix(f.commands[0], "axcli click --strategy cg-pid --pid") {
		t.Fatalf("the tap must request PID delivery explicitly: %v", f.commands)
	}
	if !strings.Contains(f.commands[0], `identifier="saveButton"`) {
		t.Fatalf("the selector must go by identifier: %v", f.commands)
	}
}

// TestAxcliErrorKeepsTheToolMessage: axcli returns exit 1 for everything
// and the reason lives only in the stderr text.
func TestAxcliErrorKeepsTheToolMessage(t *testing.T) {
	f := &fakeExec{
		tools: map[string]bool{"axcli": true},
		results: map[string]drivers.ExecResult{
			"axcli": {Stderr: "Found app: X (pid=1)\nerror: locator not found: [identifier=\"nope\"]\n", Code: 1, Err: errors.New("exit status 1")},
		},
	}
	_, err := NewAxcli(f).Tap(context.Background(), macTarget(), drivers.TapSpec{
		Selector: drivers.ElementSelector{ID: "nope"},
	})
	if err == nil {
		t.Fatal("it must fail")
	}
	// stderr also carries status lines ("Found app: ..."), so the error
	// line must be kept, not the first one.
	if !strings.Contains(err.Error(), "locator not found") {
		t.Fatalf("the real reason must survive: %v", err)
	}
	if strings.Contains(err.Error(), "Found app") {
		t.Fatalf("status lines are not the error: %v", err)
	}
}

func TestAxcliNeedsAnAppToTarget(t *testing.T) {
	f := &fakeExec{tools: map[string]bool{"axcli": true}}
	_, err := NewAxcli(f).Tap(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.TapSpec{
		Selector: drivers.ElementSelector{ID: "x"},
	})
	if err == nil {
		t.Fatal("axcli demands --app or --pid on every targeted command")
	}
}

func TestAxcliDoesNotProvideTheTree(t *testing.T) {
	// Its tree is indented text, truncated and without geometry or state.
	// Declaring it would force the router to choose between two different
	// formats for a reason unrelated to data quality.
	caps := NewAxcli(&fakeExec{}).Provides(macTarget())
	if caps.Has(drivers.CapTreeAX) {
		t.Fatal("the tree comes from cua-driver, which emits JSON with geometry")
	}
}
