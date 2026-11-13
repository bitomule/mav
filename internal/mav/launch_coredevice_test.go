package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeviceRecipeUsesLifecycleWithoutRewritingLaunchArguments(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.BundleID = "com.example.app"
	cfg.Launch.Commands = LaunchCommands{
		AppPath: "make app-path",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
		Launch:  `TOKEN=synthetic-value xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID" -AppleLanguages "(es)" -Fixture "$MAV_BUNDLE_ID"`,
	}
	commands := effectiveLaunchCommands(cfg)
	if commands.Launch != cfg.Launch.Commands.Launch {
		t.Fatalf("launch tokens were rewritten: %s", commands.Launch)
	}
	if !shouldUseDriverInstall(cfg, commands) || !shouldUseDriverLaunch(cfg, commands) {
		t.Fatalf("device recipe did not select lifecycle: %#v", commands)
	}
	if strings.Contains(commands.Install, "idb") {
		t.Fatal("device recipe still requires idb")
	}
	args, err := launchArgsAfterBundle(commands.Launch, map[string]string{"MAV_BUNDLE_ID": cfg.BundleID})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-AppleLanguages", "(es)", "-Fixture", "com.example.app"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args=%#v want=%#v", args, want)
	}
}

func TestDeviceInstallEnvironmentKeepsShellExpansionWithDevicectl(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetKind = "device"
	cfg.Launch.Commands = LaunchCommands{Install: `TOOL_PATH=$MAV_APP_PATH xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`}
	commands := effectiveLaunchCommands(cfg)
	want := `TOOL_PATH=$MAV_APP_PATH xcrun devicectl device install app --device "$MAV_UDID" "$MAV_APP_PATH"`
	if commands.Install != want {
		t.Fatalf("install=%q want=%q", commands.Install, want)
	}
	if shouldUseDriverInstall(cfg, commands) {
		t.Fatal("installer environment was silently moved into app launch")
	}
}

func TestDeviceRecipeDefaultsUseLifecycleAndLegacyIDBDoesNotRequireBinary(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetKind = "device"
	cfg.BundleID = "com.example.app"
	if !shouldUseDriverLaunch(cfg, effectiveLaunchCommands(cfg)) {
		t.Fatal("empty recipe did not use configured app lifecycle")
	}
	cfg.Launch.Commands = LaunchCommands{
		Install: `idb install --udid "$MAV_UDID" "$MAV_APP_PATH"`,
		Launch:  `ENV=example idb launch --udid "$MAV_UDID" -f "$MAV_BUNDLE_ID" -Example 1`,
	}
	commands := effectiveLaunchCommands(cfg)
	if strings.Contains(commands.Install, "idb") || !shouldUseDriverInstall(cfg, commands) || !shouldUseDriverLaunch(cfg, commands) {
		t.Fatalf("legacy recipe still requires its tool: %#v", commands)
	}
}

type coreDeviceRecipeRunner struct {
	commands   []string
	launchArgs []string
	launchEnv  map[string]string
}

func (*coreDeviceRecipeRunner) LookPath(name string) (string, error) {
	if name == "xcrun" {
		return "/usr/bin/xcrun", nil
	}
	return "", errors.New("missing")
}
func (*coreDeviceRecipeRunner) Start(context.Context, string, string, ...string) (int, error) {
	return 0, errors.New("unexpected background process")
}
func (r *coreDeviceRecipeRunner) Run(_ context.Context, name string, args ...string) CommandResult {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	if name == "/bin/sh" {
		return CommandResult{Stdout: "/workspace/Example.app\n"}
	}
	if name != "xcrun" {
		return CommandResult{Err: errors.New("unexpected tool"), Code: 1}
	}
	if len(args) > 0 && args[0] == "--find" {
		return CommandResult{Stdout: "/Applications/Xcode.app/Contents/Developer/usr/bin/devicectl\n"}
	}
	result := `{"result":{}}`
	if len(args) > 3 && args[1] == "device" && args[2] == "process" && args[3] == "launch" {
		result = `{"result":{"process":{"processIdentifier":3412}}}`
		for i, arg := range args {
			if arg == "--environment-variables" {
				_ = json.Unmarshal([]byte(args[i+1]), &r.launchEnv)
			}
			if arg == "--" {
				r.launchArgs = append([]string{}, args[i+1:]...)
				break
			}
		}
	}
	for i, arg := range args {
		if arg == "--json-output" && i+1 < len(args) {
			if err := os.WriteFile(args[i+1], []byte(result), 0600); err != nil {
				return CommandResult{Err: err, Code: 1}
			}
			break
		}
	}
	return CommandResult{}
}

func TestRunDeviceLaunchRecipeUsesCoreDeviceAndRedactsEnvironmentEvidence(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.BundleID = "com.example.app"
	cfg.Launch.Commands = LaunchCommands{
		AppPath: "make app-path",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
		Launch:  `TOKEN=synthetic-value RUN=$MAV_BUNDLE_ID xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID" -Fixture "demo value"`,
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	runner := &coreDeviceRecipeRunner{}
	c := CLI{Root: root, Runner: runner, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	_, failed, result, _ := c.runLaunchRecipe(context.Background(), cfg, run, true, "")
	if failed != nil || result.Err != nil {
		t.Fatalf("step=%#v err=%v stderr=%s", failed, result.Err, result.Stderr)
	}
	if !reflect.DeepEqual(runner.launchArgs, []string{"-Fixture", "demo value"}) {
		t.Fatalf("app arguments=%#v", runner.launchArgs)
	}
	if !reflect.DeepEqual(runner.launchEnv, map[string]string{"TOKEN": "synthetic-value", "RUN": "com.example.app"}) {
		t.Fatal("app environment did not arrive intact")
	}
	joined := strings.Join(runner.commands, "\n")
	for _, operation := range []string{"devicectl device uninstall app", "devicectl device install app", "devicectl device process launch"} {
		if !strings.Contains(joined, operation) {
			t.Fatalf("missing %s", operation)
		}
	}
	if strings.Contains(joined, "idb ") || strings.Contains(joined, "simctl ") {
		t.Fatalf("old device tool executed: %s", joined)
	}
	trail, err := os.ReadFile(filepath.Join(run.Dir, "commands.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(trail), "launch.launch driver=coredevice env=RUN,TOKEN args=2") {
		t.Fatalf("missing lifecycle evidence: %s", trail)
	}
	if strings.Contains(string(trail), "synthetic-value") {
		t.Fatal("evidence exposed launch environment")
	}
}
