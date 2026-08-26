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
// element to point at, so the pixel path must stay reachable.
func TestCuaDoubleTapByCoordinates(t *testing.T) {
	f := cuaExecWithDoubleClick()
	d := NewCua(f)
	if err := d.DoubleTap(context.Background(), macTarget(), drivers.TapSpec{X: 60, Y: 35}); err != nil {
		t.Fatal(err)
	}
	var call string
	for _, c := range f.commands {
		if strings.Contains(c, "double_click") {
			call = c
		}
	}
	if !strings.Contains(call, `"x":60`) || !strings.Contains(call, `"y":35`) {
		t.Fatalf("coordinates must reach the tool: %q", call)
	}
}

// TestCuaDoubleTapWithoutATargetFails: silently double-clicking at (0,0)
// would land on the menu bar apple.
func TestCuaDoubleTapWithoutATargetFails(t *testing.T) {
	d := NewCua(cuaExecWithDoubleClick())
	if err := d.DoubleTap(context.Background(), macTarget(), drivers.TapSpec{}); err == nil {
		t.Fatal("a double tap with no selector and no coordinates must fail")
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
