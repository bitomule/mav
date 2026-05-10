package mav

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	seq      map[string][]string
	calls    map[string]int
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
	if values := r.seq[command]; len(values) > 0 {
		index := 0
		if r.calls != nil {
			index = r.calls[command]
			if index >= len(values) {
				index = len(values) - 1
			}
			r.calls[command]++
		}
		return CommandResult{Stdout: values[index]}
	}
	return CommandResult{Stdout: r.out[command]}
}

func (r *sequenceRecordingRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 123, nil
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

type appiumFallbackRunner struct {
	tools map[string]bool
}

func (r appiumFallbackRunner) LookPath(file string) (string, error) {
	if r.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (r appiumFallbackRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	key := name + " " + strings.Join(args, " ")
	if key == "appium driver list --installed" {
		return CommandResult{Stdout: "xcuitest@7.0.0\n"}
	}
	if strings.HasPrefix(key, "axe tap ") {
		return CommandResult{Stderr: "No accessibility element matched", Err: os.ErrNotExist}
	}
	return CommandResult{}
}

func (r appiumFallbackRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 123, nil
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

type launchRetryRunner struct {
	tools        map[string]bool
	commands     []string
	appPath      string
	installCalls int
}

func (r *launchRetryRunner) LookPath(file string) (string, error) {
	if r.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (r *launchRetryRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	if name == "/bin/sh" && len(args) == 2 {
		script := args[1]
		if strings.Contains(script, "make ios-app-path") {
			return CommandResult{Stdout: r.appPath + "\n"}
		}
		if strings.Contains(script, "simctl install") {
			r.installCalls++
			if r.installCalls == 1 {
				return CommandResult{Stderr: "Permission denied", Err: os.ErrPermission, Code: 1}
			}
		}
	}
	return CommandResult{}
}

func (r *launchRetryRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
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
	if !strings.Contains(out.String(), "multitouch=missing") || !strings.Contains(out.String(), `multitouch_next="mav setup --install appium"`) {
		t.Fatalf("setup should report appium next step: %q", out.String())
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BundleID != "com.example.existing" || loaded.SimulatorUDID != "SIM-EXISTING" || loaded.Launch.Commands.Launch != "custom launch" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestSetupNonInteractiveSkipsPrompts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Info.plist"), `<plist><dict><key>CFBundleIdentifier</key><string>com.example.detected</string></dict></plist>`)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{"xcrun": true}}, Stdin: strings.NewReader("com.custom\n"), Root: root, Stdout: &out, Stderr: &stderr}
	if err := cli.Run(context.Background(), []string{"setup", "--non-interactive"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr.String(), "project_name") || strings.Contains(out.String(), "interactive=true") {
		t.Fatalf("out=%q stderr=%q", out.String(), stderr.String())
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BundleID != "com.example.detected" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestSetupInteractiveAcceptsCustomValues(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	input := "\n\n\ncom.custom.bundle\n" + strings.Repeat("\n", 10) + "make custom-build\n"
	var out bytes.Buffer
	var stderr bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{"bazelisk": true}}, Stdin: strings.NewReader(input), Root: root, Stdout: &out, Stderr: &stderr}
	if err := cli.Run(context.Background(), []string{"setup"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "interactive=true") || !strings.Contains(stderr.String(), "launch.commands.build") {
		t.Fatalf("out=%q stderr=%q", out.String(), stderr.String())
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BundleID != "com.custom.bundle" || loaded.Launch.Commands.Build != "make custom-build" {
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
	raw := `[{"AXLabel":"Largest Videos","role":"heading","AXFrame":"{{0, 10}, {200, 40}}","AXEnabled":true,"AXSubrole":"AXHeading","AXTitle":"Largest Videos","pid":123,"children":[{"AXIdentifier":"delete_button","AXLabel":"Delete","role":"button","enabled":false}]}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": raw}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"ok cmd=ui.tree", "node index=1", `label="Largest Videos"`, "frame=", "enabled=true", "subrole=AXHeading", `title="Largest Videos"`, "pid=123", "id=delete_button", "enabled=false"} {
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
			"start":          testExplicitScreen("start"),
			"largest-videos": testExplicitScreen("largest-videos"),
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
	raw := `[{"AXIdentifier":"mav.screen.largest-videos","role":"group","children":[{"AXLabel":"Largest Videos","role":"heading"},{"AXLabel":"Delete","role":"button"}]}]`
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

func TestUITreeRecognitionIsStableForSameCurrentScreen(t *testing.T) {
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
			"alpha": testExplicitScreen("alpha"),
			"beta":  testExplicitScreen("beta"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "run-tree", Dir: filepath.Join(os.TempDir(), "mav", "run-tree-stable")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "beta", run.ID)
	raw := `[{"AXIdentifier":"mav.screen.beta","role":"group","children":[{"AXLabel":"Shared","role":"heading"}]}]`
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": raw}}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	for i := 0; i < 2; i++ {
		var out bytes.Buffer
		cli.Stdout = &out
		if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
			t.Fatal(err)
		}
		if got := out.String(); !strings.Contains(got, "screen=beta") || strings.Contains(got, "recognized_screen=alpha") {
			t.Fatalf("unstable recognition on call %d: %q", i, got)
		}
	}
}

func TestUITreePendingTapPrefersScreenOtherThanPendingFrom(t *testing.T) {
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
			"alpha": testExplicitScreen("alpha"),
			"beta":  testExplicitScreen("beta"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "run-tree", Dir: filepath.Join(os.TempDir(), "mav", "run-tree-pending")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "alpha", run.ID)
	SetPendingMapAction(root, pendingMapAction{From: "alpha", ID: "next_button", Driver: "axe"})
	raw := `[{"AXIdentifier":"mav.screen.beta","role":"group","children":[{"AXLabel":"Shared","role":"heading"},{"AXLabel":"Beta","role":"heading"}]}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": raw}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "recognized_screen=beta") {
		t.Fatalf("got %q", got)
	}
	updated, err := LoadScreen(root, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Edges) != 1 || updated.Edges[0].To != "beta" || updated.Edges[0].ID != "next_button" {
		t.Fatalf("edges=%+v", updated.Edges)
	}
}

func TestUITreePrefersExplicitIdentityOverCurrentLaunch(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := DefaultAppMap("com.example.demo")
	m.Screens["home"] = testExplicitScreen("home")
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "run-tree", Dir: filepath.Join(os.TempDir(), "mav", "run-tree-launch")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "start", run.ID)
	raw := `[{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXLabel":"Home","role":"heading"}]}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": raw}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "recognized_screen=home") || !strings.Contains(got, "screen=unknown") {
		t.Fatalf("got %q", got)
	}
}

func TestUITreeNaturalIdentifierWinsOverExistingRecognizer(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":            DefaultAppMap("com.example.demo").Screens["start"],
			"old-details":      {ID: "old-details", Recognizers: []Recognizer{{Kind: "id", Value: "StaleDetailsView"}}},
			"upload-form-view": {ID: "upload-form-view", Recognizers: []Recognizer{{Kind: "id", Value: "UploadFormView"}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "run-tree", Dir: filepath.Join(os.TempDir(), "mav", "run-tree-natural-id")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	raw := `[{"AXIdentifier":"UploadFormView","role":"group"},{"AXIdentifier":"StaleDetailsView","role":"group"}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": raw}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "recognized_screen=upload-form-view") || strings.Contains(got, "recognized_screen=old-details") {
		t.Fatalf("got %q", got)
	}
}

func TestUITreePreferAppiumUsesSource(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()

	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree", "--prefer-driver", "appium"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"driver=appium", "id=EmailField", "value=Email", "enabled=true", "title=Email", "pid=321"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if !containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("source not requested: %v", *calls)
	}
}

func TestUITreeAutoFallsBackToAppiumForScreenIdentity(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.app",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
			"home":  testExplicitScreen("home"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	axeRaw := `[{"AXLabel":"Home","role":"heading"},{"AXLabel":"Email","role":"textField"}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui":                axeRaw,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"driver=appium", "recognized_screen=home", "screen_source=recognized", "id=mav.screen.home"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	home, err := LoadScreen(root, "home")
	if err != nil {
		t.Fatal(err)
	}
	if home.Driver != "appium" {
		t.Fatalf("home driver=%q", home.Driver)
	}
	if !containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("source not requested: %v", *calls)
	}
}

func TestUITreeAutoFallbackDoesNotTreatLaunchCurrentAsReadyWhilePending(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.app",
		Start: "start",
		Screens: map[string]Screen{
			"start": DefaultAppMap("com.example.app").Screens["start"],
			"home":  testExplicitScreen("home"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "start", run.ID)
	SetPendingMapAction(root, pendingMapAction{From: "start", ID: "Vender"})
	axeRaw := `[{"AXLabel":"Home","role":"heading"}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui":                axeRaw,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"driver=appium", "recognized_screen=home", "previous_screen=start"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if !containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("source not requested: %v", *calls)
	}
}

func TestTextInputWrapperDetection(t *testing.T) {
	raw := `[{"identifier":"TextAreaView.textAreaView","role":"group","children":[{"identifier":"inner","role":"XCUIElementTypeTextView"}]}]`
	if !treeNodeWithIDHasTextInputDescendant(raw, "TextAreaView.textAreaView") {
		t.Fatal("expected wrapper to be detected")
	}
	if treeNodeWithIDHasTextInputDescendant(raw, "inner") {
		t.Fatal("inner text input should not be treated as wrapper")
	}
}

func TestUITypeFailsWhenFocusMetadataShowsNoFocusedField(t *testing.T) {
	root, cfg, server, _ := setupAppiumTypeFocusTest(t, false)
	defer server.Close()
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n", "axe type hello": ""},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "type", "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "fail code=type_no_focused_field") {
		t.Fatalf("got %q", got)
	}
}

func TestUITypeReportsReceivedCharsWhenFocusedFieldChanges(t *testing.T) {
	root, cfg, server, _ := setupAppiumTypeFocusTest(t, true)
	defer server.Close()
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n", "axe type hello": ""},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "type", "hello"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "chars_sent=5") || !strings.Contains(got, "chars_received=5") {
		t.Fatalf("got %q", got)
	}
}

func TestUITypePreferAppiumUsesActiveElementValueEndpoint(t *testing.T) {
	root, cfg, server, calls := setupAppiumTypeFocusTest(t, true)
	defer server.Close()
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--prefer-driver", "appium", "ui", "type", "user@example.com"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "driver=appium") || !strings.Contains(got, "chars_sent=16") {
		t.Fatalf("got %q", got)
	}
	if !containsCall(*calls, "GET /wd/hub/session/s1/element/active") ||
		!containsCall(*calls, `POST /wd/hub/session/s1/element/el1/value {"value":["user@example.com"]}`) {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestUITapValueUsesAppiumPredicate(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--value", "Dirección de email"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "driver=appium") || !strings.Contains(got, `value="Dirección de email"`) {
		t.Fatalf("got %q", got)
	}
	if !containsCall(*calls, `"using":"predicate string"`) ||
		!containsCall(*calls, `value == 'Dirección de email'`) ||
		!containsCall(*calls, "/click") {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestUIEraseAndHideKeyboardUseAppium(t *testing.T) {
	root, cfg, server, calls := setupAppiumTypeFocusTest(t, true)
	defer server.Close()
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "erase", "--focused"}); err != nil {
		t.Fatal(err)
	}
	if err := cli.Run(context.Background(), []string{"ui", "hideKeyboard"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=ui.erase") || !strings.Contains(got, "ok cmd=ui.hideKeyboard") {
		t.Fatalf("got %q", got)
	}
	if !containsCall(*calls, "POST /wd/hub/session/s1/element/el1/clear {}") ||
		!containsCall(*calls, `POST /wd/hub/session/s1/execute/sync {"args":[],"script":"mobile: hideKeyboard"}`) {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestUIHideKeyboardRetriesAndVerifiesKeyboardDisappeared(t *testing.T) {
	sources := []string{
		`<App><XCUIElementTypeKeyboard name="Keyboard"/></App>`,
		`<App><XCUIElementTypeKeyboard name="Keyboard"/></App>`,
		`<App><XCUIElementTypeButton name="Submit" label="Submit"/></App>`,
	}
	root, cfg, server, calls := setupHideKeyboardSourceTest(t, sources)
	defer server.Close()
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "hideKeyboard"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=ui.hideKeyboard") || !strings.Contains(got, "verified=true") {
		t.Fatalf("got %q", got)
	}
	if !containsCall(*calls, `"script":"mobile: hideKeyboard"`) {
		t.Fatalf("hideKeyboard not called: %v", *calls)
	}
}

func TestUIHideKeyboardFailsWhenKeyboardRemainsVisible(t *testing.T) {
	sources := []string{
		`<App><XCUIElementTypeKeyboard name="Keyboard"/></App>`,
		`<App><XCUIElementTypeKeyboard name="Keyboard"/></App>`,
		`<App><XCUIElementTypeKeyboard name="Keyboard"/></App>`,
		`<App><XCUIElementTypeKeyboard name="Keyboard"/></App>`,
	}
	root, cfg, server, _ := setupHideKeyboardSourceTest(t, sources)
	defer server.Close()
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "hideKeyboard"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=ui_hide_keyboard_failed") || !strings.Contains(got, "reason=keyboard_still_visible") {
		t.Fatalf("got %q", got)
	}
}

func setupHideKeyboardSourceTest(t *testing.T, sources []string) (string, Config, *httptest.Server, *[]string) {
	t.Helper()
	calls := []string{}
	sourceIndex := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/session/s1/source":
			if sourceIndex >= len(sources) {
				sourceIndex = len(sources) - 1
			}
			_, _ = io.WriteString(w, `{"value":`+strconv.Quote(sources[sourceIndex])+`}`)
			sourceIndex++
		case r.URL.Path == "/wd/hub/session/s1/execute/sync":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM"}); err != nil {
		t.Fatal(err)
	}
	return root, cfg, server, &calls
}

func TestUITreeIncludeSystemUsesActiveAppSource(t *testing.T) {
	root, cfg, server, calls := setupAppiumSystemSourceTest(t)
	defer server.Close()

	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree", "--prefer-driver", "appium", "--include-system"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"driver=appium", "active_bundle=com.apple.PhotosUIService", "system_source=true", "system_overlay=true", "id=PickerSearchField"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if !containsCall(*calls, `POST /wd/hub/session/s1/execute/sync {"args":[],"script":"mobile: activeAppInfo"}`) {
		t.Fatalf("active app info not requested: %v", *calls)
	}
	if !containsCall(*calls, `POST /wd/hub/session/s1/appium/settings {"settings":{"defaultActiveApplication":"com.apple.PhotosUIService"}}`) {
		t.Fatalf("active app setting not updated: %v", *calls)
	}
}

func TestUITreeIncludeSystemAutoUsesAppiumBeforeAxe(t *testing.T) {
	root, cfg, server, calls := setupAppiumSystemSourceTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"appium driver list --installed": "xcuitest@7.0.0\n",
			"axe describe-ui":                `[{"AXLabel":"Target App","role":"heading"}]`,
		},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree", "--include-system"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "driver=appium") || !strings.Contains(got, "active_bundle=com.apple.PhotosUIService") {
		t.Fatalf("got %q", got)
	}
	if !containsCall(*calls, "mobile: activeAppInfo") {
		t.Fatalf("active app info not requested: %v", *calls)
	}
}

func TestUITreeIncludeSystemKeepsForegroundAppWhenActiveBundleMatches(t *testing.T) {
	calls := []string{}
	activeApplication := "com.example.app"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/wd/hub/session/s1/execute/sync":
			_, _ = io.WriteString(w, `{"value":{"bundleId":"com.example.app","name":"Demo","pid":42}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/wd/hub/session/s1/appium/settings":
			_, _ = io.WriteString(w, `{"value":{"defaultActiveApplication":"auto"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/wd/hub/session/s1/appium/settings":
			if strings.Contains(string(body), "springboard") {
				activeApplication = "com.apple.springboard"
			}
			_, _ = io.WriteString(w, `{"value":null}`)
		case r.URL.Path == "/wd/hub/session/s1/source":
			if activeApplication == "com.apple.springboard" {
				_, _ = io.WriteString(w, `{"value":"<AppiumAUT type=\"XCUIElementTypeApplication\" name=\"SpringBoard\"><XCUIElementTypeButton name=\"Home\" label=\"Home\"/></AppiumAUT>"}`)
			} else {
				_, _ = io.WriteString(w, `{"value":"<AppiumAUT type=\"XCUIElementTypeApplication\" name=\"Demo\"><XCUIElementTypeButton name=\"Upload\" label=\"Upload\"/></AppiumAUT>"}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
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
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree", "--prefer-driver", "appium", "--include-system"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "label=Upload") || strings.Contains(got, "SpringBoard") || containsCall(calls, "springboard") {
		t.Fatalf("got=%q calls=%v", got, calls)
	}
}

func TestUITreePreferAxeDoesNotFallbackToAppium(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	raw := `[{"role":"AXApplication","AXFrame":"{{0, 0}, {0, 0}}"}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     raw,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree", "--prefer-driver", "axe"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "fail code=ui_tree_empty") || !strings.Contains(got, "driver=axe") {
		t.Fatalf("got %q", got)
	}
	if containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("unexpected appium source request: %v", *calls)
	}
}

func TestIsWeakLaunchMatch(t *testing.T) {
	cases := []struct {
		name string
		in   uiTreeState
		want bool
	}{
		{name: "start_via_current_is_weak", in: uiTreeState{Screen: "start", ScreenSource: "current"}, want: true},
		{name: "start_via_recognized_is_strong", in: uiTreeState{Screen: "start", ScreenSource: "recognized"}, want: false},
		{name: "non_start_current_is_strong", in: uiTreeState{Screen: "home", ScreenSource: "current"}, want: false},
		{name: "explicit_id_match_is_strong", in: uiTreeState{Screen: "home", ScreenSource: "explicit_id"}, want: false},
		{name: "empty_state_is_not_weak", in: uiTreeState{}, want: false},
		{name: "identity_missing_is_not_weak", in: uiTreeState{Screen: "unknown", ScreenSource: "identity_missing"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWeakLaunchMatch(tc.in); got != tc.want {
				t.Fatalf("isWeakLaunchMatch(%+v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestWaitForTreeReadyProbesAppiumHostAppOnWeakLaunchMatch covers the
// recognition path that closes feedback v0.2.11 §5: AXe returns a tree where
// the only match for the synthetic `start` screen is its `kind: launch`
// recognizer (because the explicit screen identifier lives on a non-leaf
// container view that AXe's leaf-only traversal cannot see). The Appium
// describe-tree against the host app exposes the container id and
// `waitForTreeReady` must adopt the Appium tree so callers like `mav go`
// observe the real screen instead of timing out on `launch_tree_not_ready`.
func TestWaitForTreeReadyProbesAppiumHostAppOnWeakLaunchMatch(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	// AppMap with a synthetic `start` (launch recognizer) and a target screen
	// keyed by the explicit container id that the Appium fixture exposes.
	m := DefaultAppMap(cfg.BundleID)
	m.Screens["home"] = Screen{
		ID:          "home",
		Recognizers: []Recognizer{{Kind: "id", Value: "mav.screen.home"}},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	// Simulate `mav open` having stamped the synthetic `start` as the current
	// screen — this is what makes AXe match `start` via launch in observeUITree.
	SetCurrentScreen(root, "start", "run1")

	// AXe response: a non-empty tree with no explicit screen identity on any
	// element. The application root + a couple of leaves are enough for
	// observeUITree to fall through to the launch recognizer match.
	axeRaw := `[{"role":"AXApplication","AXLabel":"Demo","children":[{"role":"button","AXLabel":"Settings"},{"role":"button","AXLabel":"Help"}]}]`

	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     axeRaw,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}

	tree, err := cli.waitForTreeReady(context.Background(), cfg, 2*time.Second)
	if err != nil {
		t.Fatalf("waitForTreeReady err=%v", err)
	}
	if tree.Driver != "appium" {
		t.Fatalf("expected Appium tree to be adopted, got driver=%q", tree.Driver)
	}
	hasContainer := false
	for _, el := range ExtractElements(tree.Raw) {
		if el.ID == "mav.screen.home" {
			hasContainer = true
			break
		}
	}
	if !hasContainer {
		t.Fatalf("expected explicit container id in adopted tree, raw=%s", tree.Raw)
	}
	if !containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("expected Appium /source request to be made, calls=%v", *calls)
	}
}

func TestUITreeAutoFallsBackToAppiumForEmptyTree(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	raw := `[{"role":"AXApplication","AXFrame":"{{0, 0}, {0, 0}}"}]`
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     raw,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "driver=appium") || !containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("got %q calls=%v", got, *calls)
	}
}

func TestUITapAutoFallsBackToAppiumByID(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: appiumFallbackRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--id", "checkout_button"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "driver=appium") || !strings.Contains(got, "fallback=axe") {
		t.Fatalf("got %q", got)
	}
	if !containsCall(*calls, `"using":"accessibility id"`) || !containsCall(*calls, `"value":"checkout_button"`) || !containsCall(*calls, "/click") {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestUITapAutoFallsBackToAppiumPredicateForText(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: appiumFallbackRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--text", "Email"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "driver=appium") || !strings.Contains(got, "fallback=axe") {
		t.Fatalf("got %q", got)
	}
	if !containsCall(*calls, `"using":"predicate string"`) ||
		!containsCall(*calls, "value == 'Email' OR name == 'Email' OR label == 'Email'") ||
		!containsCall(*calls, "/click") {
		t.Fatalf("calls=%v", *calls)
	}
}

func TestUITapAppiumRetriesTextPredicateWithClassChain(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/session/s1/element" && strings.Contains(string(body), `"using":"predicate string"`):
			http.Error(w, `{"value":{"error":"invalid selector","message":"Locator Strategy 'predicate string' is not supported for this session"}}`, http.StatusBadRequest)
		case r.URL.Path == "/wd/hub/session/s1/element" && strings.Contains(string(body), `"using":"-ios class chain"`):
			_, _ = io.WriteString(w, `{"value":{"element-6066-11e4-a52e-4f735466cecf":"el1"}}`)
		case r.URL.Path == "/wd/hub/session/s1/element/el1/click":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
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
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--text", "Email", "--prefer-driver", "appium"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=ui.tap") || !strings.Contains(got, "driver=appium") {
		t.Fatalf("got %q", got)
	}
	if !containsCall(calls, `"using":"predicate string"`) ||
		!containsCall(calls, `"using":"-ios class chain"`) ||
		!containsCall(calls, "**/XCUIElementTypeAny") ||
		!containsCall(calls, "/click") {
		t.Fatalf("calls=%v", calls)
	}
}

func TestUITapAutoReportsAppiumErrorWhenAxeMissing(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		http.Error(w, "element not found", http.StatusNotFound)
	}))
	defer server.Close()
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
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--id", "missing_button"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=ui_tap_failed") || !strings.Contains(got, "tool=appium") {
		t.Fatalf("got %q calls=%v", got, calls)
	}
}

func TestUITapAutoReportsAppiumErrorWhenAxeFails(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		http.Error(w, "element not found", http.StatusNotFound)
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: appiumFallbackRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--id", "missing_button"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=ui_tap_failed") || !strings.Contains(got, "tool=appium") {
		t.Fatalf("got %q calls=%v", got, calls)
	}
	if !containsCall(calls, "/wd/hub/session/s1/element") {
		t.Fatalf("appium element lookup not attempted: %v", calls)
	}
}

func TestOpenWarmAppiumCreatesSession(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.SimulatorName = "iPhone"
	cfg.Tools = map[string]bool{"appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/wd/hub/status":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":{"ready":true}}`)), Header: http.Header{}}, nil
		case "/wd/hub/session":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":{"sessionId":"warm-session"}}`)), Header: http.Header{}}, nil
		default:
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`not found`)), Header: http.Header{}}, nil
		}
	})}
	defer func() { http.DefaultClient = oldClient }()
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open", "--warm-appium"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "appium_warmup=ok") || !strings.Contains(got, "appium_session=warm-session") {
		t.Fatalf("got %q", got)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readAppiumSession(run); err != nil {
		t.Fatalf("appium session not persisted: %v", err)
	}
}

