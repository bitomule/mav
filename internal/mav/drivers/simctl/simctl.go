// Package simctl wraps Apple's `xcrun simctl` for simulator lifecycle, video
// recording, log streaming, screenshots, and locale config. simctl is a
// permanent helper: it is the stock SDK path to simulators and has no viable
// replacement.
package simctl

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key for this driver.
const ID = "simctl"

// Driver wraps xcrun simctl. Sim-only (Probe reports missing on device).
type Driver struct {
	exec drivers.Executor
	path string
}

var (
	_ drivers.DeviceUtilityDriver = (*Driver)(nil)
	_ drivers.AppearanceDriver    = (*Driver)(nil)
	_ drivers.StatusBarDriver     = (*Driver)(nil)
)

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
		drivers.CapAppList,
		drivers.CapTerminate,
		drivers.CapOpenURL,
		drivers.CapLocation,
		drivers.CapClipboard,
		drivers.CapAppearance,
		drivers.CapStatusBar,
	)
}

func (d *Driver) ListApps(ctx context.Context, target drivers.Target) (string, error) {
	res := d.exec.Run(ctx, "xcrun", "simctl", "listapps", simUDID(target))
	if res.Err != nil {
		return "", errors.New(firstLine(res.Stderr))
	}
	return res.Stdout, nil
}

func (d *Driver) Terminate(ctx context.Context, target drivers.Target, bundleID string) error {
	res := d.exec.Run(ctx, "xcrun", "simctl", "terminate", simUDID(target), bundleID)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) OpenURL(ctx context.Context, target drivers.Target, url string) error {
	res := d.exec.Run(ctx, "xcrun", "simctl", "openurl", simUDID(target), url)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) SetLocation(ctx context.Context, target drivers.Target, latitude, longitude float64) error {
	value := strconv.FormatFloat(latitude, 'f', 6, 64) + "," + strconv.FormatFloat(longitude, 'f', 6, 64)
	res := d.exec.Run(ctx, "xcrun", "simctl", "location", simUDID(target), "set", value)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) ResetLocation(ctx context.Context, target drivers.Target) error {
	res := d.exec.Run(ctx, "xcrun", "simctl", "location", simUDID(target), "clear")
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) ClipboardWrite(ctx context.Context, target drivers.Target, text string) error {
	input, ok := d.exec.(drivers.InputExecutor)
	if !ok {
		return fmt.Errorf("simctl: input executor unavailable")
	}
	res := input.RunInput(ctx, text, "xcrun", "simctl", "pbcopy", simUDID(target))
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) ClipboardRead(ctx context.Context, target drivers.Target) (string, error) {
	res := d.exec.Run(ctx, "xcrun", "simctl", "pbpaste", simUDID(target))
	if res.Err != nil {
		return "", errors.New(firstLine(res.Stderr))
	}
	return res.Stdout, nil
}

