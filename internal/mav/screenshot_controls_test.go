package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func screenshotControlsSimRoot(t *testing.T) (string, *sequenceRecordingRunner) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root, &sequenceRecordingRunner{tools: cfg.Tools}
}

func screenshotControlsDeviceRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.BundleID = "com.example.app"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSimAppearanceRunsSimctlUI(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "appearance", "dark"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=sim.appearance") || !strings.Contains(out.String(), "appearance=dark") {
		t.Fatalf("output=%q", out.String())
	}
	if !containsCall(runner.commands, "xcrun simctl ui SIM appearance dark") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestSimAppearanceRejectsUnknownValue(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"sim", "appearance", "sepia"}))
	if !strings.Contains(out.String(), "fail code=appearance_invalid") {
		t.Fatalf("output=%q", out.String())
	}
	if containsCall(runner.commands, "simctl ui") {
		t.Fatalf("an invalid appearance must not reach simctl: %v", runner.commands)
	}
}

func TestSimAppearanceOnDeviceFailsWithStructuredError(t *testing.T) {
	root := screenshotControlsDeviceRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{tools: map[string]bool{"idb": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"sim", "appearance", "dark"}))
	if !strings.Contains(out.String(), "fail code=appearance_unsupported_on_device") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestSimStatusBarPresetIsTheAppStoreLook(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "statusbar", "set", "--preset", "appstore"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=sim.statusbar.set") {
		t.Fatalf("output=%q", out.String())
	}
	want := "xcrun simctl status_bar SIM override --time 9:41 --dataNetwork wifi --wifiMode active --wifiBars 3 --cellularMode active --cellularBars 4 --batteryState charged --batteryLevel 100"
	if !containsCall(runner.commands, want) {
		t.Fatalf("commands=%v", runner.commands)
	}
}

// TestSimStatusBarFlagsOverridePreset keeps the preset a starting point rather
// than a lock: the individual fields have to stay settable on top of it, or
// the only shot anyone can take is Apple's exact one.
func TestSimStatusBarFlagsOverridePreset(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "statusbar", "set", "--preset", "appstore", "--time", "10:00", "--battery-level", "80"}); err != nil {
		t.Fatal(err)
	}
	want := "xcrun simctl status_bar SIM override --time 10:00 --dataNetwork wifi --wifiMode active --wifiBars 3 --cellularMode active --cellularBars 4 --batteryState charged --batteryLevel 80"
	if !containsCall(runner.commands, want) {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestSimStatusBarSetsIndividualFieldsWithoutPreset(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "statusbar", "set", "--time", "9:41"}); err != nil {
		t.Fatal(err)
	}
	if !containsCall(runner.commands, "xcrun simctl status_bar SIM override --time 9:41") {
		t.Fatalf("commands=%v", runner.commands)
	}
	if containsCall(runner.commands, "--batteryLevel") {
		t.Fatalf("a bare --time must not drag the rest of the preset in: %v", runner.commands)
	}
}

func TestSimStatusBarRejectsOutOfRangeAndUnknownValues(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		code string
	}{
		{name: "bars out of range", args: []string{"sim", "statusbar", "set", "--cellular-bars", "9"}, code: "status_bar_value_invalid"},
		{name: "battery level not a number", args: []string{"sim", "statusbar", "set", "--battery-level", "full"}, code: "status_bar_value_invalid"},
		{name: "unknown battery state", args: []string{"sim", "statusbar", "set", "--battery-state", "melting"}, code: "status_bar_value_invalid"},
		{name: "unknown preset", args: []string{"sim", "statusbar", "set", "--preset", "instagram"}, code: "status_bar_preset_invalid"},
		{name: "no fields at all", args: []string{"sim", "statusbar", "set"}, code: "status_bar_fields_missing"},
		{name: "unknown subcommand", args: []string{"sim", "statusbar", "reset"}, code: "status_bar_unknown_command"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root, runner := screenshotControlsSimRoot(t)
			var out bytes.Buffer
			cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
			allowFail(t, cli.Run(context.Background(), testCase.args))
			if !strings.Contains(out.String(), "fail code="+testCase.code) {
				t.Fatalf("output=%q", out.String())
			}
			if containsCall(runner.commands, "simctl status_bar") {
				t.Fatalf("a rejected override must not reach simctl: %v", runner.commands)
			}
		})
	}
}

