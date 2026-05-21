package idb

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

func TestTapBuildsCoordinateCommand(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"idb": true}}
	d := New(exec)
	_, err := d.Tap(context.Background(), drivers.Target{UDID: "REAL-1"}, drivers.TapSpec{X: 10, Y: 20})
	if err != nil {
		t.Fatal(err)
	}
	want := "idb ui tap 10 20 --udid REAL-1"
	if exec.commands[0] != want {
		t.Fatalf("command=%q want=%q", exec.commands[0], want)
	}
}

func TestScreenshotBuildsCommand(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"idb": true}}
	d := New(exec)
	if err := d.Screenshot(context.Background(), drivers.Target{UDID: "REAL-1"}, drivers.ScreenshotSpec{OutPath: "/tmp/out.png"}); err != nil {
		t.Fatal(err)
	}
	want := "idb screenshot /tmp/out.png --udid REAL-1"
	if exec.commands[0] != want {
		t.Fatalf("command=%q want=%q", exec.commands[0], want)
	}
}
