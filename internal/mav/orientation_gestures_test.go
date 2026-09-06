package mav

import (
	"context"
	"strings"
	"testing"
)

// enableBaguette puts baguette in both places a gesture route consults: the
// project config's tool list and the fake runner's PATH.
func enableBaguette(t *testing.T, cli *CLI, runner *sequenceRecordingRunner) {
	t.Helper()
	runner.tools["baguette"] = true
	cfg, err := LoadConfig(cli.Root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools["baguette"] = true
	if err := SaveConfig(cli.Root, cfg); err != nil {
		t.Fatal(err)
	}
}

// baguette consumes the same native-portrait point space idb does -- measured
// side by side on a rotated simulator, both landing the same element from the
// same transformed point -- so every baguette gesture needs the transform a
// tap gets. Leaving them out was the documented gap in v0.17.0.
func TestBaguetteGesturesRotateTheirAnchor(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001" // window angle 90
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	// The anchor (249, 202) at angle 90 becomes (402-202, 249) = (200, 249),
	// and twoFingerPan's upward 60pt pan becomes +60 along the portrait x
	// axis, spreading its two fingers either side of the anchor.
	cases := []struct {
		name string
		args []string
		want string
	}{
		// `ui rotate` is absent on purpose: baguette's Provides() excludes
		// CapRotate, so the router never picks anything for it and the
		// command cannot reach a dispatch to assert on.
		{"longPress", []string{"ui", "longPress", "--x", "249", "--y", "202"}, "baguette tap --udid " + udid + " --x 200 --y 249"},
		{"pinch", []string{"ui", "pinch", "--x", "249", "--y", "202", "--scale", "2"}, "--cx 200 --cy 249"},
		{"twoFingerPan", []string{"ui", "twoFingerPan", "--x", "249", "--y", "202", "--pan-x", "0", "--pan-y", "-60"}, "--dx 60 --dy 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
			enableBaguette(t, &cli, runner)
			if err := cli.Run(context.Background(), tc.args); err != nil {
				t.Fatalf("err=%v out=%s", err, out.String())
			}
			if !strings.Contains(out.String(), "rotation=90") {
				t.Fatalf("%s did not report the rotation it applied: %q", tc.name, out.String())
			}
			joined := strings.Join(runner.commands, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("%s did not dispatch %q: %q", tc.name, tc.want, runner.commands)
			}
			if strings.Contains(joined, "--x 249 --y 202") || strings.Contains(joined, "ui tap 249 202") {
				t.Fatalf("%s dispatched the tree-space anchor verbatim: %q", tc.name, runner.commands)
			}
		})
	}
}

