package mav

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type fakeRunner struct {
	tools   map[string]bool
	runs    []string
	out     map[string]string
	seq     map[string][]string
	calls   map[string]int
	results map[string]CommandResult
}

func (f fakeRunner) LookPath(file string) (string, error) {
	if f.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (f fakeRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	if name == "/usr/bin/avconvert" {
		var source, out string
		for i := 0; i < len(args)-1; i++ {
			switch args[i] {
			case "-s":
				source = args[i+1]
			case "-o":
				out = args[i+1]
			}
		}
		if out != "" {
			_ = os.MkdirAll(filepath.Dir(out), 0o755)
			data := testMovieWithDuration(600, 1200)
			if source != "" {
				if sourceData, err := os.ReadFile(source); err == nil {
					data = sourceData
				}
			}
			_ = os.WriteFile(out, data, 0o644)
		}
		return CommandResult{}
	}
	key := name
	for _, arg := range args {
		key += " " + arg
	}
	if result, ok := f.results[key]; ok {
		if f.calls != nil {
			f.calls[key]++
		}
		return result
	}
	if values := f.seq[key]; len(values) > 0 {
		index := 0
		if f.calls != nil {
			index = f.calls[key]
			if index >= len(values) {
				index = len(values) - 1
			}
			f.calls[key]++
		}
		return CommandResult{Stdout: values[index]}
	}
	return CommandResult{Stdout: f.out[key]}
}

func (f fakeRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	for i, arg := range args {
		if arg == "recordVideo" && i+1 < len(args) {
			path := args[len(args)-1]
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			_ = os.WriteFile(path, testMovieWithDuration(600, 1200), 0o644)
		}
	}
	return 123, nil
}

func TestSaveConfigRoundTripsIncludingExplicitEmptyOverride(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.ProjectName = "nokoru"
	cfg.AppTarget = "//App:NokoruiOS"
	cfg.BundleID = "com.davidcollado.nokoru.debug"
	cfg.ProcessName = "nNokoru"
	cfg.SimulatorName = "iPhone 17 Pro"
	cfg.TargetCommand = `simpool lease --device "iPhone 17 Pro" --os 26.3`
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Build:   "bazelisk build '//App:NokoruiOS'",
		AppPath: "./scripts/mav-app-path.sh",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	back, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, want, got string }{
		{"project_name", cfg.ProjectName, back.ProjectName},
		{"app_target", cfg.AppTarget, back.AppTarget},
		{"bundle_id", cfg.BundleID, back.BundleID},
		{"process_name", cfg.ProcessName, back.ProcessName},
		{"target_command", cfg.TargetCommand, back.TargetCommand},
		{"launch.build", cfg.Launch.Commands.Build, back.Launch.Commands.Build},
		{"launch.app_path", cfg.Launch.Commands.AppPath, back.Launch.Commands.AppPath},
		{"launch.install", cfg.Launch.Commands.Install, back.Launch.Commands.Install},
	} {
		if c.want != c.got {
			t.Fatalf("%s: round-trip lost the value: want %q got %q", c.name, c.want, c.got)
		}
	}
	// The previous writer omitted empty values, so a command that was never
	// configured must not reappear as an empty string in the file: a real
	// repo's config gets read by hand and the noise costs.
	data, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "cleanup:") || strings.Contains(string(data), "healthcheck:") {
		t.Fatalf("unconfigured commands must not be written:\n%s", data)
	}
}

func TestSetupConfigFindsBazelApp(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "MODULE.bazel"), "module(name = \"Demo\")\n")
	mustWrite(t, filepath.Join(root, "Demo", "BUILD.bazel"), `load("@build_bazel_rules_apple//apple:ios.bzl", "ios_application")
ios_application(
    name = "DemoApp",
    bundle_id = "com.example.demo",
)
`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true, "xcrun": true, "axe": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppTarget != "//Demo:DemoApp" {
		t.Fatalf("target=%q", cfg.AppTarget)
	}
	if cfg.BundleID != "com.example.demo" {
		t.Fatalf("bundle=%q", cfg.BundleID)
	}
	if cfg.PreferredUIDriver != "axe" {
		t.Fatalf("driver=%q", cfg.PreferredUIDriver)
	}
}

