package mav

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
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

func TestSetupLLDBDAPVerifiesSelectedXcodeTool(t *testing.T) {
	runner := &recordingRunner{result: CommandResult{Stdout: "/Applications/Xcode.app/Contents/Developer/usr/bin/lldb-dap\n"}}
	cli := CLI{Runner: runner, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	ok, err := cli.setupLLDBDAP(context.Background())
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if runner.command != "xcrun lldb-dap --help" {
		t.Fatalf("last command=%q", runner.command)
	}
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
	startPID int
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
	writeScreenshotForCommand(name, args)
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
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	writeStartedArtifact(name, args)
	if r.startPID == 0 {
		r.startPID = 123
	}
	pid := r.startPID
	r.startPID++
	return pid, nil
}

func writeScreenshotForCommand(name string, args []string) {
	switch name {
	case "axe":
		for i, arg := range args {
			if arg == "--output" && i+1 < len(args) {
				_ = writeTestPNG(args[i+1])
				return
			}
		}
	case "idb":
		if len(args) >= 2 && args[0] == "screenshot" {
			_ = writeTestPNG(args[1])
		}
	case "xcrun":
		for i, arg := range args {
			if arg == "screenshot" && i+1 < len(args) {
				_ = writeTestPNG(args[i+1])
				return
			}
		}
	}
}

func writeStartedArtifact(name string, args []string) {
	if name == "xcrun" {
		for i, arg := range args {
			if arg == "recordVideo" && i+1 < len(args) {
				_ = os.MkdirAll(filepath.Dir(args[len(args)-1]), 0o755)
				_ = os.WriteFile(args[len(args)-1], testMovieWithDuration(600, 1200), 0o644)
				return
			}
		}
	}
	if name == "mitmdump" {
		for i, arg := range args {
			if arg == "--set" && i+1 < len(args) && strings.HasPrefix(args[i+1], "hardump=") {
				path := strings.TrimPrefix(args[i+1], "hardump=")
				_ = os.MkdirAll(filepath.Dir(path), 0o755)
				har := `{"log":{"entries":[{"request":{"url":"https://api.example.com/refresh"},"response":{"status":200}},{"request":{"url":"https://cdn.example.com/image"},"response":{"status":304}}]}}`
				_ = os.WriteFile(path, []byte(har), 0o644)
				return
			}
		}
	}
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
		return CommandResult{}
	}
	// Steps that go through a driver (simctl/idb via the router) do not
	// pass through the shell, so they match against the full line. Without
	// this a test cannot simulate the failure of, say, clear_state's
	// uninstall.
	for needle, result := range r.results {
		if strings.Contains(command, needle) {
			return result
		}
	}
	return CommandResult{}
}

func (r *launchRecipeRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 0, nil
}

type deviceListRunner struct {
	tools   map[string]bool
	devices string
}

func (r deviceListRunner) LookPath(file string) (string, error) {
	if r.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (r deviceListRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	for i, arg := range args {
		if arg == "--json-output" && i+1 < len(args) {
			_ = os.WriteFile(args[i+1], []byte(r.devices), 0o644)
		}
	}
	return CommandResult{}
}

func (r deviceListRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
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

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if strings.Contains(call, want) {
			return true
		}
	}
	return false
}

func indexOfCall(calls []string, want string) int {
	for i, call := range calls {
		if strings.Contains(call, want) {
			return i
		}
	}
	return -1
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

func TestDoctorReportsBaguetteMultitouch(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: map[string]bool{"go": true, "bazelisk": true, "xcrun": true, "axe": true, "idb": true, "baguette": true},
	}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "multitouch=ok") || !strings.Contains(got, "multitouch_driver=baguette") {
		t.Fatalf("got %q", got)
	}
}

