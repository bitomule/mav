package mav

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests exercise target_command the same way TestLogsReportsPinnedTargetUDID
// / TestLogsResolvesBootedSimulatorUDIDWhenUnset / TestBootedSimulatorUDIDResolvedOnceThenCachedForRun
// exercise the pre-existing booted-simulator resolution: `mav logs --run <id>`
// only ever touches Runner.Run through target resolution, so it isolates the
// resolution behavior (including its run-scoped cache) from the rest of the
// command surface. fakeRunner (config_test.go) is the established mock for
// this kind of test in this repo; the os.Executable()/MAV_TEST_CHILD real
// -process pattern (concurrent_run_test.go) is reserved for bugs that live in
// cross-process state, which target_command resolution is not.

// targetCommandKey mirrors execTargetCommand's own construction so tests
// don't hardcode the "cd $MAV_ROOT && export MAV_ROOT=... && " prefix it
// adds to run target_command from the project root, the same convention
// launch commands and exec flow steps already use.
func targetCommandKey(root, command string) string {
	return "/bin/bash -lc " + shellEnvPrefix(map[string]string{"MAV_ROOT": root}) + " " + command
}

func newTargetCommandRun(t *testing.T, cfg Config) (string, string) {
	t.Helper()
	root := cfg.Root
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
	return root, run.ID
}

func TestTargetCommandResolvesSimulatorUDID(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "echo TC-UDID-1"
	root, runID := newTargetCommandRun(t, cfg)

	runner := fakeRunner{out: map[string]string{
		targetCommandKey(root, "echo TC-UDID-1"): "TC-UDID-1\n",
	}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "udid=TC-UDID-1") {
		t.Fatalf("got %q, want udid=TC-UDID-1", out.String())
	}
	if !strings.Contains(out.String(), "target_kind=simulator") {
		t.Fatalf("got %q, want target_kind=simulator", out.String())
	}
	if strings.Contains(out.String(), "target_command_warn") {
		t.Fatalf("got %q, want no target_command_warn on success", out.String())
	}
}

// TestTargetCommandInvokedOncePerRun is the perf regression for target_command
// specifically: it is expected to shell out to an external pool manager, so a
// hot-path navigation of dozens of commands must not re-run it (and pay its
// process-start + pool-manager round trip) on every single command any more
// than the pre-existing booted-simulator resolution does.
func TestTargetCommandInvokedOncePerRun(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "print-target"
	root, runID := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "print-target")
	runner := fakeRunner{
		out:   map[string]string{key: "TC-UDID-CACHED\n"},
		seq:   map[string][]string{key: {"TC-UDID-CACHED\n"}},
		calls: map[string]int{},
	}

	for i := 0; i < 3; i++ {
		var out bytes.Buffer
		cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if !strings.Contains(out.String(), "udid=TC-UDID-CACHED") {
			t.Fatalf("call %d: got %q, want udid=TC-UDID-CACHED", i, out.String())
		}
	}
	if got := runner.calls[key]; got != 1 {
		t.Fatalf("target_command calls=%d, want 1 (later commands in the same run should hit the run-scoped cache)", got)
	}
}

// TestTargetCommandLosesToPinnedSimulatorUDID: simulator_udid already pinned
// in config.yaml (e.g. via `mav sim select`) is already-resolved explicit
// state, so it must win over a configured target_command without ever
// running it -- but the conflict must still be visible (see
// TestTargetCommandPinnedConfigWarnsWhenTargetCommandAlsoSet below), not a
// silent no-op that leaves target_command looking like dead configuration.
func TestTargetCommandLosesToPinnedSimulatorUDID(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.SimulatorUDID = "SIM-PINNED"
	cfg.SimulatorName = "iPhone 17 Pro"
	cfg.TargetCommand = "echo SHOULD-NOT-RUN"
	root, runID := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "echo SHOULD-NOT-RUN")
	runner := fakeRunner{out: map[string]string{key: "SHOULD-NOT-RUN\n"}, calls: map[string]int{}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "udid=SIM-PINNED") {
		t.Fatalf("got %q, want udid=SIM-PINNED", out.String())
	}
	if got := runner.calls[key]; got != 0 {
		t.Fatalf("target_command calls=%d, want 0 (pinned simulator_udid must win without invoking it)", got)
	}
	if !strings.Contains(out.String(), "target_command_warn=") {
		t.Fatalf("got %q, want a target_command_warn flagging the dead target_command config", out.String())
	}
}

