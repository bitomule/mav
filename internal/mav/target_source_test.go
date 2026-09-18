package mav

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// The defect these tests pin down, measured on 2026-09-19: with several
// simulators booted and nothing in .mav/config.yaml naming one, mav drove
// the first entry of a randomised map iteration and answered `ok`. An
// agent whose config had a misspelt key ended up reading the accessibility
// tree of a simulator another agent had leased, and the only trace was a
// UDID field nobody compares by hand.
//
// `mav logs --run <id>` is the established probe for target resolution in
// this repo (see target_command_test.go's header): it touches Runner.Run
// only through resolution, so nothing else in the command surface can
// explain a result.

const twoBootedJSON = `{"devices":{` +
	`"com.apple.CoreSimulator.SimRuntime.iOS-26-3":[` +
	`{"udid":"AAAA-1111","name":"iPhone 17 Pro","state":"Booted"}],` +
	`"com.apple.CoreSimulator.SimRuntime.iOS-27-1":[` +
	`{"udid":"BBBB-2222","name":"iPhone Duo","state":"Booted"}]}}`

const oneBootedJSON = `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-3":[` +
	`{"udid":"AAAA-1111","name":"iPhone 17 Pro","state":"Booted"}]}}`

func newLogsRun(t *testing.T, cfg Config) (string, string) {
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

func runLogs(t *testing.T, root, runID string, booted string) string {
	t.Helper()
	runner := fakeRunner{out: map[string]string{
		"xcrun simctl list devices booted -j": booted,
	}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	_ = cli.Run(context.Background(), []string{"logs", "--run", runID})
	return out.String()
}

// The bug itself, in the command it was measured with: `mav ui tree` with
// two simulators booted and nothing configured used to read the tree of
// whichever one simctl's map handed over first, and report ok. mav must
// refuse and say which two, so the reader can pick one without going back
// to simctl.
func TestTwoBootedSimulatorsAndNoConfigIsRefused(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Tools = map[string]bool{}
	root, _ := newTargetCommandRun(t, cfg)

	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		out: map[string]string{
			"xcrun simctl list devices booted -j": twoBootedJSON,
			"axe describe-ui --udid AAAA-1111":    `[{"AXUniqueId":"HomeView","AXRole":"Application"}]`,
			"axe describe-ui --udid BBBB-2222":    `[{"AXUniqueId":"HomeView","AXRole":"Application"}]`,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err == nil {
		t.Fatalf("got a zero exit, want a non-zero one; output=%q", out.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "fail code=ambiguous_booted_simulator ") {
		t.Fatalf("an ambiguous choice must be a refusal, not an ok:\n%s", got)
	}
	for _, want := range []string{"AAAA-1111", "BBBB-2222", "iPhone Duo", "booted_count=2", "fallback=none", "mav sim select"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output should contain %q so the reader can pick without going back to simctl:\n%s", want, got)
		}
	}
}

// The same two booted simulators with a correctly written config: `ui
// tree` reads the pinned one and nothing changes from before.
func TestTwoBootedSimulatorsWithPinDispatchToThePin(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Tools = map[string]bool{}
	cfg.SimulatorUDID = "BBBB-2222"
	cfg.SimulatorName = "iPhone Duo"
	root, _ := newTargetCommandRun(t, cfg)

	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		out: map[string]string{
			"xcrun simctl list devices booted -j": twoBootedJSON,
			"axe describe-ui --udid BBBB-2222":    `[{"AXUniqueId":"HomeView","AXRole":"Application"}]`,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatalf("a pinned target is never ambiguous: %v; output=%q", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "udid=BBBB-2222") || !strings.Contains(got, "target_source=config") {
		t.Fatalf("the ok line must name the pin and attribute it to the config:\n%s", got)
	}
}

// The control: one booted and nothing configured is NOT ambiguous, and
// keeps working exactly as before -- plus it now says where the simulator
// came from.
func TestSingleBootedSimulatorStillResolves(t *testing.T) {
	root, runID := newLogsRun(t, DefaultConfig(t.TempDir()))
	got := runLogs(t, root, runID, oneBootedJSON)

	if !strings.Contains(got, "udid=AAAA-1111") {
		t.Fatalf("one booted simulator is an unambiguous answer:\n%s", got)
	}
	if !strings.Contains(got, "target_source=booted") {
		t.Fatalf("the ok line must say mav chose this one itself:\n%s", got)
	}
}

// The other control, and the one that matters most: a config that IS
// written correctly must behave exactly as it did before the change, with
// the two booted simulators that used to make the choice ambiguous.
func TestPinnedSimulatorWinsOverSeveralBooted(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.SimulatorUDID = "PINNED-9999"
	cfg.SimulatorName = "iPhone 17 Pro"
	root, runID := newLogsRun(t, cfg)
	got := runLogs(t, root, runID, twoBootedJSON)

	if !strings.Contains(got, "udid=PINNED-9999") {
		t.Fatalf("an explicit simulator_udid wins over anything booted:\n%s", got)
	}
	if !strings.Contains(got, "target_source=config") {
		t.Fatalf("the ok line must attribute the choice to the config:\n%s", got)
	}
	if strings.Contains(got, "ambiguous_booted_simulator") {
		t.Fatalf("a pinned target is never ambiguous:\n%s", got)
	}
}

// The simpool case: a leased slot arrives through target_command, and it
// wins over whatever else is booted on a shared machine.
func TestTargetCommandWinsOverSeveralBooted(t *testing.T) {
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
		t.Fatalf("the leased slot is the target:\n%s", got)
	}
	if !strings.Contains(got, "target_source=target_command") {
		t.Fatalf("the ok line must attribute the choice to the pool manager:\n%s", got)
	}
}

// MAV_TARGET_* is what `simpool with`/`acquire` export around a whole
// command, and what `mav run --matrix` sets on its children. It short-
// circuits everything below it, and says so.
func TestEnvTargetIsReportedAsItsOwnSource(t *testing.T) {
	root, runID := newLogsRun(t, DefaultConfig(t.TempDir()))
	t.Setenv("MAV_TARGET_KIND", "simulator")
	t.Setenv("MAV_TARGET_UDID", "ENV-5555")
	t.Setenv("MAV_TARGET_NAME", "iPhone 17 Pro")
	got := runLogs(t, root, runID, twoBootedJSON)

	if !strings.Contains(got, "udid=ENV-5555") {
		t.Fatalf("MAV_TARGET_UDID wins:\n%s", got)
	}
	if !strings.Contains(got, "target_source=env") {
		t.Fatalf("the ok line must attribute the choice to the environment:\n%s", got)
	}
}

// The ambiguity is a property of the machine right now, not of the run: it
// must not be cached, or shutting a simulator down would leave the run
// failing on evidence it never re-tested.
func TestAmbiguityIsNotCachedAcrossCommands(t *testing.T) {
	root, runID := newLogsRun(t, DefaultConfig(t.TempDir()))
	if got := runLogs(t, root, runID, twoBootedJSON); !strings.Contains(got, "ambiguous_booted_simulator") {
		t.Fatalf("expected the refusal first:\n%s", got)
	}
	got := runLogs(t, root, runID, oneBootedJSON)
	if !strings.Contains(got, "udid=AAAA-1111") {
		t.Fatalf("once only one simulator is booted the run must recover:\n%s", got)
	}
}

// detectBootedSimulators is the layer the randomness lived in: it used to
// return the first match of a `range` over simctl's runtime map. Sorted
// output is what makes both the refusal message and this test stable.
func TestBootedSimulatorsAreListedDeterministically(t *testing.T) {
	runner := fakeRunner{out: map[string]string{
		"xcrun simctl list devices booted -j": twoBootedJSON,
	}}
	for i := 0; i < 20; i++ {
		booted := detectBootedSimulators(runner)
		if len(booted) != 2 || booted[0].UDID != "AAAA-1111" || booted[1].UDID != "BBBB-2222" {
			t.Fatalf("iteration %d: got %+v, want a stable AAAA,BBBB order", i, booted)
		}
	}
}