func TestSetupConfigSkipsHiddenWorktrees(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "worktrees", "bad", "Undolly", "BUILD.bazel"), `ios_application(name = "Clean similar photos with Undolly", bundle_id = "bad")`)
	mustWrite(t, filepath.Join(root, "Undolly", "BUILD.bazel"), `ios_application(name = "UndollyApp", bundle_id = "com.example.release")`)
	mustWrite(t, filepath.Join(root, "tools", "shared.bzl"), `app_info = struct(bundle_id_debug = "com.example.debug", bundle_id = "com.example.release", executable_name = "Undolly")`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppTarget != "//Undolly:UndollyApp" {
		t.Fatalf("target=%q", cfg.AppTarget)
	}
	if cfg.BundleID != "com.example.debug" {
		t.Fatalf("bundle=%q", cfg.BundleID)
	}
	if cfg.ProcessName != "Undolly" {
		t.Fatalf("process=%q", cfg.ProcessName)
	}
}

func TestSetupConfigOnlyUsesIOSApplicationRuleNames(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `load("@build_bazel_rules_apple//apple:ios.bzl", "ios_application")
swift_macro(
    name = "DIMacros",
)
ios_application(
    name = "DemoApp",
    bundle_id = "com.example.demo",
)
`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppTarget != "//:DemoApp" {
		t.Fatalf("target=%q", cfg.AppTarget)
	}
}

func TestSetupConfigPrefersProjectLaunchScripts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "Makefile"), "mav-build:\n\ttrue\nmav-app-path:\n\tprintf /tmp/App.app\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Launch.Commands.Build != "make mav-build" || cfg.Launch.Commands.AppPath != "make mav-app-path" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigDoesNotUsePartialMakefileRecipe(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "Makefile"), "build-ios:\n\ttrue\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Launch.Commands.Build, "make") || cfg.Launch.Commands.AppPath == "" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigUsesExplicitMAVScripts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "scripts", "mav-build"), "#!/bin/sh\ntrue\n")
	mustWrite(t, filepath.Join(root, "scripts", "mav-app-path"), "#!/bin/sh\nprintf /tmp/App.app\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Launch.Commands.Build != "./scripts/mav-build" || cfg.Launch.Commands.AppPath != "./scripts/mav-app-path" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigIgnoresDotMAVLaunchScripts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "Makefile"), "mav-build:\n\ttrue\nmav-app-path:\n\tprintf /tmp/App.app\n")
	mustWrite(t, filepath.Join(root, ".mav", "mav-build.sh"), "#!/bin/sh\ntrue\n")
	mustWrite(t, filepath.Join(root, ".mav", "mav-app-path.sh"), "#!/bin/sh\nprintf /tmp/App.app\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Launch.Commands.Build != "make mav-build" || cfg.Launch.Commands.AppPath != "make mav-app-path" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigUsesExplicitJustMAVRecipes(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "justfile"), "mav-build:\n  true\nmav-app-path:\n  printf /tmp/App.app\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Launch.Commands.Build != "just mav-build" || cfg.Launch.Commands.AppPath != "just mav-app-path" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigDoesNotGuessUnrelatedJustfile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "justfile"), "build:\n  true\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Launch.Commands.Build, "just") {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigDoesNotDetectFastlane(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "fastlane", "Fastfile"), `
lane :mav_build do
end
private_lane :mav_app_path do
end
`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Launch.Commands.Build, "fastlane") {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigDoesNotGuessTenteWrapper(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "tente.toml"), "[build]\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Launch.Commands.Build, "tente") {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigStoresBootedSimulator(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Demo", "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	out := `{"devices":{"runtime":[{"udid":"SIM-1","name":"iPhone 17 Pro Max","state":"Booted"}]}}`
	cfg, err := SetupConfig(root, fakeRunner{
		tools: map[string]bool{"xcrun": true},
		out:   map[string]string{"xcrun simctl list devices booted -j": out},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SimulatorUDID != "SIM-1" || cfg.SimulatorName != "iPhone 17 Pro Max" || cfg.SimulatorRuntime != "runtime" {
		t.Fatalf("sim=%q %q %q", cfg.SimulatorUDID, cfg.SimulatorName, cfg.SimulatorRuntime)
	}
}

func TestSetupConfigDoesNotRequireBazelAppTarget(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Info.plist"), `<plist><dict><key>CFBundleIdentifier</key><string>com.example.demo</string></dict></plist>`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"xcrun": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppTarget != "" {
		t.Fatalf("target=%q", cfg.AppTarget)
	}
	if cfg.BundleID != "com.example.demo" {
		t.Fatalf("bundle=%q", cfg.BundleID)
	}
}

func TestSaveLoadConfig(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.ProjectName = "Demo"
	cfg.AppTarget = "//Demo:DemoApp"
	cfg.BundleID = "com.example.demo"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{Build: "make build-ios", AppPath: "make app-path", Launch: `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tools:") {
		t.Fatalf("config should not persist runtime tool detection:\n%s", data)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AppTarget != cfg.AppTarget || loaded.Launch.Commands.AppPath != "make app-path" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestLoadConfigIgnoresLegacyToolsSection(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ConfigFile), `project_name: Demo
target_kind: simulator
app:
  bundle_id: com.example.demo
  process_name: Demo
bundle_id: com.example.demo
process_name: Demo
simulator_udid: SIM
tools:
  axe: true
  idb: true
`)
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Tools) != 0 {
		t.Fatalf("legacy tools should not be loaded into config state: %+v", loaded.Tools)
	}
	if err := SaveConfig(root, loaded); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tools:") || strings.Contains(string(data), "axe: true") {
		t.Fatalf("legacy tools should be dropped on save:\n%s", data)
	}
}

