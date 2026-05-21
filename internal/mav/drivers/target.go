package drivers

// TargetKind discriminates simulator vs physical device. A few drivers (axe)
// happen to work on both; most are tied to one.
type TargetKind string

const (
	KindSim    TargetKind = "sim"
	KindDevice TargetKind = "device"
)

// Target is the device/sim a driver operates on for a single call. It replaces
// passing the bulky Config struct around: callers extract just what drivers
// need.
type Target struct {
	Kind     TargetKind
	UDID     string
	Name     string
	Runtime  string // simulator runtime (e.g. "iOS-26-2"), empty for device
	BundleID string
	Locale   string
	Language string
}

// IsSim is a convenience for the common branch.
func (t Target) IsSim() bool { return t.Kind == KindSim }

// IsDevice mirrors IsSim.
func (t Target) IsDevice() bool { return t.Kind == KindDevice }

// --- input specs ---------------------------------------------------------

// TapSpec describes a single tap. Either Coord OR Selector should be set;
// drivers that only support one path inspect which fields are populated.
type TapSpec struct {
	Selector ElementSelector
	X, Y     int // when Selector is zero-valued, the tap is by coordinate
	Duration int // ms; 0 = default
}

// ElementSelector identifies a UI element semantically. Drivers match fields
// in priority order: ID, then Text, then Value. Empty fields are ignored.
type ElementSelector struct {
	ID    string
	Text  string
	Value string
}

// IsZero reports whether the selector has no matching criteria.
func (s ElementSelector) IsZero() bool {
	return s.ID == "" && s.Text == "" && s.Value == ""
}

// SwipeSpec describes a single-finger swipe.
type SwipeSpec struct {
	Direction      string // "up"|"down"|"left"|"right" or empty when start/end are set
	StartX, StartY int
	EndX, EndY     int
	DurationMs     int
}

// PinchSpec is a two-finger pinch centred at (X, Y) with Scale > 1 to zoom in
// and Scale < 1 to zoom out.
type PinchSpec struct {
	X, Y       int
	Scale      float64
	PanX, PanY int // optional drift during the pinch
	DurationMs int
}

// RotateSpec is a two-finger rotation around (X, Y) by Degrees.
type RotateSpec struct {
	X, Y       int
	Degrees    float64
	DurationMs int
}

// TwoFingerPanSpec moves both fingers in parallel from (X, Y) by (PanX, PanY).
type TwoFingerPanSpec struct {
	X, Y       int
	PanX, PanY int
	DurationMs int
	HoldMs     int // optional hold at the end before lift
}

// TextSpec is the body of a type/erase call.
type TextSpec struct {
	Text     string
	Selector ElementSelector // optional: type into a specific field
	Focused  bool            // if true, target the currently focused field
}

// TreeSpec controls how the accessibility tree is collected.
type TreeSpec struct {
	IncludeSystem bool // if true, expose SpringBoard / system UI nodes
	MaxElements   int  // 0 = driver default (typically 80)
}

// ScreenshotSpec is a placeholder for future options (region, scale).
type ScreenshotSpec struct {
	OutPath string // absolute path; drivers write the PNG here
}

// LogStreamSpec configures a tail of device/sim logs.
type LogStreamSpec struct {
	OutPath  string
	BundleID string // optional: filter to a single app
}

// VideoSpec configures a video recording.
type VideoSpec struct {
	OutPath string
}

// CrashSpec scopes a crash fetch.
type CrashSpec struct {
	BundleID string
	OutDir   string // where to write .ips files + summaries
}

// LaunchSpec is the body of an app launch.
type LaunchSpec struct {
	BundleID string
	Args     []string
	Env      map[string]string
}

// InstallSpec describes an .app/.ipa install.
type InstallSpec struct {
	Path string
}

// NetworkCaptureSpec configures a HAR-style traffic capture.
type NetworkCaptureSpec struct {
	OutPath    string // .har destination
	ListenPort int    // sim only; 0 = pick free
}

// HardwareButton enumerates physical/simulator buttons.
type HardwareButton string

const (
	BtnHome       HardwareButton = "home"
	BtnLock       HardwareButton = "lock"
	BtnVolumeUp   HardwareButton = "volume_up"
	BtnVolumeDown HardwareButton = "volume_down"
)
