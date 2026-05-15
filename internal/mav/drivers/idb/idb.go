// Package idb wraps Facebook's idb_companion. This driver exists as a bridge
// for the duration of P2 only: P3 deletes it entirely and replaces the device
// path with the go-ios driver. Do not add features here.
package idb

import (
	"context"
	"errors"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key. Treated as a transitional name; gone in P3.
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

// Cost is set high across the board: idb is the last-resort path during P2 and
// scheduled for deletion. Other drivers should win every routing comparison.
func (d *Driver) Cost(_ drivers.Capability, _ drivers.Target) int { return 200 }

// Probe checks idb is on PATH.
func (d *Driver) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("idb")
	if err != nil {
		return drivers.HealthReport{State: drivers.HealthMissing, Detail: "idb not on PATH"}
	}
	d.path = path
	return drivers.HealthReport{State: drivers.HealthDegraded, Detail: "idb scheduled for removal in P3", Tools: map[string]string{"idb": path}}
}

func (d *Driver) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// --- functional methods (stubbed, never implemented; deleted in P3) -----

var errNotYet = errors.New("idb: bridge driver, not implemented (gone in P3)")

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
