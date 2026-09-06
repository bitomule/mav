package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shapes `defaults read com.apple.iphonesimulator DevicePreferences`
// actually prints, taken from a real machine. LandscapeRight is written as
// the quoted string "-90", not 270, and a device block nests another
// dictionary, so the end of a block cannot be found by the next brace.
const devicePreferencesDump = `{
    DevicePreferences =     {
        "AAAAAAAA-0000-0000-0000-000000000001" =         {
            ChromeTint = "";
            SimulatorWindowGeometry =             {
                "1C4804D2-7060-46B2-8DA0-1BE785AC8BED" =                 {
                    WindowCenter = "{1046, 529.5}";
                };
            };
            SimulatorWindowOrientation = LandscapeLeft;
            SimulatorWindowRotationAngle = 90;
        };
        "BBBBBBBB-0000-0000-0000-000000000002" =         {
            SimulatorWindowOrientation = LandscapeRight;
            SimulatorWindowRotationAngle = "-90";
        };
        "CCCCCCCC-0000-0000-0000-000000000003" =         {
            SimulatorWindowOrientation = Portrait;
            SimulatorWindowRotationAngle = 0;
        };
        "DDDDDDDD-0000-0000-0000-000000000004" =         {
            ChromeTint = "";
        };
    };
}`

func TestParseRotationAngleReadsEveryShapeSimulatorWrites(t *testing.T) {
	cases := []struct {
		udid string
		want int
	}{
		{"AAAAAAAA-0000-0000-0000-000000000001", 90},
		// A quoted negative is the LandscapeRight case; parsing it as 0
		// leaves exactly one of the two landscapes broken.
		{"BBBBBBBB-0000-0000-0000-000000000002", 270},
		{"CCCCCCCC-0000-0000-0000-000000000003", 0},
		// No angle key at all: a device Simulator.app never rotated.
		{"DDDDDDDD-0000-0000-0000-000000000004", 0},
		// Unknown device: not in the dump at all.
		{"EEEEEEEE-0000-0000-0000-000000000005", 0},
	}
	for _, tc := range cases {
		if got := parseRotationAngle(devicePreferencesDump, tc.udid); got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.udid, got, tc.want)
		}
	}
}

// PlistBuddy renders the same key as a float; a caller reading it that way
// must not fall through to "no rotation".
func TestParseRotationAngleAcceptsAFloatValue(t *testing.T) {
	dump := `{
    "AAAAAAAA-0000-0000-0000-000000000001" =     {
        SimulatorWindowRotationAngle = 90.000000;
    };
}`
	if got := parseRotationAngle(dump, "AAAAAAAA-0000-0000-0000-000000000001"); got != 90 {
		t.Fatalf("got %d want 90", got)
	}
}

// The real shape `defaults read com.apple.iphonesimulator DevicePreferences`
// prints on a live machine: no `DevicePreferences = {` wrapper, device UDID
// keys at exactly four spaces, and a nested SimulatorWindowGeometry
// dictionary at eight. devicePreferencesDump above is the wrong shape (it
// wraps everything in an outer `DevicePreferences` key, which pushes every
// device key to eight spaces) and so never exercises parseRotationAngle's
// block-truncation branch at all.
const realDevicePreferencesDump = `{
    "AAAAAAAA-0000-0000-0000-000000000001" =     {
        SimulatorWindowGeometry =         {
            "1C4804D2-7060-46B2-8DA0-1BE785AC8BED" =             {
                WindowCenter = "{1046, 529.5}";
            };
        };
        SimulatorWindowOrientation = Portrait;
    };
    "BBBBBBBB-0000-0000-0000-000000000002" =     {
        SimulatorWindowOrientation = LandscapeLeft;
        SimulatorWindowRotationAngle = 90;
    };
    "CCCCCCCC-0000-0000-0000-000000000003" =     {
        SimulatorWindowOrientation = LandscapeRight;
        SimulatorWindowRotationAngle = "-90";
    };
}`