func TestSimStatusBarClearRunsSimctlClear(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "statusbar", "clear"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=sim.statusbar.clear") {
		t.Fatalf("output=%q", out.String())
	}
	if !containsCall(runner.commands, "xcrun simctl status_bar SIM clear") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestSimStatusBarOnDeviceFailsWithStructuredError(t *testing.T) {
	for _, args := range [][]string{
		{"sim", "statusbar", "set", "--preset", "appstore"},
		{"sim", "statusbar", "clear"},
	} {
		root := screenshotControlsDeviceRoot(t)
		var out bytes.Buffer
		cli := CLI{Runner: &sequenceRecordingRunner{tools: map[string]bool{"idb": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		allowFail(t, cli.Run(context.Background(), args))
		if !strings.Contains(out.String(), "fail code=status_bar_unsupported_on_device") {
			t.Fatalf("args=%v output=%q", args, out.String())
		}
	}
}

func TestScreenshotControlsOnMacOSFailWithTheirOwnCode(t *testing.T) {
	for _, testCase := range []struct {
		args []string
		code string
	}{
		{args: []string{"sim", "appearance", "dark"}, code: "appearance_unsupported_on_macos"},
		{args: []string{"sim", "statusbar", "clear"}, code: "status_bar_unsupported_on_macos"},
	} {
		root := t.TempDir()
		cfg := DefaultConfig(root)
		cfg.TargetKind = "macos"
		cfg.BundleID = "com.example.app"
		if err := SaveConfig(root, cfg); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		cli := CLI{Runner: &sequenceRecordingRunner{tools: map[string]bool{}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		allowFail(t, cli.Run(context.Background(), testCase.args))
		if !strings.Contains(out.String(), "fail code="+testCase.code) {
			t.Fatalf("args=%v output=%q", testCase.args, out.String())
		}
	}
}

func TestScreenshotControlFlowActionsReachSimctl(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	flow := "name: screenshots\nsteps:\n" +
		"  - sim.appearance: { appearance: dark }\n" +
		"  - sim.statusbar.set: { preset: appstore, time: \"9:41\" }\n" +
		"  - sim.statusbar.clear: {}\n"
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=run") {
		t.Fatalf("output=%q", out.String())
	}
	for _, want := range []string{
		"xcrun simctl ui SIM appearance dark",
		"xcrun simctl status_bar SIM override --time 9:41",
		"xcrun simctl status_bar SIM clear",
	} {
		if !containsCall(runner.commands, want) {
			t.Fatalf("missing %q in %v", want, runner.commands)
		}
	}
}

// TestScreenshotControlFlowActionsAreAccepted guards the half-wired case: an
// action the executor understands but the flow parser rejects is a flow that
// dies before its first step. The params themselves are covered by the lint
// tests below.
func TestScreenshotControlFlowActionsAreAccepted(t *testing.T) {
	for _, action := range []string{"sim.appearance", "sim.statusbar.set", "sim.statusbar.clear"} {
		if !isSupportedFlowAction(action) {
			t.Fatalf("%s is executable but not accepted as a supported action", action)
		}
	}
}

// TestStatusBarStepDoesNotOverwriteTheEvidenceTimestamp pins the collision the
// review found: `time` is both a status bar field and the reserved key of a
// commands.jsonl record, and the step fields used to win. The evidence bundle
// ships that file verbatim, so a screenshot run stamped 9:41 as the wall clock.
func TestStatusBarStepDoesNotOverwriteTheEvidenceTimestamp(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	flowPath := filepath.Join(root, "flow.yaml")
	flow := "name: shots\nsteps:\n  - sim.statusbar.set: { time: \"9:41\" }\n"
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatal(err)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(run.Commands)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &record); err != nil {
		t.Fatalf("commands.jsonl=%q err=%v", data, err)
	}
	if record["time"] == "9:41" {
		t.Fatalf("the step param overwrote the record timestamp: %v", record)
	}
	if _, parseErr := time.Parse(time.RFC3339, record["time"].(string)); parseErr != nil {
		t.Fatalf("time=%v is not a timestamp: %v", record["time"], parseErr)
	}
	if record["action"] != "sim.statusbar.set" {
		t.Fatalf("the record lost its own action key: %v", record)
	}
	// Reserving the key must not cost the value: the clock that was actually
	// set is still in the record, under a name the record does not own.
	if record["statusBarTime"] != "9:41" {
		t.Fatalf("the overridden clock is missing from the record: %v", record)
	}
}

// TestFlowStepMapLiteralsNeverUseAReservedRecordKey generalises past the two
// instances of the bug: commands.jsonl owns time/step/action/status/elapsed and
// the failure record adds code, and a step field named after one of those
// either loses its own value or replaces the record's.
//
// It sees only hardcoded map literals in the executor's switch, which is why the
// name says so. It does NOT see `fields["x"] = ...` assignments, the ~30
// `copyParams(step.Params)` returns, or maps built in helpers like
// captureEvidenceStep and execFlowShell. Those shapes stay a reading job; this
// catches the shape both real instances took, and it t.Fatals rather than
// quietly passing if the function it scans is renamed or moved.
func TestFlowStepMapLiteralsNeverUseAReservedRecordKey(t *testing.T) {
	// "run" is deliberately absent: many steps return it, always as the same
	// run.ID the record itself writes, so the two agree by construction.
	reserved := map[string]bool{"time": true, "step": true, "action": true, "status": true, "elapsed": true, "code": true}
	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	executor := string(source)
	start := strings.Index(executor, "func (c CLI) executeFlowStepWithOptions(")
	if start < 0 {
		t.Fatal("executeFlowStepWithOptions not found")
	}
	end := strings.Index(executor[start:], "\nfunc (c CLI) executeWhenFlowStep(")
	if end < 0 {
		t.Fatal("end of executeFlowStepWithOptions not found")
	}
	literal := regexp.MustCompile(`map\[string\]string\{([^}]*)\}`)
	key := regexp.MustCompile(`"([^"]+)":`)
	for _, match := range literal.FindAllStringSubmatch(executor[start:start+end], -1) {
		for _, found := range key.FindAllStringSubmatch(match[1], -1) {
			if reserved[found[1]] {
				t.Errorf("flow step field %q collides with a reserved commands.jsonl key: %s", found[1], match[0])
			}
		}
	}
}

func TestSimStatusBarRejectsAFlagUsedAsAValue(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"sim", "statusbar", "set", "--time", "--preset", "appstore"}))
	if !strings.Contains(out.String(), "fail code=status_bar_value_missing") {
		t.Fatalf("output=%q", out.String())
	}
	if containsCall(runner.commands, "simctl status_bar") {
		t.Fatalf("a flag swallowed as a value must not reach simctl: %v", runner.commands)
	}
}

