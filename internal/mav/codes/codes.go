// Package codes is MAV's structured error vocabulary. Every user-facing
// failure has a stable ID, a human-readable Title, and a Remediation field
// pointing the operator (or agent) at the next step. The aim is that an
// agent reading a `fail code=...` line can self-recover or pick a different
// path without re-asking the user.
//
// Today's cli.go uses ad-hoc strings for codes (e.g. "tool_missing",
// "unknown_command"); P5 wires this package in incrementally. Old codes
// will keep working for now -- the registry below is additive, not
// exclusive.
package codes

// Code is a single error contract: a stable id plus the fields the agent
// needs to react.
type Code struct {
	// ID is the wire identifier emitted as `code=<id>`. Stable. Snake_case.
	ID string
	// Title is a one-line human summary. Shown in `mav doctor` and in error
	// messages. ~60 chars.
	Title string
	// Remediation is the suggested next action. CLI command, doc URL, or
	// a short directive. Empty when the failure is purely informational.
	Remediation string
	// Driver names the driver responsible for the failure, when applicable.
	// "" when the failure is cross-driver.
	Driver string
	// Capability names the capability that could not be served, when known.
	Capability string
}

// Fields returns the structured key/value pairs to emit in an `Output.Fields`
// map alongside the code itself.
func (c Code) Fields() map[string]string {
	f := map[string]string{}
	if c.Title != "" {
		f["title"] = c.Title
	}
	if c.Remediation != "" {
		f["remediation"] = c.Remediation
	}
	if c.Driver != "" {
		f["driver"] = c.Driver
	}
	if c.Capability != "" {
		f["capability"] = c.Capability
	}
	return f
}

// --- canonical codes -----------------------------------------------------

// DriverUnhealthy is emitted when a router-selected driver fails its probe
// (e.g. baguette installed but SimulatorKit symbols moved on a new iOS).
var DriverUnhealthy = Code{
	ID:          "driver_unhealthy",
	Title:       "Driver installed but failed its health probe",
	Remediation: "Check the driver's upstream issue tracker; rerun `mav doctor`",
}

// GestureUnsupportedOnDevice is emitted when an agent attempts pinch/rotate/
// two-finger-pan/hide-keyboard/erase on a physical device. With Appium gone,
// device flows that need multi-touch are deliberately unsupported.
var GestureUnsupportedOnDevice = Code{
	ID:          "gesture_unsupported_on_device",
	Title:       "Multi-touch gestures are not available on physical devices",
	Remediation: "Run this flow against the simulator, or simulate the gesture by hand",
	Capability:  "gesture",
}

// TreeSystemUnsupportedOnDevice mirrors the above for the system UI tree.
var TreeSystemUnsupportedOnDevice = Code{
	ID:          "tree_system_unsupported_on_device",
	Title:       "System UI tree (SpringBoard/permissions) is not available on device",
	Remediation: "Run this flow against the simulator",
	Capability:  "tree.system",
}

// NoDriverForCapability is the router's "I tried everyone and nobody fit"
// outcome. Considered drivers + reasons travel in the message so the agent
// can pick a different `--prefer-driver` or install something missing.
var NoDriverForCapability = Code{
	ID:          "no_driver_for_capability",
	Title:       "No registered driver can serve this operation",
	Remediation: "Run `mav doctor` to see which capabilities are covered",
}

// ToolMissing is for missing host-side prerequisites (xcrun, idb, axe, ...).
// The Tool field travels in the Output fields map; Driver/Capability are
// often empty here because tool detection happens before routing.
var ToolMissing = Code{
	ID:          "tool_missing",
	Title:       "A required tool is not installed",
	Remediation: "Run `mav setup`",
}

// PreferDriverInvalid is emitted when --prefer-driver gets a value the
// router doesn't know about.
var PreferDriverInvalid = Code{
	ID:          "prefer_driver_invalid",
	Title:       "Unknown driver name",
	Remediation: "Run `mav doctor` to see registered drivers",
}

// Registry is a name -> Code lookup that the HTML report and `mav doctor`
// can iterate to print the full vocabulary. Add new codes here; tests
// guard against accidental ID collisions.
var Registry = map[string]Code{
	DriverUnhealthy.ID:                DriverUnhealthy,
	GestureUnsupportedOnDevice.ID:     GestureUnsupportedOnDevice,
	TreeSystemUnsupportedOnDevice.ID:  TreeSystemUnsupportedOnDevice,
	NoDriverForCapability.ID:          NoDriverForCapability,
	ToolMissing.ID:                    ToolMissing,
	PreferDriverInvalid.ID:            PreferDriverInvalid,
}