func TestWaitForTreeReadyFallsBackToAppiumForUnmatchedTree(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := AppMap{
		AppID: "com.example.app",
		Start: "home",
		Screens: map[string]Screen{
			"home": testExplicitScreen("home"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", "run-appium")
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     `[{"AXLabel":"Buy","role":"button"}]`,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	tree, err := cli.waitForTreeReady(context.Background(), cfg, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tree.Raw, "EmailField") || tree.Driver != "appium" || !containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("tree=%+v calls=%v", tree, *calls)
	}
}

func TestWaitForTreeReadyReturnsAppiumDriverForScreenIdentityFallback(t *testing.T) {
	root, cfg, server, _ := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.app",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
			"home":  testExplicitScreen("home"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     `[{"AXLabel":"Home","role":"heading"}]`,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	tree, err := cli.waitForTreeReady(context.Background(), cfg, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Driver != "appium" || !strings.Contains(tree.Raw, "mav.screen.home") {
		t.Fatalf("tree=%+v", tree)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveScreenDetailedWithDriver(root, cfg, run, tree.Raw, tree.Driver); err != nil {
		t.Fatal(err)
	}
	home, err := LoadScreen(root, "home")
	if err != nil {
		t.Fatal(err)
	}
	if home.Driver != "appium" {
		t.Fatalf("home driver=%q", home.Driver)
	}
}

func TestWaitForTreeReadyKeepsAxeDriverWhenAxeRecognizesWithWarmAppium(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.app",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
			"home":  testExplicitScreen("home"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     `[{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXLabel":"Home","role":"heading"}]}]`,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	tree, err := cli.waitForTreeReady(context.Background(), cfg, 350*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Driver != "axe" || !strings.Contains(tree.Raw, "mav.screen.home") {
		t.Fatalf("tree=%+v", tree)
	}
	if containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("appium source should not be requested: %v", *calls)
	}
}

func TestWaitForTreeReadyUsesAppiumWhenItIsOnlyTreeTool(t *testing.T) {
	root, cfg, server, calls := setupAppiumSemanticTest(t)
	defer server.Close()
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.app",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
			"home":  testExplicitScreen("home"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	tree, err := cli.waitForTreeReady(context.Background(), cfg, 350*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Driver != "appium" || !strings.Contains(tree.Raw, "mav.screen.home") {
		t.Fatalf("tree=%+v", tree)
	}
	if !containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("appium source not requested: %v", *calls)
	}
}

func TestWaitForTreeReadyAcceptsExplicitUnmappedScreen(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := AppMap{
		AppID: "com.example.app",
		Start: "home",
		Screens: map[string]Screen{
			"home": testExplicitScreen("home"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", "run-axe")
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM": `[{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"}]}]`,
		},
	}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	tree, err := cli.waitForTreeReady(context.Background(), cfg, 350*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tree.Raw, "mav.screen.settings") || tree.Driver != "axe" {
		t.Fatalf("tree=%+v", tree)
	}
}

func TestWaitForTreeReadyDoesNotAcceptUnmatchedTreeWhenAppiumFails(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		http.Error(w, "source unavailable", http.StatusInternalServerError)
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := AppMap{
		AppID: "com.example.app",
		Start: "home",
		Screens: map[string]Screen{
			"home": testExplicitScreen("home"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", "run-appium")
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     `[{"AXLabel":"Buy","role":"button"},{"AXLabel":"Other","role":"button"}]`,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	tree, err := cli.waitForTreeReady(context.Background(), cfg, 350*time.Millisecond)
	if err == nil {
		t.Fatalf("expected tree_not_ready, got tree=%+v", tree)
	}
	if !strings.Contains(err.Error(), "tree_not_ready") {
		t.Fatalf("err=%v", err)
	}
	if !containsCall(calls, "/wd/hub/session/s1/source") {
		t.Fatalf("appium source not attempted: %v", calls)
	}
}

func TestGoFailsEarlyWhenRequiredAppiumWarmupFails(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"axe": true, "xcrun": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home":     testExplicitScreenWithEdges("home", Edge{To: "settings", ID: "settings_button", Driver: "appium"}),
			"settings": testExplicitScreen("settings"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/wd/hub/status":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":{"ready":true}}`)), Header: http.Header{}}, nil
		case "/wd/hub/session":
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{"value":{"error":"session not created"}}`)), Header: http.Header{}}, nil
		default:
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`not found`)), Header: http.Header{}}, nil
		}
	})}
	defer func() { http.DefaultClient = oldClient }()
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"go", "settings"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=required_driver_missing") ||
		!strings.Contains(got, "required_driver=appium") ||
		!strings.Contains(got, "issue=appium_warmup_failed") {
		t.Fatalf("got %q", got)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	commands, err := os.ReadFile(run.Commands)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(commands), "mav stop") {
		t.Fatalf("expected stop command, commands=%s", commands)
	}
}

func setupAppiumSemanticTest(t *testing.T) (string, Config, *httptest.Server, *[]string) {
	t.Helper()
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/session/s1/source":
			_, _ = io.WriteString(w, `{"value":"<AppiumAUT type=\"XCUIElementTypeApplication\" name=\"Demo\"><XCUIElementTypeOther name=\"mav.screen.home\"><XCUIElementTypeTextField name=\"EmailField\" label=\"\" value=\"Email\" enabled=\"true\" title=\"Email\" pid=\"321\" x=\"10\" y=\"20\" width=\"100\" height=\"40\"/></XCUIElementTypeOther></AppiumAUT>"}`)
		case r.URL.Path == "/wd/hub/session/s1/element":
			_, _ = io.WriteString(w, `{"value":{"element-6066-11e4-a52e-4f735466cecf":"el1"}}`)
		case r.URL.Path == "/wd/hub/session/s1/element/el1/click":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
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
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	return root, cfg, server, &calls
}

func setupAppiumSystemSourceTest(t *testing.T) (string, Config, *httptest.Server, *[]string) {
	t.Helper()
	calls := []string{}
	activeApplication := "com.example.app"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/wd/hub/session/s1/execute/sync":
			_, _ = io.WriteString(w, `{"value":{"bundleId":"com.apple.PhotosUIService","name":"Photos","pid":42}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/wd/hub/session/s1/appium/settings":
			_, _ = io.WriteString(w, `{"value":{"defaultActiveApplication":"auto"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/wd/hub/session/s1/appium/settings":
			if strings.Contains(string(body), "PhotosUIService") {
				activeApplication = "com.apple.PhotosUIService"
			} else {
				activeApplication = "com.example.app"
			}
			_, _ = io.WriteString(w, `{"value":null}`)
		case r.URL.Path == "/wd/hub/session/s1/source":
			if activeApplication == "com.apple.PhotosUIService" {
				_, _ = io.WriteString(w, `{"value":"<AppiumAUT type=\"XCUIElementTypeApplication\" name=\"Photos\"><XCUIElementTypeTextField name=\"PickerSearchField\" label=\"Search\" value=\"\" focused=\"true\"/></AppiumAUT>"}`)
			} else {
				_, _ = io.WriteString(w, `{"value":"<AppiumAUT type=\"XCUIElementTypeApplication\" name=\"Demo\"><XCUIElementTypeButton name=\"Upload\" label=\"Upload\"/></AppiumAUT>"}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
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
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	return root, cfg, server, &calls
}

func setupAppiumTypeFocusTest(t *testing.T, focused bool) (string, Config, *httptest.Server, *[]string) {
	t.Helper()
	calls := []string{}
	sourceCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/wd/hub/session/s1/execute/sync":
			if strings.Contains(string(body), "hideKeyboard") {
				_, _ = io.WriteString(w, `{"value":null}`)
			} else {
				_, _ = io.WriteString(w, `{"value":{"bundleId":"com.example.app","name":"Demo","pid":42}}`)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/wd/hub/session/s1/element/active":
			_, _ = io.WriteString(w, `{"value":{"element-6066-11e4-a52e-4f735466cecf":"el1"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/wd/hub/session/s1/element/el1/value":
			_, _ = io.WriteString(w, `{"value":null}`)
		case r.Method == http.MethodPost && r.URL.Path == "/wd/hub/session/s1/element/el1/clear":
			_, _ = io.WriteString(w, `{"value":null}`)
		case r.URL.Path == "/wd/hub/session/s1/source":
			sourceCalls++
			focusedValue := "false"
			value := ""
			if focused {
				focusedValue = "true"
				if sourceCalls > 1 {
					value = "hello"
				}
			}
			_, _ = io.WriteString(w, `{"value":"<AppiumAUT type=\"XCUIElementTypeApplication\" name=\"Demo\"><XCUIElementTypeTextView name=\"Description\" value=\"`+value+`\" focused=\"`+focusedValue+`\"/></AppiumAUT>"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"appium": true, "node": true, "axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	return root, cfg, server, &calls
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if strings.Contains(call, want) {
			return true
		}
	}
	return false
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

func TestSetupAppiumFallsBackToXCUITest8WhenServer2RejectsLatest(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"npm": true, "node": true, "appium": true},
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
		err: map[string]CommandResult{
			"appium driver install xcuitest": {
				Stderr: "Error: 'xcuitest' cannot be installed because the server version it requires (^3.0.0-rc.2) does not meet the currently installed one (2.19.0). Please install a compatible server version first.",
				Err:    os.ErrInvalid,
			},
		},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"setup", "--install", "appium"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"npm install -g appium",
		"appium driver install xcuitest",
		"appium driver install xcuitest@8",
		"appium driver list --installed",
	}
	if strings.Join(runner.commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands=%q want=%q", runner.commands, want)
	}
	if !strings.Contains(out.String(), "ok cmd=setup") {
		t.Fatalf("got %q", out.String())
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

func TestDoctorReportsXCUITestVersionAndPredicateSupport(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: map[string]bool{"go": true, "bazelisk": true, "xcrun": true, "axe": true, "idb": true, "node": true, "npm": true, "appium": true},
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "xcuitest_driver_version=8.4.3") ||
		!strings.Contains(got, "predicate_supported=false") {
		t.Fatalf("got %q", got)
	}
}

func TestAppiumWarmupErrorResultUsesSpecificIssues(t *testing.T) {
	missing := appiumWarmupErrorResult(appiumError{Code: "tool_missing", Message: "xcuitest driver missing", Next: "mav setup --install appium"})
	if missing.Issue != "xcuitest_driver_missing" || missing.Next == "" {
		t.Fatalf("missing=%+v", missing)
	}
	session := appiumWarmupErrorResult(appiumError{Code: "session_create_failed", Message: "session not created"})
	if session.Issue != "session_create_failed" {
		t.Fatalf("session=%+v", session)
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
			"home":     testExplicitScreenWithEdges("home", Edge{To: "settings", Text: "Settings", Wait: "1"}),
			"settings": testExplicitScreen("settings"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := fakeRunner{
		tools: map[string]bool{"axe": true, "xcrun": true},
		seq: map[string][]string{"axe describe-ui": {
			`{"AXIdentifier":"mav.screen.home","children":[{"AXLabel":"Settings"}]}`,
			`{"AXIdentifier":"mav.screen.home","children":[{"AXLabel":"Settings"}]}`,
			`{"AXIdentifier":"mav.screen.settings","children":[{"AXLabel":"Settings"},{"AXLabel":"Daily Reminder"}]}`,
			`{"AXIdentifier":"mav.screen.settings","children":[{"AXLabel":"Settings"},{"AXLabel":"Daily Reminder"}]}`,
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

func TestRunFlowPersistsAppiumScreenFallbackAfterTap(t *testing.T) {
	root, cfg, server, _ := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.app",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "start", "run-flow")
	flowPath := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, flowPath, `
name: fallback-map
steps:
  - tap: { id: upload_button }
`)
	runner := fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe tap --id upload_button --udid SIM": "",
			"axe describe-ui --udid SIM":            `[{"AXLabel":"Home","role":"heading"}]`,
			"appium driver list --installed":        "xcuitest@7.0.0\n",
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=run") {
		t.Fatalf("got %q", out.String())
	}
	home, err := LoadScreen(root, "home")
	if err != nil {
		t.Fatal(err)
	}
	if home.Driver != "appium" || !screenHasExplicitScreenIdentity(home) {
		t.Fatalf("home=%+v", home)
	}
	start, err := LoadScreen(root, "start")
	if err != nil {
		t.Fatal(err)
	}
	if len(start.Edges) != 1 || start.Edges[0].To != "home" || start.Edges[0].Driver != "axe" {
		t.Fatalf("edges=%+v", start.Edges)
	}
}

func TestRunFlowPersistsAppiumScreenFallbackAfterEmptyAXTree(t *testing.T) {
	root, cfg, server, _ := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.app",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "start", "run-flow")
	flowPath := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, flowPath, `
name: fallback-map-empty
steps:
  - tap: { id: upload_button }
`)
	runner := fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe tap --id upload_button --udid SIM": "",
			"axe describe-ui --udid SIM":            `[{"role":"AXApplication","AXFrame":"{{0, 0}, {0, 0}}","children":[]}]`,
			"appium driver list --installed":        "xcuitest@7.0.0\n",
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatal(err)
	}
	home, err := LoadScreen(root, "home")
	if err != nil {
		t.Fatal(err)
	}
	if home.Driver != "appium" || !screenHasExplicitScreenIdentity(home) {
		t.Fatalf("home=%+v output=%q", home, out.String())
	}
}

func TestRunFlowPersistsChildStepScreenWithChildPreferDriver(t *testing.T) {
	root, cfg, server, _ := setupAppiumSemanticTest(t)
	defer server.Close()
	cfg.Tools["axe"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.app",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "start", "run-flow")
	flowPath := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, flowPath, `
name: child-driver-map
steps:
  - when: { id: gate_button, prefer-driver: axe }
    do:
      - tap: { id: upload_button, prefer-driver: appium }
`)
	runner := fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     `[{"AXUniqueId":"gate_button","AXLabel":"Gate"}]`,
			"appium driver list --installed": "xcuitest@7.0.0\n",
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatal(err)
	}
	home, err := LoadScreen(root, "home")
	if err != nil {
		t.Fatal(err)
	}
	if home.Driver != "appium" || !screenHasExplicitScreenIdentity(home) {
		t.Fatalf("home=%+v output=%q", home, out.String())
	}
	start, err := LoadScreen(root, "start")
	if err != nil {
		t.Fatal(err)
	}
	if len(start.Edges) != 1 || start.Edges[0].To != "home" || start.Edges[0].Driver != "appium" {
		t.Fatalf("edges=%+v", start.Edges)
	}
	if CurrentScreen(root) != "home" {
		t.Fatalf("current=%q", CurrentScreen(root))
	}
}

func TestGoAlreadyAtTargetAfterLaunchIsNoop(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true, "xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":    testExplicitScreen("start"),
			"settings": testExplicitScreen("settings"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := fakeRunner{
		tools: map[string]bool{"axe": true, "xcrun": true},
		seq: map[string][]string{"axe describe-ui": {
			`{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"},{"AXLabel":"Daily Reminder","role":"static text"}]}`,
			`{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"},{"AXLabel":"Daily Reminder","role":"static text"}]}`,
		}},
		calls: map[string]int{},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"go", "settings"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=go") || !strings.Contains(got, "already_at_target=true") || !strings.Contains(got, "steps=0") {
		t.Fatalf("got %q", got)
	}
}

func TestNavigateFailsWhenChangedTreeIsNotTarget(t *testing.T) {
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
			"home":     testExplicitScreenWithEdges("home", Edge{From: "home", To: "settings", ID: "settings_button", Driver: "axe"}),
			"profile":  testExplicitScreen("profile"),
			"settings": testExplicitScreen("settings"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "run-route", Dir: filepath.Join(t.TempDir(), "run-route")}
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", run.ID)
	runner := fakeRunner{
		tools: map[string]bool{"axe": true},
		seq: map[string][]string{"axe describe-ui": {
			`{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXIdentifier":"settings_button","AXLabel":"Settings","role":"button"}]}`,
			`{"AXIdentifier":"mav.screen.profile","role":"group","children":[{"AXLabel":"Profile","role":"heading"},{"AXLabel":"Name","role":"static text"}]}`,
		}},
		calls: map[string]int{},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.navigateToScreen(context.Background(), "settings")
	if err == nil || err.Error() != "route_target_not_observed" {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if fields["stuck_at"] != "profile" || fields["edge_target"] != "settings" {
		t.Fatalf("fields=%v", fields)
	}
	loaded, loadErr := LoadScreen(root, "home")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(loaded.Edges) != 1 || loaded.Edges[0].To != "settings" {
		t.Fatalf("route playback should not learn failed edge: %+v", loaded.Edges)
	}
	if _, ok := peekPendingMapAction(root); ok {
		t.Fatalf("route playback should not leave pending map action")
	}
}

func TestGoDoesNotReuseStartRouteWhenLaunchScreenDiffers(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true, "xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":      testExplicitScreenWithEdges("start", Edge{From: "start", To: "onboarding", ID: "next_button"}),
			"onboarding": testExplicitScreenWithEdges("onboarding", Edge{From: "onboarding", To: "settings", ID: "settings_button"}),
			"home":       testExplicitScreen("home"),
			"settings":   testExplicitScreen("settings"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true, "xcrun": true},
		out: map[string]string{
			"axe describe-ui": `{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXLabel":"Home","role":"heading"},{"AXLabel":"Settings","role":"button"}]}`,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"go", "settings"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=route_not_found") {
		t.Fatalf("got %q", got)
	}
	for _, command := range runner.commands {
		if strings.Contains(command, "axe tap") {
			t.Fatalf("should not execute stale start route, commands=%v", runner.commands)
		}
	}
}

func TestGoStartDoesNotSucceedWhenLaunchRecognizesDifferentScreen(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true, "xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
			"home":  testExplicitScreen("home"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: map[string]bool{"axe": true, "xcrun": true},
		out: map[string]string{
			"axe describe-ui": `{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXLabel":"Home","role":"heading"},{"AXLabel":"Settings","role":"button"}]}`,
		},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"go", "start"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=route_not_found") || strings.Contains(got, "already_at_target=true") {
		t.Fatalf("got %q", got)
	}
}

func TestLegacyCoordinateRouteStillExecutesIDBTap(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home":     testExplicitScreenWithEdges("home", Edge{From: "home", To: "settings", X: "10", Y: "20", Driver: "idb"}),
			"settings": testExplicitScreen("settings"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "run-coordinate", Dir: filepath.Join(t.TempDir(), "run-coordinate")}
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", run.ID)
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true, "idb": true},
		seq: map[string][]string{"axe describe-ui": {
			`{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXLabel":"Home","role":"heading"},{"AXLabel":"Settings","role":"button"}]}`,
			`{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"},{"AXLabel":"Daily Reminder","role":"static text"}]}`,
		}},
		calls: map[string]int{},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.navigateToScreen(context.Background(), "settings")
	if err != nil {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if fields["screen"] != "settings" {
		t.Fatalf("fields=%v", fields)
	}
	if !containsCall(runner.commands, "idb ui tap 10 20") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestRouteEdgeDriversDoesNotPersistAutoAsDriver(t *testing.T) {
	drivers := routeEdgeDrivers(Edge{To: "settings", ID: "settings_button"})
	if len(drivers) != 2 || drivers[0] != "" || drivers[1] != "appium" {
		t.Fatalf("drivers=%v", drivers)
	}
	drivers = routeEdgeDrivers(Edge{To: "settings", ID: "settings_button", Driver: "auto"})
	if len(drivers) != 2 || drivers[0] != "" || drivers[1] != "appium" {
		t.Fatalf("drivers=%v", drivers)
	}
}

func TestRoutePlaybackReportsTapFailureAndClearsPending(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home":     testExplicitScreenWithEdges("home", Edge{From: "home", To: "settings", X: "10", Y: "20", Driver: "idb"}),
			"settings": testExplicitScreen("settings"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "run-tap-fail", Dir: filepath.Join(t.TempDir(), "run-tap-fail")}
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", run.ID)
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui": `{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXLabel":"Home","role":"heading"},{"AXLabel":"Settings","role":"button"}]}`,
		},
	}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.navigateToScreen(context.Background(), "settings")
	if err == nil || err.Error() != "tap_failed" {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if _, ok := peekPendingMapAction(root); ok {
		t.Fatalf("tap failure should clear pending map action")
	}
}

func TestOutputFailureCode(t *testing.T) {
	code, ok := outputFailureCode("fail code=tool_missing tool=idb\n")
	if !ok || code != "tool_missing" {
		t.Fatalf("code=%q ok=%v", code, ok)
	}
	if _, ok := outputFailureCode("ok cmd=ui.tap driver=axe\n"); ok {
		t.Fatalf("ok output should not be failure")
	}
}

func TestCoordinateTapDoesNotCreatePendingMapAction(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.Tools = map[string]bool{"idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home": testExplicitScreen("home"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", "run1")
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "10", "--y", "20"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := peekPendingMapAction(root); ok {
		t.Fatalf("coordinate tap should not be recorded as pending map action")
	}
	got := out.String()
	if !strings.Contains(got, "driver=idb") || !strings.Contains(got, "route_recorded=false") {
		t.Fatalf("got %q", got)
	}
}

func TestMapVerifyWarnsAboutCoordinateAndDuplicateEdges(t *testing.T) {
	root := t.TempDir()
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home": testExplicitScreenWithEdges("home",
				Edge{To: "settings", ID: "open"},
				Edge{To: "profile", ID: "open"},
				Edge{To: "picker", X: "10", Y: "20"},
				Edge{From: "start", To: "settings", ID: "wrong_from"},
			),
			"settings": testExplicitScreen("settings"),
			"profile":  testExplicitScreen("profile"),
			"picker":   testExplicitScreen("picker"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"map", "verify"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"fail code=app_map_warnings", "coordinate_edges=1", "duplicate_selectors=1", "from_mismatches=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestGoWarmsAndForcesAppiumForMappedAppiumEdge(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.demo"
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"axe": true, "xcrun": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	m := AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home":     testExplicitScreenWithEdges("home", Edge{To: "settings", ID: "settings_button", Wait: "1", Driver: "appium"}),
			"settings": testExplicitScreen("settings"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	httpCalls := []string{}
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := []byte{}
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}
		httpCalls = append(httpCalls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/status":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":{"ready":true}}`)), Header: http.Header{}}, nil
		case r.URL.Path == "/wd/hub/session":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":{"sessionId":"s1"}}`)), Header: http.Header{}}, nil
		case r.URL.Path == "/wd/hub/session/s1/element":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":{"element-6066-11e4-a52e-4f735466cecf":"el1"}}`)), Header: http.Header{}}, nil
		case r.URL.Path == "/wd/hub/session/s1/element/el1/click":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":null}`)), Header: http.Header{}}, nil
		default:
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`not found`)), Header: http.Header{}}, nil
		}
	})}
	defer func() { http.DefaultClient = oldClient }()
	var out bytes.Buffer
	runner := fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
		seq: map[string][]string{"axe describe-ui --udid SIM": {
			`{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXLabel":"Home","role":"heading"},{"AXLabel":"Settings","role":"button"}]}`,
			`{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXLabel":"Home","role":"heading"},{"AXLabel":"Settings","role":"button"}]}`,
			`{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"},{"AXLabel":"Daily Reminder","role":"static text"}]}`,
			`{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"},{"AXLabel":"Daily Reminder","role":"static text"}]}`,
		}},
		calls: map[string]int{},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"go", "settings"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=go") || !strings.Contains(got, "required_driver=appium") {
		t.Fatalf("got %q", got)
	}
	if !containsCall(httpCalls, "/wd/hub/session ") || !containsCall(httpCalls, `"using":"accessibility id"`) || !containsCall(httpCalls, "/click") {
		t.Fatalf("httpCalls=%v", httpCalls)
	}
}

