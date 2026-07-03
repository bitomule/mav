package simctl

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
}

func (*fakeExec) LookPath(name string) (string, error) {
	if name == "xcrun" {
		return "/usr/bin/xcrun", nil
	}
	return "", errors.New("missing")
}
func (f *fakeExec) Run(_ context.Context, name string, args ...string) drivers.ExecResult {
	f.name, f.args = name, append([]string(nil), args...)
	return drivers.ExecResult{}
}
func (*fakeExec) Start(context.Context, string, string, ...string) (int, error) { return 0, nil }
func (f *fakeExec) RunInput(_ context.Context, _ string, name string, args ...string) drivers.ExecResult {
	f.name, f.args = name, append([]string(nil), args...)
	return drivers.ExecResult{}
}

func TestOpenURLCommand(t *testing.T) {
	exec := &fakeExec{}
	driver := New(exec)
	target := drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"}
	if err := driver.OpenURL(context.Background(), target, "myapp://item/1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"simctl", "openurl", "SIM-1", "myapp://item/1"}
	if exec.name != "xcrun" || !reflect.DeepEqual(exec.args, want) {
		t.Fatalf("got %s %v want xcrun %v", exec.name, exec.args, want)
	}
}

func TestLocationAndClipboardCommands(t *testing.T) {
	exec := &fakeExec{}
	driver := New(exec)
	target := drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"}
	if err := driver.SetLocation(context.Background(), target, 40.4168, -3.7038); err != nil {
		t.Fatal(err)
	}
	if got := exec.args; !reflect.DeepEqual(got, []string{"simctl", "location", "SIM-1", "set", "40.416800,-3.703800"}) {
		t.Fatalf("location args=%v", got)
	}
	if err := driver.ClipboardWrite(context.Background(), target, "hello"); err != nil {
		t.Fatal(err)
	}
	if got := exec.args; !reflect.DeepEqual(got, []string{"simctl", "pbcopy", "SIM-1"}) {
		t.Fatalf("clipboard args=%v", got)
	}
}
