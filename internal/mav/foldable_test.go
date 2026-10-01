package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const shadowQuery = "xcrun simctl spawn SIM notifyutil -g com.apple.coredevice.dtuhidd.active"

func foldableRoot(t *testing.T) (string, *sequenceRecordingRunner) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"xcrun": true, "baguette": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root, &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"baguette --version":                      "0.2.1\n",
			"baguette hinge --udid SIM --pose open":   `{"ok":true,"angleDegrees":130.0}`,
			"baguette hinge --udid SIM --angle 95":    `{"ok":true,"angleDegrees":95.0}`,
			"baguette hinge --udid SIM":               `{"ok":true,"angleDegrees":null}`,
			"baguette hinge --udid SIM --pose closed": `{"ok":true,"angleDegrees":0.0}`,
		},
	}
}

func runFoldable(t *testing.T, root string, runner *sequenceRecordingRunner, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), args))
	return out.String()
}

func TestSimHingeFoldsToAPoseThroughBaguette(t *testing.T) {
	root, runner := foldableRoot(t)
	out := runFoldable(t, root, runner, "sim", "hinge", "open")
	if !strings.Contains(out, "ok cmd=sim.hinge") || !strings.Contains(out, "angle=130") || !strings.Contains(out, "pose=open") {
		t.Fatalf("output=%q", out)
	}
	if !containsCall(runner.commands, "baguette hinge --udid SIM --pose open") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestSimHingeTakesAnAngle(t *testing.T) {
	root, runner := foldableRoot(t)
	out := runFoldable(t, root, runner, "sim", "hinge", "--angle", "95")
	if !strings.Contains(out, "angle=95") || !containsCall(runner.commands, "baguette hinge --udid SIM --angle 95") {
		t.Fatalf("output=%q commands=%v", out, runner.commands)
	}
}

// After a heal the guest stops publishing the hinge until it next moves, and
// baguette answers null. That is "unknown", not a 0° hinge.
func TestSimHingeReadsAnUnknownAngleAsUnknown(t *testing.T) {
	root, runner := foldableRoot(t)
	out := runFoldable(t, root, runner, "sim", "hinge")
	if !strings.Contains(out, "angle=unknown") {
		t.Fatalf("output=%q", out)
	}
}

func TestSimHingeRefusesWhatBaguetteWouldRefuseBeforeCallingIt(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"sim", "hinge", "half"}, "hinge_pose_invalid"},
		{[]string{"sim", "hinge", "--angle", "200"}, "hinge_angle_invalid"},
		{[]string{"sim", "hinge", "open", "--angle", "90"}, "hinge_pose_and_angle"},
	} {
		root, runner := foldableRoot(t)
		out := runFoldable(t, root, runner, tc.args...)
		if !strings.Contains(out, "fail code="+tc.code) {
			t.Fatalf("args=%v output=%q", tc.args, out)
		}
		if containsCall(runner.commands, "baguette hinge") {
			t.Fatalf("args=%v reached baguette: %v", tc.args, runner.commands)
		}
	}
}

func TestSimHealRestartsInputOnlyWhenDeviceHubShadowsIt(t *testing.T) {
	root, runner := foldableRoot(t)
	runner.out[shadowQuery] = "com.apple.coredevice.dtuhidd.active 0\n"
	out := runFoldable(t, root, runner, "sim", "heal")
	if !strings.Contains(out, "shadowed=false") || containsCall(runner.commands, "baguette heal") {
		t.Fatalf("an unshadowed simulator must not be healed: output=%q commands=%v", out, runner.commands)
	}

	root, runner = foldableRoot(t)
	runner.seq = map[string][]string{shadowQuery: {"com.apple.coredevice.dtuhidd.active 1\n", "com.apple.coredevice.dtuhidd.active 0\n"}}
	runner.calls = map[string]int{}
	out = runFoldable(t, root, runner, "sim", "heal")
	if !containsCall(runner.commands, "baguette heal --udid SIM") {
		t.Fatalf("a shadowed simulator was not healed: %v", runner.commands)
	}
	if !strings.Contains(out, "healed=true") || !strings.Contains(out, "shadowed=true") || !strings.Contains(out, "after=false") {
		t.Fatalf("output=%q", out)
	}
}

func TestHingeAndHealAreFlowSteps(t *testing.T) {
	root, runner := foldableRoot(t)
	runner.out[shadowQuery] = "com.apple.coredevice.dtuhidd.active 1\n"
	flow := "name: duo\nsteps:\n" +
		"  - sim.heal: {}\n" +
		"  - sim.hinge: { pose: open }\n" +
		"  - sim.hinge: { angle: \"95\" }\n" +
		"  - sim.hinge: { pose: closed }\n"
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runFoldable(t, root, runner, "run", flowPath)
	if !strings.Contains(out, "ok cmd=run") {
		t.Fatalf("output=%q", out)
	}
	for _, want := range []string{
		"baguette heal --udid SIM",
		"baguette hinge --udid SIM --pose open",
		"baguette hinge --udid SIM --angle 95",
		"baguette hinge --udid SIM --pose closed",
	} {
		if !containsCall(runner.commands, want) {
			t.Fatalf("missing %q in %v", want, runner.commands)
		}
	}
}

func TestFlowLintRefusesAHingeStepTheCommandWouldRefuse(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	for _, tc := range []struct {
		params map[string]string
		bad    bool
	}{
		{map[string]string{"pose": "open"}, false},
		{map[string]string{"angle": "95", "duration": "1.2"}, false},
		{map[string]string{"pose": "${params.pose}"}, false},
		{map[string]string{"pose": "half"}, true},
		{map[string]string{"angle": "181"}, true},
		{map[string]string{}, true},
	} {
		issues := lintFlowStep(0, FlowStep{Action: "sim.hinge", Params: tc.params}, cfg)
		got := false
		for _, issue := range issues {
			if issue.Code == "hinge_invalid" {
				got = true
			}
		}
		if got != tc.bad {
			t.Errorf("params=%v: hinge_invalid=%v, want %v (issues=%v)", tc.params, got, tc.bad, issues)
		}
	}
}
