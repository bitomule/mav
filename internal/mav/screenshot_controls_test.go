package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if !strings.Contains(out.String(), "ok cmd=sim.statusBar.set") {
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
	if !strings.Contains(out.String(), "ok cmd=sim.statusBar.clear") {
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
		"  - sim.statusBar.set: { preset: appstore, time: \"9:41\" }\n" +
		"  - sim.statusBar.clear: {}\n"
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

// TestScreenshotControlFlowActionsAreLinted guards the half-wired case: a step
// the executor understands but the linter does not is a flow that fails only
// once it is already running.
func TestScreenshotControlFlowActionsAreLinted(t *testing.T) {
	for _, action := range []string{"sim.appearance", "sim.statusBar.set", "sim.statusBar.clear"} {
		if !isSupportedFlowAction(action) {
			t.Fatalf("%s is executable but not linted as a supported action", action)
		}
	}
}
