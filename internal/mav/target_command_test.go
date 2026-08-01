package mav

import (
	"bytes"
	"context"
	"fmt"
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

// TestTargetCommandFailureFallsBackWithActionableWarning proves the "never a
// panic or a hang" requirement: a target_command that exits non-zero must
// not fail the command it decorates, must not crash mav, and must surface an
// actionable next step instead of silently pretending nothing happened.
func TestTargetCommandFailureFallsBackWithActionableWarning(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "simpool with --print-udid"
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
			"xcrun simctl list devices booted -j":     bootedJSON,
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

	cli.pingTargetCommandKeepAlive(run, root, "print-target", "TC-UDID-1")

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

	cli.pingTargetCommandKeepAlive(run, root, "print-target", "TC-UDID-ORIGINAL")

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

	cli.pingTargetCommandKeepAlive(run, root, "print-target", "TC-UDID-ORIGINAL")

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
