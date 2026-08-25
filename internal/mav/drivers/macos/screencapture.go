// Package macos groups the drivers that operate on the Mac's own apps.
//
// Unlike iOS, there is no single CLI here that does everything: the
// accessibility tree and the menus come from one tool, the input from
// another, and the captures from the system itself. The router already
// knows how to split by capability and cost, so each driver declares what
// it does well and leaves the rest.
package macos

import (
	"context"
	"errors"
	"strconv"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ScreencaptureID is the registry key of the system capture driver.
const ScreencaptureID = "screencapture"

// Screencapture wraps the `screencapture` that ships with macOS. It is the
// last resort for CapScreenshot: it captures the whole screen, which as
// evidence of a specific app is worse than a capture bounded to its window.
// A driver that can resolve the window id must declare a lower cost and
// beat it.
type Screencapture struct {
	exec drivers.Executor
}

var _ drivers.ScreenshotDriver = (*Screencapture)(nil)

// NewScreencapture builds the driver.
func NewScreencapture(exec drivers.Executor) *Screencapture { return &Screencapture{exec: exec} }

func (d *Screencapture) ID() string { return ScreencaptureID }

// Provides only declares capabilities on the Mac: on the simulator and on
// device there are better paths already covered.
func (d *Screencapture) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindMac {
		return drivers.NewSet()
	}
	return drivers.NewSet(drivers.CapScreenshot, drivers.CapVideo)
}

// Cost places screencapture as an acceptable fallback, not the canonical
// path: whole screen instead of window.
func (d *Screencapture) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapVideo:
		// Behind cua: this records only when mav already runs inside the
		// graphical session, which is true on the user's own Mac and never
		// true over SSH.
		return 50
	case drivers.CapScreenshot:
		return 50
	default:
		return 100
	}
}

// Probe checks that the system binary is where it should be. It cannot
// check the Screen Recording permission: that belongs to the parent process
// (the terminal or the agent's harness), not to mav, and there is no cheap
// way to ask without attempting a real capture.
func (d *Screencapture) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("screencapture")
	if err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "screencapture not on PATH",
			Next:   "screencapture ships with macOS; check PATH",
		}
	}
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"screencapture": path}}
}

func (d *Screencapture) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// Screenshot captures the screen. `-x` silences the shutter sound, which in
// an automated session is only noise.
func (d *Screencapture) Screenshot(ctx context.Context, _ drivers.Target, spec drivers.ScreenshotSpec) error {
	if spec.OutPath == "" {
		return errors.New("screencapture: screenshot output path missing")
	}
	res := d.exec.Run(ctx, "screencapture", "-x", spec.OutPath)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

// VideoStart begins a recording. screencapture -v records until it is sent
// SIGINT, which is how VideoStop stops it.
func (d *Screencapture) VideoStart(ctx context.Context, _ drivers.Target, spec drivers.VideoSpec) (drivers.VideoResult, error) {
	if spec.OutPath == "" {
		return drivers.VideoResult{}, errors.New("screencapture: video output path missing")
	}
	pid, err := d.exec.Start(ctx, "", "screencapture", "-v", "-x", spec.OutPath)
	if err != nil {
		return drivers.VideoResult{}, err
	}
	return drivers.VideoResult{PID: pid, OutPath: spec.OutPath}, nil
}

// VideoStop cuts the recording with SIGINT. Killing with SIGKILL would
// leave the .mov half-written and without an index, that is, unreadable.
func (d *Screencapture) VideoStop(ctx context.Context, _ drivers.Target, pid int) error {
	if pid <= 0 {
		return errors.New("screencapture: video pid missing")
	}
	// Through the executor, not exec.Command: when mav drives a VM the
	// recorder's pid is the guest's, and signalling it here would not fail
	// loudly, it would hit whatever local process holds that number.
	if res := d.exec.Run(ctx, "kill", "-INT", strconv.Itoa(pid)); res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	if s == "" {
		return "command failed"
	}
	return s
}