// TestSimStatusBarValidatesBeforeRouting keeps the answer about the value the
// user got wrong, not about the toolchain: routing first made a machine with
// no working xcrun report status_bar_unsupported for --wifi-bars 9.
func TestSimStatusBarValidatesBeforeRouting(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{tools: map[string]bool{}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"sim", "statusbar", "set", "--wifi-bars", "9"}))
	if !strings.Contains(out.String(), "fail code=status_bar_value_invalid") {
		t.Fatalf("output=%q", out.String())
	}
}

// TestSimAppearanceWaitsForTheRepaint pins the settle the live run found: the
// capture path MAV prefers on a simulator serves the pre-switch frame, so
// returning as soon as simctl accepts the style produced a light screenshot
// from a dark step -- with nothing in the output saying so.
func TestSimAppearanceWaitsForTheRepaint(t *testing.T) {
	root, runner := screenshotControlsSimRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	start := time.Now()
	if err := cli.Run(context.Background(), []string{"sim", "appearance", "dark"}); err != nil {
		t.Fatal(err)
	}
	// Against a literal, not against appearanceSettle: comparing the wait to
	// its own constant passes just as happily when the constant is zeroed.
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("returned after %s, before the screen can have repainted", elapsed)
	}
}

func TestScreenshotControlStepsPassLintWhenValid(t *testing.T) {
	root, _ := screenshotControlsSimRoot(t)
	flowPath := filepath.Join(root, "flow.yaml")
	flow := "name: shots\nsteps:\n" +
		"  - sim.appearance: { appearance: dark }\n" +
		"  - sim.statusbar.set: { preset: appstore }\n" +
		"  - sim.statusbar.set: { time: \"9:41\", batteryState: charged, batteryLevel: \"100\", cellularBars: \"4\", wifiBars: \"3\" }\n" +
		"  - sim.statusbar.clear: {}\n"
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"flow", "lint", flowPath}); err != nil {
		t.Fatalf("lint output=%q err=%v", out.String(), err)
	}
	if !strings.Contains(out.String(), "ok cmd=flow.lint") || !strings.Contains(out.String(), "errors=0") || !strings.Contains(out.String(), "warnings=0") {
		t.Fatalf("lint output=%q", out.String())
	}
}