// TestTargetCommandPinnedConfigWarnsWhenTargetCommandAlsoSet is the
// dedicated regression for the real-world case this guards against: a repo
// pins simulator_udid (e.g. via `mav sim select`, or carried over from
// before target_command existed) and separately configures target_command.
// The pin still wins -- inverting that would make `mav sim select` an
// unreliable no-op -- but the conflict must be loud, actionable, and never
// fail or hang the command it decorates: silence here is exactly the
// failure mode this feature exists to eliminate.
func TestTargetCommandPinnedConfigWarnsWhenTargetCommandAlsoSet(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.SimulatorUDID = "7D0487E4-DD78-4E43-80EB-EDBFDB1C875B"
	cfg.SimulatorName = "iPhone 17 Pro"
	cfg.TargetCommand = "simpool with --print-udid"
	root, runID := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "simpool with --print-udid")
	runner := fakeRunner{out: map[string]string{key: "SHOULD-NOT-RUN\n"}, calls: map[string]int{}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "udid=7D0487E4-DD78-4E43-80EB-EDBFDB1C875B") {
		t.Fatalf("got %q, want the pin to still win", got)
	}
	if !strings.Contains(got, "target_command_warn=") {
		t.Fatalf("got %q, want an actionable target_command_warn field", got)
	}
	if !strings.Contains(got, "target_command_ignored") {
		t.Fatalf("got %q, want target_command_ignored in the warning", got)
	}
	if !strings.Contains(got, "7D0487E4-DD78-4E43-80EB-EDBFDB1C875B") {
		t.Fatalf("got %q, want the warning to name the pin that's winning", got)
	}
	if !strings.Contains(got, "next:") {
		t.Fatalf("got %q, want an actionable next step (remove the pin or remove target_command)", got)
	}
	if strings.Contains(got, "fail ") {
		t.Fatalf("got %q, want an ambiguous-but-resolvable config to still succeed, not fail", got)
	}
	if gotCalls := runner.calls[key]; gotCalls != 0 {
		t.Fatalf("target_command calls=%d, want 0 (the warning must not require actually invoking it)", gotCalls)
	}
}

// TestTargetCommandLosesToMAVTargetEnv covers the flag/MAV_TARGET_* tier:
// `mav run --target` sets MAV_TARGET_KIND/MAV_TARGET_UDID on the child
// process (see matrix.go), and that must win over target_command exactly
// like it already wins over the plain booted fallback.
func TestTargetCommandLosesToMAVTargetEnv(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "echo SHOULD-NOT-RUN"
	root, runID := newTargetCommandRun(t, cfg)

	t.Setenv("MAV_TARGET_KIND", "simulator")
	t.Setenv("MAV_TARGET_UDID", "ENV-UDID")
	t.Setenv("MAV_TARGET_NAME", "iPhone Env")

	key := targetCommandKey(root, "echo SHOULD-NOT-RUN")
	runner := fakeRunner{out: map[string]string{key: "SHOULD-NOT-RUN\n"}, calls: map[string]int{}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "udid=ENV-UDID") {
		t.Fatalf("got %q, want udid=ENV-UDID", out.String())
	}
	if got := runner.calls[key]; got != 0 {
		t.Fatalf("target_command calls=%d, want 0 (MAV_TARGET_* must win without invoking it)", got)
	}
}

// TestTargetCommandSkippedForPhysicalDevice: target_command only ever
// resolves a simulator UDID; a device-kind target must never invoke it.
func TestTargetCommandSkippedForPhysicalDevice(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "DEV-1"
	cfg.DeviceName = "David iPhone"
	cfg.TargetCommand = "echo SHOULD-NOT-RUN"
	root, runID := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "echo SHOULD-NOT-RUN")
	runner := fakeRunner{out: map[string]string{key: "SHOULD-NOT-RUN\n"}, calls: map[string]int{}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "udid=DEV-1") || !strings.Contains(out.String(), "target_kind=device") {
		t.Fatalf("got %q, want udid=DEV-1 target_kind=device", out.String())
	}
	if got := runner.calls[key]; got != 0 {
		t.Fatalf("target_command calls=%d, want 0 (device targets must never invoke target_command)", got)
	}
}

// TestTargetCommandFailureFallsBackWithActionableWarning covers the
// explicit opt-out, target_command_required: false. There, and only there,
// a target_command that exits non-zero must not fail the command it
// decorates, must not crash mav, and must surface an actionable next step
// instead of silently pretending nothing happened. The default (required)
// behavior is TestTargetCommandFailureIsFatalByDefault below.
func TestTargetCommandFailureFallsBackWithActionableWarning(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "simpool with --print-udid"
	cfg.TargetCommandRequired = boolPtr(false)
	root, runID := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "simpool with --print-udid")
	runner := fakeRunner{results: map[string]CommandResult{
		key: {Stderr: "simpool: no free slot", Code: 1, Err: fmt.Errorf("exit status 1")},
	}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "target_command_warn=") {
		t.Fatalf("got %q, want an actionable target_command_warn field", out.String())
	}
	if !strings.Contains(out.String(), "simpool: no free slot") {
		t.Fatalf("got %q, want the command's own stderr surfaced", out.String())
	}
	if !strings.Contains(out.String(), "next:") {
		t.Fatalf("got %q, want an actionable next step", out.String())
	}
	if strings.Contains(out.String(), "fail ") {
		t.Fatalf("got %q, want the decorated command to still succeed (fall back, not fail)", out.String())
	}
}