// The device immediately before a rotated one has no rotation key at all -
// exactly the case that goes unnoticed if block truncation stops working:
// with no boundary, the search for SimulatorWindowRotationAngle inside
// AAAAAAAA's "block" runs off the end of the dump and finds BBBBBBBB's own
// 90, attributing a neighbour's rotation to a device that was never
// rotated. Revert parseRotationAngle's block-truncation search (e.g. make it
// always take the rest of the dump) and this test fails.
func TestParseRotationAngleDoesNotBleedIntoTheNextDeviceBlock(t *testing.T) {
	if got := parseRotationAngle(realDevicePreferencesDump, "AAAAAAAA-0000-0000-0000-000000000001"); got != 0 {
		t.Fatalf("got %d want 0 (no rotation key, and must not see BBBBBBBB's)", got)
	}
	if got := parseRotationAngle(realDevicePreferencesDump, "BBBBBBBB-0000-0000-0000-000000000002"); got != 90 {
		t.Fatalf("got %d want 90", got)
	}
	if got := parseRotationAngle(realDevicePreferencesDump, "CCCCCCCC-0000-0000-0000-000000000003"); got != 270 {
		t.Fatalf("got %d want 270", got)
	}
}

func rotationCLI(t *testing.T, udid string, dump string, treeJSON string) (CLI, *sequenceRecordingRunner, *bytes.Buffer, string) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	cfg.SimulatorUDID = udid
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"defaults read com.apple.iphonesimulator DevicePreferences": dump,
			"axe describe-ui --udid " + udid:                            treeJSON,
		},
	}
	var out bytes.Buffer
	return CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}, runner, &out, root
}

// A 402x874 device rotated to LandscapeLeft reports a 874x402 tree, but idb
// and axe still dispatch touches in the portrait space. Revert hidPoint's
// call site and this test fails: the tap goes out at the tree's own
// coordinates and lands somewhere else on screen.
func TestUITapRotatesCoordinatesIntoTheHIDSpace(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "249", "--y", "202"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"rotation=90", "hid_x=200", "hid_y=249", "x=249", "y=202"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "idb ui tap 200 249") {
		t.Fatalf("the tap was dispatched unrotated: %q", runner.commands)
	}
}

// LandscapeRight is the other 90 degrees, and the transform is not the same
// one mirrored: it reads the portrait HEIGHT where LandscapeLeft reads the
// width. This is the case the original report came from.
func TestUITapRotatesLandscapeRightTheOtherWay(t *testing.T) {
	const udid = "BBBBBBBB-0000-0000-0000-000000000002"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "250", "--y", "330"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"rotation=270", "hid_x=330", "hid_y=624"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "idb ui tap 330 624") {
		t.Fatalf("the tap was dispatched unrotated: %q", runner.commands)
	}
}

// The overwhelmingly common case: nothing rotated, so nothing moves, nothing
// is reported, and the accessibility tree is never read to find out.
func TestUITapLeavesPortraitCoordinatesAndTheTreeAlone(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	portraitTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, runner, out, root := rotationCLI(t, udid, devicePreferencesDump, portraitTree)
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "301", "--y", "460"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "rotation=") || strings.Contains(got, "hid_x=") {
		t.Fatalf("an unrotated tap reported a rotation: %q", got)
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "idb ui tap 301 460") {
		t.Fatalf("the tap moved on an unrotated simulator: %q", runner.commands)
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "describe-ui") {
		t.Fatalf("the hot path read the tree with nothing to rotate: %q", runner.commands)
	}
	if _, err := os.Stat(filepath.Join(root, MavDir, "screens")); !os.IsNotExist(err) {
		t.Fatalf("a portrait run wrote a screen cache")
	}
}