// TestScreenshotControlStepsLintBindingsAsUnknown covers the flow the lint
// exists for: a matrix parameterised on light/dark. The value is resolved at
// run time, so its literal spelling is not something lint may judge -- and a
// step whose only field is a binding is not a step with no fields.
func TestScreenshotControlStepsLintBindingsAsUnknown(t *testing.T) {
	root, _ := screenshotControlsSimRoot(t)
	flowPath := filepath.Join(root, "flow.yaml")
	flow := "name: shots\nparams:\n  theme: { required: true }\n  bars: { required: true }\nsteps:\n" +
		"  - sim.appearance: { appearance: \"${params.theme}\" }\n" +
		"  - sim.statusbar.set: { wifiBars: \"${params.bars}\" }\n" +
		"  - sim.statusbar.set: { preset: appstore, time: \"${params.clock}\" }\n"
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"flow", "lint", "--raw", flowPath}); err != nil {
		t.Fatalf("lint output=%q err=%v", out.String(), err)
	}
	if !strings.Contains(out.String(), "ok cmd=flow.lint") || !strings.Contains(out.String(), "errors=0") {
		t.Fatalf("lint output=%q", out.String())
	}
}

// TestScreenshotControlStepsFailLintOnBadArguments covers the values simctl
// would otherwise reject mid-run, halfway through a capture matrix. The
// message has to name the YAML key, not the CLI flag the step is translated
// into.
func TestScreenshotControlStepsFailLintOnBadArguments(t *testing.T) {
	for _, testCase := range []struct {
		name string
		step string
		code string
		want string
	}{
		{name: "appearance value", step: "  - sim.appearance: { appearance: sepia }\n", code: "appearance_invalid", want: "light|dark"},
		{name: "appearance missing", step: "  - sim.appearance: {}\n", code: "appearance_invalid", want: "light|dark"},
		{name: "preset", step: "  - sim.statusbar.set: { preset: marketing }\n", code: "status_bar_preset_invalid", want: "appstore"},
		{name: "enum field", step: "  - sim.statusbar.set: { batteryState: full }\n", code: "status_bar_value_invalid", want: "batteryState"},
		{name: "range field", step: "  - sim.statusbar.set: { wifiBars: \"9\" }\n", code: "status_bar_value_invalid", want: "wifiBars"},
		{name: "no fields", step: "  - sim.statusbar.set: {}\n", code: "status_bar_fields_missing", want: "at least one"},
	} {
		root, _ := screenshotControlsSimRoot(t)
		flowPath := filepath.Join(root, "flow.yaml")
		if err := os.WriteFile(flowPath, []byte("name: shots\nsteps:\n"+testCase.step), 0o644); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		allowFail(t, cli.Run(context.Background(), []string{"flow", "lint", "--raw", flowPath}))
		got := out.String()
		if !strings.Contains(got, "fail code=flow_lint_failed") || !strings.Contains(got, "errors=1") {
			t.Fatalf("%s: lint output=%q", testCase.name, got)
		}
		if !strings.Contains(got, "code="+testCase.code) || !strings.Contains(got, testCase.want) {
			t.Fatalf("%s: lint output=%q", testCase.name, got)
		}
	}
}

// TestStatusBarClearWithFieldsIsAWarning pins the silent case: clear takes no
// fields, so a step that carries them was written expecting an override and
// gets a reset instead.
func TestStatusBarClearWithFieldsIsAWarning(t *testing.T) {
	root, _ := screenshotControlsSimRoot(t)
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("name: shots\nsteps:\n  - sim.statusbar.clear: { preset: appstore, time: \"9:41\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"flow", "lint", "--raw", flowPath}); err != nil {
		t.Fatalf("lint output=%q err=%v", out.String(), err)
	}
	got := out.String()
	if !strings.Contains(got, "warnings=1") || !strings.Contains(got, "errors=0") {
		t.Fatalf("lint output=%q", got)
	}
	if !strings.Contains(got, "status_bar_clear_params_ignored") || !strings.Contains(got, "preset, time") {
		t.Fatalf("lint output=%q", got)
	}
}
