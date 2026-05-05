package mav

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakeRunner struct {
	tools map[string]bool
	runs  []string
	out   map[string]string
	seq   map[string][]string
	calls map[string]int
}

func (f fakeRunner) LookPath(file string) (string, error) {
	if f.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (f fakeRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	key := name
	for _, arg := range args {
		key += " " + arg
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
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AppTarget != cfg.AppTarget || !loaded.Tools["axe"] || loaded.Launch.Commands.AppPath != "make app-path" {
		t.Fatalf("loaded=%+v", loaded)
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
