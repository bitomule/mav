package macos

import (
	"context"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func cuaExecWithDoubleClick() *fakeExec {
	f := cuaExec()
	f.results["cua-driver call double_click"] = drivers.ExecResult{Stdout: `{"effect":"unverifiable"}`}
	return f
}

// TestCuaProvidesDoubleTapOnMac: without this declaration the router never
// even considers cua for a double click, and `mav ui doubletap` on a Mac
// dies asking for baguette, an iOS-simulator tool.
func TestCuaProvidesDoubleTapOnMac(t *testing.T) {
	d := NewCua(&fakeExec{})
	if !d.Provides(drivers.Target{Kind: drivers.KindMac}).Has(drivers.CapDoubleTap) {
		t.Fatal("cua must declare double_tap on mac: cua-driver ships a double_click tool")
	}
	if d.Provides(drivers.Target{Kind: drivers.KindSim}).Has(drivers.CapDoubleTap) {
		t.Fatal("cua must declare nothing off mac")
	}
}

// TestCuaDoubleTapBySelectorUsesTheElementToken: the token path is what
// works on background windows without moving the cursor, and it names the
// exact element instead of trusting a coordinate space.
func TestCuaDoubleTapBySelectorUsesTheElementToken(t *testing.T) {
	f := cuaExecWithDoubleClick()
	d := NewCua(f)
	spec := drivers.TapSpec{Selector: drivers.ElementSelector{Text: "Get started"}}
	if err := d.DoubleTap(context.Background(), macTarget(), spec); err != nil {
		t.Fatal(err)
	}
	var call string
	for _, c := range f.commands {
		if strings.Contains(c, "double_click") {
			call = c
		}
	}
	if call == "" {
		t.Fatalf("no double_click call issued: %v", f.commands)
	}
	if !strings.Contains(call, `"element_token":"s1:1"`) {
		t.Fatalf("must target the matched element by token: %q", call)
	}
}

// TestCuaDoubleTapByCoordinates: a canvas or custom-drawn surface has no AX
// element to point at, so the pixel path must stay reachable — and it must
// not pay for a window snapshot (tree + screenshot) it never reads: the pid
// is all the pixel path needs.
func TestCuaDoubleTapByCoordinates(t *testing.T) {
	f := cuaExecWithDoubleClick()
	d := NewCua(f)
	if err := d.DoubleTap(context.Background(), macTarget(), drivers.TapSpec{X: 60, Y: 35}); err != nil {
		t.Fatal(err)
	}
	var call string
	for _, c := range f.commands {
		if strings.Contains(c, "get_window_state") {
			t.Fatalf("the pixel path must not snapshot the window: %v", f.commands)
		}
		if strings.Contains(c, "double_click") {
			call = c
		}
	}
	if !strings.Contains(call, `"x":60`) || !strings.Contains(call, `"y":35`) {
		t.Fatalf("coordinates must reach the tool: %q", call)
	}
}

// TestCuaDoubleTapAtTheOrigin: (0,0) is a legal corner coordinate, not a
// "no target" sentinel — deciding whether the caller provided a target is
// the CLI's job, and a sentinel here would make the origin unclickable.
func TestCuaDoubleTapAtTheOrigin(t *testing.T) {
	f := cuaExecWithDoubleClick()
	d := NewCua(f)
	if err := d.DoubleTap(context.Background(), macTarget(), drivers.TapSpec{}); err != nil {
		t.Fatal(err)
	}
	var call string
	for _, c := range f.commands {
		if strings.Contains(c, "double_click") {
			call = c
		}
	}
	if !strings.Contains(call, `"x":0`) || !strings.Contains(call, `"y":0`) {
		t.Fatalf("an origin double click must be representable: %q", call)
	}
}

// TestCuaDoubleTapSelectorMissFails: the selector not matching must be an
// error, not a click on nothing.
func TestCuaDoubleTapSelectorMissFails(t *testing.T) {
	d := NewCua(cuaExecWithDoubleClick())
	spec := drivers.TapSpec{Selector: drivers.ElementSelector{Text: "No such label"}}
	if err := d.DoubleTap(context.Background(), macTarget(), spec); err == nil {
		t.Fatal("an unmatched selector must fail")
	}
}
