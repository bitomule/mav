package mav

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRawScreenCache plants a screen cache entry the way an earlier run
// would have left it, without going through writeScreenCache, so a test can
// start from a cache that is already on disk.
func writeRawScreenCache(t *testing.T, root, udid, contents string) {
	t.Helper()
	path := filepath.Join(root, MavDir, "screens", udid+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// One endpoint supplied and three left at their direction defaults is not a
// gesture: the defaults are portrait-HID-space constants, so the rotation
// gate would push three touch-space points through the tree-space transform.
// Remove the completeness check and this test fails -- the swipe is
// dispatched, and on a rotated simulator the untouched default start-x of 220
// is rotated as if the caller had read it off the tree.
func TestUISwipeRejectsAPartialCoordinateSet(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	allowFail(t, cli.Run(context.Background(), []string{"ui", "swipe", "--start-y", "200"}))
	got := out.String()
	if !strings.Contains(got, "code=swipe_coordinates_incomplete") {
		t.Fatalf("a partial coordinate set was accepted: %q", got)
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "swipe") {
		t.Fatalf("a partial coordinate set still dispatched a swipe: %q", runner.commands)
	}
}

// The flow step is the same code path, and a flow is where a half-specified
// swipe is most likely to be written by hand.
func TestFlowSwipeStepRejectsAPartialCoordinateSet(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, _, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	step := FlowStep{Action: "swipe", Params: map[string]string{"startY": "200"}}
	if _, err := cli.executeFlowStep(context.Background(), RunState{}, 0, step); err == nil {
		t.Fatalf("a flow swipe step with only startY succeeded")
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "swipe") {
		t.Fatalf("a partial flow swipe still dispatched: %q", runner.commands)
	}
}

// Portrait is every headless run, and it must stay a plain ok.
func TestUISwipeDirectionDefaultsSayNothingOnAnUnrotatedSimulator(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	portraitTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, _, out, _ := rotationCLI(t, udid, devicePreferencesDump, portraitTree)
	if err := cli.Run(context.Background(), []string{"ui", "swipe", "--direction", "up"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "rotation") {
		t.Fatalf("an unrotated direction swipe reported a rotation: %q", out.String())
	}
}

// A 180 window rotation is indistinguishable, by tree shape, from a
// portrait-locked app that never flipped: both report h > w. Mirroring the
// point for an app still rendering plain portrait sends the tap to the
// diagonally opposite corner, in bounds, stamped rotation=180 -- silently
// wrong. So MAV never applies one, which is what makes carrying it any
// further pointless: see windowRotation.
const flippedPreferencesDump = `{
    DevicePreferences =     {
        "FFFFFFFF-0000-0000-0000-000000000006" =         {
            SimulatorWindowOrientation = PortraitUpsideDown;
            SimulatorWindowRotationAngle = 180;
        };
    };
}`

// A 180 window angle changes nothing about the point MAV dispatches, so it
// must change nothing about the line MAV prints either. Stamping
// rotation_unavailable=180 on a tap that went out exactly where the caller
// asked reads as "your coordinate may be in the wrong space" and sends them
// off to re-derive a coordinate that was already correct. Revert
// windowRotation's 180 case and this test fails on the phantom field.
func TestUITapDispatchesRawUnderA180Rotation(t *testing.T) {
	const udid = "FFFFFFFF-0000-0000-0000-000000000006"
	portraitTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, runner, out, root := rotationCLI(t, udid, flippedPreferencesDump, portraitTree)
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "150", "--y", "300"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "rotation=") || strings.Contains(got, "hid_x=") {
		t.Fatalf("a 180 rotation was reported as applied: %q", got)
	}
	if strings.Contains(got, "rotation_unavailable") {
		t.Fatalf("a 180 that changed no coordinate was still surfaced as a rotation: %q", got)
	}
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "-x 150 -y 300") {
		t.Fatalf("the tap was not dispatched at its original coordinates: %q", runner.commands)
	}
	if strings.Contains(joined, "describe-ui") {
		t.Fatalf("an unappliable 180 still probed the tree: %q", runner.commands)
	}
	if _, err := os.Stat(filepath.Join(root, MavDir, "screens")); !os.IsNotExist(err) {
		t.Fatalf("a 180 run wrote a screen cache")
	}
}