// TestTargetCommandEmptyOutputFallsBackWithActionableWarning: a command that
// exits 0 but prints nothing is just as unusable as one that fails outright,
// and must degrade the same way.
func TestTargetCommandEmptyOutputFallsBackWithActionableWarning(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "true"
	cfg.TargetCommandRequired = boolPtr(false)
	root, runID := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "true")
	runner := fakeRunner{out: map[string]string{key: ""}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "target_command_warn=") {
		t.Fatalf("got %q, want an actionable target_command_warn field", out.String())
	}
	if !strings.Contains(out.String(), "target_command_empty") {
		t.Fatalf("got %q, want target_command_empty in the warning", out.String())
	}
}

func TestTargetCommandRoundTripsThroughConfig(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetCommand = "simpool with --print-udid"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TargetCommand != cfg.TargetCommand {
		t.Fatalf("target_command=%q, want %q", loaded.TargetCommand, cfg.TargetCommand)
	}
}

// TestTargetCommandFailureFallsBackToBootedUDIDForRealDispatch is the
// production regression: every test above only proves the warning is
// surfaced, never that the fallback UDID actually reaches the point where a
// command uses it. `mav doctor` reports a resolved udid when target_command
// fails because its report goes through withResolvedTarget, which used to
// carry the only copy of the booted-simulator fallback -- but `mav ui tree`
// builds its axe invocation straight from the cfg the ui dispatcher resolved
// earlier, so with the old code that invocation still carried an empty
// UDID and axe rejected it outright ("Missing expected argument '--udid
// <udid>'"), even though the same command's own target_command_warn field
// claimed mav had fallen back to the booted simulator. This must fail
// against the pre-fix code and pass once resolveConfigTarget applies the
// fallback to cfg itself.
func TestTargetCommandFailureFallsBackToBootedUDIDForRealDispatch(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "simpool lease --key A"
	cfg.TargetCommandRequired = boolPtr(false)
	cfg.Tools = map[string]bool{}
	root, _ := newTargetCommandRun(t, cfg)

	bootedJSON := `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-0":[` +
		`{"udid":"BOOTED-FALLBACK","name":"iPhone 17","state":"Booted"}]}}`
	targetKey := targetCommandKey(root, "simpool lease --key A")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			targetKey: {Stderr: "simpool: pool at capacity", Code: 1, Err: fmt.Errorf("exit status 1")},
		},
		out: map[string]string{
			"xcrun simctl list devices booted -j":    bootedJSON,
			"axe describe-ui --udid BOOTED-FALLBACK": `[{"AXUniqueId":"HomeView","AXRole":"Application"}]`,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.HasPrefix(got, "fail ") {
		t.Fatalf("got %q, want ui tree to succeed via the booted fallback, not fail", got)
	}
	if !strings.Contains(got, "target_command_warn=") {
		t.Fatalf("got %q, want target_command_warn reporting the target_command failure", got)
	}
	if !strings.Contains(got, "udid=BOOTED-FALLBACK") {
		t.Fatalf("got %q, want the fallback udid reported on success", got)
	}
	if !containsCall(runner.commands, "axe describe-ui --udid BOOTED-FALLBACK") {
		t.Fatalf("axe was never invoked with the fallback udid; commands=%v", runner.commands)
	}
}

// These tests exercise the target_command keepalive: `mav run` reinvoking
// target_command periodically as a pure liveness signal so a pool manager
// with its own wall-clock TTL (simpool's `lease`) never reclaims the slot
// out from under a run whose only long silence is a single build step. The
// three pingTargetCommandKeepAlive tests below are the deterministic core
// (no goroutines, no waiting on real time); TestRunFlowPingsTargetCommandKeepAliveDuringLongExecStep
// is the one integration test that proves the ticker is actually wired into
// `mav run`, using a real `sleep` inside an exec step (exec steps run
// through a real os/exec, not the injectable Runner -- see
// execFlowShellOutput) so the keepalive goroutine gets genuine wall-clock
// time to fire more than once.

func TestPingTargetCommandKeepAliveMatchingUDIDLogsNothing(t *testing.T) {
	root := t.TempDir()
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })

	key := targetCommandKey(root, "print-target")
	runner := fakeRunner{results: map[string]CommandResult{key: {Stdout: "TC-UDID-1\n"}}, calls: map[string]int{}}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}

	cli.pingTargetCommandKeepAlive(run, root, "print-target", defaultTargetCommandTimeout, "TC-UDID-1")

	if got := runner.calls[key]; got != 1 {
		t.Fatalf("target_command calls=%d, want 1 (the ping itself)", got)
	}
	data, _ := os.ReadFile(run.LogsPath)
	if strings.Contains(string(data), "keepalive") {
		t.Fatalf("logs=%q, want no keepalive warning when the ping's udid still matches", string(data))
	}
}

