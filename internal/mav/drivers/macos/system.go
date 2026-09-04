package macos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// SystemID is the driver's registry key.
const SystemID = "macsystem"

// System covers the lifecycle and utilities of a macOS app with the tools
// the system itself ships. It wraps no third-party CLI because none is
// needed: `open`, `pbcopy`, `pbpaste` and the filesystem are enough.
//
// It exists above all for one concrete reason: without a CapTerminate
// provider on the Mac, closing the app before seeding a fixture was a
// silent no-op, and the fixture would write the database while the previous
// instance kept it open.
type System struct {
	exec drivers.Executor
}

var (
	_ drivers.LifecycleDriver     = (*System)(nil)
	_ drivers.DeviceUtilityDriver = (*System)(nil)
)

// NewSystem builds the driver.
func NewSystem(exec drivers.Executor) *System { return &System{exec: exec} }

func (d *System) ID() string { return SystemID }

func (d *System) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindMac {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapInstall,
		drivers.CapLaunch,
		drivers.CapUninstall,
		drivers.CapTerminate,
		drivers.CapOpenURL,
		drivers.CapClipboard,
		drivers.CapAppList,
	)
}

// Cost: it is the only provider of all this on the Mac.
func (d *System) Cost(drivers.Capability, drivers.Target) int { return 0 }

// Probe needs to check nothing installable: these are system binaries. What
// it does check is that we are on macOS, because a mac target on another
// system is not a recoverable failure but an impossible config.
func (d *System) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	if _, err := p.LookPath("open"); err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "`open` not on PATH; macOS targets need a Mac",
		}
	}
	return drivers.HealthReport{State: drivers.HealthOK}
}

func (d *System) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// Install on macOS copies nothing to /Applications: to validate, the app is
// run from wherever the build left it. "Installing" here is checking that
// the bundle is where we say, which is the only part that can fail and the
// one that gives a useful error when the launch recipe did not produce what
// it believed.
func (d *System) Install(_ context.Context, _ drivers.Target, spec drivers.InstallSpec) error {
	if strings.TrimSpace(spec.Path) == "" {
		return errors.New("macsystem: no app path; the launch recipe must produce one via app_path")
	}
	info, err := os.Stat(spec.Path)
	if err != nil {
		return errors.New("macsystem: app bundle not found at " + spec.Path)
	}
	if !info.IsDir() || !strings.HasSuffix(spec.Path, ".app") {
		return errors.New("macsystem: not an app bundle: " + spec.Path)
	}
	return nil
}

// Launch runs the binary inside the bundle instead of using `open`.
//
// It is not a whim: `open` does not propagate environment variables to the
// process it starts, and the environment is exactly how mav injects its
// configuration (the equivalent of the simulator's SIMCTL_CHILD_*). Running
// Contents/MacOS/<binary> directly inherits everything.
func (d *System) Launch(ctx context.Context, target drivers.Target, spec drivers.LaunchSpec) (drivers.LaunchResult, error) {
	if target.AppPath == "" {
		return drivers.LaunchResult{}, errors.New("macsystem: launch needs the app bundle path")
	}
	binary, err := bundleExecutable(target.AppPath)
	if err != nil {
		return drivers.LaunchResult{}, err
	}
	args := spec.Args
	name := binary
	// There is no SIMCTL_CHILD_ equivalent here and none is needed: the
	// binary is the app, so the variables go straight onto it.
	if len(spec.Env) > 0 {
		args = append(drivers.EnvArgs("", spec.Env, binary), args...)
		name = drivers.EnvPrefixPath
	}
	pid, err := d.exec.Start(ctx, "", name, args...)
	if err != nil {
		return drivers.LaunchResult{}, err
	}
	return drivers.LaunchResult{PID: pid, BundleID: spec.BundleID}, nil
}

// bundleExecutable resolves Contents/MacOS/<binary>. It picks the only
// executable in there instead of assuming it is named after the bundle,
// because they do not always match, Nokoru's, to name one, is not called
// Nokoru in all its variants.
func bundleExecutable(appPath string) (string, error) {
	dir := filepath.Join(appPath, "Contents", "MacOS")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", errors.New("macsystem: no executable directory in " + appPath)
	}
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, entry.Name()))
	}
	switch len(candidates) {
	case 0:
		return "", errors.New("macsystem: no executable in " + dir)
	case 1:
		return candidates[0], nil
	default:
		// With several, the one named after the bundle is the convention.
		want := strings.TrimSuffix(filepath.Base(appPath), ".app")
		for _, candidate := range candidates {
			if filepath.Base(candidate) == want {
				return candidate, nil
			}
		}
		return "", errors.New("macsystem: ambiguous executable in " + dir)
	}
}

// Uninstall is the honest equivalent of `simctl uninstall` on the Mac: it
// does not uninstall the app, which runs from wherever it is, but deletes
// its state, which is what --clear-state really means.
func (d *System) Uninstall(ctx context.Context, _ drivers.Target, bundleID string) error {
	if bundleID == "" {
		return errors.New("macsystem: uninstall needs a bundle id")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// Sandboxed: everything hangs off the container. Without a sandbox, the
	// preferences live separately, so both must be deleted rather than
	// assuming which one it is.
	container := filepath.Join(home, "Library", "Containers", bundleID)
	if err := os.RemoveAll(container); err != nil {
		return err
	}
	d.exec.Run(ctx, "defaults", "delete", bundleID)
	return nil
}

// Boot does not exist on the Mac: the machine is already booted.
func (d *System) Boot(_ context.Context, _ drivers.Target) error { return nil }

// Terminate closes the app. `osascript quit` asks for a clean shutdown,
// which is what lets the app close its database instead of leaving the WAL
// half-written, exactly what the fixture needs before seeding.
func (d *System) Terminate(ctx context.Context, _ drivers.Target, bundleID string) error {
	if bundleID == "" {
		return errors.New("macsystem: terminate needs a bundle id")
	}
	script := `tell application id "` + bundleID + `" to quit`
	if res := d.exec.Run(ctx, "osascript", "-e", script); res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *System) OpenURL(ctx context.Context, _ drivers.Target, url string) error {
	if res := d.exec.Run(ctx, "open", url); res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *System) ClipboardWrite(ctx context.Context, _ drivers.Target, text string) error {
	input, ok := d.exec.(drivers.InputExecutor)
	if !ok {
		return errors.New("macsystem: input executor unavailable")
	}
	if res := input.RunInput(ctx, text, "pbcopy"); res.Err != nil {
		return errors.New(firstLine(res.Stderr))
	}
	return nil
}

func (d *System) ClipboardRead(ctx context.Context, _ drivers.Target) (string, error) {
	res := d.exec.Run(ctx, "pbpaste")
	if res.Err != nil {
		return "", errors.New(firstLine(res.Stderr))
	}
	return res.Stdout, nil
}

func (d *System) ListApps(ctx context.Context, _ drivers.Target) (string, error) {
	res := d.exec.Run(ctx, "ls", "/Applications")
	if res.Err != nil {
		return "", errors.New(firstLine(res.Stderr))
	}
	return res.Stdout, nil
}

// SetLocation and ResetLocation have no equivalent: macOS does not allow
// overriding the location of an already launched app. The path that does
// exist is an Xcode scheme's "Simulate Location", which happens at launch
// and not here.
func (d *System) SetLocation(context.Context, drivers.Target, float64, float64) error {
	return errors.New("location_unsupported_on_macos")
}

func (d *System) ResetLocation(context.Context, drivers.Target) error {
	return errors.New("location_unsupported_on_macos")
}
