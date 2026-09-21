package mav

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Where a rotation came from, and what MAV is allowed to conclude from it.
//
// Two things can rotate a simulator and they do not agree with each other:
//
//   - Simulator.app's Device ▸ Rotate menu turns the WINDOW, and records the
//     angle in a per-device user default MAV can read.
//   - `baguette orientation` (and anything else driving GSEvents) turns the
//     DEVICE without Simulator.app ever hearing about it, so that default
//     stays at whatever it was — usually 0 — while the tree goes landscape.
//
// In the second case the accessibility tree says "landscape" and nothing on
// the machine says WHICH landscape. The two differ by 180°, so guessing puts
// every tap in the diagonally opposite corner. `mav ui orientation` exists to
// remove the guess: MAV sets the orientation itself and remembers what it
// set, which is the only source that cannot be stale about the direction.
const (
	orientationSourceDeclared = "mav"    // mav ui orientation set it
	orientationSourceWindow   = "window" // Simulator.app's window angle
)

// MAV's orientation vocabulary is baguette's, passed through unchanged,
// because baguette is what applies it. These are UIDeviceOrientation names.
// Note that UIKit's device and interface orientations are inverses of one
// another, so `landscape-left` here is the rotation Simulator.app's menu
// calls "Rotate Right" — which is exactly why MAV maps each source with its
// own measured constant instead of trusting the shared word "left".
const (
	orientationPortrait       = "portrait"
	orientationLandscapeLeft  = "landscape-left"
	orientationLandscapeRight = "landscape-right"
	orientationUpsideDown     = "portrait-upside-down"
)

func orientationValues() []string {
	values := []string{orientationPortrait, orientationLandscapeLeft, orientationLandscapeRight, orientationUpsideDown}
	sort.Strings(values)
	return values
}

// declaredOrientationRotation is the rotation MAV must apply to a tree-space
// point after setting this orientation, measured on a booted iPhone 17 Pro
// (iOS 26.3) by dispatching a known element's centre through every candidate
// transform and reading back which one the app reported as tapped:
//
//	baguette orientation landscape-left  -> (y, portraitHeight - x)   = 270
//	baguette orientation landscape-right -> (portraitWidth - y, x)    = 90
//
// portrait-upside-down resolves to no rotation on purpose: an upside-down
// tree is portrait-shaped exactly like an app that refused to flip, and most
// apps (SpringBoard included on home-button-less iPhones) refuse, so there is
// no observation that tells the two apart. See hidPoint's 180 branch.
func declaredOrientationRotation(value string) (int, bool) {
	switch value {
	case orientationPortrait, orientationUpsideDown:
		return 0, true
	case orientationLandscapeLeft:
		return 270, true
	case orientationLandscapeRight:
		return 90, true
	}
	return 0, false
}

// declaredOrientation is what `mav ui orientation` last applied to a device.
// Like screenCache, it is stored under this project's .mav directory, keyed
// by UDID: the same simulator declared from two different project roots is
// tracked separately in each root, not shared globally.
// WindowAngle is the Simulator.app window angle observed at the moment this
// declaration was written, so a LATER window rotation can be told apart from
// a stale leftover one. It is a pointer, not an int, because a declaration
// file written before this field existed has no window_angle key at all, and
// that "unknown" case must be told apart from a recorded angle of 0 (a
// genuinely portrait window at declare time): only the latter should ever be
// compared against a live reading. nil always keeps the pre-existing
// behaviour of trusting the declaration outright.
type declaredOrientation struct {
	Value       string `json:"value"`
	Rotation    int    `json:"rotation"`
	WindowAngle *int   `json:"window_angle,omitempty"`
}

func declaredOrientationPath(root, udid string) string {
	return filepath.Join(root, MavDir, "orientation", udid+".json")
}

func readDeclaredOrientation(root, udid string) (declaredOrientation, bool) {
	if udid == "" {
		return declaredOrientation{}, false
	}
	data, err := os.ReadFile(declaredOrientationPath(root, udid))
	if err != nil {
		return declaredOrientation{}, false
	}
	var declared declaredOrientation
	if json.Unmarshal(data, &declared) != nil {
		return declaredOrientation{}, false
	}
	// A value MAV does not recognise is not a rotation it can apply. This
	// also rejects a file written by a future version with a vocabulary
	// this one does not have, instead of reading its rotation as gospel.
	if _, ok := declaredOrientationRotation(declared.Value); !ok {
		return declaredOrientation{}, false
	}
	return declared, true
}

func writeDeclaredOrientation(root, udid string, declared declaredOrientation) error {
	path := declaredOrientationPath(root, udid)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(declared)
	if err != nil {
		return err
	}
	// Written through a temporary file in the same directory: a torn write
	// here would be read back as "no declaration" on the next tap, silently
	// dropping the rotation for the rest of the run.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func clearDeclaredOrientation(root, udid string) {
	_ = os.Remove(declaredOrientationPath(root, udid))
}

