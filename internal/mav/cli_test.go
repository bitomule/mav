package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoUnknownScreenFailsDeterministically(t *testing.T) {
	root := t.TempDir()
	if err := SaveAppMap(root, DefaultAppMap("com.example.demo")); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{}}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	var out bytes.Buffer
	cli.Stdout = &out
	err := cli.Run(context.Background(), []string{"go", "settings"})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=screen_not_found") {
		t.Fatalf("got %q", got)
	}
}

func TestLogsReadsCurrentRunFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "abc", Dir: filepath.Join(os.TempDir(), "mav", "abc"), LogsPath: filepath.Join(os.TempDir(), "mav", "abc", "logs.txt")}
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.LogsPath, []byte("one\nCheckoutView.render\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--contains", "CheckoutView"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "matches=1") {
		t.Fatalf("got %q", out.String())
	}
}

func TestPreviewRequiresConfig(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AppTarget = "//App:App"
	cfg.BundleID = "com.example.app"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"preview", "settings"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "fail code=preview_not_configured") {
		t.Fatalf("got %q", out.String())
	}
}

func TestCountTreeNodes(t *testing.T) {
	raw := `[{"children":[{"children":[]},{"children":[{"children":[]}]}]}]`
	if got := countTreeNodes(raw); got != 4 {
		t.Fatalf("got %d", got)
	}
}
