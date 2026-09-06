package mav

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A simulator rotated by anything other than Simulator.app leaves the window
// angle at 0, so MAV cannot tell landscape-left from landscape-right -- and
// they differ by 180 degrees. `mav ui orientation` removes the guess by being
// the thing that rotates it. Revert uiOrientation's declaration write and
// this test fails: the tap goes out unrotated because nothing claims a
// rotation.
func TestDeclaredOrientationDrivesTheRotationWhenTheWindowKnowsNothing(t *testing.T) {
	// CCCCCCCC is the portrait/0 device in the shared fixture, i.e. the
	// window default says there is nothing to compensate for.
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, root := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)

	if err := cli.Run(context.Background(), []string{"ui", "orientation", "landscape-right"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"ok cmd=ui.orientation", "value=landscape-right", "driver=baguette", "rotation=90"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "baguette orientation --udid "+udid+" landscape-right") {
		t.Fatalf("baguette was not asked to rotate: %q", runner.commands)
	}

	out.Reset()
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "624", "--y", "330"}); err != nil {
		t.Fatal(err)
	}
	tapped := out.String()
	for _, want := range []string{"rotation=90", "hid_x=72", "hid_y=624", "rotation_source=mav"} {
		if !strings.Contains(tapped, want) {
			t.Fatalf("missing %q in %q", want, tapped)
		}
	}
	if _, err := os.Stat(filepath.Join(root, MavDir, "orientation", udid+".json")); err != nil {
		t.Fatalf("the declaration was not persisted: %v", err)
	}
}

// baguette's landscape-left and landscape-right are UIDeviceOrientation
// names, which are the inverse of the UIInterfaceOrientation names
// Simulator.app's menu uses. Both mappings were measured on a real device;
// swapping either one silently sends every tap to the opposite corner, and
// nothing else in the suite would notice.
func TestDeclaredOrientationRotationsAreTheMeasuredOnes(t *testing.T) {
	cases := map[string]int{
		orientationPortrait:       0,
		orientationLandscapeLeft:  270,
		orientationLandscapeRight: 90,
		orientationUpsideDown:     0,
	}
	for value, want := range cases {
		got, ok := declaredOrientationRotation(value)
		if !ok {
			t.Fatalf("%s: not recognised", value)
		}
		if got != want {
			t.Fatalf("%s: got %d want %d", value, got, want)
		}
	}
	if _, ok := declaredOrientationRotation("sideways"); ok {
		t.Fatal("an unknown orientation was accepted")
	}
}

// A rotation MAV applied itself outranks a window angle: the declaration is
// about the device, which is what the touch surface follows, while the window
// default can be left over from an earlier session or another tool.
func TestDeclaredOrientationOutranksTheWindowAngle(t *testing.T) {
	// AAAAAAAA's window angle is 90 in the fixture; declaring
	// landscape-left means the device is at 270.
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	root := t.TempDir()
	if err := writeDeclaredOrientation(root, udid, declaredOrientation{Value: orientationLandscapeLeft, Rotation: 270}); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true, "idb": true},
		out:   map[string]string{"defaults read com.apple.iphonesimulator DevicePreferences": devicePreferencesDump},
	}
	reading := resolveRotation(runner, root, udid)
	if reading.Angle != 270 || reading.Source != orientationSourceDeclared {
		t.Fatalf("got %+v, want the declared 270", reading)
	}
}

// A declaration is not forever: Simulator.app's Device > Rotate menu can turn
// the window again AFTER `mav ui orientation` ran, and that later rotation
// must win, because it is the last thing that actually happened to the
// device. The declaration recorded a window angle of 0 at declare time (the
// portrait/0 device in the fixture); a live reading of 90 disagrees with
// that, so the declaration -- and the screen cache that goes with it -- must
// be dropped in favour of the window.
func TestALaterWindowRotationInvalidatesAStaleDeclaration(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	root := t.TempDir()
	windowAngleAtDeclare := 0
	if err := writeDeclaredOrientation(root, udid, declaredOrientation{
		Value: orientationLandscapeLeft, Rotation: 270, WindowAngle: &windowAngleAtDeclare,
	}); err != nil {
		t.Fatal(err)
	}
	writeScreenCache(root, udid, screenCache{PortraitWidth: 402, PortraitHeight: 874, Angle: 270})

	// The fixture's AAAAAAAA block reads SimulatorWindowRotationAngle = 90,
	// so borrowing it under CCCCCCCC's key stands in for "someone rotated
	// the window since the declaration was written".
	liveDump := strings.Replace(devicePreferencesDump, "AAAAAAAA-0000-0000-0000-000000000001", udid, 1)
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true, "idb": true},
		out:   map[string]string{"defaults read com.apple.iphonesimulator DevicePreferences": liveDump},
	}
	reading := resolveRotation(runner, root, udid)
	if reading.Angle != 90 || reading.Source != orientationSourceWindow {
		t.Fatalf("got %+v, want the live window's 90", reading)
	}
	if _, ok := readDeclaredOrientation(root, udid); ok {
		t.Fatal("the stale declaration survived a disagreeing live window angle")
	}
	if _, ok := readScreenCache(root, udid); ok {
		t.Fatal("the screen cache survived a disagreeing live window angle")
	}
}