func TestLoadLegacyConfigDefaultsToSimulatorTarget(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ConfigFile), `project_name: Demo
bundle_id: com.example.demo
simulator_udid: SIM-1
`)
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TargetKind != "simulator" || targetKind(loaded) != drivers.KindSim {
		t.Fatalf("target=%q loaded=%+v", loaded.TargetKind, loaded)
	}
}

func TestSaveLoadConfigPreservesDeviceTarget(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.ProjectName = "Demo"
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.DeviceName = "David iPhone"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TargetKind != "device" || loaded.DeviceUDID != "REAL-1" || loaded.DeviceName != "David iPhone" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestSplitYAMLKVPreservesInnerShellQuotes(t *testing.T) {
	key, value, ok := splitYAMLKV(`app_path: echo "$MAV_ROOT/bazel-bin/Demo.app"`)
	if !ok {
		t.Fatal("expected key/value")
	}
	if key != "app_path" {
		t.Fatalf("key=%q", key)
	}
	if value != `echo "$MAV_ROOT/bazel-bin/Demo.app"` {
		t.Fatalf("value=%q", value)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRawConfig(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ConfigFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const profileFixture = `project_name: nokoru
app_target: "//App:NokoruiOS"
bundle_id: com.davidcollado.nokoru.debug
process_name: nNokoru
target_command: simpool lease --device "iPhone 17 Pro"
launch:
  mode: custom
  commands:
    build: "bazelisk build '//App:NokoruiOS'"
    app_path: "./scripts/mav-app-path.sh"
    install: "xcrun simctl install $MAV_UDID $MAV_APP_PATH"
    launch: "xcrun simctl launch $MAV_UDID $MAV_BUNDLE_ID"
profiles:
  mac:
    app_target: "//App:NokoruMac"
    process_name: Nokoru
    target_command: ""
    launch:
      commands:
        build: "bazelisk build '//App:NokoruMac'"
        install: ""
`

func TestProfileOverlayReplacesOnlyDeclaredFields(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture)
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfile != "mac" {
		t.Fatalf("active profile=%q", cfg.ActiveProfile)
	}
	if cfg.AppTarget != "//App:NokoruMac" || cfg.ProcessName != "Nokoru" {
		t.Fatalf("the profile must win: app_target=%q process_name=%q", cfg.AppTarget, cfg.ProcessName)
	}
	if cfg.Launch.Commands.Build != "bazelisk build '//App:NokoruMac'" {
		t.Fatalf("build=%q", cfg.Launch.Commands.Build)
	}
	// Inherited: the profile does not mention it.
	if cfg.Launch.Commands.Launch != "xcrun simctl launch $MAV_UDID $MAV_BUNDLE_ID" {
		t.Fatalf("launch should inherit from the base: %q", cfg.Launch.Commands.Launch)
	}
	if cfg.BundleID != "com.davidcollado.nokoru.debug" {
		t.Fatalf("bundle_id should be inherited: %q", cfg.BundleID)
	}
}

func TestProfileEmptyStringAnnulsInheritedValue(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture)
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatal(err)
	}
	// This is the distinction that justifies the pointers: present-and-empty
	// is NOT absent. A simctl install inherited in a macOS run would
	// install the Mac app on a simulator.
	if cfg.Launch.Commands.Install != "" {
		t.Fatalf(`install: "" in the profile must cancel the base's, got %q`, cfg.Launch.Commands.Install)
	}
	if cfg.TargetCommand != "" {
		t.Fatalf(`target_command: "" must cancel the simpool lease, got %q`, cfg.TargetCommand)
	}
}

func TestProfileNotFoundFailsListingAvailable(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture)
	_, err := LoadConfigWithProfile(root, "windows")
	if err == nil {
		t.Fatal("a nonexistent profile must fail, not silently fall to the base")
	}
	if !strings.Contains(err.Error(), "profile_not_found") || !strings.Contains(err.Error(), "mac") {
		t.Fatalf("the error must name the valid profiles: %v", err)
	}
}

