package macos

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type fakeExec struct {
	commands []string
	results  map[string]drivers.ExecResult
	tools    map[string]bool
	startPID int

	// onCommand lets a test change the world when a command runs, which is
	// what testing a daemon start needs: the same command fails before and
	// works after.
	onCommand func(string)
}

func (f *fakeExec) LookPath(name string) (string, error) {
	if f.tools[name] {
		return "/usr/sbin/" + name, nil
	}
	return "", os.ErrNotExist
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) drivers.ExecResult {
	command := name + " " + strings.Join(args, " ")
	f.commands = append(f.commands, command)
	if f.onCommand != nil {
		f.onCommand(command)
	}
	for needle, res := range f.results {
		if strings.Contains(command, needle) {
			return res
		}
	}
	return drivers.ExecResult{}
}

func (f *fakeExec) Start(_ context.Context, _ string, name string, args ...string) (int, error) {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	if f.startPID == 0 {
		f.startPID = 4242
	}
	return f.startPID, nil
}

func TestScreencaptureOnlyProvidesOnMac(t *testing.T) {
	d := NewScreencapture(&fakeExec{})
	for _, kind := range []drivers.TargetKind{drivers.KindSim, drivers.KindDevice} {
		if len(d.Provides(drivers.Target{Kind: kind})) != 0 {
			t.Fatalf("screencapture must declare nothing on %s: simulator and device have better paths", kind)
		}
	}
	caps := d.Provides(drivers.Target{Kind: drivers.KindMac})
	if !caps.Has(drivers.CapScreenshot) || !caps.Has(drivers.CapVideo) {
		t.Fatalf("on mac it must declare screenshot and video: %v", caps)
	}
}

func TestScreencaptureIsAFallbackForScreenshots(t *testing.T) {
	d := NewScreencapture(&fakeExec{})
	mac := drivers.Target{Kind: drivers.KindMac}
	// It captures the whole screen, which as evidence of a specific app is
	// worse than bounding it to its window. A driver that resolves the
	// window id must be able to beat it on cost.
	if got := d.Cost(drivers.CapScreenshot, mac); got == 0 {
		t.Fatalf("screenshot should not be canonical cost, got %d", got)
	}
	// Video is a fallback too, and the reason is not quality: this records
	// only when mav already runs inside the graphical session. Over SSH,
	// which is every run against a VM, it sees no display at all, so a
	// driver that goes through the permission-holding daemon has to be able
	// to win.
	if got := d.Cost(drivers.CapVideo, mac); got == 0 {
		t.Fatalf("video should not be canonical cost, got %d", got)
	}
}

// TestScreencaptureStopsThroughTheExecutor: the recorder's pid belongs to
// whichever machine the executor reaches. Signalling it from this process
// would not fail loudly when that machine is a VM, it would hit whatever
// local process holds that number.
func TestScreencaptureStopsThroughTheExecutor(t *testing.T) {
	f := &fakeExec{}
	d := NewScreencapture(f)
	if err := d.VideoStop(context.Background(), drivers.Target{Kind: drivers.KindMac}, 4321); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "kill") || !strings.Contains(f.commands[0], "4321") {
		t.Fatalf("the stop never went through the executor: %v", f.commands)
	}
}

func TestScreencaptureSilencesTheShutter(t *testing.T) {
	f := &fakeExec{}
	d := NewScreencapture(f)
	if err := d.Screenshot(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.ScreenshotSpec{OutPath: "/tmp/x.png"}); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || !strings.Contains(f.commands[0], "-x") {
		t.Fatalf("an automated session must make no noise: %v", f.commands)
	}
}

func TestScreencaptureScreenshotRequiresPath(t *testing.T) {
	d := NewScreencapture(&fakeExec{})
	if err := d.Screenshot(context.Background(), drivers.Target{Kind: drivers.KindMac}, drivers.ScreenshotSpec{}); err == nil {
		t.Fatal("with no output path it must fail")
	}
}