// simulatorAlreadyBooted reports whether a device is booted RIGHT NOW, for
// deciding whether a `mav sim boot` is about to be a real state transition or
// simctl's documented no-op.
//
// It exists next to isSimulatorBooted rather than reusing it because the two
// answer different questions and therefore need opposite failure defaults.
// isSimulatorBooted guards a retry, so an unknown state defaults to "booted"
// and skips a pointless ~15s re-resolve. Here an unknown state must default
// to "not booted": the caller clears a rotation declaration when the device
// really booted, and getting that wrong in the other direction leaves a
// declaration describing an orientation the device no longer has, so every
// later gesture is confidently transformed into the wrong space. Clearing
// when we did not have to costs one screen re-probe.
func simulatorAlreadyBooted(runner Runner, udid string) bool {
	if runner == nil || udid == "" {
		return false
	}
	result := runner.Run(context.Background(), "xcrun", "simctl", "list", "devices", "booted", "-j")
	if result.Err != nil {
		return false
	}
	var parsed struct {
		Devices map[string][]struct {
			UDID  string `json:"udid"`
			State string `json:"state"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return false
	}
	for _, devices := range parsed.Devices {
		for _, device := range devices {
			if device.UDID == udid && device.State == "Booted" {
				return true
			}
		}
	}
	return false
}

// rotationReading is what MAV concluded about a device's rotation and how it
// got there. Source is empty exactly when Angle is 0 and nothing claimed a
// rotation at all.
type rotationReading struct {
	Angle  int
	Source string
}

// resolveRotation prefers what MAV itself applied over what Simulator.app's
// window says, because they answer different questions: the declaration is
// about the DEVICE (which is what the touch surface follows) and the window
// default is about a window that may not even exist on a headless boot. When
// MAV has declared an orientation, a stale window angle left over from an
// earlier session must not override it.
//
// But a declaration can itself go stale: Simulator.app's Device ▸ Rotate menu
// can turn the window again AFTER `mav ui orientation` ran, and nothing else
// would ever notice -- the declaration file never expires on its own. So the
// live window angle is still read whenever a declaration exists, and the
// declaration is dropped in favour of it, but only when the live angle is
// non-zero AND differs from the angle recorded at declare time. A live 0 is
// indistinguishable from "no window at all" (headless boot) or "nobody
// touched the window since", either of which must keep the declaration; and a
// declaration with no recorded window angle at all (WindowAngle == nil, i.e.
// written by an older MAV) is never second-guessed, so upgrading MAV mid-run
// does not retroactively invalidate a declaration that predates this check.
func resolveRotation(runner Runner, root, udid string) rotationReading {
	if udid == "" {
		return rotationReading{}
	}
	if declared, ok := readDeclaredOrientation(root, udid); ok {
		liveAngle := simulatorRotationAngle(runner, udid)
		if liveAngle != 0 && declared.WindowAngle != nil && liveAngle != *declared.WindowAngle {
			clearDeclaredOrientation(root, udid)
			clearScreenCache(root, udid)
			return windowRotation(liveAngle)
		}
		return rotationReading{Angle: declared.Rotation, Source: orientationSourceDeclared}
	}
	return windowRotation(simulatorRotationAngle(runner, udid))
}

// windowRotation turns a Simulator.app window angle into a reading MAV will
// act on, and 180 is not one of them.
//
// Two measurements say so. First, MAV already refuses to use it: hidPoint
// will not mirror a point at 180 (an upside-down tree is portrait-shaped
// exactly like an app that refused to flip, so there is no observation that
// tells them apart), and rotatedDirectionSwipe fails for the same reason. A
// 180 that reaches the gesture path can therefore change nothing about the
// coordinates -- it only ever reroutes the gesture off AXe and stamps
// `rotation_unavailable=180` onto a drag that was dispatched exactly as a
// portrait one. The advice that rides with it ("pass --start-x/--start-y
// read from `mav ui tree`") leads nowhere: those coordinates come back
// through hidPoint and are dispatched raw too.
//
// Second, the reroute's premise is false at 180. It exists because AXe 1.7+
// refuses coordinate gestures on a rotated simulator whose orientation
// SimulatorKit will not report -- but measured on 2026-09-21 against a
// headless simpool iPhone 17 Pro (iOS 26.3) whose DevicePreferences entry
// reads SimulatorWindowRotationAngle = "-180", `axe tap` and `axe swipe`
// both succeeded and both moved the screen. AXe refuses at 90 and 270, not
// here.
//
// And the window angle is the one source that goes stale invisibly: it
// describes a Simulator.app window, which a headless boot does not have. The
// simpool slot above is upright in its own screenshot while the host
// preference has said PortraitUpsideDown since some earlier session, and
// nothing ever clears it -- so every swipe on that slot was being rerouted
// and mislabelled for a window that does not exist.
func windowRotation(angle int) rotationReading {
	if angle == 0 || angle == 180 {
		return rotationReading{}
	}
	return rotationReading{Angle: angle, Source: orientationSourceWindow}
}

// orientationValueUsage renders the accepted values for an error line.
func orientationValueUsage() string {
	return strings.Join(orientationValues(), "|")
}
