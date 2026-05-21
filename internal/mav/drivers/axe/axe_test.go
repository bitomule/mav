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
	want := "axe tap --udid SIM-1 --id settings"
	if exec.commands[0] != want {
		t.Fatalf("command=%q want=%q", exec.commands[0], want)
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
