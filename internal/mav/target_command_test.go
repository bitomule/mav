package mav

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
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
