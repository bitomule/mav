// Package idb wraps Facebook's idb_companion. After the May 2026 plan revision
// (go-ios proved unviable as a drop-in: requires sudo for tunnel on iOS 17+,
// no gesture API, no HAR), idb is a *permanent* driver in MAV's portfolio:
// the only viable path for device coord taps, screenshots, logs, crashes, and
// install/launch without root. This package stays stubbed during P2/P3 because
// the cli.go migration off hasTool() is deferred -- cli.go still calls idb
// directly today. Functional bodies fill in when that migration happens.
package idb

import (
	"context"
	"errors"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key. Permanent.
const ID = "idb"

// Driver wraps the idb CLI.
type Driver struct {
	exec drivers.Executor
	path string
}

// New constructs a Driver.
func New(exec drivers.Executor) *Driver { return &Driver{exec: exec} }

func (d *Driver) ID() string { return ID }

// Provides covers the operations idb is currently responsible for in cli.go:
// device lifecycle, device logs/crashes, coordinate taps, fallback screenshot
// when AXe is missing.
func (d *Driver) Provides(_ drivers.Target) drivers.CapabilitySet {
	return drivers.NewSet(
		drivers.CapCoordTap,
		drivers.CapScreenshot,
		drivers.CapLogStream,
		drivers.CapCrashFetch,
		drivers.CapInstall,
		drivers.CapLaunch,
		drivers.CapUninstall,
	)
}

// Cost is canonical (0) for the device-only capabilities idb owns; medium (50)
// for sim capabilities where AXe/simctl are typically preferred.
func (d *Driver) Cost(c drivers.Capability, target drivers.Target) int {
	if target.IsDevice() {
		return 0
	}
	switch c {
	case drivers.CapCoordTap:
		return 50
	default:
		return 80
	}
}

// Probe checks idb is on PATH.
func (d *Driver) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("idb")
	if err != nil {
		return drivers.HealthReport{State: drivers.HealthMissing, Detail: "idb not on PATH", Next: "mav setup --install idb"}
	}
	d.path = path
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"idb": path}}
}

func (d *Driver) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// --- functional methods (stubs until cli.go migrates to the registry) ---

var errNotYet = errors.New("idb: registry path not yet wired in cli.go (still uses hasTool)")

func (d *Driver) Tap(context.Context, drivers.Target, drivers.TapSpec) (drivers.TapResult, error) {
	return drivers.TapResult{}, errNotYet
}
func (d *Driver) Screenshot(context.Context, drivers.Target, drivers.ScreenshotSpec) error {
	return errNotYet
}
func (d *Driver) LogStreamStart(context.Context, drivers.Target, drivers.LogStreamSpec) (drivers.LogStreamResult, error) {
	return drivers.LogStreamResult{}, errNotYet
}
func (d *Driver) LogStreamStop(context.Context, int) error { return errNotYet }
func (d *Driver) CrashFetch(context.Context, drivers.Target, drivers.CrashSpec) ([]drivers.CrashEntry, error) {
	return nil, errNotYet
}
func (d *Driver) Install(context.Context, drivers.Target, drivers.InstallSpec) error { return errNotYet }
func (d *Driver) Launch(context.Context, drivers.Target, drivers.LaunchSpec) (drivers.LaunchResult, error) {
	return drivers.LaunchResult{}, errNotYet
}
func (d *Driver) Uninstall(context.Context, drivers.Target, string) error { return errNotYet }
func (d *Driver) Boot(context.Context, drivers.Target) error              { return errNotYet }