func TestPingTargetCommandKeepAliveMismatchWarnsButNeverChangesTarget(t *testing.T) {
	root := t.TempDir()
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })

	key := targetCommandKey(root, "print-target")
	runner := fakeRunner{out: map[string]string{key: "TC-UDID-STOLEN\n"}, calls: map[string]int{}}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}

	cli.pingTargetCommandKeepAlive(run, root, "print-target", defaultTargetCommandTimeout, "TC-UDID-ORIGINAL")

	data, err := os.ReadFile(run.LogsPath)
	if err != nil {
		t.Fatal(err)
	}
	logs := string(data)
	if !strings.Contains(logs, "keepalive") {
		t.Fatalf("logs=%q, want a keepalive warning on udid mismatch", logs)
	}
	if !strings.Contains(logs, "TC-UDID-STOLEN") || !strings.Contains(logs, "TC-UDID-ORIGINAL") {
		t.Fatalf("logs=%q, want both the new and the original udid named", logs)
	}
}

func TestPingTargetCommandKeepAliveFailureWarnsWithoutPanicking(t *testing.T) {
	root := t.TempDir()
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })

	key := targetCommandKey(root, "print-target")
	runner := fakeRunner{results: map[string]CommandResult{
		key: {Stderr: "simpool: no free slot", Code: 1, Err: fmt.Errorf("exit status 1")},
	}}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}

	cli.pingTargetCommandKeepAlive(run, root, "print-target", defaultTargetCommandTimeout, "TC-UDID-ORIGINAL")

	data, err := os.ReadFile(run.LogsPath)
	if err != nil {
		t.Fatal(err)
	}
	logs := string(data)
	if !strings.Contains(logs, "keepalive") || !strings.Contains(logs, "target_command_failed") {
		t.Fatalf("logs=%q, want a keepalive warning naming the failure", logs)
	}
}

func TestStartTargetCommandKeepAliveNoopWhenNotInEffect(t *testing.T) {
	root := t.TempDir()
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })

	runner := fakeRunner{calls: map[string]int{}}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	cfg := DefaultConfig(root)
	cfg.Root = root
	cfg.TargetCommand = "print-target"
	cfg.SimulatorUDID = "PINNED" // resolved cfg: target_command lost to a pin

	stop := cli.startTargetCommandKeepAlive(run, cfg, false /* inEffect: pin preempted it */)
	stop()

	if len(runner.calls) != 0 {
		t.Fatalf("calls=%v, want no target_command invocation when it's not actually in effect", runner.calls)
	}
}

// keepAliveExecRunner is a minimal, mutex-guarded Runner for
// TestRunFlowPingsTargetCommandKeepAliveDuringLongExecStep. The plain
// map-backed fakeRunner isn't safe here: unlike every other flow-step test,
// this one has two goroutines genuinely running at once (the keepalive
// ticker and the flow loop finishing up after the exec step), so a bare map
// write from each would race.
type keepAliveExecRunner struct {
	targetKey string
	udid      string

	mu    sync.Mutex
	calls int
}

func (r *keepAliveExecRunner) LookPath(file string) (string, error) { return "/usr/bin/" + file, nil }

func (r *keepAliveExecRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	key := name
	for _, a := range args {
		key += " " + a
	}
	if key == r.targetKey {
		r.mu.Lock()
		r.calls++
		r.mu.Unlock()
		return CommandResult{Stdout: r.udid + "\n"}
	}
	return CommandResult{}
}

func (r *keepAliveExecRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	return 123, nil
}

