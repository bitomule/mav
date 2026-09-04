package simctl

import (
	"context"
	"reflect"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// simctl copies its own SIMCTL_CHILD_* variables into the app it spawns, so
// a launch that carries an environment has to go through /usr/bin/env with
// that prefix. Without this the variable was dropped and nobody was told.
func TestLaunchCarriesTheEnvironmentAsSimctlChild(t *testing.T) {
	exec := &fakeExec{}
	target := drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"}
	spec := drivers.LaunchSpec{BundleID: "com.example.app", Env: map[string]string{"FOO": "bar", "A": "1"}}
	if _, err := New(exec).Launch(context.Background(), target, spec); err != nil {
		t.Fatal(err)
	}
	if exec.name != "/usr/bin/env" {
		t.Fatalf("name=%s: the variables must be set for this invocation only", exec.name)
	}
	want := []string{
		"SIMCTL_CHILD_A=1", "SIMCTL_CHILD_FOO=bar", "xcrun",
		"simctl", "launch", "SIM-1", "com.example.app",
	}
	if !reflect.DeepEqual(exec.args, want) {
		t.Fatalf("args=%v want=%v", exec.args, want)
	}
}

// The plain launch stays exactly as it was.
func TestLaunchWithoutEnvIsUnchanged(t *testing.T) {
	exec := &fakeExec{}
	target := drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"}
	if _, err := New(exec).Launch(context.Background(), target, drivers.LaunchSpec{BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"simctl", "launch", "SIM-1", "com.example.app"}
	if exec.name != "xcrun" || !reflect.DeepEqual(exec.args, want) {
		t.Fatalf("got %s %v want xcrun %v", exec.name, exec.args, want)
	}
}
