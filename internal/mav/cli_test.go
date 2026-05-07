package mav

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
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
	if !strings.Contains(got, "driver=appium") || !strings.Contains(got, "id=EmailField") || !strings.Contains(got, "value=Email") {
		t.Fatalf("got %q", got)
	}
	if !containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("source not requested: %v", *calls)
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
			"home": {ID: "home", AssertText: "Home"},
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
	raw, err := cli.waitForTreeReady(context.Background(), cfg, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "EmailField") || !containsCall(*calls, "/wd/hub/session/s1/source") {
		t.Fatalf("raw=%q calls=%v", raw, *calls)
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
			"home": {ID: "home", AssertText: "Home"},
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
	raw, err := cli.waitForTreeReady(context.Background(), cfg, 350*time.Millisecond)
	if err == nil {
		t.Fatalf("expected tree_not_ready, got raw=%q", raw)
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
			"home":     {ID: "home", Edges: []Edge{{To: "settings", ID: "settings_button", Driver: "appium"}}},
			"settings": {ID: "settings", AssertText: "Settings"},
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
			_, _ = io.WriteString(w, `{"value":"<AppiumAUT type=\"XCUIElementTypeApplication\" name=\"Demo\"><XCUIElementTypeTextField name=\"EmailField\" label=\"\" value=\"Email\" x=\"10\" y=\"20\" width=\"100\" height=\"40\"/></AppiumAUT>"}`)
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
			"home":     {ID: "home", Edges: []Edge{{To: "settings", ID: "settings_button", Wait: "1", Driver: "appium"}}},
			"settings": {ID: "settings", AssertText: "Settings"},
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
