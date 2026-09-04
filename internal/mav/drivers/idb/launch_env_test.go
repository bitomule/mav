package idb

import (
	"context"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// idb forwards its own IDB_* variables to the app with the prefix stripped:
// that is the physical device's equivalent of SIMCTL_CHILD_*.
func TestLaunchCarriesTheEnvironmentAsIDBPrefixed(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"idb": true}}
	spec := drivers.LaunchSpec{BundleID: "com.example.app", Env: map[string]string{"FOO": "bar"}}
	if _, err := New(exec).Launch(context.Background(), drivers.Target{UDID: "REAL-1"}, spec); err != nil {
		t.Fatal(err)
	}
	want := "/usr/bin/env IDB_FOO=bar idb launch -f com.example.app --udid REAL-1"
	if exec.commands[0] != want {
		t.Fatalf("command=%q want=%q", exec.commands[0], want)
	}
}

// A variable named after one idb reads for itself would retarget idb instead
// of reaching the app. Refusing is the point: obeying it halfway is the bug
// this whole change exists to remove.
func TestLaunchRefusesEnvThatCollidesWithIDBsOwn(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"idb": true}}
	spec := drivers.LaunchSpec{BundleID: "com.example.app", Env: map[string]string{"UDID": "OTHER"}}
	_, err := New(exec).Launch(context.Background(), drivers.Target{UDID: "REAL-1"}, spec)
	if err == nil {
		t.Fatal("a colliding variable must fail loudly")
	}
	if !strings.Contains(err.Error(), "UDID") {
		t.Fatalf("the error must name the variable: %v", err)
	}
	if len(exec.commands) != 0 {
		t.Fatalf("nothing must be launched: %v", exec.commands)
	}
}

func TestLaunchWithoutEnvIsUnchanged(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"idb": true}}
	if _, err := New(exec).Launch(context.Background(), drivers.Target{UDID: "REAL-1"}, drivers.LaunchSpec{BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	want := "idb launch -f com.example.app --udid REAL-1"
	if exec.commands[0] != want {
		t.Fatalf("command=%q want=%q", exec.commands[0], want)
	}
}