func (d *Driver) SetAppearance(ctx context.Context, target drivers.Target, appearance string) error {
	res := d.exec.Run(ctx, "xcrun", "simctl", "ui", simUDID(target), "appearance", appearance)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) SetStatusBar(ctx context.Context, target drivers.Target, spec drivers.StatusBarSpec) error {
	args := append([]string{"simctl", "status_bar", simUDID(target), "override"}, statusBarOverrideArgs(spec)...)
	res := d.exec.Run(ctx, "xcrun", args...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) ClearStatusBar(ctx context.Context, target drivers.Target) error {
	res := d.exec.Run(ctx, "xcrun", "simctl", "status_bar", simUDID(target), "clear")
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

// statusBarOverrideArgs emits one flag pair per populated field, in a fixed
// order so the command line is reproducible in tests and evidence.
func statusBarOverrideArgs(spec drivers.StatusBarSpec) []string {
	pairs := []struct {
		flag  string
		value string
	}{
		{"--time", spec.Time},
		{"--dataNetwork", spec.DataNetwork},
		{"--wifiMode", spec.WifiMode},
		{"--wifiBars", spec.WifiBars},
		{"--cellularMode", spec.CellularMode},
		{"--cellularBars", spec.CellularBars},
		{"--operatorName", spec.OperatorName},
		{"--batteryState", spec.BatteryState},
		{"--batteryLevel", spec.BatteryLevel},
	}
	args := []string{}
	for _, pair := range pairs {
		if pair.value == "" {
			continue
		}
		args = append(args, pair.flag, pair.value)
	}
	return args
}

// Cost ranks simctl as authoritative for lifecycle/video/logs on sim, and as a
// fallback for screenshots (axe is preferred).
func (d *Driver) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapBoot, drivers.CapLocale, drivers.CapInstall, drivers.CapLaunch, drivers.CapUninstall, drivers.CapVideo, drivers.CapLogStream, drivers.CapAppearance, drivers.CapStatusBar:
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

var errNotYet = errors.New("simctl: functional implementation not migrated yet")

func (d *Driver) Boot(ctx context.Context, target drivers.Target) error {
	udid := simUDID(target)
	res := d.exec.Run(ctx, "xcrun", "simctl", "boot", udid)
	if res.Err != nil && !strings.Contains(res.Stderr, "Unable to boot device in current state") {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}
func (d *Driver) Install(ctx context.Context, target drivers.Target, spec drivers.InstallSpec) error {
	res := d.exec.Run(ctx, "xcrun", "simctl", "install", simUDID(target), spec.Path)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}
func (d *Driver) Launch(ctx context.Context, target drivers.Target, spec drivers.LaunchSpec) (drivers.LaunchResult, error) {
	bundleID := spec.BundleID
	if bundleID == "" {
		bundleID = target.BundleID
	}
	args := []string{"simctl", "launch", simUDID(target), bundleID}
	args = append(args, simctlLanguageArgs(target)...)
	res := d.exec.Run(ctx, "xcrun", args...)
	if res.Err != nil {
		return drivers.LaunchResult{}, errors.New(firstLine(res.Stderr))
	}
	return drivers.LaunchResult{BundleID: bundleID}, nil
}
func (d *Driver) Uninstall(ctx context.Context, target drivers.Target, bundleID string) error {
	res := d.exec.Run(ctx, "xcrun", "simctl", "uninstall", simUDID(target), bundleID)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}
func (d *Driver) Screenshot(ctx context.Context, target drivers.Target, spec drivers.ScreenshotSpec) error {
	udid := target.UDID
	if udid == "" {
		udid = "booted"
	}
	res := d.exec.Run(ctx, "xcrun", "simctl", "io", udid, "screenshot", spec.OutPath)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}
func (d *Driver) VideoStart(context.Context, drivers.Target, drivers.VideoSpec) (drivers.VideoResult, error) {
	return drivers.VideoResult{}, errNotYet
}
func (d *Driver) VideoStop(context.Context, drivers.Target, int) error { return errNotYet }
func (d *Driver) LogStreamStart(ctx context.Context, target drivers.Target, spec drivers.LogStreamSpec) (drivers.LogStreamResult, error) {
	predicate := `subsystem == "` + spec.BundleID + `"`
	args := []string{"simctl", "spawn", simUDID(target), "log", "stream", "--style", "compact", "--level", "debug", "--predicate", predicate}
	pid, err := d.exec.Start(ctx, spec.OutPath, "xcrun", args...)
	if err != nil {
		return drivers.LogStreamResult{}, err
	}
	return drivers.LogStreamResult{PID: pid, OutPath: spec.OutPath}, nil
}
func (d *Driver) LogStreamStop(context.Context, int) error { return errNotYet }

func simUDID(target drivers.Target) string {
	if target.UDID != "" {
		return target.UDID
	}
	return "booted"
}

func simctlLanguageArgs(target drivers.Target) []string {
	args := []string{}
	if target.Language != "" {
		args = append(args, "-AppleLanguages", "("+target.Language+")")
		if target.Locale != "" {
			args = append(args, "-AppleLocale", target.Locale)
		}
	} else if target.Locale != "" {
		args = append(args, "-AppleLocale", target.Locale)
	}
	return args
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