func (r *keepAliveExecRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestRunFlowPingsTargetCommandKeepAliveDuringLongExecStep is the
// production regression this whole mechanism exists for: a `mav run` whose
// only step is a build-shaped exec step that runs for real wall-clock time
// (a real `sleep`, not a canned Runner response -- exec steps bypass the
// injectable Runner entirely) must still reinvoke target_command more than
// once while that step is in flight, with the keepalive interval shrunk so
// the test doesn't need to wait tens of seconds. This must fail against the
// pre-fix code, where nothing touches target_command again once
// bindFlowTarget resolves it before the loop starts.
func TestRunFlowPingsTargetCommandKeepAliveDuringLongExecStep(t *testing.T) {
	original := targetCommandKeepAliveInterval
	targetCommandKeepAliveInterval = 60 * time.Millisecond
	t.Cleanup(func() { targetCommandKeepAliveInterval = original })

	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AllowShell = true
	cfg.TargetCommand = "print-target"
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

	flowPath := filepath.Join(root, "flow.yaml")
	flowYAML := "name: keepalive-flow\nsteps:\n  - exec: { cmd: \"sleep 0.35\" }\n"
	if err := os.WriteFile(flowPath, []byte(flowYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &keepAliveExecRunner{targetKey: targetCommandKey(root, "print-target"), udid: "TC-UDID-STABLE"}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath, "--run", run.ID}); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(out.String(), "fail ") {
		t.Fatalf("run failed: %s", out.String())
	}

	if calls := runner.callCount(); calls < 3 {
		t.Fatalf("target_command calls=%d, want >=3 (1 initial resolution + at least 2 keepalive pings during the 0.35s exec step)", calls)
	}
	data, _ := os.ReadFile(run.LogsPath)
	if strings.Contains(string(data), "keepalive") {
		t.Fatalf("logs=%q, want no keepalive warning: target_command returned the same udid every time", string(data))
	}
}

// TestTargetCommandFailureIsFatalByDefault is the regression this whole
// change exists for. Before it, a target_command that failed left mav
// driving whatever simulator happened to be booted, and the only trace was
// a warn field that nearly every call site discarded -- so a screenshot run
// piping mav to /dev/null published images from a device nobody selected.
// With target_command configured and target_command_required left unset,
// the command must fail, name the command, and say that no fallback was
// taken.
func TestTargetCommandFailureIsFatalByDefault(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "simpool lease --key A"
	cfg.Tools = map[string]bool{}
	root, _ := newTargetCommandRun(t, cfg)

	bootedJSON := `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-0":[` +
		`{"udid":"BOOTED-FALLBACK","name":"iPhone 17","state":"Booted"}]}}`
	targetKey := targetCommandKey(root, "simpool lease --key A")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			targetKey: {Stderr: "simpool: pool at capacity", Code: 1, Err: fmt.Errorf("exit status 1")},
		},
		out: map[string]string{
			"xcrun simctl list devices booted -j":    bootedJSON,
			"axe describe-ui --udid BOOTED-FALLBACK": `[{"AXUniqueId":"HomeView","AXRole":"Application"}]`,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err == nil {
		t.Fatalf("got a zero exit, want a non-zero one; output=%q", out.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "fail code=target_command_failed ") {
		t.Fatalf("got %q, want a fail line with code=target_command_failed", got)
	}
	for _, want := range []string{
		"fallback=none",
		"simpool: pool at capacity",
		"simpool lease --key A",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "BOOTED-FALLBACK") {
		t.Fatalf("got %q, want no trace of the booted simulator: it must not be resolved at all", got)
	}
	if containsCall(runner.commands, "axe describe-ui --udid BOOTED-FALLBACK") {
		t.Fatalf("axe was dispatched against the booted fallback; commands=%v", runner.commands)
	}
}

// TestTargetCommandEmptyOutputIsFatalByDefault: a command that exits 0 but
// prints nothing is just as unusable as one that fails outright, and by
// default must be just as fatal -- with its own code, because "fix the
// command's contract" is a different next step from "the pool said no".
func TestTargetCommandEmptyOutputIsFatalByDefault(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "true"
	cfg.Tools = map[string]bool{}
	root, _ := newTargetCommandRun(t, cfg)

	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		out: map[string]string{
			targetCommandKey(root, "true"):        "",
			"xcrun simctl list devices booted -j": `{"devices":{"iOS":[{"udid":"BOOTED-FALLBACK","name":"iPhone 17","state":"Booted"}]}}`,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err == nil {
		t.Fatalf("got a zero exit, want a non-zero one; output=%q", out.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "fail code=target_command_empty ") {
		t.Fatalf("got %q, want a fail line with code=target_command_empty", got)
	}
	if !strings.Contains(got, "fallback=none") {
		t.Fatalf("got %q, want fallback=none", got)
	}
}

// TestTargetCommandTimeoutIsFatalAndDoesNotFallBack is the reported defect
// in its original shape: the timeout was 10s, simpool's cold lease takes
// about two minutes, so every cold lease timed out and mav carried on
// against a booted simulator. This drives a real /bin/bash through the real
// runner -- the only way to exercise the context deadline, which a fake
// runner would never honour -- with target_command_timeout dialled down so
// the test costs a second rather than a minute.
func TestTargetCommandTimeoutIsFatalAndDoesNotFallBack(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetCommand = "sleep 5; echo NEVER-REACHED"
	cfg.TargetCommandTimeout = "300ms"
	cfg.Tools = map[string]bool{}
	newTargetCommandRun(t, cfg)

	var out bytes.Buffer
	cli := CLI{Runner: ExecRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	start := time.Now()
	err := cli.Run(context.Background(), []string{"ui", "tree"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("got a zero exit, want a non-zero one; output=%q", out.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "fail code=target_command_timeout ") {
		t.Fatalf("got %q, want a fail line with code=target_command_timeout", got)
	}
	for _, want := range []string{"timeout=300ms", "fallback=none"} {
		if !strings.Contains(got, want) {
			t.Fatalf("got %q, want it to contain %q", got, want)
		}
	}
	if elapsed > 4*time.Second {
		t.Fatalf("took %s, want the configured timeout to actually bound the wait, not the 5s the command sleeps", elapsed)
	}
}

// TestTargetCommandTimeoutDefaultsPastAColdLease guards the default itself.
// 10s was below the documented cost of the one consumer target_command was
// designed for, which is what turned every cold lease into a silent
// fallback. A default under two minutes is that bug again.
func TestTargetCommandTimeoutDefaultsPastAColdLease(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "simpool lease"
	timeout, err := targetCommandTimeoutFor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if timeout != defaultTargetCommandTimeout {
		t.Fatalf("timeout=%s, want the default %s", timeout, defaultTargetCommandTimeout)
	}
	if timeout < 2*time.Minute {
		t.Fatalf("default timeout=%s, want at least the ~2min a cold simpool lease costs", timeout)
	}
}

// TestTargetCommandTimeoutInvalidIsFatal: silently substituting the default
// for a value the config states is the same silent-substitution bug in
// miniature, so a malformed duration is refused outright.
func TestTargetCommandTimeoutInvalidIsFatal(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "simpool lease"
	cfg.TargetCommandTimeout = "two minutes"
	cfg.Tools = map[string]bool{}
	root, _ := newTargetCommandRun(t, cfg)

	runner := &sequenceRecordingRunner{tools: map[string]bool{"axe": true}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err == nil {
		t.Fatalf("got a zero exit, want a non-zero one; output=%q", out.String())
	}
	if !strings.HasPrefix(out.String(), "fail code=target_command_timeout_invalid ") {
		t.Fatalf("got %q, want code=target_command_timeout_invalid", out.String())
	}
}

// TestTargetCommandFailureIsNotCachedWhenRequired: the run-scoped cache
// keeps negative entries only in the opt-out mode, where the run carries on
// and would otherwise pay the timeout on every command that follows. When
// target_command is required the command exits, so there is no such
// sequence to protect -- and caching the failure would make the next run
// inside the TTL fail on evidence it never re-tested, which is precisely
// the "second run after a slow lease also fails without retrying" problem.
func TestTargetCommandFailureIsNotCachedWhenRequired(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "print-target"
	cfg.Tools = map[string]bool{}
	root, _ := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "print-target")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			key: {Stderr: "simpool: pool at capacity", Code: 1, Err: fmt.Errorf("exit status 1")},
		},
	}
	for i := 0; i < 2; i++ {
		var out bytes.Buffer
		cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), []string{"ui", "tree"}); err == nil {
			t.Fatalf("call %d: got a zero exit, want a non-zero one; output=%q", i, out.String())
		}
	}
	invocations := 0
	for _, command := range runner.commands {
		if strings.Contains(command, "print-target") {
			invocations++
		}
	}
	if invocations != 2 {
		t.Fatalf("target_command ran %d times across two failing commands, want 2 (a required failure must not be cached)", invocations)
	}
}

// TestTargetCommandRequiredOptOutStillCachesFailures is the other half of
// that decision: the opt-out mode is exactly the case negative caching was
// built for, and it must keep working there.
func TestTargetCommandRequiredOptOutStillCachesFailures(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "print-target"
	cfg.TargetCommandRequired = boolPtr(false)
	root, runID := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "print-target")
	runner := fakeRunner{
		results: map[string]CommandResult{
			key: {Stderr: "simpool: pool at capacity", Code: 1, Err: fmt.Errorf("exit status 1")},
		},
		calls: map[string]int{},
	}
	for i := 0; i < 3; i++ {
		var out bytes.Buffer
		cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := runner.calls[key]; got != 1 {
		t.Fatalf("target_command calls=%d, want 1 (the opt-out mode keeps caching failures for the run)", got)
	}
}

// TestTargetCommandRequiredRoundTripsThroughConfig: both new knobs have to
// survive a save/load cycle, and an unset target_command_required has to
// stay unset rather than being written back as an explicit false, which
// would silently pin the old behavior into every config mav rewrites.
func TestTargetCommandRequiredRoundTripsThroughConfig(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetCommand = "simpool lease"
	cfg.TargetCommandRequired = boolPtr(false)
	cfg.TargetCommandTimeout = "4m"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TargetCommandRequired == nil || *loaded.TargetCommandRequired {
		t.Fatalf("target_command_required=%v, want an explicit false", loaded.TargetCommandRequired)
	}
	if loaded.TargetCommandTimeout != "4m" {
		t.Fatalf("target_command_timeout=%q, want 4m", loaded.TargetCommandTimeout)
	}

	plain := DefaultConfig(root)
	plain.TargetCommand = "simpool lease"
	if err := SaveConfig(root, plain); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.TargetCommandRequired != nil {
		t.Fatalf("target_command_required=%v, want it to stay unset", *reloaded.TargetCommandRequired)
	}
	if !targetCommandRequired(reloaded) {
		t.Fatal("an unset target_command_required must resolve to required")
	}
}

// TestTargetCommandFailureFailsAFlowBeforeAnyStepRuns: runFlow resolves the
// target once, up front, so a target_command that is already failing kills
// the run before step 1 -- no step is attempted and no driver is invoked.
// It does NOT cover flowStepTargetFailure; see
// TestTargetCommandFailureMidFlowCarriesItsFieldsIntoTheStep for that.
func TestTargetCommandFailureFailsAFlowBeforeAnyStepRuns(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetCommand = "print-target"
	cfg.Tools = map[string]bool{}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flow := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flow, []byte("name: t\nsteps:\n  - tap:\n      id: save\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	key := targetCommandKey(root, "print-target")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			key: {Stderr: "simpool: pool at capacity", Code: 1, Err: fmt.Errorf("exit status 1")},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flow}); err == nil {
		t.Fatalf("got a zero exit, want a non-zero one; output=%q", out.String())
	}
	got := out.String()
	if !strings.Contains(got, "code=target_command_failed") {
		t.Fatalf("got %q, want code=target_command_failed", got)
	}
	if !strings.Contains(got, "fallback=none") {
		t.Fatalf("got %q, want fallback=none", got)
	}
	if strings.Contains(got, "step=") {
		t.Fatalf("got %q, want the run to die before any step is attempted", got)
	}
	if containsCall(runner.commands, "axe describe-ui") {
		t.Fatalf("a driver was invoked before the target was resolved; commands=%v", runner.commands)
	}
}

// TestTargetCommandFailureMidFlowCarriesItsFieldsIntoTheStep covers the
// path the test above does not: target_command resolves fine at flow start,
// the run-scoped cache is dropped mid-run (which is what
// staleSimulatorTargetRetry does, and what the TTL does on a long flow),
// and the next step re-resolves into a failure. That step must report the
// stable code -- not the whole sentence, which is what a flow step's error
// text becomes in the run's `code=` field -- and must carry the command,
// the timeout and fallback=none in its own fields.
func TestTargetCommandFailureMidFlowCarriesItsFieldsIntoTheStep(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetCommand = "print-target"
	cfg.Tools = map[string]bool{}
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
	// Seed the run-scoped cache with a good resolution, then have the
	// command itself fail: the flow's up-front resolution is served from
	// the cache, and the step that re-resolves after the cache is dropped
	// pays the real, failing call.
	writeTargetCommandCache(run, "TC-UDID-GOOD", "iPhone 17", "")

	key := targetCommandKey(root, "print-target")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			key: {Stderr: "simpool: slot reclaimed", Code: 1, Err: fmt.Errorf("exit status 1")},
		},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: io.Discard, Stderr: &bytes.Buffer{}}.withRun(run)

	step := FlowStep{Action: "tap", Params: map[string]string{"id": "save", "timeout": "5"}}
	invalidateTargetCommandCache(run)
	fields, stepErr := cli.executeFlowStepWithOptions(context.Background(), GlobalOptions{}, run, 1, step)
	if stepErr == nil {
		t.Fatal("the step succeeded; want it to fail on the re-resolved target_command")
	}
	if stepErr.Error() != "target_command_failed" {
		t.Fatalf("step error=%q, want the bare code (it is emitted verbatim as the run's code= field)", stepErr.Error())
	}
	if fields["fallback"] != "none" {
		t.Fatalf("fields=%v, want fallback=none", fields)
	}
	if fields["target_command"] != "print-target" {
		t.Fatalf("fields=%v, want the command named", fields)
	}
	if fields["target_command_timeout"] == "" {
		t.Fatalf("fields=%v, want the timeout reported", fields)
	}
	// The step's own `timeout` param must survive: target_command's timeout
	// is a different number under a different key on purpose.
	if fields["timeout"] != "5" {
		t.Fatalf("fields=%v, want the step's own timeout param untouched", fields)
	}
	if containsCall(runner.commands, "axe") {
		t.Fatalf("axe was dispatched despite an unresolved target; commands=%v", runner.commands)
	}
}

// TestDoctorDiagnosesThroughAFailingTargetCommand: doctor is the command you
// run BECAUSE the target is broken, so it reports the failure as a field and
// still produces the diagnosis. Failing outright here would withhold the
// tool and driver state at exactly the moment it is wanted.
func TestDoctorDiagnosesThroughAFailingTargetCommand(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "print-target"
	root, _ := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "print-target")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			key: {Stderr: "simpool: pool at capacity", Code: 1, Err: fmt.Errorf("exit status 1")},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatalf("doctor failed: %v; output=%q", err, out.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "ok cmd=doctor ") {
		t.Fatalf("got %q, want doctor to still report", got)
	}
	if !strings.Contains(got, "target_command_warn=") {
		t.Fatalf("got %q, want the target_command failure reported as a field", got)
	}
	if !strings.Contains(got, "target_kind=") {
		t.Fatalf("got %q, want the diagnosis itself to survive the failure", got)
	}
}

