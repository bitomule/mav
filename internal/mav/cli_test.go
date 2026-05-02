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

func TestDoctorDoesNotRequireMaestro(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{"go": true, "bazelisk": true, "xcrun": true, "axe": true, "idb": true}}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "maestro") {
		t.Fatalf("doctor should not mention maestro: %q", got)
	}
}

func TestGoUsesNativeMAVActions(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home":     {ID: "home", Edges: []Edge{{To: "settings", Text: "Settings", Wait: "1"}}},
			"settings": {ID: "settings", AssertText: "Settings"},
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := fakeRunner{tools: map[string]bool{"axe": true}, out: map[string]string{"axe describe-ui": `{"AXLabel":"Settings"}`}}
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"go", "settings"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=go") {
		t.Fatalf("got %q", out.String())
	}
}

func TestUIScrollUntilFindsElement(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := fakeRunner{tools: map[string]bool{"axe": true}, out: map[string]string{"axe describe-ui": `{"AXLabel":"Privacy Policy"}`}}
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "scrollUntil", "--text", "Privacy Policy"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=ui.scrollUntil") || !strings.Contains(out.String(), "swipes=0") {
		t.Fatalf("got %q", out.String())
	}
}

func TestExecStepRequiresOptIn(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "abc", Dir: filepath.Join(t.TempDir(), "run"), LogsPath: filepath.Join(t.TempDir(), "logs.txt")}
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.execFlowShell(context.Background(), run, 1, map[string]string{"cmd": "echo hi"})
	if err == nil || err.Error() != "shell_not_allowed" || !strings.Contains(fields["next"], "allow_shell") {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
}

func TestExecStepRunsInProjectRoot(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AllowShell = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "abc", Dir: filepath.Join(t.TempDir(), "run"), LogsPath: filepath.Join(t.TempDir(), "logs.txt")}
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.execFlowShell(context.Background(), run, 2, map[string]string{"cmd": "pwd", "contains": root})
	if err != nil {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	data, err := os.ReadFile(fields["stdout"])
	if err != nil {
		t.Fatal(err)
	}
	gotRoot, err := filepath.EvalSymlinks(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if gotRoot != wantRoot {
		t.Fatalf("stdout=%q", string(data))
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

func TestLogsFiltersMAVKey(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "abc-key", Dir: filepath.Join(os.TempDir(), "mav", "abc-key"), LogsPath: filepath.Join(os.TempDir(), "mav", "abc-key", "logs.txt")}
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	logs := strings.Join([]string{
		`MAV_LOG key=SettingsReached value=true`,
		`MAV_LOG key=Other value=true`,
		`SettingsReached without marker`,
	}, "\n")
	if err := os.WriteFile(run.LogsPath, []byte(logs), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--key", "SettingsReached"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "matches=1") || !strings.Contains(got, "key=SettingsReached") {
		t.Fatalf("got %q", got)
	}
}

func TestStopTerminatesRunProcesses(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "stop-test", Dir: filepath.Join(os.TempDir(), "mav", "stop-test"), LogsPath: filepath.Join(os.TempDir(), "mav", "stop-test", "logs.txt"), Commands: filepath.Join(os.TempDir(), "mav", "stop-test", "commands.jsonl"), Processes: filepath.Join(os.TempDir(), "mav", "stop-test", "processes.jsonl")}
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	appendProcess(run, "logs", 999999, "fake")
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"stop"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=stop") || !strings.Contains(got, "stopped=1") {
		t.Fatalf("got %q", got)
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

func TestPreviewInitCreatesHostAndConfig(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"preview", "init"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=preview.init") {
		t.Fatalf("got %q", out.String())
	}
	if !exists(filepath.Join(root, "MAVPreview", "BUILD.bazel")) || !exists(filepath.Join(root, "MAVPreview", "PreviewHostApp.swift")) || !exists(filepath.Join(root, "MAVPreview", "Info.plist")) {
		t.Fatalf("preview host was not created")
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PreviewTarget != "//MAVPreview:MAVPreviewApp" || loaded.PreviewBundleID != "com.example.app.mavpreview" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestLaunchLanguageArgs(t *testing.T) {
	got := strings.Join(simctlLaunchLanguageArgs(Config{Language: "es", Locale: "es_ES"}), " ")
	if got != "-AppleLanguages (es) -AppleLocale es_ES" {
		t.Fatalf("got %q", got)
	}
}

func TestCountTreeNodes(t *testing.T) {
	raw := `[{"children":[{"children":[]},{"children":[{"children":[]}]}]}]`
	if got := countTreeNodes(raw); got != 4 {
		t.Fatalf("got %d", got)
	}
}