func TestProfilePrecedenceFlagOverEnvOverDefault(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture+"default_profile: mac\n")

	// 3) default_profile when there is nothing else
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfile != "mac" {
		t.Fatalf("default_profile should apply: %q", cfg.ActiveProfile)
	}

	// 2) MAV_PROFILE beats the default
	t.Setenv("MAV_PROFILE", "nope")
	if _, err := LoadConfig(root); err == nil {
		t.Fatal("MAV_PROFILE should beat default_profile and fail for not existing")
	}

	// 1) the explicit override beats MAV_PROFILE
	cfg, err = LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatalf("the explicit override must beat MAV_PROFILE: %v", err)
	}
	if cfg.ActiveProfile != "mac" {
		t.Fatalf("active profile=%q", cfg.ActiveProfile)
	}
}

func TestConfigWithoutProfilesIsUnchanged(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: plain\nbundle_id: com.example.app\n")
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfile != "" || len(cfg.Profiles) != 0 {
		t.Fatalf("a config without profiles must activate none: %q %v", cfg.ActiveProfile, cfg.Profiles)
	}
	if cfg.BundleID != "com.example.app" {
		t.Fatalf("bundle_id=%q", cfg.BundleID)
	}
}

func TestSaveConfigRefusesToFlattenAnActiveProfile(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture)
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatal(err)
	}
	// Saving this would write //App:NokoruMac as the BASE app_target and
	// leave the profile meaningless, silently and with no way back.
	if err := SaveConfig(root, cfg); err == nil {
		t.Fatal("SaveConfig must reject a config with an applied profile")
	} else if !strings.Contains(err.Error(), "config_save_with_active_profile") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestProfileOverridableFieldsAreExhaustive breaks when configYAML gains a