func TestDoctorRecommendsBaguetteWhenMissing(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{
		tools: map[string]bool{"go": true, "bazelisk": true, "xcrun": true, "axe": true, "idb": true},
	}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "multitouch=missing") ||
		!strings.Contains(got, `multitouch_next="mav setup --install baguette"`) {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, `next="mav setup --install baguette"`) {
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

func TestUITreeUsesResolvedAxeCapability(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		out: map[string]string{
			"axe describe-ui --udid SIM": "Application, 0x1, pid 1\n  Window, 0x2\n",
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--raw", "ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Application") {
		t.Fatalf("got %q", out.String())
	}
	if !containsCall(runner.commands, "axe describe-ui --udid SIM") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestCaptureUsesResolvedXcrunCapability(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: map[string]bool{"xcrun": true}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"capture", "--name", "probe"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=capture") {
		t.Fatalf("got %q", out.String())
	}
	if !containsCall(runner.commands, "xcrun simctl io SIM screenshot") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestOpenRefusesFreshSimulatorLockFromOtherProject(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM-LOCKED"
	cfg.Tools = map[string]bool{"xcrun": true}
	cfg.Launch = LaunchConfig{Mode: "already_installed", Commands: LaunchCommands{Launch: `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		removeSimulatorLock("SIM-LOCKED", other)
		_ = os.RemoveAll(run.Dir)
	})
	if err := writeSimulatorLock("SIM-LOCKED", run, other, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{"xcrun": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"open"}))
	if !strings.Contains(out.String(), "fail code=sim_locked") || !strings.Contains(out.String(), "SIM-LOCKED") {
		t.Fatalf("got %q", out.String())
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
	if !strings.Contains(out.String(), `idb_next="pipx install --python python3.12 fb-idb"`) {
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
	if !strings.Contains(out.String(), "multitouch=missing") || !strings.Contains(out.String(), `multitouch_next="mav setup --install baguette"`) {
		t.Fatalf("setup should report baguette next step: %q", out.String())
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

func TestNestedHelpAcceptsLongHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "flow", args: []string{"flow", "--help"}, want: "mav flow lint flow.yaml"},
		{name: "flow lint", args: []string{"flow", "lint", "--help"}, want: "Parses and validates"},
		{name: "ui tap", args: []string{"ui", "tap", "--help"}, want: "mav ui tap --id ID"},
		{name: "evidence report", args: []string{"evidence", "report", "--help"}, want: "verified evidence manifest"},
		{name: "network start", args: []string{"network", "start", "--help"}, want: "simulator HAR capture"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			cli := CLI{Runner: fakeRunner{}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
			if err := cli.Run(context.Background(), tc.args); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); !strings.Contains(got, tc.want) {
				t.Fatalf("missing %q in %q", tc.want, got)
			}
		})
	}
}

func TestHelpVerbAliases(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"tap", "--help"}, want: "mav ui tap --id ID"},
		{args: []string{"tree", "--help"}, want: "mav ui tree"},
		{args: []string{"help", "screenshot"}, want: "mav capture"},
	}
	for _, tc := range tests {
		var out bytes.Buffer
		cli := CLI{Runner: fakeRunner{}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), tc.args); err != nil {
			t.Fatal(err)
		}
		if got := out.String(); !strings.Contains(got, tc.want) {
			t.Fatalf("missing %q in %q", tc.want, got)
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

func TestTargetScreenSizeCachesPerUDID(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	cfg.SimulatorUDID = "SIM-SCREEN-CACHE-TEST"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	key := "axe describe-ui --udid " + cfg.SimulatorUDID
	first := `[{"AXLabel":"Root","role":"window","AXFrame":"{{0, 0}, {100, 200}}"}]`
	second := `[{"AXLabel":"Root","role":"window","AXFrame":"{{0, 0}, {999, 999}}"}]`
	runner := fakeRunner{
		tools: cfg.Tools,
		seq:   map[string][]string{key: {first, second}},
		calls: map[string]int{},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	w1, h1 := cli.targetScreenSize(context.Background(), cfg)
	if w1 != 100 || h1 != 200 {
		t.Fatalf("first call got (%d,%d), want (100,200)", w1, h1)
	}
	w2, h2 := cli.targetScreenSize(context.Background(), cfg)
	if w2 != w1 || h2 != h1 {
		t.Fatalf("expected cached screen size (%d,%d), got (%d,%d)", w1, h1, w2, h2)
	}
	if got := runner.calls[key]; got != 1 {
		t.Fatalf("expected axe describe-ui invoked once (cached on second call), got %d calls", got)
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
	allowFail(t, cli.Run(context.Background(), []string{"ui", "tree"}))
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
	allowFail(t, cli.Run(context.Background(), []string{"install-skills"}))
	if !strings.Contains(out.String(), "fail code=install_skills_unavailable") || !strings.Contains(out.String(), "tool=npx") {
		t.Fatalf("got %q", out.String())
	}
}

func TestSetupInstallsBaguette(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	runner := &sequenceRecordingRunner{tools: map[string]bool{"brew": true}}
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"setup", "--install", "baguette"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) == 0 || !strings.Contains(runner.commands[0], "brew install tddworks/tap/baguette") {
		t.Fatalf("commands=%v", runner.commands)
	}
	if !strings.Contains(out.String(), "ok cmd=setup") || !strings.Contains(out.String(), "installed=baguette") {
		t.Fatalf("got %q", out.String())
	}
}

func TestSetupRejectsAppium(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, DefaultConfig(root)); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"setup", "--install", "appium"}))
	if !strings.Contains(out.String(), "fail code=setup_unknown_tool") || !strings.Contains(out.String(), "tool=appium") {
		t.Fatalf("got %q", out.String())
	}
}

func TestPreferDriverRejectsAppium(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"--prefer-driver", "appium", "ui", "tree"}))
	if !strings.Contains(out.String(), "fail code=prefer_driver_invalid") {
		t.Fatalf("got %q", out.String())
	}
}

// TestPreferDriverAcceptsRegisteredNonAxeDriver locks in FIX B.1: --prefer-driver
// is validated against the driver registry, not a frozen auto|axe list, so a
// driver id that was rejected before (baguette was never in the switch) is now
// accepted. uiPress ignores opts.PreferDriver entirely (its Route call always
// hints "baguette"), so this uses ui swipe -- one of the handlers that actually
// calls normalizePreferDriver -- to exercise the validation path itself.
func TestPreferDriverAcceptsRegisteredNonAxeDriver(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"baguette": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--prefer-driver", "baguette", "ui", "swipe", "up"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "prefer_driver_invalid") {
		t.Fatalf("baguette should be a valid --prefer-driver value now: got %q", out.String())
	}
}

// TestPreferDriverUsageListsRegisteredDrivers locks in the other half of FIX
// B.1: the prefer_driver_invalid error's usage field must enumerate the ids
// Registry.All() actually returns, not the frozen "auto|axe" string.
func TestPreferDriverUsageListsRegisteredDrivers(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"--prefer-driver", "appium", "ui", "tree"}))
	got := out.String()
	if !strings.Contains(got, "fail code=prefer_driver_invalid") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "auto|axcli|axe|baguette|cua|idb|macsystem|mitmproxy|screencapture|simctl|simtime") {
		t.Fatalf("usage field should list registered drivers, got %q", got)
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
	allowFail(t, cli.Run(context.Background(), []string{"ui", "wait", "--value", "Email", "--timeout", "1ms"}))
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
	allowFail(t, cli.Run(context.Background(), []string{"--prefer-driver", "axe", "ui", "tap", "--text", "Email"}))
	got := out.String()
	for _, want := range []string{"fail code=ui_tap_text_no_label_match", "matched_value=1", "matched_label=0"} {
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

func TestOpenNoRelaunchSkipsLaunchRecipe(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Build:  "make mav-build",
		Launch: `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{tools: cfg.Tools}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open", "--no-relaunch"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=open") || !strings.Contains(got, "relaunch=false") {
		t.Fatalf("got %q", got)
	}
	if containsCall(runner.commands, "make mav-build") || containsCall(runner.commands, "simctl launch") {
		t.Fatalf("launch recipe should not run: %v", runner.commands)
	}
}

func TestOpenBootsSelectedSimulatorBeforeInstall(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM-1"
	cfg.Tools = map[string]bool{"xcrun": true}
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		AppPath: "./app-path",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
		Launch:  `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{
		tools: cfg.Tools,
		results: map[string]CommandResult{
			"./app-path": {Stdout: "/tmp/Demo.app\n"},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open"}); err != nil {
		t.Fatal(err)
	}
	boot := indexOfCall(runner.commands, "xcrun simctl boot SIM-1")
	status := indexOfCall(runner.commands, "xcrun simctl bootstatus SIM-1 -b")
	install := indexOfCall(runner.commands, "simctl install")
	if boot < 0 || status < 0 || install < 0 {
		t.Fatalf("missing boot/status/install in commands=%v output=%s", runner.commands, out.String())
	}
	if boot > install || status > install {
		t.Fatalf("simulator should boot before install: %v", runner.commands)
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
	binding, ok := bindings["credentials"]
	if !ok {
		t.Fatalf("binding missing")
	}
	if !binding.HasJSON {
		t.Fatalf("binding not parsed as JSON: %+v", binding)
	}
}

func TestExecStepBindsRawStringOutput(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AllowShell = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	runner := &sequenceRecordingRunner{}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	bindings := flowExecBindings{}
	_, err = cli.executeFlowStepBound(context.Background(), run, 1, FlowStep{Action: "exec", Params: map[string]string{
		"cmd": `printf 'plain'`,
		"out": "name",
	}}, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if bindings["name"].Raw != "plain" || bindings["name"].HasJSON {
		t.Fatalf("binding=%+v", bindings["name"])
	}
}

func TestExecBindingErrors(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AllowShell = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	cli := CLI{Runner: &sequenceRecordingRunner{}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	bindings := flowExecBindings{}
	_, err = cli.executeFlowStepBound(context.Background(), run, 1, FlowStep{Action: "exec", Params: map[string]string{
		"cmd": "printf ''",
		"out": "result",
	}}, bindings)
	if err == nil || err.Error() != "exec_output_missing" {
		t.Fatalf("err=%v", err)
	}
}

func TestLogsReadsCurrentRunFile(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.LogsPath, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", run.ID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=logs") || !strings.Contains(out.String(), "matches=2") {
		t.Fatalf("got %q", out.String())
	}
}

// TestLogsReportsPinnedTargetUDID covers the MAV_TARGET_UDID / explicitly
// selected simulator case: cfg.SimulatorUDID is set, and any command's
// success output -- not just sim.select/sim.boot, which already reported it
// -- should say which target it acted on, so a hot-path agent driving mav
// command-by-command can keep pinning its next calls to the same device
// instead of guessing.
func TestLogsReportsPinnedTargetUDID(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM-PINNED"
	cfg.SimulatorName = "iPhone 17 Pro"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.LogsPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", run.ID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "udid=SIM-PINNED") {
		t.Fatalf("got %q, want udid=SIM-PINNED", out.String())
	}
	if !strings.Contains(out.String(), "target_kind=simulator") {
		t.Fatalf("got %q, want target_kind=simulator", out.String())
	}
}

// TestLogsResolvesBootedSimulatorUDIDWhenUnset covers the now-common case
// (see config.go's MAV_TARGET_KIND/MAV_TARGET_UDID handling) where a
// project's config carries no simulator_udid at all, so every command
// implicitly targets "whatever simulator is booted". The report should
// resolve and show the concrete UDID that fell out of, rather than leaving
// that implicit.
func TestLogsResolvesBootedSimulatorUDIDWhenUnset(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.LogsPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bootedJSON := `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-0":[` +
		`{"udid":"BOOTED-FALLBACK","name":"iPhone 17","state":"Booted"}]}}`
	runner := fakeRunner{out: map[string]string{
		"xcrun simctl list devices booted -j": bootedJSON,
	}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", run.ID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "udid=BOOTED-FALLBACK") {
		t.Fatalf("got %q, want udid=BOOTED-FALLBACK", out.String())
	}
}

// TestBootedSimulatorUDIDResolvedOnceThenCachedForRun is the actual
// regression for the perf issue: `xcrun simctl list devices booted -j`
// measures ~0.75s regardless of how it's invoked, and mav starts a new
// process per command, so re-running it on every command in a hot-path
// navigation (dozens of `mav ui tap` / `mav logs` calls against one run)
// would add tens of seconds. Two separate commands against the same run
// must resolve the booted device only once between them.
func TestBootedSimulatorUDIDResolvedOnceThenCachedForRun(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(run.LogsPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bootedJSON := `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-0":[` +
		`{"udid":"BOOTED-ONCE","name":"iPhone 17","state":"Booted"}]}}`
	bootedKey := "xcrun simctl list devices booted -j"
	runner := fakeRunner{
		out:   map[string]string{bootedKey: bootedJSON},
		calls: map[string]int{},
	}
	// fakeRunner only counts calls for keys registered under seq; give it a
	// single-answer sequence for the booted-detection key so runner.calls
	// tracks how many times it was actually invoked.
	runner.seq = map[string][]string{bootedKey: {bootedJSON}}

	for i := 0; i < 2; i++ {
		var out bytes.Buffer
		cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), []string{"logs", "--run", run.ID}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if !strings.Contains(out.String(), "udid=BOOTED-ONCE") {
			t.Fatalf("call %d: got %q, want udid=BOOTED-ONCE", i, out.String())
		}
	}
	if got := runner.calls[bootedKey]; got != 1 {
		t.Fatalf("booted-detection calls=%d, want 1 (second command should have hit the run-scoped cache)", got)
	}
}

func TestLogsFiltersMAVKey(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	lines := "MAV_LOG event=ui key=alpha\nMAV_LOG event=ui key=beta\nMAV_LOG event=ui key=alpha\n"
	if err := os.WriteFile(run.LogsPath, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--key", "alpha", "--run", run.ID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "matches=2") {
		t.Fatalf("got %q", out.String())
	}
}

func TestStopTerminatesRunProcesses(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	// emulate process record with pid that won't exist
	appendProcess(run, "probe-logs", 0, "fake")
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"stop", "--run", run.ID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=stop") {
		t.Fatalf("got %q", out.String())
	}
}

func TestEvidenceStartRejectsRunWithExistingVideo(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, "video.mov"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"evidence", "start", "--run", run.ID}))
	if !strings.Contains(out.String(), "fail code=evidence_run_not_clean") {
		t.Fatalf("got %q", out.String())
	}
}

