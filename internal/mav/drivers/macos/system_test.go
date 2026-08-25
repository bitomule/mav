package macos

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func makeBundle(t *testing.T, name string, executables ...string) string {
	t.Helper()
	root := t.TempDir()
	app := filepath.Join(root, name+".app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, exe := range executables {
		if err := os.WriteFile(filepath.Join(macos, exe), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return app
}

// TestSystemLaunchesTheBinaryNotOpen: `open` does not propagate environment
// variables to the process it starts, and the environment is exactly how
// mav injects its configuration, the equivalent of the simulator's
// SIMCTL_CHILD_*.
func TestSystemLaunchesTheBinaryNotOpen(t *testing.T) {
	app := makeBundle(t, "Nokoru", "Nokoru")
	f := &fakeExec{tools: map[string]bool{"open": true}}
	res, err := NewSystem(f).Launch(context.Background(), drivers.Target{Kind: drivers.KindMac, AppPath: app}, drivers.LaunchSpec{BundleID: "com.example.app"})
	if err != nil {
		t.Fatal(err)
	}
	if res.PID == 0 {
		t.Fatal("it must return the pid of the launched process")
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "Contents/MacOS/Nokoru") {
		t.Fatalf("the bundle's binary must be executed, not `open`: %v", f.commands)
	}
	if strings.HasPrefix(f.commands[0], "open ") {
		t.Fatalf("`open` would lose the environment: %v", f.commands)
	}
}

// The binary is not always named after the bundle, so the only one present
// is picked before assuming the name.
func TestSystemResolvesASingleExecutableWhateverItsName(t *testing.T) {
	app := makeBundle(t, "Nokoru", "nNokoru")
	f := &fakeExec{}
	if _, err := NewSystem(f).Launch(context.Background(), drivers.Target{Kind: drivers.KindMac, AppPath: app}, drivers.LaunchSpec{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.commands[0], "nNokoru") {
		t.Fatalf("%v", f.commands)
	}
}

func TestSystemPrefersTheBundleNameWhenAmbiguous(t *testing.T) {
	app := makeBundle(t, "Nokoru", "helper", "Nokoru")
	f := &fakeExec{}
	if _, err := NewSystem(f).Launch(context.Background(), drivers.Target{Kind: drivers.KindMac, AppPath: app}, drivers.LaunchSpec{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(f.commands[0]), "/Nokoru") {
		t.Fatalf("with several candidates the naming convention rules: %v", f.commands)
	}
}

// Install on macOS copies nothing: it checks that the bundle is where the
// recipe said. It is the only part that can fail, and it gives a useful
// error when the build did not produce what it believed.
func TestSystemInstallVerifiesTheBundleExists(t *testing.T) {
	d := NewSystem(&fakeExec{})
	if err := d.Install(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.InstallSpec{Path: "/nope/Missing.app"}); err == nil {
		t.Fatal("a bundle that does not exist must fail here, not two layers down")
	}
	app := makeBundle(t, "Nokoru", "Nokoru")
	if err := d.Install(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.InstallSpec{Path: app}); err != nil {
		t.Fatalf("a valid bundle must not fail: %v", err)
	}
}

// TestSystemTerminateAsksForACleanQuit: the fixture needs the app to have
// closed its database, not to have been killed with the WAL half-written.
func TestSystemTerminateAsksForACleanQuit(t *testing.T) {
	f := &fakeExec{}
	if err := NewSystem(f).Terminate(context.Background(), drivers.Target{Kind: drivers.KindMac}, "com.example.app"); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "to quit") {
		t.Fatalf("a clean quit must be requested: %v", f.commands)
	}
	if strings.Contains(f.commands[0], "kill") {
		t.Fatalf("killing it would leave the WAL half-written: %v", f.commands)
	}
}

func TestSystemProvidesTerminateOnMac(t *testing.T) {
	// Without this, the shutdown before seeding a fixture was a silent
	// no-op and the fixture wrote while the previous instance kept the
	// sqlite open.
	caps := NewSystem(&fakeExec{}).Provides(drivers.Target{Kind: drivers.KindMac})
	if !caps.Has(drivers.CapTerminate) {
		t.Fatal("somebody has to provide CapTerminate on the Mac")
	}
	if len(NewSystem(&fakeExec{}).Provides(drivers.Target{Kind: drivers.KindSim})) != 0 {
		t.Fatal("on the simulator simctl rules")
	}
}

func TestSystemLocationIsHonestlyUnsupported(t *testing.T) {
	d := NewSystem(&fakeExec{})
	if err := d.SetLocation(context.Background(), drivers.Target{Kind: drivers.KindMac}, 1, 2); err == nil {
		t.Fatal("macOS does not allow overriding the location of an already launched app; it must say so")
	}
}
