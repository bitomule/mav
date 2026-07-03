// Package idb wraps Facebook's idb_companion. After the May 2026 plan revision
// (go-ios proved unviable as a drop-in: requires sudo for tunnel on iOS 17+,
// no gesture API, no HAR), idb is a *permanent* driver in MAV's portfolio:
// the only viable path for device coord taps, screenshots, logs, crashes, and
// install/launch without root.
package idb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
		drivers.CapSwipe,
		drivers.CapScreenshot,
		drivers.CapLogStream,
		drivers.CapCrashFetch,
		drivers.CapInstall,
		drivers.CapLaunch,
		drivers.CapUninstall,
		drivers.CapAppList,
		drivers.CapTerminate,
		drivers.CapOpenURL,
		drivers.CapLocation,
	)
}

func (d *Driver) ListApps(ctx context.Context, target drivers.Target) (string, error) {
	res := d.exec.Run(ctx, "idb", targetArgs(target, "list-apps", "--json")...)
	if res.Err != nil {
		return "", errors.New(firstLine(res.Stderr))
	}
	return res.Stdout, nil
}

func (d *Driver) Terminate(ctx context.Context, target drivers.Target, bundleID string) error {
	res := d.exec.Run(ctx, "idb", targetArgs(target, "terminate", bundleID)...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) OpenURL(ctx context.Context, target drivers.Target, url string) error {
	res := d.exec.Run(ctx, "idb", targetArgs(target, "open", url)...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) SetLocation(ctx context.Context, target drivers.Target, latitude, longitude float64) error {
	res := d.exec.Run(ctx, "idb", targetArgs(target, "set-location",
		strconv.FormatFloat(latitude, 'f', 6, 64),
		strconv.FormatFloat(longitude, 'f', 6, 64))...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *Driver) ResetLocation(context.Context, drivers.Target) error {
	return errors.New("idb: resetting device location is unsupported")
}

func (d *Driver) ClipboardWrite(context.Context, drivers.Target, string) error {
	return errors.New("idb: clipboard write is unsupported")
}

func (d *Driver) ClipboardRead(context.Context, drivers.Target) (string, error) {
	return "", errors.New("idb: clipboard read is unsupported")
}

// Cost is canonical (0) for the device-only capabilities idb owns; medium (50)
// for sim capabilities where AXe/simctl are typically preferred.
func (d *Driver) Cost(c drivers.Capability, target drivers.Target) int {
	if target.IsDevice() {
		return 0
	}
	switch c {
	case drivers.CapCoordTap, drivers.CapSwipe:
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

func (d *Driver) Tap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	if !spec.Selector.IsZero() {
		return drivers.TapResult{}, errors.New("idb: semantic tap unsupported")
	}
	args := targetArgs(target, "ui", "tap", strconv.Itoa(spec.X), strconv.Itoa(spec.Y))
	res := d.exec.Run(ctx, "idb", args...)
	if res.Err != nil {
		return drivers.TapResult{}, errors.New(firstLine(res.Stderr))
	}
	return drivers.TapResult{X: spec.X, Y: spec.Y}, nil
}
func (d *Driver) Screenshot(ctx context.Context, target drivers.Target, spec drivers.ScreenshotSpec) error {
	if spec.OutPath == "" {
		return errors.New("idb: screenshot output path missing")
	}
	res := d.exec.Run(ctx, "idb", targetArgs(target, "screenshot", spec.OutPath)...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}
func (d *Driver) Swipe(ctx context.Context, target drivers.Target, spec drivers.SwipeSpec) error {
	res := d.exec.Run(ctx, "idb", targetArgs(target, "ui", "swipe", strconv.Itoa(spec.StartX), strconv.Itoa(spec.StartY), strconv.Itoa(spec.EndX), strconv.Itoa(spec.EndY))...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}
func (d *Driver) LogStreamStart(ctx context.Context, target drivers.Target, spec drivers.LogStreamSpec) (drivers.LogStreamResult, error) {
	if spec.OutPath == "" {
		return drivers.LogStreamResult{}, errors.New("idb: log output path missing")
	}
	args := targetArgs(target, "log")
	if spec.BundleID != "" {
		args = append(args, "--predicate", `subsystem == "`+spec.BundleID+`"`)
	}
	pid, err := d.exec.Start(ctx, spec.OutPath, "idb", args...)
	if err != nil {
		return drivers.LogStreamResult{}, err
	}
	return drivers.LogStreamResult{PID: pid, OutPath: spec.OutPath}, nil
}
func (d *Driver) LogStreamStop(_ context.Context, _ int) error { return nil }
func (d *Driver) CrashFetch(ctx context.Context, target drivers.Target, spec drivers.CrashSpec) ([]drivers.CrashEntry, error) {
	args := targetArgs(target, "crash", "list")
	if spec.BundleID != "" {
		args = append(args, "--bundle-id", spec.BundleID)
	}
	list := d.exec.Run(ctx, "idb", args...)
	if list.Err != nil {
		return nil, errors.New(firstLine(list.Stderr))
	}
	names := parseCrashNames(list.Stdout)
	entries := make([]drivers.CrashEntry, 0, len(names))
	if spec.OutDir != "" {
		_ = os.MkdirAll(spec.OutDir, 0o755)
	}
	for i, name := range names {
		body := d.exec.Run(ctx, "idb", targetArgs(target, "crash", "show", name)...)
		if body.Err != nil || body.Stdout == "" {
			continue
		}
		path := ""
		if spec.OutDir != "" {
			path = filepath.Join(spec.OutDir, fmt.Sprintf("%02d.ips", i+1))
			_ = os.WriteFile(path, []byte(body.Stdout), 0o644)
		}
		entries = append(entries, drivers.CrashEntry{Path: path, Body: []byte(body.Stdout)})
	}
	return entries, nil
}
func (d *Driver) Install(ctx context.Context, target drivers.Target, spec drivers.InstallSpec) error {
	res := d.exec.Run(ctx, "idb", targetArgs(target, "install", spec.Path)...)
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
	res := d.exec.Run(ctx, "idb", targetArgs(target, "launch", "-f", bundleID)...)
	if res.Err != nil {
		return drivers.LaunchResult{}, errors.New(firstLine(res.Stderr))
	}
	return drivers.LaunchResult{BundleID: bundleID}, nil
}
func (d *Driver) Uninstall(ctx context.Context, target drivers.Target, bundleID string) error {
	res := d.exec.Run(ctx, "idb", targetArgs(target, "uninstall", bundleID)...)
	if res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}
func (d *Driver) Boot(context.Context, drivers.Target) error { return nil }

func targetArgs(target drivers.Target, args ...string) []string {
	out := append([]string{}, args...)
	if target.UDID != "" {
		out = append(out, "--udid", target.UDID)
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "command failed"
	}
	return s
}

func parseCrashNames(stdout string) []string {
	var out []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		line = strings.TrimSuffix(line, ".ips")
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
