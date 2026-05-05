package mav

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	err      map[string]CommandResult
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
	if result, ok := r.err[command]; ok {
		return result
	}
	return CommandResult{Stdout: r.out[command]}
}

func (r *sequenceRecordingRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 0, nil
}

type errorRunner struct {
	tools  map[string]bool
	result CommandResult
}

func (r errorRunner) LookPath(file string) (string, error) {
	if r.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (r errorRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	_ = name
	_ = args
	return r.result
}

func (r errorRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 0, r.result.Err
}

type appiumHomeRetryRunner struct{}

func (r appiumHomeRetryRunner) LookPath(file string) (string, error) {
	switch file {
	case "go", "bazelisk", "xcrun", "axe", "idb", "node", "npm", "appium":
		return "/usr/bin/" + file, nil
	default:
		return "", os.ErrNotExist
	}
}

func (r appiumHomeRetryRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	if name == "appium" && strings.Join(args, " ") == "driver list --installed" {
		if os.Getenv("APPIUM_HOME") != "" {
			return CommandResult{Stdout: "xcuitest@7.0.0\n"}
		}
		return CommandResult{Stderr: "The autodetected Appium home path '/Users/me/.appium' must be writeable", Err: os.ErrPermission}
	}
	return CommandResult{}
}

func (r appiumHomeRetryRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 0, nil
}

type launchRecipeRunner struct {
	tools    map[string]bool
	commands []string
	results  map[string]CommandResult
}

func (r *launchRecipeRunner) LookPath(file string) (string, error) {
	if r.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (r *launchRecipeRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	if name == "/bin/sh" && len(args) == 2 {
		script := args[1]
		for needle, result := range r.results {
			if strings.Contains(script, needle) {
				return result
			}
		}
	}
	return CommandResult{}
}

func (r *launchRecipeRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 123, nil
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

func TestCoordinateTapUsesResolvedIDBCapability(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: map[string]bool{"idb": true}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "10", "--y", "20"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=ui.tap") {
		t.Fatalf("got %q", out.String())
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "idb ui tap 10 20 --udid SIM") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestDoctorReportsIDBPythonUnsupported(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: errorRunner{
		tools:  map[string]bool{"idb": true},
		result: CommandResult{Stderr: "RuntimeError: asyncio.get_event_loop() Python 3.14", Err: os.ErrInvalid},
	}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "idb_next=\"pipx install --python python3.12 fb-idb\"") {
		t.Fatalf("got %q", out.String())
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

func TestSetupScaffoldsProjectIdempotently(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Info.plist"), `<plist><dict><key>CFBundleIdentifier</key><string>com.example.detected</string></dict></plist>`)
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.existing"
	cfg.SimulatorUDID = "SIM-EXISTING"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{Launch: "custom launch"}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{"xcrun": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"setup"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=setup") {
		t.Fatalf("got %q", out.String())
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BundleID != "com.example.existing" || loaded.SimulatorUDID != "SIM-EXISTING" || loaded.Launch.Commands.Launch != "custom launch" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestUIHelpShowsSpecificPinchFlags(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "pinch", "--help"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"mav ui pinch", "--scale", "--pan-x", "--duration", "--hold"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestCapturePathUsesNameAndAvoidsCollisions(t *testing.T) {
	run := RunState{Dir: t.TempDir()}
	first := uniqueCapturePath(run, "Largest Videos")
	if filepath.Base(first) != "largest-videos.png" {
		t.Fatalf("first=%q", first)
	}
	if err := os.MkdirAll(filepath.Dir(first), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := uniqueCapturePath(run, "Largest Videos")
	if filepath.Base(second) != "largest-videos-2.png" {
		t.Fatalf("second=%q", second)
	}
}

func TestUITreePrintsNodeDetailsByDefault(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	raw := `[{"AXLabel":"Largest Videos","role":"heading","AXFrame":"{{0, 10}, {200, 40}}","children":[{"AXIdentifier":"delete_button","AXLabel":"Delete","role":"button"}]}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": raw}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"ok cmd=ui.tree", "node index=1", `label="Largest Videos"`, "frame=", "id=delete_button"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestUITreeReportsRecognizedScreenSeparately(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":          {ID: "start", AssertText: "Home"},
			"largest-videos": {ID: "largest-videos", Recognizers: []Recognizer{{Kind: "text", Value: "Largest Videos"}}},
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "run-tree", Dir: filepath.Join(os.TempDir(), "mav", "run-tree")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	raw := `[{"AXLabel":"Largest Videos","role":"heading","children":[{"AXLabel":"Delete","role":"button"}]}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": raw}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"screen=unknown", "recognized_screen=largest-videos", "screen_source=recognized", "screen_confidence=0.80"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestSandboxHintForUITreeFailure(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := errorRunner{tools: cfg.Tools, result: CommandResult{Stderr: "CoreSimulator operation not permitted", Err: os.ErrPermission}}
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "next=") || !strings.Contains(got, "rerun outside sandbox") {
		t.Fatalf("got %q", got)
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

func TestDoctorReportsAppiumHomePermissionInsteadOfNodeRuntime(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: errorRunner{
		tools:  map[string]bool{"go": true, "bazelisk": true, "xcrun": true, "axe": true, "idb": true, "node": true, "npm": true, "appium": true},
		result: CommandResult{Stderr: "The autodetected Appium home path '/Users/me/.appium' must be writeable", Err: os.ErrPermission},
	}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "multitouch=missing") ||
		!strings.Contains(got, "multitouch_issue=appium_home_not_writable") ||
		strings.Contains(got, "appium_node_runtime_failed") {
		t.Fatalf("got %q", got)
	}
}

func TestDoctorRetriesAppiumWithWritableHome(t *testing.T) {
	var out bytes.Buffer
	t.Setenv("APPIUM_HOME", "")
	cli := CLI{Runner: appiumHomeRetryRunner{}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "multitouch=ok") ||
		!strings.Contains(got, "multitouch_driver=appium") ||
		strings.Contains(got, "appium_home_not_writable") {
		t.Fatalf("got %q", got)
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

func TestUISwipeWithCustomCoordinatesReportsCustomDirection(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "swipe", "--start-x", "10", "--start-y", "20", "--end-x", "30", "--end-y", "40"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"ok cmd=ui.swipe", "direction=custom", "start_x=10", "start_y=20", "end_x=30", "end_y=40"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "direction=--start-x") {
		t.Fatalf("got confusing direction: %q", got)
	}
}

func TestUITreeReportsPendingMapActionForUnknownScreen(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":            {ID: "start", AssertText: "Home"},
			"photos-to-delete": {ID: "photos-to-delete", AssertText: "Photos to Delete"},
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "run-pending", Dir: filepath.Join(os.TempDir(), "mav", "run-pending")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "photos-to-delete", run.ID)
	SetPendingMapAction(root, pendingMapAction{From: "photos-to-delete", ID: "large_videos_button"})

	raw := `[{"AXLabel":"Play","role":"button"}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": raw}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"screen=unknown", "map_pending=true", "previous_screen=photos-to-delete", `next="add accessibility ids or capture/inspect before mapping"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
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

func TestEvidenceStartRejectsRunWithExistingVideo(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "abc-video", Dir: filepath.Join(os.TempDir(), "mav", "abc-video")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, "video.mov"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"evidence", "start"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=evidence_run_not_clean") || !strings.Contains(got, "issue=video_exists") {
		t.Fatalf("got %q", got)
	}
}

func TestEvidenceReportReportsMissingVideo(t *testing.T) {
	root := t.TempDir()
	run := RunState{ID: "abc-no-video", Dir: filepath.Join(os.TempDir(), "mav", "abc-no-video"), LogsPath: filepath.Join(os.TempDir(), "mav", "abc-no-video", "logs.txt")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"evidence", "report"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=evidence.report") || !strings.Contains(got, "video=missing") {
		t.Fatalf("got %q", got)
	}
}

func TestEvidenceReportReportsValidVideoPath(t *testing.T) {
	root := t.TempDir()
	run := RunState{ID: "abc-video-report", Dir: filepath.Join(os.TempDir(), "mav", "abc-video-report"), LogsPath: filepath.Join(os.TempDir(), "mav", "abc-video-report", "logs.txt")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(run.Dir, "video.mov")
	if err := os.WriteFile(video, testMovieWithDuration(600, 1200), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"evidence", "report"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=evidence.report") || !strings.Contains(got, "video="+video) || !strings.Contains(got, "video_duration=") {
		t.Fatalf("got %q", got)
	}
}

func TestFlowVideoStartStopAliasesRecordVideo(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "abc-flow-video", Dir: filepath.Join(os.TempDir(), "mav", "abc-flow-video")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.executeFlowStep(context.Background(), run, 1, FlowStep{Action: "video.start"})
	if err != nil {
		t.Fatalf("start fields=%v err=%v", fields, err)
	}
	if !fileExists(filepath.Join(run.Dir, "video.pid")) {
		t.Fatalf("video pid was not created")
	}
	fields, err = cli.executeFlowStep(context.Background(), run, 2, FlowStep{Action: "video.stop", Params: map[string]string{"note": "Done"}})
	if err != nil {
		t.Fatalf("stop fields=%v err=%v", fields, err)
	}
	if !fileExists(filepath.Join(run.Dir, "video.mov")) {
		t.Fatalf("video was not written")
	}
	if fileExists(filepath.Join(run.Dir, "video.pid")) {
		t.Fatalf("video pid should be removed after stop")
	}
}

func TestUIPinchRecordsHighLevelCommand(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: "http://appium.local", SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":null}`)), Header: http.Header{}}, nil
	})}
	defer func() { http.DefaultClient = oldClient }()
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "pinch", "--x", "200", "--y", "450", "--scale", "0.5", "--duration", "1ms"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(run.Commands)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "mav ui pinch --x 200 --y 450 --scale 0.5 --duration 1ms") {
		t.Fatalf("commands=%s output=%s", data, out.String())
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

func TestOpenRunsConfiguredLaunchRecipe(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.SimulatorName = "iPhone"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Build:   "make build-ios",
		AppPath: "make ios-app-path",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
		Launch:  `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{
		tools: map[string]bool{"xcrun": true},
		results: map[string]CommandResult{
			"make ios-app-path": {Stdout: "/tmp/App.app\n"},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=open") || !strings.Contains(out.String(), "app=/tmp/App.app") {
		t.Fatalf("got %q", out.String())
	}
	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{"make build-ios", "make ios-app-path", "simctl install", "simctl launch"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("commands missing %q:\n%s", want, joined)
		}
	}
}

func TestOpenRejectsInvalidAppPathOutput(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		AppPath: "make ios-app-path",
		Launch:  `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{
		tools: map[string]bool{"xcrun": true},
		results: map[string]CommandResult{
			"make ios-app-path": {Stdout: "/tmp/One.app\n/tmp/Two.app\n"},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "fail code=launch_step_failed") || !strings.Contains(out.String(), "step=app_path") {
		t.Fatalf("got %q", out.String())
	}
}

func TestLaunchLanguageArgs(t *testing.T) {
	got := strings.Join(simctlLaunchLanguageArgs(Config{Language: "es", Locale: "es_ES"}), " ")
	if got != "-AppleLanguages (es) -AppleLocale es_ES" {
		t.Fatalf("got %q", got)
	}
}

func TestMP4DurationRejectsZeroDuration(t *testing.T) {
	if duration, err := mp4Duration(testMovieWithDuration(600, 0)); err == nil || duration != 0 {
		t.Fatalf("duration=%v err=%v", duration, err)
	}
}

func TestMP4DurationAcceptsPositiveDuration(t *testing.T) {
	duration, err := mp4Duration(testMovieWithDuration(600, 1200))
	if err != nil {
		t.Fatal(err)
	}
	if duration != 2*time.Second {
		t.Fatalf("duration=%v", duration)
	}
}

func testMovieWithDuration(timescale, duration uint32) []byte {
	mvhdPayload := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhdPayload[12:16], timescale)
	binary.BigEndian.PutUint32(mvhdPayload[16:20], duration)
	mvhd := testAtom("mvhd", mvhdPayload)
	moov := testAtom("moov", mvhd)
	return append(testAtom("ftyp", []byte("qt  \x00\x00\x00\x00qt  ")), moov...)
}

func testAtom(kind string, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(len(out)))
	copy(out[4:8], kind)
	copy(out[8:], payload)
	return out
}

func TestCountTreeNodes(t *testing.T) {
	raw := `[{"children":[{"children":[]},{"children":[{"children":[]}]}]}]`
	if got := countTreeNodes(raw); got != 4 {
		t.Fatalf("got %d", got)
	}
}
