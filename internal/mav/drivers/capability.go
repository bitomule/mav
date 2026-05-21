// Package drivers defines the pluggable driver layer that MAV uses to talk to
// iOS simulators and physical devices. Concrete drivers (axe, go-ios, baguette,
// simctl, ...) live in subpackages and declare which capabilities they provide
// via the Capability enum below. The router (see router.go) picks the best
// driver per operation from a capability matrix at runtime.
package drivers

// Capability identifies a single operation that a driver may implement.
// Drivers declare the set of capabilities they provide; the router uses that
// set, plus health and cost, to choose who serves a given request.
type Capability string

const (
	// Input
	CapTap          Capability = "tap"            // tap by target (coord or semantic) — meta
	CapSemanticTap  Capability = "tap.semantic"   // tap by accessibility id/text/value
	CapCoordTap     Capability = "tap.coord"      // tap by (x, y)
	CapType         Capability = "type"           // type text via keyboard
	CapErase        Capability = "erase"          // erase focused field
	CapHideKeyboard Capability = "hide_keyboard"  // dismiss the on-screen keyboard
	CapSwipe        Capability = "swipe"          // single-finger swipe
	CapPinch        Capability = "pinch"          // two-finger pinch
	CapRotate       Capability = "rotate"         // two-finger rotate
	CapTwoFingerPan Capability = "two_finger_pan" // two-finger pan
	CapW3CActions   Capability = "w3c_actions"    // W3C Actions JSON dispatch
	CapHardwareBtn  Capability = "hardware_btn"   // home/volume/lock

	// Introspection
	CapTreeAX     Capability = "tree.accessibility" // app accessibility tree
	CapTreeSystem Capability = "tree.system"        // SpringBoard/system UI tree
	CapScreenshot Capability = "screenshot"

	// Lifecycle
	CapInstall   Capability = "lifecycle.install"
	CapLaunch    Capability = "lifecycle.launch"
	CapUninstall Capability = "lifecycle.uninstall"
	CapBoot      Capability = "lifecycle.boot"
	CapLocale    Capability = "lifecycle.locale"

	// Evidence / observation
	CapVideo          Capability = "video"
	CapLogStream      Capability = "log.stream"
	CapCrashFetch     Capability = "crash.fetch"
	CapNetworkCapture Capability = "network.capture"
	CapHIDSync        Capability = "hid.sync" // optional: synchronous HID delivery confirmation
)

// CapabilitySet is the unordered set of capabilities a driver provides.
type CapabilitySet map[Capability]struct{}

// NewSet builds a CapabilitySet from a varargs list.
func NewSet(caps ...Capability) CapabilitySet {
	set := make(CapabilitySet, len(caps))
	for _, c := range caps {
		set[c] = struct{}{}
	}
	return set
}

// Has returns true if the set contains cap.
func (s CapabilitySet) Has(cap Capability) bool {
	_, ok := s[cap]
	return ok
}

// Add inserts cap into the set in place.
func (s CapabilitySet) Add(cap Capability) {
	s[cap] = struct{}{}
}
