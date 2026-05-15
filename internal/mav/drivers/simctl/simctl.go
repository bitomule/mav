// Package simctl wraps Apple's `xcrun simctl` for simulator lifecycle, video
// recording, log streaming, screenshots, and locale config. simctl is a
// permanent helper: it is the stock SDK path to simulators and has no viable
// replacement.
package simctl

import (
	"context"
	"errors"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key for this driver.
const ID = "simctl"

// Driver wraps xcrun simctl. Sim-only (Probe reports missing on device).
type Driver struct {
	exec drivers.Executor
	path string
}

// New constructs a Driver.
func New(exec drivers.Executor) *Driver { return &Driver{exec: exec} }

func (d *Driver) ID() string { return ID }

// Provides advertises simctl capabilities only on simulator targets.
func (d *Driver) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindSim {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapBoot,
		drivers.CapLocale,
		drivers.CapInstall,
		drivers.CapLaunch,
		drivers.CapUninstall,
		drivers.CapScreenshot, // fallback path; axe is cheaper
		drivers.CapVideo,
		drivers.CapLogStream,
	)
}

// Cost ranks simctl as authoritative for lifecycle/video/logs on sim, and as a
// fallback for screenshots (axe is preferred).
func (d *Driver) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapBoot, drivers.CapLocale, drivers.CapInstall, drivers.CapLaunch, drivers.CapUninstall, drivers.CapVideo, drivers.CapLogStream:
		return 0
	case drivers.CapScreenshot:
		return 80
	default:
		return 100
	}
}

// Probe checks xcrun is on PATH.
func (d *Driver) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("xcrun")
	if err != nil {
		return drivers.HealthReport{State: drivers.HealthMissing, Detail: "xcrun not on PATH", Next: "install Xcode command-line tools"}
	}
	d.path = path
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"xcrun": path}}
}

func (d *Driver) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// --- functional methods (filled in P3/P5 when migrated from cli.go) -----

var errNotYet = errors.New("simctl: functional implementation lands in P3+")

func (d *Driver) Boot(context.Context, drivers.Target) error                                  { return errNotYet }
func (d *Driver) Install(context.Context, drivers.Target, drivers.InstallSpec) error          { return errNotYet }
func (d *Driver) Launch(context.Context, drivers.Target, drivers.LaunchSpec) (drivers.LaunchResult, error) {
	return drivers.LaunchResult{}, errNotYet
}
func (d *Driver) Uninstall(context.Context, drivers.Target, string) error { return errNotYet }
func (d *Driver) Screenshot(context.Context, drivers.Target, drivers.ScreenshotSpec) error {
	return errNotYet
}
func (d *Driver) VideoStart(context.Context, drivers.Target, drivers.VideoSpec) (drivers.VideoResult, error) {
	return drivers.VideoResult{}, errNotYet
}
func (d *Driver) VideoStop(context.Context, drivers.Target, int) error { return errNotYet }
func (d *Driver) LogStreamStart(context.Context, drivers.Target, drivers.LogStreamSpec) (drivers.LogStreamResult, error) {
	return drivers.LogStreamResult{}, errNotYet
}
func (d *Driver) LogStreamStop(context.Context, int) error { return errNotYet }
