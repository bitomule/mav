package mav

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AXe 2 consumes points in the active display's current orientation. Old
// portrait transforms and idb cover-panel workarounds must never touch them.
func TestAXe2GesturesKeepCurrentOrientationCoordinates(t *testing.T) {
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"tap", []string{"ui", "tap", "--x", "249", "--y", "202"}, "tap --tap-style physical -x 249 -y 202"},
		{"long press", []string{"ui", "longPress", "--x", "249", "--y", "202", "--duration", "800ms"}, "tap --tap-style physical -x 249 -y 202 --hold 0.8"},
		{"double tap", []string{"ui", "doubleTap", "--x", "249", "--y", "202"}, "tap --tap-style physical -x 249 -y 202 --count 2"},
		{"swipe", []string{"ui", "swipe", "--start-x", "249", "--start-y", "202", "--end-x", "330", "--end-y", "100"}, "swipe --start-x 249 --start-y 202 --end-x 330 --end-y 100"},
		{"drag", []string{"ui", "drag", "--start-x", "249", "--start-y", "202", "--end-x", "330", "--end-y", "100"}, "drag --start-x 249 --start-y 202 --end-x 330 --end-y 100"},
		{"pinch", []string{"ui", "pinch", "--x", "249", "--y", "202", "--scale", "2"}, "gesture pinch -x 249 -y 202"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli, runner, out, root := rotationCLI(t, udid, devicePreferencesDump, `[{"AXLabel":"Landscape","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`)
			// A stale declaration and screen cache used to change coordinates.
			if err := writeDeclaredOrientation(root, udid, declaredOrientation{Value: orientationLandscapeRight, Rotation: 90}); err != nil {
				t.Fatal(err)
			}
			writeDeclaredHinge(root, udid, 180)
			if err := cli.Run(context.Background(), tc.args); err != nil {
				t.Fatalf("err=%v output=%s commands=%v", err, out, runner.commands)
			}
			joined := strings.Join(runner.commands, "\n")
			if !strings.Contains(joined, "axe --output json "+tc.want) {
				t.Fatalf("coordinates changed: %s", joined)
			}
			for _, forbidden := range []string{"defaults read", "idb ", "baguette ", "describe-ui", "simctl io", "hid_x=", "rotation=90"} {
				if strings.Contains(joined, forbidden) || strings.Contains(out.String(), forbidden) {
					t.Fatalf("legacy transform/probe used (%s): output=%s commands=%s", forbidden, out, joined)
				}
			}
		})
	}
}

func TestAXe2CaptureUsesTheActiveFoldableDisplayWithoutPanelProbes(t *testing.T) {
	root, runner := foldableRoot(t)
	runner.tools["axe"] = true
	runner.tools["idb"] = true
	writeDeclaredHinge(root, "SIM", 180)
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "screen.png")
	result, err := (CLI{Root: root, Runner: runner}).captureScreenshot(context.Background(), cfg, path)
	if err != nil || result.Err != nil {
		t.Fatalf("err=%v result=%#v", err, result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.commands, "\n")
	if !strings.Contains(joined, "axe --output json screenshot --output "+path) {
		t.Fatalf("capture was not routed to AXe: %s", joined)
	}
	for _, forbidden := range []string{"simctl io", "idb screenshot", "baguette screenshot", "describe-ui", "defaults read"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("old panel workaround used: %s", joined)
		}
	}
}

func TestAXe2FoldableSemanticTapUsesTheActiveDisplayWithoutCoordinateFallback(t *testing.T) {
	for _, pose := range []float64{0, 130, 180} {
		root, runner := foldableRoot(t)
		runner.tools["axe"] = true
		runner.tools["idb"] = true
		writeDeclaredHinge(root, "SIM", pose)
		out := runFoldable(t, root, runner, "ui", "tap", "--text", "Accesibilidad")
		if !strings.Contains(out, "driver=axe") || !containsCall(runner.commands, "axe --output json tap --tap-style physical --label Accesibilidad") {
			t.Fatalf("pose=%v output=%s commands=%v", pose, out, runner.commands)
		}
		for _, command := range runner.commands {
			if strings.Contains(command, "idb ") || strings.Contains(command, "describe-ui") {
				t.Fatalf("old cover workaround executed: %s", command)
			}
		}
	}
}