func TestRouteRequiredDriverChecksIntermediateScreens(t *testing.T) {
	m := AppMap{
		Start: "home",
		Screens: map[string]Screen{
			"home":     {ID: "home"},
			"picker":   {ID: "picker", Driver: "appium"},
			"settings": {ID: "settings"},
		},
	}
	route := []Edge{{To: "picker", ID: "picker_button"}, {To: "settings", ID: "done_button"}}
	if got := routeRequiredDriver(m, "settings", route); got != "appium" {
		t.Fatalf("driver=%q", got)
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

func TestUIWaitAcceptsTextAndValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		raw  string
		want string
	}{
		{name: "text", args: []string{"ui", "wait", "--text", "Privacy Policy", "--timeout", "1ms"}, raw: `{"AXLabel":"Privacy Policy"}`, want: `text="Privacy Policy"`},
		{name: "value", args: []string{"ui", "wait", "--value", "Email", "--timeout", "1ms"}, raw: `{"AXValue":"Email"}`, want: "value=Email"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := DefaultConfig(root)
			cfg.Tools = map[string]bool{"axe": true}
			if err := SaveConfig(root, cfg); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": tc.raw}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
			if err := cli.Run(context.Background(), tc.args); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			if !strings.Contains(got, "ok cmd=ui.wait") || !strings.Contains(got, tc.want) {
				t.Fatalf("got %q", got)
			}
		})
	}
}

