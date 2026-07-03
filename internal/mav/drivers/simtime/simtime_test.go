package simtime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type fakeExec struct {
	name string
	args []string
	out  drivers.ExecResult
}

func (f *fakeExec) LookPath(name string) (string, error) {
	if name == "simtime" {
		return "/opt/homebrew/bin/simtime", nil
	}
	return "", errors.New("missing")
}
func (f *fakeExec) Run(_ context.Context, name string, args ...string) drivers.ExecResult {
	f.name, f.args = name, append([]string(nil), args...)
	return f.out
}
func (*fakeExec) Start(context.Context, string, string, ...string) (int, error) { return 0, nil }

func TestFreezeBuildsDocumentedCommand(t *testing.T) {
	exec := &fakeExec{out: drivers.ExecResult{Stdout: "frozen"}}
	driver := New(exec)
	target := drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1", BundleID: "com.example.app"}
	got, err := driver.FreezeTime(context.Background(), target, "2032-01-15T10:00:00Z")
	if err != nil || got != "frozen" {
		t.Fatalf("got %q, %v", got, err)
	}
	want := []string{"freeze", "--udid", "SIM-1", "--bundle", "com.example.app", "2032-01-15T10:00:00Z"}
	if exec.name != "simtime" || !reflect.DeepEqual(exec.args, want) {
		t.Fatalf("command = %s %v, want simtime %v", exec.name, exec.args, want)
	}
}

func TestOnlyProvidesWallClockOnSimulator(t *testing.T) {
	driver := New(&fakeExec{})
	if !driver.Provides(drivers.Target{Kind: drivers.KindSim}).Has(drivers.CapWallClock) {
		t.Fatal("simulator should provide wall clock")
	}
	if len(driver.Provides(drivers.Target{Kind: drivers.KindDevice})) != 0 {
		t.Fatal("device must not provide wall clock")
	}
}