func TestEvidenceReportReportsMissingVideo(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"evidence", "report", "--run", run.ID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "video=missing") {
		t.Fatalf("got %q", out.String())
	}
	if !strings.Contains(out.String(), "next=") || !strings.Contains(out.String(), "report.html") {
		t.Fatalf("missing html next hint: %q", out.String())
	}
}

func TestEvidenceStopTranscodesMP4(t *testing.T) {
	root := t.TempDir()
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, "video.pid"), []byte("123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mov := filepath.Join(run.Dir, "video.mov")
	if err := os.WriteFile(mov, testMovieWithDuration(600, 1200), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"evidence", "stop", "--run", run.ID, "--no-capture"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "video_mp4=") {
		t.Fatalf("got %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "video.mp4")); err != nil {
		t.Fatalf("missing video.mp4: %v", err)
	}
}

func TestFlowVideoStartStopAliasesRecordVideo(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 1, FlowStep{Action: "video.start"}, flowExecBindings{}); err != nil {
		t.Fatalf("video.start: %v", err)
	}
}

func TestFlowNetworkStartStop(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"mitmdump": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 1, FlowStep{Action: "network.start", Params: map[string]string{"port": "9090"}}, flowExecBindings{}); err != nil {
		t.Fatalf("network.start: %v", err)
	}
	if got := findRunningNetworkPID(run); got != 123 {
		t.Fatalf("network pid=%d", got)
	}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 2, FlowStep{Action: "network.stop"}, flowExecBindings{}); err != nil {
		t.Fatalf("network.stop: %v", err)
	}
	if got := findRunningNetworkPID(run); got != 0 {
		t.Fatalf("network pid after stop=%d", got)
	}
	if !containsCall(runner.commands, "mitmdump --listen-port 9090 --quiet --set hardump="+filepath.Join(run.Dir, "network.har")) {
		t.Fatalf("commands=%v", runner.commands)
	}
	if !containsCall(runner.commands, "kill -TERM 123") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestRunFlowRecordsVideoNetworkEvidenceAndReportWithMockedDrivers(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"xcrun": true, "axe": true, "mitmdump": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte(`
name: evidence_network_e2e
steps:
  - evidence.start: { network: true, port: 9092 }
  - evidence.step: { name: before-refresh, note: Before triggering refresh }
  - network.status: {}
  - evidence.stop: { note: Refresh completed }
  - report: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui --udid SIM": `[{"AXUniqueId":"HomeView","AXRole":"Application"}]`,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=run") {
		t.Fatalf("run output=%q", out.String())
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(run.Dir, "report.json")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report ReportData
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("invalid report: %v\n%s", err, data)
	}
	if report.VideoStatus != "accepted" || report.VideoMP4 == "" {
		t.Fatalf("video status=%s mp4=%q report=%s", report.VideoStatus, report.VideoMP4, data)
	}
	if !report.Network.OK || report.Network.Requests != 2 || report.Network.UniqueDomains != 2 {
		t.Fatalf("network=%+v report=%s", report.Network, data)
	}
	if report.ValidStepCount < 2 {
		t.Fatalf("expected named + final evidence steps, got valid=%d data=%s", report.ValidStepCount, data)
	}
	for _, want := range []string{
		"xcrun simctl io SIM recordVideo",
		"mitmdump --listen-port 9092 --quiet --set hardump=" + filepath.Join(run.Dir, "network.har"),
		"axe screenshot --output",
		"kill -TERM 124",
		"/usr/bin/avconvert",
	} {
		if !containsCall(runner.commands, want) {
			t.Fatalf("missing %q in commands=%v", want, runner.commands)
		}
	}
	if got := findRunningNetworkPID(run); got != 0 {
		t.Fatalf("network pid after flow=%d records=%+v", got, loadProcessRecords(run))
	}
}

func TestEvidenceStartWithNetworkStartsHARCapture(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"xcrun": true, "mitmdump": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.evidenceStart(context.Background(), GlobalOptions{}, []string{"--run", run.ID, "--network", "--port", "9091"}); err != nil {
		t.Fatalf("evidence.start: %v", err)
	}
	if got := findRunningNetworkPID(run); got != 124 {
		t.Fatalf("network pid=%d out=%q records=%+v commands=%v", got, out.String(), loadProcessRecords(run), runner.commands)
	}
	if !containsCall(runner.commands, "mitmdump ") {
		t.Fatalf("commands=%v", runner.commands)
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
	if err := cli.Run(context.Background(), []string{"--prefer-driver", "auto", "run", flowPath}); err != nil {
		t.Fatal(err)
	}
	if !containsCall(runner.commands, "axe tap --label Continue") {
		t.Fatalf("step override should force axe: commands=%v output=%q", runner.commands, out.String())
	}
}

func TestUISwipePreferAxeDoesNotFallbackToIDB(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"--prefer-driver", "axe", "ui", "swipe", "up"}))
	got := out.String()
	if !strings.Contains(got, "fail code=tool_missing") || !strings.Contains(got, "tool=axe") {
		t.Fatalf("got %q", got)
	}
	if containsCall(runner.commands, "idb ui swipe") {
		t.Fatalf("prefer axe should not run idb: %v", runner.commands)
	}
}

// TestUISwipeExplicitPreferIsHonoured guards the regression that opening
// --prefer-driver to any registered id introduces: uiSwipe pinned
// preferred="axe" as soon as cfg had axe, so a --prefer-driver that was
// previously rejected outright (prefer_driver_invalid) became accepted and
// executed with axe without a word. Accepting a flag and then ignoring it
// is exactly the kind of dead configuration target_command_ignored exists
// to make visible.
// TestOpenClearStateFailureWarnsInsteadOfBeingSilent pins the behavior
// that replaces the `_ =` that discarded the uninstall's result: a failing
// clear_state does NOT abort the open, the common case is that the app was
// not installed yet, but it cannot disappear either, because then
// --clear-state lies and the run drags the previous one's state along.
func TestOpenClearStateFailureWarnsInsteadOfBeingSilent(t *testing.T) {
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
			`xcrun simctl uninstall SIM com.example.app`: {
				Stderr: "An error was encountered processing the command",
				Code:   1,
				Err:    errors.New("exit status 1"),
			},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open", "--clear-state"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.HasPrefix(strings.TrimSpace(got), "fail ") {
		t.Fatalf("a failed clear_state must not abort the open: %q", got)
	}
	if !strings.Contains(got, "clear_state_warn=") || !strings.Contains(got, "clear_state_incomplete") {
		t.Fatalf("the uninstall failure must surface as a warning in the response: %q", got)
	}
	if !containsCall(runner.commands, "simctl install") {
		t.Fatalf("the open must carry on to the install: %v", runner.commands)
	}
}

func TestUISwipeExplicitPreferIsHonoured(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "baguette": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--prefer-driver", "baguette", "ui", "swipe", "up"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "driver=baguette") {
		t.Fatalf("--prefer-driver baguette must be served with baguette, not overwritten with axe: %q", got)
	}
	if containsCall(runner.commands, "axe swipe") {
		t.Fatalf("--prefer-driver baguette must not silently run with axe: %v", runner.commands)
	}
}

// TestUISwipeExplicitPreferThatCannotServeFailsLoudly covers the other
// half: a --prefer-driver now valid as an id but unable to serve this
// capability must fail naming it, not silently fall to the default driver.
func TestUISwipeExplicitPreferThatCannotServeFailsLoudly(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "simtime": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"--prefer-driver", "simtime", "ui", "swipe", "up"}))
	got := out.String()
	if !strings.Contains(got, "fail code=prefer_driver_unusable") || !strings.Contains(got, "driver=simtime") {
		t.Fatalf("a prefer that cannot serve swipe must fail naming it, got %q", got)
	}
	if containsCall(runner.commands, "axe swipe") {
		t.Fatalf("it must not silently fall to axe: %v", runner.commands)
	}
}

// TestSingleProviderCapabilitiesStayHardcodedAfterPreferDriverRemoval locks
// in FIX B.2: dropping the redundant --prefer-driver hints on single-provider
// capabilities (CapHardwareBtn, CapDoubleTap, CapWallClock) must not change
// which driver actually serves the request, since that driver was already
// the only healthy candidate.
func TestSingleProviderCapabilitiesStayHardcodedAfterPreferDriverRemoval(t *testing.T) {
	root := t.TempDir()
	t.Cleanup(func() { removeSimulatorLock("SIM", root) })
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"axe": true, "baguette": true, "idb": true, "xcrun": true, "simtime": true}
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
	if err := os.WriteFile(filepath.Join(run.Dir, "time-control.enabled"), []byte(cfg.BundleID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"simtime --udid SIM --bundle com.example.app": "idle"},
	}

	t.Run("press reaches baguette", func(t *testing.T) {
		var out bytes.Buffer
		cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), []string{"ui", "press", "--button", "home"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "driver=baguette") {
			t.Fatalf("got %q", out.String())
		}
	})

	t.Run("doubleTap reaches baguette", func(t *testing.T) {
		var out bytes.Buffer
		cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), []string{"ui", "doubleTap", "--x", "1", "--y", "1"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "driver=baguette") {
			t.Fatalf("got %q", out.String())
		}
	})

	t.Run("time status reaches simtime", func(t *testing.T) {
		var out bytes.Buffer
		cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), []string{"time", "status"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "driver=simtime") {
			t.Fatalf("got %q", out.String())
		}
	})

	t.Run("coord tap still reaches idb", func(t *testing.T) {
		// Regression guard for the CapCoordTap prefer kept in cli.go: without
		// it, axe/baguette/idb tie at cost 50 and the ID tie-break would pick
		// axe instead.
		var out bytes.Buffer
		cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), []string{"ui", "tap", "--x", "10", "--y", "10"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "driver=idb") {
			t.Fatalf("got %q", out.String())
		}
	})
}

func TestOpenStartsOnlyProbeLogsByDefault(t *testing.T) {
	root := t.TempDir()
	t.Cleanup(func() { removeSimulatorLock("SIM", root) })
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
	allowFail(t, cli.Run(context.Background(), []string{"open", "--clear-state"}))
	got := out.String()
	if !strings.Contains(got, "fail code=launch_step_failed") || !strings.Contains(got, "step=clear_state") || !strings.Contains(got, "clearState requires an install command") {
		t.Fatalf("got %q", got)
	}
}

func skipBuildProject(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	appPath := filepath.Join(root, "App.app")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Build:   "make mav-build",
		AppPath: "make mav-app-path",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
		Launch:  `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root, appPath
}

func TestOpenSkipBuildRunsEverythingButTheBuild(t *testing.T) {
	root, appPath := skipBuildProject(t)
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"make mav-app-path": {Stdout: appPath + "\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open", "--skip-build"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.commands, "\n")
	if strings.Contains(joined, "make mav-build") {
		t.Fatalf("build ran with --skip-build: %s", joined)
	}
	for _, needle := range []string{"make mav-app-path", "simctl install", "simctl launch"} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("missing %s in %s", needle, joined)
		}
	}
}