func TestUIWaitMatchesSpecificElementFields(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools, out: map[string]string{"axe describe-ui": `{"AXLabel":"Email"}`}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "wait", "--value", "Email", "--timeout", "1ms"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=ui_wait_timeout") || !strings.Contains(got, "value=Email") {
		t.Fatalf("got %q", got)
	}
}

func TestUITapTextFailureReportsValueMatch(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[{"AXValue":"Email","role":"text field"}]`},
		err: map[string]CommandResult{
			"axe tap --label Email": {Stderr: "No accessibility element matched --label 'Email'", Err: os.ErrNotExist},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--prefer-driver", "axe", "ui", "tap", "--text", "Email"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"fail code=ui_tap_text_no_label_match", "matched_value=1", "matched_label=0", "--prefer-driver appium"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
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
			"start":            testExplicitScreen("start"),
			"photos-to-delete": testExplicitScreen("photos-to-delete"),
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
	for _, want := range []string{"screen=unknown", "previous_screen=photos-to-delete", "screen root"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "map_pending=true") {
		t.Fatalf("identity-missing tree should discard pending map action: %q", got)
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

func TestExecStepBindsJSONOutput(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AllowShell = true
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	bindings := flowExecBindings{}
	fields, err := cli.executeFlowStepBound(context.Background(), run, 1, FlowStep{Action: "exec", Params: map[string]string{
		"cmd": `printf '{"email":"seller@example.com","profile":{"code":42},"enabled":true}'`,
		"out": "credentials",
	}}, bindings)
	if err != nil {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if fields["out"] != "credentials" {
		t.Fatalf("fields=%v", fields)
	}
	if _, ok := bindings["credentials"]; !ok {
		t.Fatalf("bindings=%+v", bindings)
	}
	_, err = cli.executeFlowStepBound(context.Background(), run, 2, FlowStep{Action: "type", Params: map[string]string{"text": "${exec.credentials.email}-${exec.credentials.profile.code}-${exec.credentials.enabled}"}}, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if !containsCall(runner.commands, "axe type seller@example.com-42-true") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestExecStepBindsRawStringOutput(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AllowShell = true
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	bindings := flowExecBindings{}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 1, FlowStep{Action: "exec", Params: map[string]string{"cmd": "printf raw-token", "out": "token"}}, bindings); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 2, FlowStep{Action: "type", Params: map[string]string{"text": "${exec.token}"}}, bindings); err != nil {
		t.Fatal(err)
	}
	if !containsCall(runner.commands, "axe type raw-token") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestExecBindingErrors(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AllowShell = true
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	cli := CLI{Runner: &sequenceRecordingRunner{tools: cfg.Tools}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	bindings := flowExecBindings{}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 1, FlowStep{Action: "type", Params: map[string]string{"text": "${exec.missing}"}}, bindings); err == nil || !strings.Contains(err.Error(), "exec_binding_missing name=missing") {
		t.Fatalf("err=%v", err)
	}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 2, FlowStep{Action: "exec", Params: map[string]string{"cmd": "true", "out": "empty"}}, bindings); err == nil || err.Error() != "exec_output_missing" {
		t.Fatalf("err=%v", err)
	}
	sideEffect := filepath.Join(root, "should-not-exist")
	if fields, err := cli.executeFlowStepBound(context.Background(), run, 3, FlowStep{Action: "exec", Params: map[string]string{"cmd": "touch " + shellQuote(sideEffect), "out": "bad.name"}}, bindings); err == nil || err.Error() != "exec_out_invalid" || fields["out"] != "bad.name" {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if _, err := os.Stat(sideEffect); !os.IsNotExist(err) {
		t.Fatalf("invalid out command should not run, stat err=%v", err)
	}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 4, FlowStep{Action: "exec", Params: map[string]string{"cmd": `printf '{"email":"seller@example.com"}'`, "out": "credentials"}}, bindings); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 5, FlowStep{Action: "type", Params: map[string]string{"text": "${exec.credentials.password}"}}, bindings); err == nil || !strings.Contains(err.Error(), "exec_json_path_missing name=credentials path=password") {
		t.Fatalf("err=%v", err)
	}
}

func TestExecBindingSubstitutionDoesNotMutateStep(t *testing.T) {
	original := FlowStep{
		Action: "when",
		Params: map[string]string{"text": "${exec.token}"},
		Any:    []FlowCondition{{Text: "${exec.token}"}},
		Do:     []FlowStep{{Action: "type", Params: map[string]string{"text": "${exec.token}"}}},
	}
	bindings := flowExecBindings{"token": newFlowExecBinding("raw-token")}
	prepared, err := substituteExecBindingsInStep(original, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Params["text"] != "raw-token" || prepared.Any[0].Text != "raw-token" || prepared.Do[0].Params["text"] != "raw-token" {
		t.Fatalf("prepared=%+v", prepared)
	}
	if original.Params["text"] != "${exec.token}" || original.Any[0].Text != "${exec.token}" || original.Do[0].Params["text"] != "${exec.token}" {
		t.Fatalf("original mutated=%+v", original)
	}
}

func TestExecBindingSubstitutionTreatsInsertedPlaceholdersAsLiteral(t *testing.T) {
	bindings := flowExecBindings{
		"token": newFlowExecBinding("${exec.token}"),
		"tail":  newFlowExecBinding("done"),
	}
	got, err := substituteExecBindings("value=${exec.token};tail=${exec.tail}", bindings)
	if err != nil {
		t.Fatal(err)
	}
	if got != "value=${exec.token};tail=done" {
		t.Fatalf("got=%q", got)
	}
}

func TestExecBindingFromIncludeEnv(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AllowShell = true
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	writeTestFlow(t, filepath.Join(root, "main.yaml"), `
steps:
  - exec: { cmd: "printf '{\"email\":\"seller@example.com\"}'", out: credentials }
  - include:
      file: login.yaml
      env:
        EMAIL: "${exec.credentials.email}"
`)
	writeTestFlow(t, filepath.Join(root, "login.yaml"), `
steps:
  - type: "${env.EMAIL}"
`)
	flow, err := LoadFlow(filepath.Join(root, "main.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Steps) != 2 || flow.Steps[1].Params["text"] != "${exec.credentials.email}" {
		t.Fatalf("steps=%+v", flow.Steps)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	bindings := flowExecBindings{}
	for index, step := range flow.Steps {
		if _, err := cli.executeFlowStepBound(context.Background(), run, index+1, step, bindings); err != nil {
			t.Fatal(err)
		}
	}
	if !containsCall(runner.commands, "axe type seller@example.com") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestExecBindingInsideSkippedWhenDoIsNotResolved(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[{"AXUniqueId":"Other"}]`},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.executeFlowStepBound(context.Background(), run, 1, FlowStep{
		Action: "when",
		Params: map[string]string{"id": "Gate"},
		Do:     []FlowStep{{Action: "type", Params: map[string]string{"text": "${exec.later.value}"}}},
	}, flowExecBindings{})
	if err != nil {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if fields["matched"] != "false" || fields["skipped"] != "1" {
		t.Fatalf("fields=%v", fields)
	}
	if containsCall(runner.commands, "axe type") {
		t.Fatalf("commands=%v", runner.commands)
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

func TestFlowWhenExecutesDoBlockWhenVisible(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[{"AXUniqueId":"ToggleX","AXLabel":"Toggle"}]`},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.executeFlowStep(context.Background(), run, 1, FlowStep{
		Action: "when",
		Params: map[string]string{"id": "ToggleX"},
		Do:     []FlowStep{{Action: "tap", Params: map[string]string{"id": "ToggleX"}}},
	})
	if err != nil {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if fields["matched"] != "true" || fields["executed"] != "1" {
		t.Fatalf("fields=%v", fields)
	}
	if !containsCall(runner.commands, "axe describe-ui") || !containsCall(runner.commands, "axe tap --id ToggleX") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestFlowWhenSkipsDoBlockWhenNotVisible(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[{"AXUniqueId":"Other","AXLabel":"Other"}]`},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.executeFlowStep(context.Background(), run, 1, FlowStep{
		Action: "when",
		Params: map[string]string{"id": "ToggleX"},
		Do:     []FlowStep{{Action: "tap", Params: map[string]string{"id": "ToggleX"}}},
	})
	if err != nil {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if fields["matched"] != "false" || fields["skipped"] != "1" {
		t.Fatalf("fields=%v", fields)
	}
	if containsCall(runner.commands, "axe tap --id ToggleX") {
		t.Fatalf("tap should not run: %v", runner.commands)
	}
}

func TestFlowStepInheritsGlobalPreferDriver(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/session/s1/element":
			_, _ = io.WriteString(w, `{"value":{"element-6066-11e4-a52e-4f735466cecf":"el1"}}`)
		case r.URL.Path == "/wd/hub/session/s1/element/el1/click":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, flowPath, `
steps:
  - tap: { text: Continue }
`)
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--prefer-driver", "appium", "run", flowPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=run") || !containsCall(calls, "/wd/hub/session/s1/element/el1/click") {
		t.Fatalf("output=%q calls=%v", out.String(), calls)
	}
	if containsCall(runner.commands, "axe tap --label Continue") {
		t.Fatalf("global appium should bypass axe tap: %v", runner.commands)
	}
}