// field and nobody decides whether a platform profile should be able to
// override it. Without this test that omission goes unnoticed: the field
// is simply ignored inside the profile, silently, which is the kind of
// dead configuration this project hunts.
func TestProfileOverridableFieldsAreExhaustive(t *testing.T) {
	overridable := map[string]bool{
		"target_kind":    true, // Phase 2: the platform axis
		"app_target":     true, // //App:NokoruiOS vs //App:NokoruMac
		"process_name":   true, // the Mac binary is not named the same
		"target_command": true, // a simpool lease has no business on macOS
		"log_subsystem":  true,
		"log_category":   true,
		"launch":         true, // different recipes per platform
		"vm":             true, // a macOS profile in a VM, a local one beside it
	}
	notOverridable := map[string]string{
		"project_name":        "the project is the same on every platform",
		"bundle_id":           "shared; if it diverged it would be another app",
		"app":                 "mirror of bundle_id/process_name",
		"device_target":       "physical iOS device axis, orthogonal to the platform",
		"device_udid":         "device selection, persisted separately",
		"device_name":         "same",
		"simulator_udid":      "simulator selection, persisted separately",
		"simulator_name":      "same",
		"simulator_runtime":   "same",
		"locale":              "set per invocation, not per platform",
		"language":            "same",
		"preferred_ui_driver": "the router decides it by capability and cost",
		"allow_shell":         "repo policy, not platform policy",
		"default_profile":     "it is the selector, it cannot live inside what is selected",
		"profiles":            "same",
		"fixtures":            "named states belong to the repo, not the platform; a fixture only valid for one names it",
	}
	typ := reflect.TypeOf(configYAML{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("yaml")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if overridable[name] {
			continue
		}
		if _, ok := notOverridable[name]; ok {
			continue
		}
		t.Fatalf("configYAML has the field %q and nobody has decided whether a profile can override it.\n"+
			"Add it to overridable (and to profileYAML + applyProfile) or to notOverridable with the reason.", name)
	}
}

func TestUnknownTargetKindFailsFromEverySource(t *testing.T) {
	// File
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\ntarget_kind: windows\n")
	if _, err := LoadConfig(root); err == nil {
		t.Fatal("an unknown target_kind in the file must fail")
	} else if !strings.Contains(err.Error(), "target_kind_invalid") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Profile
	root2 := t.TempDir()
	writeRawConfig(t, root2, "project_name: x\nprofiles:\n  weird:\n    target_kind: windows\n")
	if _, err := LoadConfigWithProfile(root2, "weird"); err == nil {
		t.Fatal("an unknown target_kind in a profile must fail")
	}
	if _, err := LoadConfig(root2); err != nil {
		t.Fatalf("without an active profile it should not fail: %v", err)
	}

	// Environment
	root3 := t.TempDir()
	writeRawConfig(t, root3, "project_name: x\n")
	t.Setenv("MAV_TARGET_KIND", "windows")
	if _, err := LoadConfig(root3); err == nil {
		t.Fatal("an unknown MAV_TARGET_KIND must fail")
	}
}

func TestMacosIsAValidTargetKind(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\nprofiles:\n  mac:\n    target_kind: macos\n")
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatalf("macos must be a valid target_kind: %v", err)
	}
	if targetKind(cfg) != drivers.KindMac {
		t.Fatalf("kind=%v", targetKind(cfg))
	}
	// The public spelling is kept: it is contract with the agents and with
	// the config.yaml files already written.
	if got := targetKindLabel(targetKind(cfg)); got != "macos" {
		t.Fatalf("label=%q", got)
	}
	// A macOS app has no UDID; reporting one would be lying.
	if targetUDID(cfg) != "" {
		t.Fatalf("udid=%q", targetUDID(cfg))
	}
}

// TestMacTargetSkipsSimulatorResolution: a macOS target must resolve no
// target_command and not fall to the booted simulator. It is the guard
// that prevents the most expensive scenario the review found, a "macOS"
// run leasing a simulated iPhone and, with --clear-state, uninstalling the
// iOS app from it because the bundle_id is shared.
func TestMacTargetSkipsSimulatorResolution(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\nbundle_id: com.example.app\ntarget_command: echo SHOULD-NOT-RUN\nprofiles:\n  mac:\n    target_kind: macos\n")
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatal(err)
	}
	cli := CLI{Root: root}
	if warn := cli.resolveConfigTarget(&cfg); warn != "" {
		t.Fatalf("a macOS target should not warn about target_command: %q", warn)
	}
	if cfg.SimulatorUDID != "" {
		t.Fatalf("a macOS target must not resolve a simulator, got %q", cfg.SimulatorUDID)
	}
}