func TestOpenWithoutSkipBuildStillBuilds(t *testing.T) {
	root, appPath := skipBuildProject(t)
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"make mav-app-path": {Stdout: appPath + "\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "make mav-build") {
		t.Fatalf("build did not run: %v", runner.commands)
	}
}

func TestOpenSkipBuildReportsMissingArtifactAsItsOwnCode(t *testing.T) {
	root, _ := skipBuildProject(t)
	runner := &launchRecipeRunner{
		tools: map[string]bool{"xcrun": true},
		results: map[string]CommandResult{
			"make mav-app-path": {Stderr: "make: *** No rule to make target", Err: errors.New("exit status 2")},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"open", "--skip-build"}))
	got := out.String()
	if !strings.Contains(got, "fail code=build_skipped_app_missing") || !strings.Contains(got, "step=app_path") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "build was skipped") || !strings.Contains(got, "next=") {
		t.Fatalf("got %q", got)
	}
}

func TestOpenSkipBuildRejectsAnAppPathThatIsNotOnDisk(t *testing.T) {
	root, _ := skipBuildProject(t)
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"make mav-app-path": {Stdout: filepath.Join(root, "never-built.app") + "\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"open", "--skip-build"}))
	got := out.String()
	if !strings.Contains(got, "fail code=build_skipped_app_missing") || !strings.Contains(got, "does not exist") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "simctl install") {
		t.Fatalf("install ran against a missing app: %v", runner.commands)
	}
}

