package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
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

// Measured on iPhone Duo / iOS 27.1 open flat: idb 1.6.4 navigated 2 of 2,
// AXe and baguette 0 of 4 while reporting success.
func TestAnOpenFoldableTapsThroughIdb(t *testing.T) {
	for _, tc := range []struct {
		declared *float64
		want     string
	}{
		{nil, "axe tap --tap-style physical --udid SIM -x 160 -y 288"},
		{ptr(180.0), "idb ui tap 160 288 --udid SIM"},
		{ptr(0.0), "axe tap --tap-style physical --udid SIM -x 160 -y 288"},
	} {
		root, runner := foldableRoot(t)
		runner.tools["axe"] = true
		runner.tools["idb"] = true
		if tc.declared != nil {
			writeDeclaredHinge(root, "SIM", *tc.declared)
		}
		runFoldable(t, root, runner, "ui", "tap", "--x", "160", "--y", "288")
		if !containsCall(runner.commands, tc.want) {
			t.Errorf("declared=%v: missing %q in %v", tc.declared, tc.want, runner.commands)
		}
	}
}

func ptr(v float64) *float64 { return &v }

const accessibilityRowTree = `[{"AXLabel":"Ajustes","type":"Application","AXFrame":"{{0, 0}, {669, 951}}","frame":{"x":0,"y":0,"width":669,"height":951},"children":[` +
	`{"AXLabel":"Accesibilidad","type":"Button","role":"AXButton","AXFrame":"{{20, 270}, {280, 36}}","frame":{"x":20,"y":270,"width":280,"height":36}}]}]`

// A tap by text on an open Duo used to go to `axe tap --label`, which lands on
// the dark cover: measured 0 of 2. It now reads the element from the tree and
// taps its centre through idb.
func TestAnOpenFoldableTapsATextThroughIdbAtTheElementsCentre(t *testing.T) {
	root, runner := foldableRoot(t)
	runner.tools["axe"] = true
	runner.tools["idb"] = true
	runner.out["axe describe-ui --udid SIM"] = accessibilityRowTree
	writeDeclaredHinge(root, "SIM", 180)
	out := runFoldable(t, root, runner, "ui", "tap", "--text", "Accesibilidad")
	if !containsCall(runner.commands, "idb ui tap 160 288 --udid SIM") {
		t.Fatalf("output=%q commands=%v", out, runner.commands)
	}
	if containsCall(runner.commands, "axe tap --tap-style physical --udid SIM --label") {
		t.Fatalf("the label tap that lands on the cover was still sent: %v", runner.commands)
	}
}

func treeWith(children ...string) string {
	return `[{"AXLabel":"Ajustes","type":"Application","AXFrame":"{{0, 0}, {669, 951}}","children":[` + strings.Join(children, ",") + `]}]`
}

func element(label, role string, x, y, w, h int) string {
	return `{"AXLabel":"` + label + `","type":"` + role + `","role":"AX` + role + `","AXFrame":"{{` +
		strconv.Itoa(x) + `, ` + strconv.Itoa(y) + `}, {` + strconv.Itoa(w) + `, ` + strconv.Itoa(h) + `}}"}`
}

// Settings' sidebar row matches "Accesibilidad" twice: the button and its own
// label inside it. That is one control; measured on an open Duo the strict
// resolver refused it as ambiguous and the tap never went out.
func TestAnOpenFoldableResolvesALabelInsideItsOwnButton(t *testing.T) {
	root, runner := foldableRoot(t)
	runner.tools["axe"] = true
	runner.tools["idb"] = true
	runner.out["axe describe-ui --udid SIM"] = treeWith(
		element("Accesibilidad", "Button", 20, 270, 280, 36),
		element("Accesibilidad", "StaticText", 60, 278, 120, 20),
	)
	writeDeclaredHinge(root, "SIM", 180)
	out := runFoldable(t, root, runner, "ui", "tap", "--text", "Accesibilidad")
	if !containsCall(runner.commands, "idb ui tap 160 288 --udid SIM") {
		t.Fatalf("output=%q commands=%v", out, runner.commands)
	}
}

func TestAnOpenFoldableStillRefusesTwoSeparateMatches(t *testing.T) {
	root, runner := foldableRoot(t)
	runner.tools["axe"] = true
	runner.tools["idb"] = true
	runner.out["axe describe-ui --udid SIM"] = treeWith(
		element("Accesibilidad", "Button", 20, 270, 280, 36),
		element("Accesibilidad", "Button", 360, 600, 280, 36),
	)
	writeDeclaredHinge(root, "SIM", 180)
	out := runFoldable(t, root, runner, "ui", "tap", "--text", "Accesibilidad")
	if !strings.Contains(out, "selector_ambiguous") || containsCall(runner.commands, "idb ui tap") {
		t.Fatalf("two separate rows must stay ambiguous: output=%q commands=%v", out, runner.commands)
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

// One command has to leave a fresh Mac, and one with 2022's idb, on the
// versions the probes demand: Homebrew 7 refuses third-party taps until they
// are trusted, `brew install` upgrades an outdated formula, and pipx needs
// --force to replace an installed fb-idb.
func TestSetupInstallDepsTrustsEachTapAndUpgrades(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, DefaultConfig(root)); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: map[string]bool{"brew": true, "pipx": true, "python3.12": true}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"setup", "--install", "deps"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"brew trust --tap cameroncooke/axe",
		"brew install cameroncooke/axe/axe",
		"pipx install --force --python python3.12 fb-idb",
		"brew trust --tap facebook/fb",
		"brew install facebook/fb/idb-companion",
		"brew trust --tap tddworks/tap",
		"brew install tddworks/tap/baguette",
	}
	if strings.Join(runner.commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(runner.commands, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(out.String(), "installed=axe,idb,baguette") {
		t.Fatalf("output=%q", out.String())
	}
}