func TestMacMissingPermissionsRefusesToGuess(t *testing.T) {
	// Returning "all good" for a format that is not understood would be
	// worse than admitting it is not known: the user would learn about the
	// missing permission when a command failed, not when asking.
	for name, stdout := range map[string]string{
		"vacio":      "",
		"no-json":    "error: something",
		"otra-forma": `{"success":false}`,
		// With no daemon to ask, the answer carries null, and that is not
		// "both are missing": it is that nobody answered.
		"sin-demonio": `{"accessibility":null,"screen_recording":null}`,
	} {
		if got := macMissingPermissions(stdout); len(got) != 1 || got[0] != "unreadable" {
			t.Fatalf("%s: what is not understood must not pass as good, got %v", name, got)
		}
	}
	granted := `{"accessibility":true,"screen_recording":true}`
	if got := macMissingPermissions(granted); len(got) != 0 {
		t.Fatalf("with everything granted nothing is missing: %v", got)
	}
	partial := `{"accessibility":true,"screen_recording":false}`
	got := macMissingPermissions(partial)
	if len(got) != 1 || got[0] != "screen_recording" {
		t.Fatalf("got %v", got)
	}
}

// TestVMOnlyAppliesToMacOS: `vm: true` on a simulator or a device profile
// would leave someone believing their run is isolated in a throwaway
// machine when it is driving the same simulator as everything else.
func TestVMOnlyAppliesToMacOS(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\nprofiles:\n  sim:\n    target_kind: simulator\n    vm: true\n")
	if _, err := LoadConfigWithProfile(root, "sim"); err == nil {
		t.Fatal("vm: true on a simulator profile must fail")
	} else if !strings.Contains(err.Error(), "vm_unsupported_target") {
		t.Fatalf("unexpected error: %v", err)
	}

	mac := t.TempDir()
	writeRawConfig(t, mac, "project_name: x\nprofiles:\n  mac:\n    target_kind: macos\n    vm: true\n")
	cfg, err := LoadConfigWithProfile(mac, "mac")
	if err != nil {
		t.Fatalf("vm on a macOS profile is valid: %v", err)
	}
	if !cfg.VM {
		t.Fatal("the profile asked for a VM and the config came back without one")
	}
}

// TestProfileCanTurnVMOff: a base with `vm: true` plus a profile that runs
// on this machine has to be expressible, or the only way to test locally is
// editing the base and remembering to put it back.
func TestProfileCanTurnVMOff(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\ntarget_kind: macos\nvm: true\nprofiles:\n  here:\n    vm: false\n")
	cfg, err := LoadConfigWithProfile(root, "here")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VM {
		t.Fatal("the profile said vm: false and it was ignored")
	}
	base, err := LoadConfigWithProfile(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !base.VM {
		t.Fatal("without a profile the base's vm: true must stand")
	}
}

// TestProfileUnknownKeyIsAnError: yaml.Unmarshal silently ignores what it
// does not know. In a profile that is expensive: you write a key that does
// not exist, nothing happens, and from the outside it is indistinguishable
// from it applying and having no effect.
func TestProfileUnknownKeyIsAnError(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\nprofiles:\n  mac:\n    target_kind: macos\n    fixture: onboarding\n")
	_, err := LoadConfigWithProfile(root, "mac")
	if err == nil {
		t.Fatal("a nonexistent key in a profile cannot pass silently")
	}
	if !strings.Contains(err.Error(), "profile_unknown_key") || !strings.Contains(err.Error(), "fixture") {
		t.Fatalf("the error must name the key: %v", err)
	}

	// And what is valid keeps loading.
	ok := t.TempDir()
	writeRawConfig(t, ok, "project_name: x\nprofiles:\n  mac:\n    target_kind: macos\n    vm: true\n")
	if _, err := LoadConfigWithProfile(ok, "mac"); err != nil {
		t.Fatal(err)
	}
}