func TestOpenSkipBuildRejectsNoRelaunch(t *testing.T) {
	root, _ := skipBuildProject(t)
	var out bytes.Buffer
	cli := CLI{Runner: &launchRecipeRunner{tools: map[string]bool{"xcrun": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"open", "--no-relaunch", "--skip-build"}))
	if got := out.String(); !strings.Contains(got, "fail code=open_flags_invalid") || !strings.Contains(got, "--skip-build") {
		t.Fatalf("got %q", got)
	}
}

func TestRunFlowSkipBuildAppliesToEveryOpenStep(t *testing.T) {
	root, appPath := skipBuildProject(t)
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("steps:\n  - open: {}\n  - open: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"make mav-app-path": {Stdout: appPath + "\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath, "--skip-build"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	joined := strings.Join(runner.commands, "\n")
	if strings.Contains(joined, "make mav-build") {
		t.Fatalf("build ran with --skip-build: %s", joined)
	}
	if strings.Count(joined, "make mav-app-path") != 2 {
		t.Fatalf("app_path did not run once per open step: %s", joined)
	}
}

func TestRunFlowOpenStepSkipBuild(t *testing.T) {
	root, appPath := skipBuildProject(t)
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("steps:\n  - open: { skipBuild: true }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"make mav-app-path": {Stdout: appPath + "\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	joined := strings.Join(runner.commands, "\n")
	if strings.Contains(joined, "make mav-build") {
		t.Fatalf("build ran for a skipBuild open step: %s", joined)
	}
	if !strings.Contains(joined, "simctl launch") {
		t.Fatalf("launch did not run: %s", joined)
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
	allowFail(t, cli.Run(context.Background(), []string{"run", flowPath}))
	got := out.String()
	if !strings.Contains(got, "fail code=open_failed") || !strings.Contains(got, "action=open") {
		t.Fatalf("got %q", got)
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

func TestEraseOnDeviceFailsWithStructuredError(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.BundleID = "com.example.app"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{tools: map[string]bool{"idb": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"ui", "erase", "--focused"}))
	if !strings.Contains(out.String(), "fail code=erase_unsupported_on_device") {
		t.Fatalf("got %q", out.String())
	}
}

func TestHideKeyboardOnDeviceFailsWithStructuredError(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{tools: map[string]bool{"idb": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"ui", "hideKeyboard"}))
	if !strings.Contains(out.String(), "fail code=hide_keyboard_unsupported_on_device") {
		t.Fatalf("got %q", out.String())
	}
}

func TestEraseAndHideKeyboardUseBaguetteOnSimulator(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"baguette": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "erase", "--focused"}); err != nil {
		t.Fatal(err)
	}
	if err := cli.Run(context.Background(), []string{"ui", "hideKeyboard"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=ui.erase") || !strings.Contains(out.String(), "ok cmd=ui.hideKeyboard") {
		t.Fatalf("output=%q", out.String())
	}
	if !containsCall(runner.commands, "baguette key --udid SIM --code Backspace") {
		t.Fatalf("commands=%v", runner.commands)
	}
	if !containsCall(runner.commands, "baguette key --udid SIM --code Escape") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestPinchOnDeviceFailsWithStructuredError(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{tools: map[string]bool{"idb": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"ui", "pinch", "--x", "100", "--y", "100", "--scale", "1.5"}))
	if !strings.Contains(out.String(), "fail code=gesture_unsupported_on_device") {
		t.Fatalf("got %q", out.String())
	}
}

func TestUITreeIncludeSystemOnDeviceFailsWithStructuredError(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.Tools = map[string]bool{"idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"ui", "tree", "--include-system"}))
	if !strings.Contains(out.String(), "fail code=tree_system_unsupported_on_device") {
		t.Fatalf("got %q", out.String())
	}
}

func TestUITreeIncludeSystemUsesBaguetteOnSimulator(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"baguette": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"baguette describe-ui --udid SIM": `[{"AXUniqueId":"SpringBoard","AXRole":"Application"}]`,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"--raw", "ui", "tree", "--include-system"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "SpringBoard") {
		t.Fatalf("output=%q", out.String())
	}
	if !containsCall(runner.commands, "baguette describe-ui --udid SIM") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestLaunchLanguageArgs(t *testing.T) {
	got := strings.Join(simctlLaunchLanguageArgs(Config{Language: "es", Locale: "es_ES"}), " ")
	if got != "-AppleLanguages (es) -AppleLocale es_ES" {
		t.Fatalf("got %q", got)
	}
}

func TestDeviceListParsesDevicectlJSONOutputFile(t *testing.T) {
	devices := `{"result":{"devices":[{"identifier":"REAL-1","name":"David iPhone"}]}}`
	var out bytes.Buffer
	cli := CLI{Runner: deviceListRunner{tools: map[string]bool{"xcrun": true}, devices: devices}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"device", "list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=device.list count=1") || !strings.Contains(out.String(), `device udid=REAL-1 name="David iPhone"`) {
		t.Fatalf("got %q", out.String())
	}
}

func TestDeviceSelectPersistsPhysicalTarget(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.ProjectName = "Demo"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	devices := `{"result":{"devices":[{"identifier":"REAL-1","name":"David iPhone"}]}}`
	var out bytes.Buffer
	cli := CLI{Runner: deviceListRunner{tools: map[string]bool{"xcrun": true}, devices: devices}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"device", "select", "--udid", "REAL-1"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TargetKind != "device" || loaded.DeviceUDID != "REAL-1" || loaded.DeviceName != "David iPhone" {
		t.Fatalf("loaded=%+v output=%s", loaded, out.String())
	}
}

func TestDeviceSelectReportsNotFound(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, DefaultConfig(root)); err != nil {
		t.Fatal(err)
	}
	devices := `{"result":{"devices":[{"identifier":"REAL-1","name":"David iPhone"}]}}`
	var out bytes.Buffer
	cli := CLI{Runner: deviceListRunner{tools: map[string]bool{"xcrun": true}, devices: devices}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"device", "select", "--udid", "MISSING"}))
	if !strings.Contains(out.String(), "fail code=device_not_found") {
		t.Fatalf("got %q", out.String())
	}
}

func TestSimSelectResetsTargetKindToSimulator(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		out: map[string]string{"xcrun simctl list devices -j": `{"devices":{"runtime":[{"udid":"SIM-1","name":"iPhone","state":"Shutdown","isAvailable":true}]}}`},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "select", "--udid", "SIM-1"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TargetKind != "simulator" || loaded.SimulatorUDID != "SIM-1" {
		t.Fatalf("loaded=%+v output=%s", loaded, out.String())
	}
}

func TestSimBootRejectsPhysicalTarget(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"sim", "boot"}))
	if !strings.Contains(out.String(), "fail code=sim_not_applicable") {
		t.Fatalf("got %q", out.String())
	}
}