// A long press is dispatched as a coordinate tap, which AXe ties for and wins
// on alphabetical order -- and AXe 1.7+ refuses coordinate gestures on a
// rotated simulator. Revert the routerWithout in uiLongPress and this test
// fails: the press goes to axe, which cannot serve it here.
func TestRotatedLongPressRoutesAwayFromAxe(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	enableBaguette(t, &cli, runner)
	if err := cli.Run(context.Background(), []string{"ui", "longPress", "--x", "249", "--y", "202"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "axe tap") {
		t.Fatalf("a rotated long press went to axe: %q", runner.commands)
	}
}

// A rotation can be DETECTED without being APPLIED -- a 180 window angle is
// the case hidPoint refuses to guess on, so hid.Rotation stays 0 even though
// hid.Detected is 180. Gating the axe exclusion on hid.Rotation != 0 (the old
// condition) left this case routed straight into axe, which cannot serve a
// coordinate tap under any rotation; revert uiLongPress's unconditional
// routerWithout("axe") and this test fails with axe's selector error instead
// of a raw dispatch through baguette.
func TestLongPressDetectedButUnappliedStillRoutesAwayFromAxe(t *testing.T) {
	const udid = "FFFFFFFF-0000-0000-0000-000000000006"
	portraitTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, flippedPreferencesDump, portraitTree)
	enableBaguette(t, &cli, runner)
	if err := cli.Run(context.Background(), []string{"ui", "longPress", "--x", "150", "--y", "300"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "rotation_unavailable=180") {
		t.Fatalf("the unapplied 180 was not surfaced: %q", got)
	}
	joined := strings.Join(runner.commands, "\n")
	if strings.Contains(joined, "axe tap") {
		t.Fatalf("a detected-but-unapplied rotation still went to axe: %q", runner.commands)
	}
	if !strings.Contains(joined, "baguette tap --udid "+udid+" --x 150 --y 300") {
		t.Fatalf("the long press did not dispatch raw through baguette: %q", runner.commands)
	}
}

// A pan is a displacement, not a position: rotating it as a point would add
// the screen's width to it and send the gesture off the surface. At 90 the
// derivative of (x, y) -> (W-y, x) is (dx, dy) -> (-dy, dx), so an upward
// pan of 60 becomes a pan of +60 along the portrait x axis.
func TestPanDeltasRotateAsVectorsNotPoints(t *testing.T) {
	if gotX, gotY := hidVector(0, -60, 90); gotX != 60 || gotY != 0 {
		t.Fatalf("90: got (%d,%d) want (60,0)", gotX, gotY)
	}
	if gotX, gotY := hidVector(0, -60, 270); gotX != -60 || gotY != 0 {
		t.Fatalf("270: got (%d,%d) want (-60,0)", gotX, gotY)
	}
	if gotX, gotY := hidVector(12, -60, 0); gotX != 12 || gotY != -60 {
		t.Fatalf("0: a vector moved with no rotation: (%d,%d)", gotX, gotY)
	}
}

// doubleTap has two dispatch paths -- the gesture worker and the driver --
// and rotating only one of them would make the same command land in two
// different places depending on whether the worker happened to be up.
func TestDoubleTapRotatesOnTheDriverPathToo(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	enableBaguette(t, &cli, runner)
	if err := cli.Run(context.Background(), []string{"ui", "doubleTap", "--x", "249", "--y", "202"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "rotation=90") || !strings.Contains(got, "hid_x=200") || !strings.Contains(got, "hid_y=249") {
		t.Fatalf("doubleTap did not rotate: %q", got)
	}
	// The caller's own coordinates stay on the line unchanged: they are what
	// the caller can look up in the tree again.
	if !strings.Contains(got, "x=249") || !strings.Contains(got, "y=202") {
		t.Fatalf("doubleTap lost the caller's coordinates: %q", got)
	}
	// The result-line fields above come from hidPoint alone (addRotationFields
	// reads only the hidResult, not what was dispatched), so they pass even if
	// the driver call underneath was never rotated. Assert on what baguette
	// actually received.
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "double-tap --udid "+udid+" --x 200 --y 249") {
		t.Fatalf("doubleTap did not dispatch the rotated point: %q", runner.commands)
	}
	if strings.Contains(joined, "--x 249 --y 202") {
		t.Fatalf("doubleTap dispatched the tree-space anchor verbatim: %q", runner.commands)
	}
}

// ui drag was left out of the rotation migration: both endpoints must go
// through hidPoint atomically, like uiSwipe's customCoordinates block, or a
// straight drag on a rotated simulator lands as a diagonal one (or worse,
// off-surface). Revert uiDrag's hidPoint calls and this test fails: baguette
// receives the caller's tree-space coordinates verbatim.
func TestDragRotatesBothEndpointsOnTheDriverPath(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	enableBaguette(t, &cli, runner)
	if err := cli.Run(context.Background(), []string{"ui", "drag",
		"--start-x", "249", "--start-y", "202", "--end-x", "700", "--end-y", "202"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "rotation=90") {
		t.Fatalf("drag did not report the rotation it applied: %q", got)
	}
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "--start-x 200 --start-y 249") || !strings.Contains(joined, "--end-x 200 --end-y 700") {
		t.Fatalf("drag did not dispatch both endpoints rotated: %q", runner.commands)
	}
	if strings.Contains(joined, "--start-x 249") || strings.Contains(joined, "--end-x 700 --end-y 202") {
		t.Fatalf("drag dispatched a tree-space endpoint verbatim: %q", runner.commands)
	}
}

// AXe 1.7+ refuses coordinate gestures outright on a rotated simulator whose
// orientation SimulatorKit will not report -- which is every headless boot.
// Preferring another driver is not enough, because AXe is canonical (cost 0)
// for CapSwipe and wins the route anyway; it has to be taken out of the
// running. Revert routerWithout and this test fails: the swipe routes to axe
// and dies with AXe's own error.
func TestRotatedSwipeRoutesAwayFromAxe(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, runner, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	if err := cli.Run(context.Background(), []string{"ui", "swipe",
		"--start-x", "100", "--start-y", "200", "--end-x", "700", "--end-y", "200"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "rotation_rerouted=axe") {
		t.Fatalf("the reroute was not reported: %q", got)
	}
	if strings.Contains(got, "driver=axe") {
		t.Fatalf("a rotated swipe still routed to axe: %q", got)
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "axe swipe") {
		t.Fatalf("axe was asked to swipe on a rotated simulator: %q", runner.commands)
	}
}

// scrollUntil's timeout diagnosis used to key off the raw detected angle
// alone, which was accurate before uiSwipe learned to re-derive a direction
// swipe's endpoints -- but on a rotated simulator with a valid landscape
// tree, rotatedDirectionSwipe now succeeds, so the swipes actually ran
// axis-correct and simply found no match. Revert scrollUntil's timeout block
// to `if angle := ...; angle != 0 { rotation_unavailable }` and this test
// fails: it reports rotation_unavailable=90 and tells the caller to
// hand-supply coordinates, which skills/mav/SKILL.md forbids because it
// compensates twice.
func TestScrollUntilTimeoutReportsDerivedWhenReDerivationSucceeded(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	landscapeTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`
	cli, _, out, _ := rotationCLI(t, udid, devicePreferencesDump, landscapeTree)
	err := cli.Run(context.Background(), []string{"ui", "scrollUntil", "--text", "NoSuchElementXYZ", "--max-swipes", "1"})
	if err == nil {
		t.Fatal("expected scroll_until_timeout")
	}
	got := out.String()
	if !strings.Contains(got, "rotation=90") || !strings.Contains(got, "direction_endpoints=derived") {
		t.Fatalf("a re-derived scrollUntil should report rotation=90 direction_endpoints=derived, got %q", got)
	}
	if strings.Contains(got, "rotation_unavailable") {
		t.Fatalf("a successfully re-derived scrollUntil should not report rotation_unavailable: %q", got)
	}
}

// Portrait is every headless run and must keep routing to axe, which is the
// canonical swipe driver and the faster one.
func TestUnrotatedSwipeStillRoutesToAxe(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	portraitTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, _, out, _ := rotationCLI(t, udid, devicePreferencesDump, portraitTree)
	if err := cli.Run(context.Background(), []string{"ui", "swipe",
		"--start-x", "100", "--start-y", "200", "--end-x", "300", "--end-y", "200"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "driver=axe") {
		t.Fatalf("an unrotated swipe left axe: %q", got)
	}
	if strings.Contains(got, "rotation") {
		t.Fatalf("an unrotated swipe reported a rotation: %q", got)
	}
}
