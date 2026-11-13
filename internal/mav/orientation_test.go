package mav

import (
	"bytes"
	"context"
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
			"axe --output json describe-ui --udid " + udid:              treeJSON,
		},
	}
	var out bytes.Buffer
	return CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}, runner, &out, root
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
	if !strings.Contains(strings.Join(runner.commands, "\n"), "-x 301 -y 460") {
		t.Fatalf("the tap moved on an unrotated simulator: %q", runner.commands)
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "describe-ui") {
		t.Fatalf("the hot path read the tree with nothing to rotate: %q", runner.commands)
	}
	if _, err := os.Stat(filepath.Join(root, MavDir, "screens")); !os.IsNotExist(err) {
		t.Fatalf("a portrait run wrote a screen cache")
	}
}
