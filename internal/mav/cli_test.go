package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingRunner struct {
	tools   map[string]bool
	command string
	result  CommandResult
}

func (r *recordingRunner) LookPath(file string) (string, error) {
	if r.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (r *recordingRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	r.command = name + " " + strings.Join(args, " ")
	return r.result
}

func (r *recordingRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 0, nil
}

type sequenceRecordingRunner struct {
	tools    map[string]bool
	commands []string
	out      map[string]string
}

func (r *sequenceRecordingRunner) LookPath(file string) (string, error) {
	if r.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (r *sequenceRecordingRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	return CommandResult{Stdout: r.out[command]}
}

func (r *sequenceRecordingRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 0, nil
}

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

func TestDoctorReportsAppiumReadiness(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: map[string]bool{"go": true, "bazelisk": true, "xcrun": true, "axe": true, "idb": true, "node": true, "npm": true, "appium": true},
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "accessibility=ok") ||
		!strings.Contains(got, "coordinate_tap=ok") ||
		!strings.Contains(got, "multitouch=ok") ||
		!strings.Contains(got, "multitouch_driver=appium") {
		t.Fatalf("got %q", got)
	}
	for _, old := range []string{"appium=ok", "appium_node=", "appium_xcuitest=", "axe=ok", "idb=ok", "node=ok", "npm=ok"} {
		if strings.Contains(got, old) {
			t.Fatalf("doctor should not expose old tool field %q: %q", old, got)
		}
	}
}

func TestDoctorRecommendsAppiumSetupWhenDriverMissing(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: map[string]bool{"go": true, "bazelisk": true, "xcrun": true, "axe": true, "idb": true, "node": true, "npm": true, "appium": true},
	}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "multitouch=missing") ||
		!strings.Contains(got, "multitouch_issue=\"xcuitest driver missing\"") ||
		!strings.Contains(got, "next=\"mav setup --install appium\"") {
		t.Fatalf("got %q", got)
	}
}

func TestDoctorReportsCapabilityFallbacks(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: map[string]bool{"go": true, "bazelisk": true, "xcrun": true, "idb": true},
	}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"accessibility=ok",
		"accessibility_driver=idb",
		"coordinate_tap=ok",
		"coordinate_tap_driver=idb",
		"semantic_actions=missing",
		"multitouch=missing",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestHelpListsCommands(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"help"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "Usage:") || !strings.Contains(got, "ui          Inspect") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "--json") {
		t.Fatalf("help should not mention --json: %q", got)
	}
}

func TestInstallSkillsRunsVercelSkillsCLI(t *testing.T) {
	var out bytes.Buffer
	runner := &recordingRunner{tools: map[string]bool{"npx": true}}
	cli := CLI{Runner: runner, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"install-skills"}); err != nil {
		t.Fatal(err)
	}
	want := "npx skills add bitomule/mav --skill mav --global --yes"
	if runner.command != want {
		t.Fatalf("command=%q want %q", runner.command, want)
	}
	if !strings.Contains(out.String(), "ok cmd=install-skills") || !strings.Contains(out.String(), "scope=global") {
		t.Fatalf("got %q", out.String())
	}
}

func TestInstallSkillsRequiresNpx(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: &recordingRunner{}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"install-skills"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "fail code=install_skills_unavailable") || !strings.Contains(out.String(), "tool=npx") {
		t.Fatalf("got %q", out.String())
	}
}

func TestSetupInstallsAndVerifiesAppium(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"npm": true, "node": true, "appium": true},
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"setup", "--install", "appium"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"npm install -g appium",
		"appium driver install xcuitest",
		"appium driver list --installed",
	}
	if strings.Join(runner.commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands=%q want=%q", runner.commands, want)
	}
	if !strings.Contains(out.String(), "ok cmd=setup") || !strings.Contains(out.String(), "installed=appium") {
		t.Fatalf("got %q", out.String())
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Tools["appium"] {
		t.Fatalf("tools=%v", loaded.Tools)
	}
}

func TestSubcommandHelp(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "mav ui tap --id ID") {
		t.Fatalf("got %q", out.String())
	}
}

func TestGoUsesNativeMAVActions(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true, "xcrun": true}
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
	runner := fakeRunner{
		tools: map[string]bool{"axe": true, "xcrun": true},
		seq: map[string][]string{"axe describe-ui": {
			`{"AXLabel":"Home","children":[{"AXLabel":"Settings"}]}`,
			`{"AXLabel":"Home","children":[{"AXLabel":"Settings"}]}`,
			`{"AXLabel":"Settings","children":[{"AXLabel":"Daily Reminder"}]}`,
			`{"AXLabel":"Settings","children":[{"AXLabel":"Daily Reminder"}]}`,
		}},
		calls: map[string]int{},
	}
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
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
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

func TestOpenStartsOnlyProbeLogsByDefault(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AppTarget = "//App:App"
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.SimulatorName = "iPhone"
	cfg.Tools = map[string]bool{"xcrun": true, "bazelisk": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := fakeRunner{tools: cfg.Tools, out: map[string]string{"bazelisk cquery --output=files //App:App": "/tmp/App.app\n"}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open"}); err != nil {
		t.Fatal(err)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	records := loadProcessRecords(run)
	if len(records) != 1 || records[0].Kind != "probe-logs" {
		t.Fatalf("records=%+v output=%s", records, out.String())
	}
	if strings.Contains(out.String(), " log_pid=") {
		t.Fatalf("open should only report probe_log_pid: %s", out.String())
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
