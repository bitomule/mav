package drivers

import "time"

// TapResult is the output of a tap. Drivers populate the fields they can prove.
type TapResult struct {
	MatchedID    string
	MatchedText  string
	MatchedValue string
	X, Y         int // resolved coordinate when known
}

// TreeResult is a snapshot of the accessibility tree, in the MAV-compact shape
// (see the top-level mav.Element). Drivers return XML, JSON, or already-parsed
// structures; the driver-side adapter projects to []byte JSON so the caller
// (cli.go) feeds it straight into ExtractElements.
type TreeResult struct {
	JSON []byte // canonical JSON array of elements
}

// LogStreamResult is the metadata of a started log tail.
type LogStreamResult struct {
	PID     int
	OutPath string
}

// VideoResult is the metadata of a started/stopped video.
type VideoResult struct {
	PID     int
	OutPath string
}

// CrashEntry is a single crash report (the .ips body plus a parsed summary).
// Drivers MUST fetch the body, not just enumerate paths.
type CrashEntry struct {
	Path        string
	Process     string
	Termination string
	Reason      string
	When        time.Time
	Body        []byte // raw .ips
}

// LaunchResult is the PID and bundle of a launched app.
type LaunchResult struct {
	PID      int
	BundleID string
}

// NetworkCaptureResult is the metadata of a started network capture.
type NetworkCaptureResult struct {
	PID        int
	OutPath    string
	ListenPort int
	ProxyURL   string // useful for the agent to display/route
}

// HealthState classifies a driver probe outcome.
type HealthState string

const (
	HealthOK       HealthState = "ok"       // driver works and is preferred
	HealthDegraded HealthState = "degraded" // works but with caveats (e.g. iOS major drift)
	HealthMissing  HealthState = "missing"  // backing tool/lib not installed
	HealthBroken   HealthState = "broken"   // installed but failing probes
)

// HealthReport summarises a single Driver.Probe() call.
type HealthReport struct {
	State  HealthState
	Detail string            // short human-readable explanation
	Next   string            // suggested remediation (CLI command or doc URL)
	Tools  map[string]string // optional: resolved tool paths/versions
}

// IsHealthy is true when the driver is usable; the router will skip it otherwise.
func (h HealthReport) IsHealthy() bool {
	return h.State == HealthOK || h.State == HealthDegraded
}
