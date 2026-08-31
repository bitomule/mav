package simctl

import (
	"context"
	"reflect"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func TestAppearanceAndStatusBarCommands(t *testing.T) {
	exec := &fakeExec{}
	driver := New(exec)
	target := drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"}
	if err := driver.SetAppearance(context.Background(), target, "dark"); err != nil {
		t.Fatal(err)
	}
	if got := exec.args; !reflect.DeepEqual(got, []string{"simctl", "ui", "SIM-1", "appearance", "dark"}) {
		t.Fatalf("appearance args=%v", got)
	}
	spec := drivers.StatusBarSpec{Time: "9:41", CellularBars: "4", BatteryState: "charged", BatteryLevel: "100"}
	if err := driver.SetStatusBar(context.Background(), target, spec); err != nil {
		t.Fatal(err)
	}
	want := []string{"simctl", "status_bar", "SIM-1", "override", "--time", "9:41", "--cellularBars", "4", "--batteryState", "charged", "--batteryLevel", "100"}
	if got := exec.args; !reflect.DeepEqual(got, want) {
		t.Fatalf("status bar args=%v want %v", got, want)
	}
	if err := driver.ClearStatusBar(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if got := exec.args; !reflect.DeepEqual(got, []string{"simctl", "status_bar", "SIM-1", "clear"}) {
		t.Fatalf("clear args=%v", got)
	}
}

// TestStatusBarOverrideSkipsEmptyFields keeps the override additive: a caller
// that only wants the clock must not have the rest of the status bar reset
// underneath it by flags it never asked for.
func TestStatusBarOverrideSkipsEmptyFields(t *testing.T) {
	exec := &fakeExec{}
	driver := New(exec)
	target := drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"}
	if err := driver.SetStatusBar(context.Background(), target, drivers.StatusBarSpec{Time: "9:41"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"simctl", "status_bar", "SIM-1", "override", "--time", "9:41"}
	if got := exec.args; !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%v want %v", got, want)
	}
}

// TestAppearanceAndStatusBarAreSimOnly locks the capability gate at the driver
// level too: a device target must not see simctl advertise them at all.
func TestAppearanceAndStatusBarAreSimOnly(t *testing.T) {
	driver := New(&fakeExec{})
	provided := driver.Provides(drivers.Target{Kind: drivers.KindDevice, UDID: "REAL-1"})
	if provided.Has(drivers.CapAppearance) || provided.Has(drivers.CapStatusBar) {
		t.Fatalf("simctl must not advertise appearance or status bar on a device: %v", provided)
	}
	provided = driver.Provides(drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"})
	if !provided.Has(drivers.CapAppearance) || !provided.Has(drivers.CapStatusBar) {
		t.Fatalf("simctl must advertise appearance and status bar on a simulator: %v", provided)
	}
}
