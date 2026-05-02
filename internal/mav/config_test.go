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
}

func (f fakeRunner) LookPath(file string) (string, error) {
	if f.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (f fakeRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	return CommandResult{}
}

func (f fakeRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	return 123, nil
}

func TestDiscoverConfigFindsBazelApp(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "MODULE.bazel"), "module(name = \"Demo\")\n")
	mustWrite(t, filepath.Join(root, "Demo", "BUILD.bazel"), `load("@build_bazel_rules_apple//apple:ios.bzl", "ios_application")
ios_application(
    name = "DemoApp",
    bundle_id = "com.example.demo",
)
`)
	cfg, err := DiscoverConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true, "xcrun": true, "axe": true}})
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

func TestSaveLoadConfig(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.ProjectName = "Demo"
	cfg.AppTarget = "//Demo:DemoApp"
	cfg.BundleID = "com.example.demo"
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AppTarget != cfg.AppTarget || !loaded.Tools["axe"] {
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
