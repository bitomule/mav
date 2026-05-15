// Package axe wraps the AXe accessibility CLI as a MAV driver. AXe is the
// canonical fast a11y tree + semantic tap path on simulators (and works on
// physical devices for tree extraction). It does NOT do multitouch (pinch/
// rotate/two-finger) or system UI; those go to baguette / go-ios instead.
package axe

import (
	"context"
	"errors"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key for this driver. Exported so tests and routing code
// can refer to it without recreating a string literal everywhere.
const ID = "axe"

// Driver is the AXe-backed implementation of multiple driver interfaces.
// The implementation bodies are deliberately stubbed in P2; subsequent phases
// move the real AXe interaction code out of cli.go into this package.
type Driver struct {
	exec drivers.Executor
	path string // resolved binary path, populated by Probe
}

// New constructs a Driver. The Executor is wired through by the parent mav
// package (see NewExecutor in drivers_adapter.go).
func New(exec drivers.Executor) *Driver { return &Driver{exec: exec} }

func (d *Driver) ID() string { return ID }

// Provides declares the capabilities AXe can serve. AXe handles both sim and
// device for tree/screenshot, single-finger gestures, semantic tap, and type.
func (d *Driver) Provides(_ drivers.Target) drivers.CapabilitySet {
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

// --- functional methods (filled in P3/P4 when cli.go is migrated) -------

var errNotYet = errors.New("axe: functional implementation lands in P3 driver migration")

func (d *Driver) Tap(context.Context, drivers.Target, drivers.TapSpec) (drivers.TapResult, error) {
	return drivers.TapResult{}, errNotYet
}
func (d *Driver) Tree(context.Context, drivers.Target, drivers.TreeSpec) (drivers.TreeResult, error) {
	return drivers.TreeResult{}, errNotYet
}
func (d *Driver) Swipe(context.Context, drivers.Target, drivers.SwipeSpec) error { return errNotYet }
func (d *Driver) Screenshot(context.Context, drivers.Target, drivers.ScreenshotSpec) error {
	return errNotYet
}
func (d *Driver) Type(context.Context, drivers.Target, drivers.TextSpec) error { return errNotYet }
