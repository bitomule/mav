package mav

import (
	"context"
	"encoding/json"
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

// The tree-shape check in portraitScreenSize only runs on a probe, so a cache
// hit reaches the rotation with nothing having verified that the tree is
// still reporting the rotated space. Here the cache was probed under this
// very angle, so it is legitimately reused, but the app has since gone
// portrait-locked: the tap's y is beyond the portrait width and the 90
// transform sends it to x = 402 - 700 = -298. Remove hidPoint's bounds guard
// and this test fails with rotation=90 hid_x=-298 on an ok line -- an
// off-screen coordinate reported as the one the caller can trust.
func TestRotatedTapRejectsARotationThatLandsOffScreen(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	portraitShapedTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, runner, out, root := rotationCLI(t, udid, devicePreferencesDump, portraitShapedTree)
	writeRawScreenCache(t, root, udid, `{"portrait_width":402,"portrait_height":874,"angle":90}`)
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "200", "--y", "700"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "rotation=90") || strings.Contains(got, "hid_x=") {
		t.Fatalf("an out-of-bounds rotation was reported as applied: %q", got)
	}
	if !strings.Contains(got, "rotation_unavailable=90") {
		t.Fatalf("the unapplied rotation was not surfaced: %q", got)
	}
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "idb ui tap 200 700") {
		t.Fatalf("the tap was not dispatched at its original coordinates: %q", runner.commands)
	}
	if strings.Contains(joined, "-298") {
		t.Fatalf("a negative coordinate was dispatched: %q", runner.commands)
	}
	// The guard is the cheap one: a cache hit must not pay for a tree probe
	// on every gesture.
	if strings.Contains(joined, "describe-ui") {
		t.Fatalf("a cached rotated gesture re-probed the tree: %q", runner.commands)
	}
}

// A cache entry proves the tree shape agreed with the angle that was in
// effect when it was written, and nothing more. An entry from another angle
// -- including a legacy one written before the angle was recorded at all --
// has to be re-probed, because the shape check is the only thing standing
// between a portrait-locked app and a rotated tap. Drop the angle comparison
// in portraitScreenSize and this test fails: the stale entry is taken as a
// hit and the tree is never read.
func TestScreenCacheFromAnotherAngleIsReprobed(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	portraitShapedTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, runner, out, root := rotationCLI(t, udid, devicePreferencesDump, portraitShapedTree)
	writeRawScreenCache(t, root, udid, `{"portrait_width":402,"portrait_height":874}`)
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "200", "--y", "700"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "describe-ui") {
		t.Fatalf("a cache entry from another angle was reused without re-probing: %q", runner.commands)
	}
	got := out.String()
	if strings.Contains(got, "rotation=90") {
		t.Fatalf("the re-probe found a portrait tree and still rotated: %q", got)
	}
	if !strings.Contains(got, "rotation_unavailable=90") {
		t.Fatalf("the mismatch was not surfaced: %q", got)
	}
	if !strings.Contains(joined, "idb ui tap 200 700") {
		t.Fatalf("the tap was not dispatched at its original coordinates: %q", runner.commands)
	}
	data, err := os.ReadFile(filepath.Join(root, MavDir, "screens", udid+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var cached screenCache
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatal(err)
	}
	if cached.Angle != 0 {
		t.Fatalf("a probe that contradicted the angle rewrote the cache: %+v", cached)
	}
}

// The screen size is still probed once per UDID while the angle holds: the
// angle check must not turn every rotated gesture into a tree read.
func TestMatchingScreenCacheAngleStillAvoidsAProbe(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, root := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	writeRawScreenCache(t, root, udid, `{"portrait_width":402,"portrait_height":874,"angle":90}`)
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "249", "--y", "202"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "describe-ui") {
		t.Fatalf("a matching cache entry was re-probed: %q", runner.commands)
	}
	got := out.String()
	if !strings.Contains(got, "rotation=90") || !strings.Contains(got, "hid_x=200") {
		t.Fatalf("the cached screen size was not used to rotate: %q", got)
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

// Direction defaults are dispatched unrotated, which is right -- and along
// the wrong axis of a rotated screen, which nothing can fix from inside the
// gesture. Saying so is the whole remedy: an "up" swipe that drags sideways
// and returns a bare ok is how a scrollUntil burns every swipe and reports
// only a timeout. Remove the direction branch's angle read and this test
// fails.
func TestUISwipeDirectionDefaultsReportTheUncompensatedAxis(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	if err := cli.Run(context.Background(), []string{"ui", "swipe", "--direction", "up"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "rotation_unavailable=90") {
		t.Fatalf("a direction swipe on a rotated simulator claimed nothing was wrong: %q", got)
	}
	if !strings.Contains(got, "not axis-compensated") {
		t.Fatalf("the result line does not say the axis is uncompensated: %q", got)
	}
	// The contract itself is unchanged: the constants still go out as written.
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "220 760") && !strings.Contains(joined, "--start-x 220 --start-y 760") {
		t.Fatalf("the direction defaults were not dispatched unrotated: %q", runner.commands)
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