// TestSimSelectStillWorksThroughAFailingTargetCommand: pinning
// simulator_udid beats target_command in the precedence order and is the
// documented escape from a broken pool manager. Failing `mav sim select` on
// the very failure it exists to escape would close the only exit.
func TestSimSelectStillWorksThroughAFailingTargetCommand(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "print-target"
	root, _ := newTargetCommandRun(t, cfg)

	key := targetCommandKey(root, "print-target")
	simsJSON := `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-0":[` +
		`{"udid":"PINNED-UDID","name":"iPhone 17 Pro","state":"Shutdown","isAvailable":true}]}}`
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			key: {Stderr: "simpool: pool at capacity", Code: 1, Err: fmt.Errorf("exit status 1")},
		},
		out: map[string]string{"xcrun simctl list devices -j": simsJSON},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "select", "--udid", "PINNED-UDID"}); err != nil {
		t.Fatalf("sim select failed: %v; output=%q", err, out.String())
	}
	if !strings.Contains(out.String(), "udid=PINNED-UDID") {
		t.Fatalf("got %q, want the pin to be applied", out.String())
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SimulatorUDID != "PINNED-UDID" {
		t.Fatalf("simulator_udid=%q, want the pin persisted", loaded.SimulatorUDID)
	}
}

// TestTargetCommandFailureLeavesEvidenceOnDisk: the failure this whole
// change exists for is a script that pipes mav to /dev/null. A non-zero
// exit with nothing on disk to look at afterwards would only move the
// problem. The commands trail records Code, not Err, so the entry has to
// carry a non-zero one.
func TestTargetCommandFailureLeavesEvidenceOnDisk(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetCommand = "print-target"
	cfg.Tools = map[string]bool{}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flow := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flow, []byte("name: t\nsteps:\n  - tap:\n      id: save\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := targetCommandKey(root, "print-target")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			key: {Stderr: "simpool: pool at capacity", Code: 1, Err: fmt.Errorf("exit status 1")},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flow}); err == nil {
		t.Fatalf("got a zero exit, want a non-zero one; output=%q", out.String())
	}
	runID := strings.TrimSpace(readCurrentRunForTest(t, root))
	runDir := filepath.Join(root, MavDir, "runs", runID)

	commands, err := os.ReadFile(filepath.Join(runDir, "commands.jsonl"))
	if err != nil {
		t.Fatalf("no commands trail: %v", err)
	}
	if !strings.Contains(string(commands), "target_command") {
		t.Fatalf("commands trail=%q, want a target_command entry", commands)
	}
	if !strings.Contains(string(commands), `"code":1`) {
		t.Fatalf("commands trail=%q, want a non-zero code (the trail records Code, not Err)", commands)
	}
	runJSON, err := os.ReadFile(filepath.Join(runDir, "run.json"))
	if err != nil {
		t.Fatalf("no run.json: %v", err)
	}
	if !strings.Contains(string(runJSON), `"status": "failed"`) {
		t.Fatalf("run.json=%q, want status failed", runJSON)
	}
	if _, err := os.Stat(filepath.Join(runDir, "report.json")); err != nil {
		t.Fatalf("no report: %v (cleanupFailedFlow must still run its report)", err)
	}
}