// The screen size is probed once and reused: the tree costs seconds and a
// tap is the hot loop.
func TestRotatedTapProbesTheScreenOnceAndCachesIt(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, _, root := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	for i := 0; i < 3; i++ {
		if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "249", "--y", "202"}); err != nil {
			t.Fatal(err)
		}
	}
	probes := strings.Count(strings.Join(runner.commands, "\n"), "describe-ui")
	if probes != 1 {
		t.Fatalf("the screen was probed %d times, want 1: %q", probes, runner.commands)
	}
	data, err := os.ReadFile(filepath.Join(root, MavDir, "screens", udid+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var cached screenCache
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatal(err)
	}
	if cached.PortraitWidth != 402 || cached.PortraitHeight != 874 {
		t.Fatalf("cached the rotated size instead of the portrait one: %+v", cached)
	}
}

// A swipe is two points in one space. Rotating one endpoint and not the
// other would turn a vertical drag into a diagonal one, which is worse than
// not rotating at all.
func TestUISwipeRotatesBothEndpoints(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	if err := cli.Run(context.Background(), []string{"ui", "swipe",
		"--start-x", "100", "--start-y", "200", "--end-x", "700", "--end-y", "200"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "rotation=90") {
		t.Fatalf("the swipe did not report its rotation: %q", out.String())
	}
	joined := strings.Join(runner.commands, "\n")
	// (100,200) -> (402-200, 100) = (202,100); (700,200) -> (202,700).
	if !strings.Contains(joined, "swipe --start-x 202 --start-y 100 --end-x 202 --end-y 700") &&
		!strings.Contains(joined, "ui swipe 202 100 202 700") {
		t.Fatalf("the swipe endpoints were not both rotated: %q", runner.commands)
	}
}

// A direction swipe has no caller-supplied points to rotate -- its endpoints
// are fractions of the screen -- so on a rotated simulator they are
// re-derived against the screen the caller is looking at and then rotated
// like any tree-space pair. "up" must run up the visible screen, which on a
// 90-rotated device is a constant-y drag in the portrait touch space.
//
// The screen is 874x402 in tree space, so up = (437, 349) -> (437, 120),
// which at angle 90 dispatches as (53, 437) -> (282, 437). Revert the
// re-derivation and this test fails: the raw portrait constants 220,760 go
// out instead and the drag runs across the screen rather than up it.
func TestUISwipeDirectionDefaultsAreDerivedForARotatedScreen(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	if err := cli.Run(context.Background(), []string{"ui", "swipe", "--direction", "up"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"rotation=90", "direction_endpoints=derived"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "53 437 282 437") &&
		!strings.Contains(joined, "--start-x 53 --start-y 437 --end-x 282 --end-y 437") {
		t.Fatalf("the direction swipe was not re-derived for the rotated screen: %q", runner.commands)
	}
	if strings.Contains(joined, "220 760") {
		t.Fatalf("the raw portrait constants were dispatched on a rotated screen: %q", runner.commands)
	}
	// Whatever the transform does, it must never send a gesture off the
	// touch surface.
	for _, bad := range []string{"-358", "-98"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("a direction swipe went negative/off-screen: %q", runner.commands)
		}
	}
}

// A portrait-locked app keeps reporting its ~402x874 tree even though
// Simulator.app's window (and hence SimulatorWindowRotationAngle) is
// rotated to 90. Rotating the tap into that mismatched space is worse than
// not rotating at all. Revert the shape check in portraitScreenSize and
// this test fails: the tap comes out rotated even though the tree root is
// portrait-shaped.
func TestUITapSkipsRotationWhenTreeShapeContradictsTheAngle(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	portraitShapedTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, runner, out, root := rotationCLI(t, udid, devicePreferencesDump, portraitShapedTree)
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "200", "--y", "700"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "rotation=90") || strings.Contains(got, "hid_x=") {
		t.Fatalf("a portrait-shaped tree under a stale angle was still rotated: %q", got)
	}
	if !strings.Contains(got, "rotation_unavailable=90") {
		t.Fatalf("the mismatch was not surfaced: %q", got)
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "idb ui tap 200 700") {
		t.Fatalf("the tap was not dispatched at its original coordinates: %q", runner.commands)
	}
	if _, err := os.Stat(filepath.Join(root, MavDir, "screens", udid+".json")); !os.IsNotExist(err) {
		t.Fatalf("a mismatched probe wrote a screen cache")
	}
}
