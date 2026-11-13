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
