package mav

import (
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
type declaredOrientation struct {
	Value    string `json:"value"`
	Rotation int    `json:"rotation"`
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
func resolveRotation(runner Runner, root, udid string) rotationReading {
	if udid == "" {
		return rotationReading{}
	}
	if declared, ok := readDeclaredOrientation(root, udid); ok {
		return rotationReading{Angle: declared.Rotation, Source: orientationSourceDeclared}
	}
	if angle := simulatorRotationAngle(runner, udid); angle != 0 {
		return rotationReading{Angle: angle, Source: orientationSourceWindow}
	}
	return rotationReading{}
}

// orientationValueUsage renders the accepted values for an error line.
func orientationValueUsage() string {
	return strings.Join(orientationValues(), "|")
}
