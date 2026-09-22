package drivers

import (
	"context"
	"os/exec"
)

// Probe is the subset of mav.Runner that drivers may use to inspect the host.
// Driver implementations should accept this narrow interface in their
// constructors so tests can fake it without pulling in the full Runner.
type Probe interface {
	LookPath(name string) (string, error)
}

// realProbe wraps exec.LookPath for production use.
type realProbe struct{}

func (realProbe) LookPath(name string) (string, error) { return exec.LookPath(name) }

// RealProbe returns a Probe that consults the actual PATH.
func RealProbe() Probe { return realProbe{} }

// Driver is the base interface every driver implements. The functional
// interfaces below (TapDriver, GestureDriver, ...) embed Driver and add the
// methods for a specific capability. A single concrete driver typically
// implements several functional interfaces.
type Driver interface {
	// ID is the stable lookup name used in --prefer-driver, doctor output,
	// MAV_DRIVERS_DISABLE, etc. Examples: "axe", "goios", "baguette".
	ID() string
	// Provides declares which capabilities this driver can serve for the given
	// target. It MAY consult the target kind (sim/device) so a driver can
	// expose different capabilities per target.
	Provides(target Target) CapabilitySet
	// Cost is a routing weight (lower is preferred). Use 0 for the canonical
	// driver of a capability, 50 for acceptable, 100 for last-resort fallback.
	Cost(cap Capability, target Target) int
	// Probe runs a light-weight health check.
	Probe(ctx context.Context, p Probe) HealthReport
	// Warm optionally pre-establishes session state (e.g. boot a server,
	// open a tunnel). Returning nil means there is no async warm-up to wait
	// on; the channel will be closed when Warm completes.
	Warm(ctx context.Context, target Target) <-chan error
}

// --- functional interfaces ----------------------------------------------

type TapDriver interface {
	Driver
	Tap(ctx context.Context, target Target, spec TapSpec) (TapResult, error)
}

type TreeDriver interface {
	Driver
	Tree(ctx context.Context, target Target, spec TreeSpec) (TreeResult, error)
}

// PointTreeDriver describes only what is under one screen coordinate.
//
// It is a separate interface and not a field on TreeSpec because a driver that
// ignored such a field would hand back the whole screen and every caller would
// believe it had asked a cheap question. Measured on iPhone 17 Pro / iOS 26.3,
// 10 reads each of the same still screen: the whole tree is 287 ms and the
// point is 128 ms.
type PointTreeDriver interface {
	Driver
	TreeAtPoint(ctx context.Context, target Target, x, y int) (TreeResult, error)
}

type GestureDriver interface {
	Driver
	Swipe(ctx context.Context, target Target, spec SwipeSpec) error
	Pinch(ctx context.Context, target Target, spec PinchSpec) error
	Rotate(ctx context.Context, target Target, spec RotateSpec) error
	TwoFingerPan(ctx context.Context, target Target, spec TwoFingerPanSpec) error
	W3CActions(ctx context.Context, target Target, actionsJSON []byte) error
}

type TextDriver interface {
	Driver
	Type(ctx context.Context, target Target, spec TextSpec) error
	HideKeyboard(ctx context.Context, target Target) error
}

type TypeDriver interface {
	Driver
	Type(ctx context.Context, target Target, spec TextSpec) error
}

// EraseDriver is deletion, and it is its own interface because the driver
// that can type into a simulator field is not necessarily the one that can
// delete from it. Bundling Erase into TextDriver meant every typist was
// assumed to be a deleter too, and baguette — which types nothing and
// deletes nothing through its HID keyboard — satisfied the interface and
// won the route.
type EraseDriver interface {
	Driver
	Erase(ctx context.Context, target Target, spec TextSpec) error
}

type ScreenshotDriver interface {
	Driver
	Screenshot(ctx context.Context, target Target, spec ScreenshotSpec) error
}

type VideoDriver interface {
	Driver
	VideoStart(ctx context.Context, target Target, spec VideoSpec) (VideoResult, error)
	VideoStop(ctx context.Context, target Target, pid int) error
}

type LogDriver interface {
	Driver
	LogStreamStart(ctx context.Context, target Target, spec LogStreamSpec) (LogStreamResult, error)
	LogStreamStop(ctx context.Context, pid int) error
}

type CrashDriver interface {
	Driver
	CrashFetch(ctx context.Context, target Target, spec CrashSpec) ([]CrashEntry, error)
}

type LifecycleDriver interface {
	Driver
	Install(ctx context.Context, target Target, spec InstallSpec) error
	Launch(ctx context.Context, target Target, spec LaunchSpec) (LaunchResult, error)
	Uninstall(ctx context.Context, target Target, bundleID string) error
	Boot(ctx context.Context, target Target) error
}

type NetworkDriver interface {
	Driver
	NetworkStart(ctx context.Context, target Target, spec NetworkCaptureSpec) (NetworkCaptureResult, error)
	NetworkStop(ctx context.Context, pid int) error
}

type HardwareButtonDriver interface {
	Driver
	PressButton(ctx context.Context, target Target, btn HardwareButton) error
}

type AdvancedGestureDriver interface {
	Driver
	DoubleTap(ctx context.Context, target Target, spec TapSpec) error
	Drag(ctx context.Context, target Target, spec DragSpec) error
	DragPath(ctx context.Context, target Target, spec DragPathSpec) error
}

type DeviceUtilityDriver interface {
	Driver
	ListApps(ctx context.Context, target Target) (string, error)
	Terminate(ctx context.Context, target Target, bundleID string) error
	OpenURL(ctx context.Context, target Target, url string) error
	SetLocation(ctx context.Context, target Target, latitude, longitude float64) error
	ResetLocation(ctx context.Context, target Target) error
	ClipboardWrite(ctx context.Context, target Target, text string) error
	ClipboardRead(ctx context.Context, target Target) (string, error)
}

// AppearanceDriver switches the simulator-wide light/dark user interface
// style. Simulator-only: there is no equivalent on a physical device.
type AppearanceDriver interface {
	Driver
	SetAppearance(ctx context.Context, target Target, appearance string) error
}

// StatusBarDriver overrides the simulator status bar (time, battery, signal)
// so App Store screenshots do not show the real clock. Simulator-only.
type StatusBarDriver interface {
	Driver
	SetStatusBar(ctx context.Context, target Target, spec StatusBarSpec) error
	ClearStatusBar(ctx context.Context, target Target) error
}

// SystemLanguageDriver sets the simulator-wide language and region. This is
// NOT the app's language: `open: { language: ... }` is a launch argument and
// reaches one process, while the status bar, the springboard alerts and every
// system-drawn string follow the SIMULATOR's language. On iPad that gap is
// visible in every App Store screenshot, because an iPad status bar shows the
// date -- measured on iPad Pro 13-inch (M4) / iOS 26.3, an English capture on
// a Spanish-configured simulator reads "Viernes 18 de septiembre".
type SystemLanguageDriver interface {
	Driver
	SystemLanguage(ctx context.Context, target Target) (language string, locale string, err error)
	SetSystemLanguage(ctx context.Context, target Target, language string, locale string) error
}

type WallClockDriver interface {
	Driver
	InjectTimeControl(ctx context.Context, target Target) error
	FreezeTime(ctx context.Context, target Target, at string) (string, error)
	TravelTime(ctx context.Context, target Target, by string) (string, error)
	ScaleTime(ctx context.Context, target Target, factor float64) (string, error)
	TimeStatus(ctx context.Context, target Target) (string, error)
	ResetTime(ctx context.Context, target Target) error
}
