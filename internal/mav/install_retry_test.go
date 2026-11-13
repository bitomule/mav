package mav

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Bazel-built .app is read-only. simctl refuses to install it with a bare
// POSIX error 13, and the driver keeps only that first line, so the retry
// from a writable copy has to recognise it too; matching only "permission
// denied" left every Bazel project failing at install.
func TestInstallRetriesFromAWritableCopyOnSimctlPOSIX13(t *testing.T) {
	app := "/private/var/tmp/_bazel/execroot/_main/bazel-out/ios_sim_arm64-dbg/bin/WallapopApp.app"
	simctl := CommandResult{
		Stderr: "An error was encountered processing the command (domain=NSPOSIXErrorDomain, code=13):",
		Code:   1,
		Err:    errors.New("An error was encountered processing the command (domain=NSPOSIXErrorDomain, code=13):"),
	}
	if !shouldRetryInstallFromWritableCopy(simctl, app) {
		t.Fatal("simctl's POSIX 13 on a bazel-out bundle must retry from a writable copy")
	}
	other := CommandResult{Stderr: "An error was encountered processing the command (domain=NSPOSIXErrorDomain, code=2):", Code: 1, Err: errors.New("code=2")}
	if shouldRetryInstallFromWritableCopy(other, app) {
		t.Fatal("a missing file is not a permission problem")
	}
	if shouldRetryInstallFromWritableCopy(simctl, "/Users/me/DerivedData/App.app") {
		t.Fatal("only bazel-out bundles are read-only by construction")
	}
}

// The writable copy exists only to get past simctl's read-only check. Once the
// install succeeded it is dead weight, and with a real app it was hundreds of
// MB left in every run directory.
func TestOpenRemovesTheWritableCopyAfterInstalling(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "bazel-out", "ios_sim_arm64-dbg", "bin", "App.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Info.plist"), []byte("plist"), 0o444); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		AppPath: "make ios-app-path",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
		Launch:  `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	denied := "An error was encountered processing the command (domain=NSPOSIXErrorDomain, code=13):"
	runner := &launchRecipeRunner{
		tools: map[string]bool{"xcrun": true, "axe": true},
		results: map[string]CommandResult{
			"make ios-app-path":         {Stdout: app + "\n"},
			"simctl install SIM " + app: {Stderr: denied, Code: 1, Err: errors.New(denied)},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "ok cmd=open") {
		t.Fatalf("open must succeed through the writable copy: %s", out.String())
	}
	retried := false
	for _, command := range runner.commands {
		if strings.Contains(command, "simctl install SIM ") && strings.Contains(command, "app.tmp") {
			retried = true
		}
	}
	if !retried {
		t.Fatalf("the install was not retried from a copy: %v", runner.commands)
	}
	copies, _ := filepath.Glob(filepath.Join(root, ".mav", "runs", "*", "app.tmp"))
	if len(copies) != 0 {
		t.Fatalf("the writable copy must be removed after a successful install: %v", copies)
	}
	if !strings.Contains(out.String(), "app="+app) {
		t.Fatalf("open must report the original bundle, which still exists: %s", out.String())
	}
}