// A live window angle of 0 must NOT invalidate the declaration: 0 is what a
// headless boot with no window at all reads as, and is also what "nobody
// touched the window since the declaration" reads as. Either way the
// declaration -- which is about the device, not the window -- must still
// win. Same for a `defaults read` that errors outright.
func TestDeclarationSurvivesAZeroOrUnreadableLiveWindowAngle(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"

	t.Run("live angle reads zero", func(t *testing.T) {
		root := t.TempDir()
		windowAngleAtDeclare := 0
		if err := writeDeclaredOrientation(root, udid, declaredOrientation{
			Value: orientationLandscapeLeft, Rotation: 270, WindowAngle: &windowAngleAtDeclare,
		}); err != nil {
			t.Fatal(err)
		}
		runner := &sequenceRecordingRunner{
			tools: map[string]bool{"axe": true, "idb": true},
			// CCCCCCCC's own block in the fixture reads angle 0.
			out: map[string]string{"defaults read com.apple.iphonesimulator DevicePreferences": devicePreferencesDump},
		}
		reading := resolveRotation(runner, root, udid)
		if reading.Angle != 270 || reading.Source != orientationSourceDeclared {
			t.Fatalf("got %+v, want the declared 270 to survive a live 0", reading)
		}
		if _, ok := readDeclaredOrientation(root, udid); !ok {
			t.Fatal("the declaration was dropped on a live angle of 0")
		}
	})

	t.Run("defaults read errors", func(t *testing.T) {
		root := t.TempDir()
		windowAngleAtDeclare := 0
		if err := writeDeclaredOrientation(root, udid, declaredOrientation{
			Value: orientationLandscapeLeft, Rotation: 270, WindowAngle: &windowAngleAtDeclare,
		}); err != nil {
			t.Fatal(err)
		}
		runner := &sequenceRecordingRunner{
			tools: map[string]bool{"axe": true, "idb": true},
			err: map[string]CommandResult{
				"defaults read com.apple.iphonesimulator DevicePreferences": {Err: os.ErrNotExist},
			},
		}
		reading := resolveRotation(runner, root, udid)
		if reading.Angle != 270 || reading.Source != orientationSourceDeclared {
			t.Fatalf("got %+v, want the declared 270 to survive an unreadable window angle", reading)
		}
		if _, ok := readDeclaredOrientation(root, udid); !ok {
			t.Fatal("the declaration was dropped on an unreadable window angle")
		}
	})
}

// A declaration MAV cannot parse -- a future vocabulary, a truncated file --
// must read as "no declaration" and fall through to the window angle, not as
// a rotation of 0 that overrides it.
func TestUnreadableDeclarationFallsThroughToTheWindow(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	root := t.TempDir()
	path := declaredOrientationPath(root, udid)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"value":"diagonal","rotation":45}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true, "idb": true},
		out:   map[string]string{"defaults read com.apple.iphonesimulator DevicePreferences": devicePreferencesDump},
	}
	reading := resolveRotation(runner, root, udid)
	if reading.Angle != 90 || reading.Source != orientationSourceWindow {
		t.Fatalf("got %+v, want the window's 90", reading)
	}
}

// A failed rotation must not leave MAV believing one is in effect: every
// later gesture would be transformed into a space the device is not in,
// which is worse than not knowing.
func TestFailedOrientationRecordsNothing(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, _, out, root := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	cli.Runner.(*sequenceRecordingRunner).err = map[string]CommandResult{
		"baguette orientation --udid " + udid + " landscape-right": {Stderr: "no such simulator", Err: os.ErrNotExist},
	}
	allowFail(t, cli.Run(context.Background(), []string{"ui", "orientation", "landscape-right"}))
	if !strings.Contains(out.String(), "fail code=orientation_failed") {
		t.Fatalf("got %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, MavDir, "orientation", udid+".json")); !os.IsNotExist(err) {
		t.Fatal("a failed rotation was recorded as applied")
	}
}

// Declaring a new orientation must drop the screen-size cache: it is keyed by
// the angle it was probed under, and a stale entry would be reused for one
// gesture before anything re-probed.
func TestOrientationChangeInvalidatesTheScreenCache(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, _, out, root := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	writeScreenCache(root, udid, screenCache{PortraitWidth: 402, PortraitHeight: 874, Angle: 90})
	if err := cli.Run(context.Background(), []string{"ui", "orientation", "landscape-left"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	if _, ok := readScreenCache(root, udid); ok {
		t.Fatal("the screen cache survived an orientation change")
	}
}

// Upside-down is accepted and applied, but MAV says plainly that it will not
// compensate coordinates for it -- an upside-down tree is portrait-shaped
// exactly like an app that refused to flip.
func TestUpsideDownIsAppliedButNotCompensated(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	portraitTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, _, out, _ := rotationCLI(t, udid, devicePreferencesDump, portraitTree)
	if err := cli.Run(context.Background(), []string{"ui", "orientation", "portrait-upside-down"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "rotation=0") {
		t.Fatalf("upside-down claimed a rotation it does not apply: %q", got)
	}
	if !strings.Contains(got, "indistinguishable from portrait") {
		t.Fatalf("the result line does not say coordinates are uncompensated: %q", got)
	}
}

func TestOrientationRejectsAnUnknownValue(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	portraitTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, portraitTree)
	allowFail(t, cli.Run(context.Background(), []string{"ui", "orientation", "sideways"}))
	if !strings.Contains(out.String(), "fail code=orientation_value_invalid") {
		t.Fatalf("got %q", out.String())
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "baguette orientation") {
		t.Fatalf("an unknown value still reached baguette: %q", runner.commands)
	}
}
