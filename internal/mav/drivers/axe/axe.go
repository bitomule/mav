// Package axe wraps the AXe accessibility CLI as a MAV driver. AXe is the
// canonical fast a11y tree + semantic tap path on simulators (and works on
// physical devices for tree extraction). It does NOT do multitouch (pinch/
// rotate/two-finger) or system UI; those go to baguette / go-ios instead.
package axe

import (
	"context"
	"errors"
	"strconv"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key for this driver. Exported so tests and routing code
// can refer to it without recreating a string literal everywhere.
const ID = "axe"

// Driver is the AXe-backed implementation of multiple driver interfaces.
type Driver struct {
	exec drivers.Executor
	path string // resolved binary path, populated by Probe
}

var _ drivers.TypeDriver = (*Driver)(nil)

// New constructs a Driver. The Executor is wired through by the parent mav
// package (see NewExecutor in drivers_adapter.go).
func New(exec drivers.Executor) *Driver { return &Driver{exec: exec} }

func (d *Driver) ID() string { return ID }

// Provides declares the capabilities AXe can serve. AXe handles both sim and
// device for tree/screenshot, single-finger gestures, semantic tap, and type.
func (d *Driver) Provides(target drivers.Target) drivers.CapabilitySet {
	// AXe talks to iOS simulators and devices. Declaring capabilities on a
	// macOS target put it in the running for them: it tied on cost with
	// the macOS driver and the alphabetical order broke the tie, so a
	// `ui tree` against a Mac app ended up invoking AXe with no udid.
	if target.Kind == drivers.KindMac {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapTreeAX,
		drivers.CapSemanticTap,
		drivers.CapCoordTap,
		drivers.CapSwipe,
		drivers.CapScreenshot,
		drivers.CapType,
	)
}

// Cost favours AXe for the fast-path operations it is canonical for.
func (d *Driver) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapTreeAX, drivers.CapSemanticTap, drivers.CapScreenshot, drivers.CapSwipe:
		return 0
	case drivers.CapCoordTap, drivers.CapType:
		return 50
	default:
		return 100
	}
}

// Probe checks the axe binary is on PATH.
func (d *Driver) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("axe")
	if err != nil {
		return drivers.HealthReport{State: drivers.HealthMissing, Detail: "axe not on PATH", Next: "mav setup"}
	}
	d.path = path
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"axe": path}}
}

// Warm has no async work to do.
func (d *Driver) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

func (d *Driver) Tap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	args := targetArgs(target, "tap")
	if spec.Selector.ID != "" {
		args = append(args, "--id", spec.Selector.ID)
	} else if spec.Selector.Text != "" {
		args = append(args, "--label", spec.Selector.Text)
	} else {
		return drivers.TapResult{}, errors.New("axe: tap requires id or text selector")
	}
	res := d.exec.Run(ctx, "axe", args...)
	if res.Err != nil {
		return drivers.TapResult{}, errors.New(firstLine(res.Stderr))
	}
	return drivers.TapResult{MatchedID: spec.Selector.ID, MatchedText: spec.Selector.Text}, nil
}
func (d *Driver) Tree(ctx context.Context, target drivers.Target, _ drivers.TreeSpec) (drivers.TreeResult, error) {
	res := d.exec.Run(ctx, "axe", targetArgs(target, "describe-ui")...)
	if res.Err != nil {
		return drivers.TreeResult{}, errors.New(firstLine(res.Stderr))
	}
	return drivers.TreeResult{JSON: []byte(res.Stdout)}, nil
}
func (d *Driver) Swipe(ctx context.Context, target drivers.Target, spec drivers.SwipeSpec) error {
	args := targetArgs(target, "swipe",
		"--start-x", strconv.Itoa(spec.StartX),
		"--start-y", strconv.Itoa(spec.StartY),
		"--end-x", strconv.Itoa(spec.EndX),
		"--end-y", strconv.Itoa(spec.EndY),
	)
	res := d.exec.Run(ctx, "axe", args...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}
func (d *Driver) Screenshot(ctx context.Context, target drivers.Target, spec drivers.ScreenshotSpec) error {
	if spec.OutPath == "" {
		return errors.New("axe: screenshot output path missing")
	}
	res := d.exec.Run(ctx, "axe", targetArgs(target, "screenshot", "--output", spec.OutPath)...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}
func (d *Driver) Type(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	if spec.Text == "" {
		return errors.New("axe: type text missing")
	}
	res := d.exec.Run(ctx, "axe", targetArgs(target, "type", spec.Text)...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func targetArgs(target drivers.Target, args ...string) []string {
	out := append([]string{}, args...)
	if target.UDID != "" {
		out = append(out, "--udid", target.UDID)
	}
	return out
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			if i == 0 {
				return ""
			}
			return s[:i]
		}
	}
	if s == "" {
		return "command failed"
	}
	return s
}