func readCurrentRunForTest(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, CurrentRunRef))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestKeepAliveUsesTheConfiguredTimeout pins the wiring the three ping tests
// below do not: startTargetCommandKeepAlive must derive its timeout from
// cfg.TargetCommandTimeout, not hardcode the default.
func TestKeepAliveUsesTheConfiguredTimeout(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommandTimeout = "45s"
	timeout, err := targetCommandTimeoutFor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 45*time.Second {
		t.Fatalf("timeout=%s, want 45s", timeout)
	}
}

// TestSetupPreservesTargetCommandPolicy is the regression for a defect the
// review caught: mergeSetupConfig carried target_command forward but not the
// two fields that say how it is enforced, so `mav setup` on an existing
// project silently re-armed the hard failure a project had explicitly opted
// out of, and reset a slower pool manager's timeout to the default. The
// migration this whole change documents is target_command_required: false;
// the repo's own onboarding command must not delete it.
func TestSetupPreservesTargetCommandPolicy(t *testing.T) {
	existing := DefaultConfig(t.TempDir())
	existing.TargetCommand = "simpool lease"
	existing.TargetCommandRequired = boolPtr(false)
	existing.TargetCommandTimeout = "5m"

	merged := mergeSetupConfig(existing, DefaultConfig(existing.Root))
	if merged.TargetCommandRequired == nil || *merged.TargetCommandRequired {
		t.Fatalf("target_command_required=%v, want the explicit false to survive setup", merged.TargetCommandRequired)
	}
	if merged.TargetCommandTimeout != "5m" {
		t.Fatalf("target_command_timeout=%q, want 5m to survive setup", merged.TargetCommandTimeout)
	}
	if merged.TargetCommand != "simpool lease" {
		t.Fatalf("target_command=%q, want it to survive setup", merged.TargetCommand)
	}
}