// TestSimBootToleratesAlreadyBootingState locks in FIX C.2: mav sim boot now
// routes its boot through the router (CapBoot -> simctl.Boot), and that
// driver call must keep the exact same tolerance the old direct `xcrun
// simctl boot` call had for a simulator that is already transitioning to
// booted.
func TestSimBootToleratesAlreadyBootingState(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		err: map[string]CommandResult{
			"xcrun simctl boot SIM": {Stderr: "Unable to boot device in current state", Err: os.ErrNotExist},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "boot"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(strings.TrimSpace(got), "ok ") {
		t.Fatalf("boot tolerance should still yield ok, got %q", got)
	}
	if !strings.Contains(got, "udid=SIM") {
		t.Fatalf("got %q", got)
	}
}

func TestDeviceLaunchRecipeUsesIDB(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.BundleID = "com.example.app"
	cfg.Tools["idb"] = true
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
		tools: map[string]bool{"idb": true},
		results: map[string]CommandResult{
			"make ios-app-path": {Stdout: "/tmp/Demo.app\n"},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open", "--clear-state"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{"idb uninstall", "idb install", "idb launch"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in commands:\n%s\noutput=%s", want, joined, out.String())
		}
	}
	if strings.Contains(joined, "simctl install") || strings.Contains(joined, "simctl launch") {
		t.Fatalf("device launch should not use simctl:\n%s", joined)
	}
}

func TestStartProbeLogsUsesIDBForDevice(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.Tools["idb"] = true
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: map[string]bool{"idb": true}}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if _, err := cli.startProbeLogs(context.Background(), cfg, run); err != nil {
		t.Fatal(err)
	}
	records := loadProcessRecords(run)
	if len(records) != 1 || !strings.Contains(records[0].Command, "idb log --udid REAL-1") {
		t.Fatalf("records=%+v", records)
	}
}