func TestFlowStepCanOverridePreferDriver(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, flowPath, `
steps:
  - tap: { text: Continue, prefer-driver: axe }
`)
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--prefer-driver", "appium", "run", flowPath}); err != nil {
		t.Fatal(err)
	}
	if !containsCall(runner.commands, "axe tap --label Continue") {
		t.Fatalf("step override should force axe: commands=%v output=%q", runner.commands, out.String())
	}
}

func TestFlowWaitUsesPreferredDriver(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		if r.URL.Path == "/wd/hub/session/s1/source" {
			_, _ = io.WriteString(w, `{"value":"<App><XCUIElementTypeButton label=\"Continuar\" enabled=\"true\"/></App>"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	_, err = cli.executeFlowStepWithOptions(context.Background(), GlobalOptions{PreferDriver: "appium"}, run, 1, FlowStep{
		Action: "wait",
		Params: map[string]string{"text": "Continuar", "timeout": "1ms"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsCall(calls, "/wd/hub/session/s1/source") {
		t.Fatalf("appium source not used: calls=%v", calls)
	}
	if containsCall(runner.commands, "axe describe-ui") {
		t.Fatalf("wait should not use axe when appium is preferred: %v", runner.commands)
	}
}

func TestFlowConditionAutoFallsBackToAppiumWhenAxeMisses(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		if r.URL.Path == "/wd/hub/session/s1/source" {
			_, _ = io.WriteString(w, `{"value":"<App><XCUIElementTypeTabBar><XCUIElementTypeButton name=\"Vender\" label=\"Vender\" enabled=\"true\"/></XCUIElementTypeTabBar></App>"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     `[{"AXLabel":"Home","role":"AXStaticText"}]`,
			"appium driver list --installed": "xcuitest@8.4.3\n",
		},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	matched, err := cli.evaluateSingleConditionWithPrefer(context.Background(), FlowCondition{ID: "Vender"}, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if !matched || !containsCall(calls, "/wd/hub/session/s1/source") {
		t.Fatalf("matched=%v calls=%v", matched, calls)
	}
}

func TestUIWaitUsesGlobalPreferDriver(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		if r.URL.Path == "/wd/hub/session/s1/source" {
			_, _ = io.WriteString(w, `{"value":"<App><XCUIElementTypeButton label=\"Continuar\" enabled=\"true\"/></App>"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--prefer-driver", "appium", "ui", "wait", "--text", "Continuar", "--timeout", "1ms"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=ui.wait") || !containsCall(calls, "/wd/hub/session/s1/source") {
		t.Fatalf("output=%q calls=%v", out.String(), calls)
	}
	if containsCall(runner.commands, "axe describe-ui") {
		t.Fatalf("manual wait should not use axe when appium is preferred: %v", runner.commands)
	}
}

func TestFlowScrollUntilUsesPreferredDriverForSwipe(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/session/s1/source":
			_, _ = io.WriteString(w, `{"value":"<App><XCUIElementTypeStaticText label=\"Other\" enabled=\"true\"/></App>"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/wd/hub/session/s1/actions":
			_, _ = io.WriteString(w, `{"value":null}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/wd/hub/session/s1/actions":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	_, err = cli.executeFlowStepWithOptions(context.Background(), GlobalOptions{PreferDriver: "appium"}, run, 1, FlowStep{
		Action: "scrollUntil",
		Params: map[string]string{"text": "Missing", "maxSwipes": "1"},
	})
	if err == nil || err.Error() != "scroll_until_timeout" {
		t.Fatalf("err=%v", err)
	}
	if !containsCall(calls, "POST /wd/hub/session/s1/actions") {
		t.Fatalf("appium swipe actions not used: calls=%v", calls)
	}
	if containsCall(runner.commands, "axe swipe") {
		t.Fatalf("scrollUntil should not use axe swipe when appium is preferred: %v", runner.commands)
	}
}

func TestFlowSwipeUsesPreferredDriver(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/wd/hub/session/s1/actions":
			_, _ = io.WriteString(w, `{"value":null}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/wd/hub/session/s1/actions":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	_, err = cli.executeFlowStepWithOptions(context.Background(), GlobalOptions{PreferDriver: "appium"}, run, 1, FlowStep{
		Action: "swipe",
		Params: map[string]string{"direction": "up"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsCall(calls, "POST /wd/hub/session/s1/actions") {
		t.Fatalf("appium swipe actions not used: calls=%v", calls)
	}
	if containsCall(runner.commands, "axe swipe") {
		t.Fatalf("flow swipe should not use axe when appium is preferred: %v", runner.commands)
	}
}

func TestUITapAutoRoutesContainerTextTapToAppium(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/session/s1/element":
			_, _ = io.WriteString(w, `{"value":{"element-6066-11e4-a52e-4f735466cecf":"el1"}}`)
		case r.URL.Path == "/wd/hub/session/s1/element/el1/click":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	raw := `[{"role":"AXTable","children":[{"role":"AXCell","children":[{"AXLabel":"Deporte y ocio","role":"button"}]}]}]`
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     raw,
			"appium driver list --installed": "xcuitest@8.4.3\n",
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--text", "Deporte y ocio"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "driver=appium") || !strings.Contains(got, "fallback_reason=container_tap") {
		t.Fatalf("got %q", got)
	}
	if containsCall(runner.commands, "axe tap --label Deporte y ocio") {
		t.Fatalf("container tap should route before axe tap: %v", runner.commands)
	}
	if !containsCall(calls, "/wd/hub/session/s1/element/el1/click") {
		t.Fatalf("calls=%v", calls)
	}
}

func TestUITapAutoRoutesSelfCellTapToAppium(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/session/s1/element":
			_, _ = io.WriteString(w, `{"value":{"element-6066-11e4-a52e-4f735466cecf":"el1"}}`)
		case r.URL.Path == "/wd/hub/session/s1/element/el1/click":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	raw := `[{"AXLabel":"Deporte y ocio","role":"AXCell"}]`
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     raw,
			"appium driver list --installed": "xcuitest@8.4.3\n",
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--text", "Deporte y ocio"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "driver=appium") || !strings.Contains(got, "fallback_reason=container_tap") {
		t.Fatalf("got %q", got)
	}
	if containsCall(runner.commands, "axe tap --label Deporte y ocio") {
		t.Fatalf("self cell tap should route before axe tap: %v", runner.commands)
	}
	if !containsCall(calls, "/wd/hub/session/s1/element/el1/click") {
		t.Fatalf("calls=%v", calls)
	}
}

func TestUITapAppiumRoutesTabBarItemToMobileTap(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/session/s1/source":
			_, _ = io.WriteString(w, `{"value":"<App><XCUIElementTypeTabBar><XCUIElementTypeButton name=\"Vender\" label=\"Vender\" x=\"160\" y=\"820\" width=\"94\" height=\"44\" enabled=\"true\"/></XCUIElementTypeTabBar></App>"}`)
		case r.URL.Path == "/wd/hub/session/s1/execute/sync":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
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
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: cfg.Tools,
		out:   map[string]string{"appium driver list --installed": "xcuitest@8.4.3\n"},
	}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--id", "Vender", "--prefer-driver", "appium"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=ui.tap") {
		t.Fatalf("output=%q", out.String())
	}
	if !containsCall(calls, `"script":"mobile: tap"`) || !containsCall(calls, `"x":207`) || !containsCall(calls, `"y":842`) {
		t.Fatalf("calls=%v", calls)
	}
	if containsCall(calls, "/element") {
		t.Fatalf("tab bar item should not use element click: calls=%v", calls)
	}
}

func TestUITapAutoRoutesTabBarItemToAppiumMobileTap(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.URL.Path == "/wd/hub/session/s1/source":
			_, _ = io.WriteString(w, `{"value":"<App><XCUIElementTypeTabBar><XCUIElementTypeButton name=\"Vender\" label=\"Vender\" x=\"160\" y=\"820\" width=\"94\" height=\"44\" enabled=\"true\"/></XCUIElementTypeTabBar></App>"}`)
		case r.URL.Path == "/wd/hub/session/s1/execute/sync":
			_, _ = io.WriteString(w, `{"value":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	raw := `[{"role":"AXTabBar","children":[{"AXIdentifier":"Vender","AXLabel":"Vender","role":"AXTab"}]}]`
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     raw,
			"appium driver list --installed": "xcuitest@8.4.3\n",
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--id", "Vender"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "driver=appium") || !strings.Contains(got, "fallback_reason=container_tap") {
		t.Fatalf("got %q", got)
	}
	if containsCall(runner.commands, "axe tap --id Vender") {
		t.Fatalf("tab tap should route before axe tap: %v", runner.commands)
	}
	if !containsCall(calls, `"script":"mobile: tap"`) {
		t.Fatalf("calls=%v", calls)
	}
}

func TestUITapContainerRouteFailsWhenAppiumFails(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		http.Error(w, "element not found", http.StatusNotFound)
	}))
	defer server.Close()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "appium": true, "node": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := writeAppiumSession(run, appiumSessionState{PID: 123, BaseURL: server.URL + appiumBasePath, SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}); err != nil {
		t.Fatal(err)
	}
	raw := `[{"AXLabel":"Deporte y ocio","role":"AXCell"}]`
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM":     raw,
			"appium driver list --installed": "xcuitest@8.4.3\n",
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--text", "Deporte y ocio"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=ui_tap_failed") || !strings.Contains(got, "fallback_reason=container_tap") {
		t.Fatalf("got %q", got)
	}
	if containsCall(runner.commands, "axe tap --label Deporte y ocio") {
		t.Fatalf("container tap should not fall back to silent axe success: %v", runner.commands)
	}
	if !containsCall(calls, "/wd/hub/session/s1/element") {
		t.Fatalf("calls=%v", calls)
	}
}

func TestUISwipePreferAxeDoesNotFallbackToIDB(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--prefer-driver", "axe", "ui", "swipe", "--direction", "up"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=tool_missing") || !strings.Contains(got, "tool=axe") {
		t.Fatalf("got %q", got)
	}
	if containsCall(runner.commands, "idb ui swipe") {
		t.Fatalf("prefer axe should not run idb: %v", runner.commands)
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

func TestOpenClearStateRunsUninstallBeforeInstall(t *testing.T) {
	root := t.TempDir()
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
	runner := &launchRecipeRunner{
		tools: map[string]bool{"xcrun": true},
		results: map[string]CommandResult{
			"make ios-app-path": {Stdout: "/tmp/App.app\n"},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open", "--clear-state"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.commands, "\n")
	uninstall := strings.Index(joined, "simctl uninstall")
	install := strings.Index(joined, "simctl install")
	if uninstall < 0 || install < 0 || uninstall > install {
		t.Fatalf("commands=%s", joined)
	}
}

func TestOpenClearStateRequiresInstallCommand(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Launch: `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &launchRecipeRunner{tools: map[string]bool{"xcrun": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open", "--clear-state"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=launch_step_failed") || !strings.Contains(got, "step=clear_state") || !strings.Contains(got, "clearState requires an install command") {
		t.Fatalf("got %q", got)
	}
}

func TestRunFlowFailsWhenOpenClearStateCannotInstall(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Launch: `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("steps:\n  - open: { clearState: true }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &launchRecipeRunner{tools: map[string]bool{"xcrun": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "fail code=open_failed") || !strings.Contains(got, "action=open") {
		t.Fatalf("got %q", got)
	}
}

func TestRunFlowFailsWhenEraseOrHideKeyboardFail(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "erase", body: "steps:\n  - erase: { focused: true }\n", want: "fail code=erase_failed"},
		{name: "hide", body: "steps:\n  - hideKeyboard: {}\n", want: "fail code=hide_keyboard_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := DefaultConfig(root)
			cfg.BundleID = "com.example.app"
			cfg.SimulatorUDID = "SIM"
			cfg.Tools = map[string]bool{}
			if err := SaveConfig(root, cfg); err != nil {
				t.Fatal(err)
			}
			flowPath := filepath.Join(root, "flow.yaml")
			if err := os.WriteFile(flowPath, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			cli := CLI{Runner: fakeRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
			if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); !strings.Contains(got, tc.want) {
				t.Fatalf("got %q", got)
			}
		})
	}
}

func TestMapPruneApplyWarningsRemovesCoordinateAndDuplicateEdges(t *testing.T) {
	root := t.TempDir()
	m := AppMap{
		AppID: "com.example.app",
		Start: "home",
		Screens: map[string]Screen{
			"home": testExplicitScreenWithEdges("home",
				Edge{To: "details", X: "100", Y: "200"},
				Edge{To: "settings", ID: "settings_button"},
				Edge{To: "profile", ID: "settings_button"},
				Edge{From: "other", To: "details", ID: "details_button"},
			),
			"details":  testExplicitScreen("details"),
			"settings": testExplicitScreen("settings"),
			"profile":  testExplicitScreen("profile"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.mapCommand([]string{"prune", "--apply-warnings"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "pruned=3") {
		t.Fatalf("got %q", got)
	}
	loaded, err := LoadAppMap(root)
	if err != nil {
		t.Fatal(err)
	}
	edges := loaded.Screens["home"].Edges
	if len(edges) != 1 || edges[0].To != "settings" {
		t.Fatalf("edges=%+v", edges)
	}
}

func TestRunFlowOptionalTapSkipsCommandFailure(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("name: optional_tap\nsteps:\n  - tap: { id: missing, optional: true }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "ok cmd=run") {
		t.Fatalf("got %q", got)
	}
}

func TestOpenRetriesBazelOutInstallFromWritableCopy(t *testing.T) {
	root := t.TempDir()
	appPath := filepath.Join(root, "bazel-out", "ios", "bin", "Demo.app")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appPath, "Info.plist"), []byte("plist"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(appPath, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(appPath, 0o755)
		_ = os.Chmod(filepath.Join(appPath, "Info.plist"), 0o644)
	})
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
	runner := &launchRetryRunner{tools: map[string]bool{"xcrun": true}, appPath: appPath}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "ok cmd=open") || !strings.Contains(got, "app.tmp") {
		t.Fatalf("got %q", got)
	}
	if runner.installCalls != 2 {
		t.Fatalf("installCalls=%d commands=%v", runner.installCalls, runner.commands)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "app.tmp", "Demo.app", "Info.plist")); err != nil {
		t.Fatalf("copied app missing: %v", err)
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

func TestGestureContainerKindFromRoleClassifiesGestureDrivenContainers(t *testing.T) {
	cases := []struct {
		role string
		want string
	}{
		{role: "XCUIElementTypeTabBar", want: "tabbar"},
		{role: "AXTabBar", want: "tabbar"},
		{role: "XCUIElementTypeSheet", want: "sheet"},
		{role: "AXActionSheet", want: "sheet"},
		{role: "XCUIElementTypeAlert", want: "alert"},
		{role: "AXUIAlertController", want: "alert"},
		{role: "XCUIElementTypePopover", want: "popover"},
		{role: "XCUIElementTypeButton", want: ""},
		{role: "AXOther", want: ""},
		{role: "  XCUIElementTypeSheet  ", want: "sheet"},
		{role: "", want: ""},
	}
	for _, tc := range cases {
		if got := gestureContainerKindFromRole(tc.role); got != tc.want {
			t.Fatalf("role=%q got=%q want=%q", tc.role, got, tc.want)
		}
	}
}

func TestFindGestureContainerTargetFrameMatchesContainerChildren(t *testing.T) {
	cases := []struct {
		name string
		tree string
		id   string
		text string
		want string
	}{
		{
			name: "tab bar child by id",
			tree: `{"role":"AXApp","children":[{"role":"AXTabBar","children":[{"role":"AXTab","AXIdentifier":"sell","AXFrame":"{{160, 820}, {94, 44}}"}]}]}`,
			id:   "sell",
			want: "{{160, 820}, {94, 44}}",
		},
		{
			name: "action sheet button by text",
			tree: `{"role":"AXApp","children":[{"role":"XCUIElementTypeSheet","children":[{"role":"XCUIElementTypeButton","AXLabel":"Take photo","AXFrame":"{{20, 600}, {335, 56}}"}]}]}`,
			text: "Take photo",
			want: "{{20, 600}, {335, 56}}",
		},
		{
			name: "alert button by id",
			tree: `{"role":"AXApp","children":[{"role":"XCUIElementTypeAlert","children":[{"role":"XCUIElementTypeButton","AXIdentifier":"Confirm","AXFrame":"{{40, 400}, {140, 44}}"}]}]}`,
			id:   "Confirm",
			want: "{{40, 400}, {140, 44}}",
		},
		{
			name: "popover child by id",
			tree: `{"role":"AXApp","children":[{"role":"XCUIElementTypePopover","children":[{"role":"XCUIElementTypeButton","AXIdentifier":"Edit","AXFrame":"{{20, 100}, {200, 44}}"}]}]}`,
			id:   "Edit",
			want: "{{20, 100}, {200, 44}}",
		},
		{
			name: "ignores buttons outside any gesture container",
			tree: `{"role":"AXApp","children":[{"role":"XCUIElementTypeButton","AXIdentifier":"Plain","AXFrame":"{{0, 0}, {44, 44}}"}]}`,
			id:   "Plain",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var node any
			if err := json.Unmarshal([]byte(tc.tree), &node); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, ok := findGestureContainerTargetFrame(node, tc.id, tc.text, "", "")
			if tc.want == "" {
				if ok {
					t.Fatalf("expected no match, got frame=%q", got)
				}
				return
			}
			if !ok || got != tc.want {
				t.Fatalf("got=%q ok=%v want=%q", got, ok, tc.want)
			}
		})
	}
}

func TestKnownInProcessOverlayBundlesIncludesPhotosAndPrivacyServices(t *testing.T) {
	required := []string{
		"com.apple.tccd",
		"com.apple.PrivacyKitUI",
		"com.apple.PhotosUIService",
		"com.apple.SafariViewService",
		"com.apple.MailCompositionService",
	}
	bundles := knownInProcessOverlayBundles()
	got := map[string]bool{}
	for _, b := range bundles {
		got[b] = true
	}
	for _, want := range required {
		if !got[want] {
			t.Fatalf("missing %q in %v", want, bundles)
		}
	}
}
