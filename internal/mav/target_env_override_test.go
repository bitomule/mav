package mav

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The defect these pin down, measured on 2026-09-19 against the published
// v0.19.2 in a repo with target_command configured:
//
//	sin variable                          -> target_source=target_command  udid=ED10DC4F
//	MAV_TARGET_UDID=30F898A9 mav ui tree  -> target_source=target_command  udid=ED10DC4F
//	mav ui tree --target udid=30F898A9    -> target_source=target_command  udid=ED10DC4F
//
// Both overrides were ignored without a word, against SKILL.md, which
// documents MAV_TARGET_KIND/UDID/NAME/RUNTIME as pinning the target and
// beating both a config pin and target_command. The cause was that the
// whole environment overlay in loadConfig hung off MAV_TARGET_KIND being
// non-empty, so MAV_TARGET_UDID on its own reached nothing.
//
// The second half is subtler and is why every test here asserts the
// PROVENANCE as well as the UDID: resolveConfigTarget decided
// target_source=env by re-reading MAV_TARGET_KIND, so fixing only the
// overlay would drive the right simulator and label it `config` -- a
// correct UDID with a false origin, which is the one thing target_source
// exists to prevent.
//
// `mav logs --run <id>` is this repo's established probe for target
// resolution (see target_command_test.go): it reaches Runner.Run only
// through resolution, so nothing else can explain the result.

// MAV_TARGET_UDID on its own, which is the spelling `simpool acquire`
// exports and the one that was measured doing nothing.
func TestEnvUDIDWithoutKindBeatsTargetCommand(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "simpool lease --device \"iPhone 17 Pro\""
	root, runID := newLogsRun(t, cfg)

	t.Setenv("MAV_TARGET_UDID", "ENV-5555")

	runner := fakeRunner{out: map[string]string{
		"xcrun simctl list devices booted -j":     twoBootedJSON,
		targetCommandKey(root, cfg.TargetCommand): "LEASED-7777\n",
	}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "udid=ENV-5555") {
		t.Fatalf("MAV_TARGET_UDID alone must beat target_command:\n%s", got)
	}
	if !strings.Contains(got, "target_source=env") {
		t.Fatalf("a UDID from the environment must be attributed to the environment:\n%s", got)
	}
}

// A repo that pinned a simulator with `mav sim select` and an agent that
// exports MAV_TARGET_UDID around one command: the variable is the more
// specific decision and wins, and says where it came from.
func TestEnvUDIDWithoutKindBeatsConfigPin(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.SimulatorUDID = "PINNED-3333"
	root, runID := newLogsRun(t, cfg)

	t.Setenv("MAV_TARGET_UDID", "ENV-5555")
	got := runLogs(t, root, runID, twoBootedJSON)

	if !strings.Contains(got, "udid=ENV-5555") {
		t.Fatalf("MAV_TARGET_UDID alone must beat a pinned simulator_udid:\n%s", got)
	}
	if !strings.Contains(got, "target_source=env") {
		t.Fatalf("the ok line must attribute the choice to the environment:\n%s", got)
	}
}

// The control the fix must not break: nothing in the environment, a pin in
// .mav/config.yaml, and target_command configured. The pin still wins and
// still says so, and target_command still reports that it is dead
// configuration.
func TestPinnedUDIDStillWinsWithNoEnvOverride(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.SimulatorUDID = "PINNED-3333"
	cfg.TargetCommand = "simpool lease --device \"iPhone 17 Pro\""
	root, runID := newLogsRun(t, cfg)

	got := runLogs(t, root, runID, twoBootedJSON)

	if !strings.Contains(got, "udid=PINNED-3333") {
		t.Fatalf("a pin still wins over target_command:\n%s", got)
	}
	if !strings.Contains(got, "target_source=config") {
		t.Fatalf("a pin is still attributed to the config file:\n%s", got)
	}
	if !strings.Contains(got, "target_command_ignored") {
		t.Fatalf("target_command must still say it is not in effect:\n%s", got)
	}
}

// The other control: a correctly configured repo with no overrides at all
// must resolve exactly as it did before the fix.
func TestTargetCommandStillWinsWithNoOverrides(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "simpool lease --device \"iPhone 17 Pro\""
	root, runID := newLogsRun(t, cfg)

	runner := fakeRunner{out: map[string]string{
		"xcrun simctl list devices booted -j":     twoBootedJSON,
		targetCommandKey(root, cfg.TargetCommand): "LEASED-7777\n",
	}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "udid=LEASED-7777") {
		t.Fatalf("with no overrides the leased slot is still the target:\n%s", got)
	}
	if !strings.Contains(got, "target_source=target_command") {
		t.Fatalf("with no overrides the choice still belongs to the pool manager:\n%s", got)
	}
}

// MAV_TARGET_RUNTIME narrows a target, it does not name one, and nothing in
// mav resolves it to a UDID. Treating it as a pin would skip target_command
// and leave the caller with nothing to dispatch against, so it must not
// short-circuit resolution.
func TestEnvRuntimeAloneDoesNotShortCircuitResolution(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "simpool lease --device \"iPhone 17 Pro\""
	root, runID := newLogsRun(t, cfg)

	t.Setenv("MAV_TARGET_RUNTIME", "com.apple.CoreSimulator.SimRuntime.iOS-26-3")

	runner := fakeRunner{out: map[string]string{
		"xcrun simctl list devices booted -j":     twoBootedJSON,
		targetCommandKey(root, cfg.TargetCommand): "LEASED-7777\n",
	}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"logs", "--run", runID}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "udid=LEASED-7777") {
		t.Fatalf("a runtime alone must still leave target_command to answer:\n%s", got)
	}
	if !strings.Contains(got, "target_source=target_command") {
		t.Fatalf("a runtime alone does not make the environment the source:\n%s", got)
	}
}

// The smaller half of the same family: `--target` is a `mav run` flag, and
// every other command used to accept it and ignore it in silence, so
// `mav ui tree --target udid=...` inspected whatever the config resolved to
// and printed a clean ok line for it.
func TestUnsupportedTargetFlagIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"ui", "tree", "--target", "udid=30F898A9"},
		{"ui", "tree", "--target=udid=30F898A9"},
		{"capture", "--target", "30F898A9"},
	} {
		var out bytes.Buffer
		cli := CLI{Runner: fakeRunner{}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
		_ = cli.Run(context.Background(), args)
		got := out.String()
		if !strings.Contains(got, "code=flag_unsupported") {
			t.Fatalf("%v must be refused, not ignored:\n%s", args, got)
		}
		if !strings.Contains(got, "MAV_TARGET_UDID") {
			t.Fatalf("%v must be told the spelling that works:\n%s", args, got)
		}
	}
}

// ...and the control for it: `mav run` is the command that does read
// --target, and the guard must not reach it.
func TestRunStillAcceptsTheTargetFlag(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	_ = cli.Run(context.Background(), []string{"run", "nope.yaml", "--target", "AAAA-1111"})
	if strings.Contains(out.String(), "flag_unsupported") {
		t.Fatalf("mav run owns --target:\n%s", out.String())
	}
}