func TestCaptureScreenshotUsesIDBForDevice(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.Tools["idb"] = true
	runner := &recordingRunner{tools: map[string]bool{"idb": true}}
	cli := CLI{Runner: runner, Root: t.TempDir(), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if _, err := cli.captureScreenshot(context.Background(), cfg, filepath.Join(t.TempDir(), "screen.png")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.command, "idb screenshot ") || !strings.Contains(runner.command, "--udid REAL-1") {
		t.Fatalf("command=%q", runner.command)
	}
}

func TestCaptureCommandAllowedOnPhysicalDevice(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.Tools = map[string]bool{"idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: map[string]bool{"idb": true}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"capture", "--name", "probe"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "clipboard_unsupported_on_device") {
		t.Fatalf("capture must not reuse the clipboard device guard, got %q", out.String())
	}
	if !strings.Contains(out.String(), "ok cmd=capture") {
		t.Fatalf("got %q", out.String())
	}
}

func TestCrashesUsesDeviceUDID(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.BundleID = "com.example.app"
	cfg.Tools["idb"] = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{tools: map[string]bool{"idb": true}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"crashes"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.command, "idb crash list --udid REAL-1") || !strings.Contains(runner.command, "--bundle-id com.example.app") {
		t.Fatalf("command=%q output=%s", runner.command, out.String())
	}
}

func TestVideoStartUnsupportedOnDevice(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
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
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"evidence", "start"}))
	if !strings.Contains(out.String(), "fail code=video_unsupported") || !strings.Contains(out.String(), "target=device") {
		t.Fatalf("got %q", out.String())
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

func TestFlowLintReportsWarnings(t *testing.T) {
	root := t.TempDir()
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("steps:\n  - tap: { text: Settings }\n  - evidence.step: { name: after }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"flow", "lint", flowPath}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "ok cmd=flow.lint") || !strings.Contains(got, "warnings=2") || !strings.Contains(got, "errors=0") {
		t.Fatalf("lint output=%q", got)
	}
}

func TestFlowLintFailsExecWithoutAllowShell(t *testing.T) {
	root := t.TempDir()
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("steps:\n  - exec: { cmd: echo hi }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"flow", "lint", flowPath}))
	got := out.String()
	if !strings.Contains(got, "fail code=flow_lint_failed") || !strings.Contains(got, "errors=1") {
		t.Fatalf("lint output=%q", got)
	}
}

func TestNetworkStatusSummarizesHAR(t *testing.T) {
	root := t.TempDir()
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	appendProcess(run, "network", 42, "mitmproxy --listen-port 9000 --hardump "+filepath.Join(run.Dir, "network.har"))
	har := `{"log":{"entries":[{"request":{"url":"https://api.example.com/a"},"response":{"status":200}},{"request":{"url":"https://api.example.com/b"},"response":{"status":404}},{"request":{"url":"https://cdn.example.com/c"},"response":{"status":502}}]}}`
	if err := os.WriteFile(filepath.Join(run.Dir, "network.har"), []byte(har), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"network", "status", "--run", run.ID}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"ok cmd=network.status", "active=true", "pid=42", "requests=3", "responses=3", "status_4xx=1", "status_5xx=1", "unique_domains=2", "listen_port=9000"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestCountTreeNodes(t *testing.T) {
	raw := `[{"children":[{"children":[]},{"children":[{"children":[]}]}]}]`
	if got := countTreeNodes(raw); got != 4 {
		t.Fatalf("got %d", got)
	}
}

// Silence import-unused complaint when this file is the only consumer of strconv.
var _ = strconv.Itoa

func TestSelectorCLIFlagRecognizesWhereJSON(t *testing.T) {
	if !isSelectorCLIFlag("--where-json") {
		t.Fatal("--where-json must be scrubbed from ui type text args, or its JSON payload leaks into the typed text")
	}
}

func TestFlowTypeStepArgsExcludeLegacyTextSelector(t *testing.T) {
	flow, err := ParseFlow([]byte(`
steps:
  - type: { text: "hello world" }
`))
	if err != nil {
		t.Fatal(err)
	}
	step := flow.Steps[0]
	if !step.Where.IsZero() {
		t.Fatalf("expected zero Where for text-only type step, got %+v", step.Where)
	}
	if args := selectorCLIArgs(step.Where); len(args) != 0 {
		t.Fatalf("text-only type step must not produce tap-target args, got %v", args)
	}
}

func fixtureConfigRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		AppPath: "make ios-app-path",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
		Launch:  `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	cfg.Fixtures = map[string][]string{
		"seeded": {"echo seeding-one", "echo seeding-two"},
	}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestFixtureRunsBetweenInstallAndLaunch pins the window: the container
// already exists and the app has not started yet, so nothing has its
// database open. Before or after, seeding is corrupting.
func TestFixtureRunsBetweenInstallAndLaunch(t *testing.T) {
	root := fixtureConfigRoot(t)
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"make ios-app-path": {Stdout: "/tmp/App.app\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open", "--fixture", "seeded"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.commands, "\n")
	install := strings.Index(joined, "simctl install")
	seed := strings.Index(joined, "seeding-one")
	launch := strings.Index(joined, "simctl launch")
	if install < 0 || seed < 0 || launch < 0 {
		t.Fatalf("steps are missing: %s", joined)
	}
	if !(install < seed && seed < launch) {
		t.Fatalf("wrong order install=%d fixture=%d launch=%d:\n%s", install, seed, launch, joined)
	}
	if !strings.Contains(out.String(), "fixture=seeded") {
		t.Fatalf("the applied fixture must appear in the response: %q", out.String())
	}
}

func TestFixtureNotFoundFailsListingAvailable(t *testing.T) {
	root := fixtureConfigRoot(t)
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"make ios-app-path": {Stdout: "/tmp/App.app\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"open", "--fixture", "nope"}))
	got := out.String()
	if !strings.Contains(got, "fail code=launch_step_failed") || !strings.Contains(got, "fixture_not_found") {
		t.Fatalf("a nonexistent fixture must fail naming the valid ones: %q", got)
	}
	if containsCall(runner.commands, "simctl launch") {
		t.Fatalf("the app must not launch when the fixture does not exist: %v", runner.commands)
	}
}

func TestFixtureRejectedWithNoRelaunch(t *testing.T) {
	root := fixtureConfigRoot(t)
	runner := &launchRecipeRunner{tools: map[string]bool{"xcrun": true}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"open", "--no-relaunch", "--fixture", "seeded"}))
	// --no-relaunch skips the whole recipe, so the fixture would not run.
	// Accepting it and not executing it would leave the agent validating
	// against data nobody seeded.
	if !strings.Contains(out.String(), "open_flags_invalid") {
		t.Fatalf("--fixture with --no-relaunch must be rejected: %q", out.String())
	}
}

func TestFixtureFailureAbortsNamingTheStep(t *testing.T) {
	root := fixtureConfigRoot(t)
	runner := &launchRecipeRunner{
		tools: map[string]bool{"xcrun": true},
		results: map[string]CommandResult{
			"make ios-app-path": {Stdout: "/tmp/App.app\n"},
			"seeding-two":       {Stderr: "boom", Code: 1, Err: errors.New("exit status 1")},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"open", "--fixture", "seeded"}))
	got := out.String()
	if !strings.Contains(got, "step=fixture") || !strings.Contains(got, "fixture seeded step 2/2") {
		t.Fatalf("the failure must name the fixture and the step: %q", got)
	}
	if containsCall(runner.commands, "simctl launch") {
		t.Fatalf("the app must not launch after a failed fixture: %v", runner.commands)
	}
}

func TestNoFixtureRunsNothing(t *testing.T) {
	root := fixtureConfigRoot(t)
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"make ios-app-path": {Stdout: "/tmp/App.app\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open"}); err != nil {
		t.Fatal(err)
	}
	if containsCall(runner.commands, "seeding-one") {
		t.Fatalf("without --fixture nothing must be seeded: %v", runner.commands)
	}
	if strings.Contains(out.String(), "fixture=") {
		t.Fatalf("without --fixture none must be reported: %q", out.String())
	}
}

// TestTapVerificationSurvivesTheCoordinateFallback: when a semantic tap
// falls back to coordinates, that is the path most likely to tap the wrong
// place. Losing --verify there would be losing it exactly where it is
// needed.
func TestTapVerificationSurvivesTheCoordinateFallback(t *testing.T) {
	got := onlyFastPathArgs([]string{"--id", "boton", "--verify", "--observe", "delta", "--wait-timeout", "2s"})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--verify") {
		t.Fatalf("--verify must survive the coordinate fallback: %v", got)
	}
	if strings.Contains(joined, "--id") {
		t.Fatalf("the selector must not be forwarded: %v", got)
	}
}

// TestVerifyReportsUnknownRatherThanGuessing: verification is an extra. If
// the tree cannot be read, it cannot be claimed that the tap worked NOR
// that it failed, and saying either would be worse than admitting it is
// not known.
func TestVerifyReportsUnknownRatherThanGuessing(t *testing.T) {
	cli := CLI{Root: t.TempDir()}
	if got := cli.verifyTapChangedSomething(context.Background(), Config{}, nil); got != "unknown" {
		t.Fatalf("without a prior tree nothing can be verified, got %q", got)
	}
}

// TestVerifyDistinguishesChangedFromUnchanged: the "unchanged" case is
// this function's entire reason to exist, a tap that reports ok having
// done nothing, so it is worth pinning without depending on a live app.
func TestVerifyDistinguishesChangedFromUnchanged(t *testing.T) {
	same := []Element{{ID: "a", Label: "Uno"}, {ID: "b", Label: "Dos"}}
	if delta := TreeDiff(same, same); len(delta.Added)+len(delta.Removed)+len(delta.Changed) != 0 {
		t.Fatalf("two identical trees have no delta: %+v", delta)
	}
	moved := []Element{{ID: "a", Label: "Uno"}, {ID: "b", Label: "DOS"}}
	delta := TreeDiff(same, moved)
	if len(delta.Changed) == 0 {
		t.Fatalf("a label change must be detected: %+v", delta)
	}
}

// TestCaptureHonoursPreferDriver: --prefer-driver was accepted on the
// command line but captureScreenshot dropped it and always routed by cost,
// so the only way to dodge a broken capture driver was uninstalling it.
func TestCaptureHonoursPreferDriver(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Tools["idb"] = true
	cfg.Tools["simctl"] = true
	runner := &recordingRunner{tools: map[string]bool{"idb": true, "xcrun": true, "simctl": true}}
	cli := CLI{Runner: runner, Root: t.TempDir(), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if _, err := cli.captureScreenshotWith(context.Background(), cfg, filepath.Join(t.TempDir(), "s.png"), "simctl"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(runner.command, "idb screenshot") {
		t.Fatalf("--prefer-driver simctl ignored, routed by cost to idb: %q", runner.command)
	}
}

// TestTapToolGateIsNotIOSShaped: the gate asked only for axe, which is
// never installed on the Mac, so `ui tap` failed with tool_missing even
// with the macOS driver healthy, and it also named a tool that is no
// longer the only one serving input there.
func TestTapToolGateIsNotIOSShaped(t *testing.T) {
	mac := DefaultConfig(t.TempDir())
	mac.TargetKind = "macos"
	caps := Capabilities{Tools: map[string]bool{"cua-driver": true}}
	if !tapToolPresent(caps, mac) {
		t.Fatal("with the macOS driver present tapping is possible, even without axe")
	}
	if tapToolPresent(Capabilities{Tools: map[string]bool{"axe": true}}, mac) {
		t.Fatal("axe does not serve input on the Mac")
	}
	if got := tapToolMissingFields(mac)["tool"]; got != "cua-driver" {
		t.Fatalf("it must name the macOS canonical tool: %q", got)
	}

	sim := DefaultConfig(t.TempDir())
	if !tapToolPresent(Capabilities{Tools: map[string]bool{"axe": true}}, sim) {
		t.Fatal("en simulador sigue siendo axe")
	}
}

// TestVersionIsAnswerableAtAll: until this existed `mav --version` answered
// unknown_command, which turns every bug report and every run whose evidence
// is read weeks later into a guess about which mav produced it.
func TestVersionIsAnswerableAtAll(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}} {
		var out bytes.Buffer
		cli := CLI{Runner: &recordingRunner{}, Stdout: &out, Stderr: &bytes.Buffer{}, Root: t.TempDir()}
		if err := cli.Run(context.Background(), args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		// The same shape as every other command, so an agent that parses
		// the rest needs no special case for this one.
		if !strings.Contains(out.String(), "ok cmd=version version="+Version) {
			t.Fatalf("%v answered %q", args, out.String())
		}
	}
}

// TestAnUnstampedBuildDoesNotClaimAReleaseNumber: the version is stamped at
// link time by the release build. A locally built binary reporting a release
// number would send someone chasing a bug in code that was never shipped.
func TestAnUnstampedBuildDoesNotClaimAReleaseNumber(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("an unstamped build reports %q", Version)
	}
}
