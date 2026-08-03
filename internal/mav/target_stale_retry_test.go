package mav

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// These tests exercise the other half of target_command's job: a cached
// resolution isn't just slow to obtain, it can go stale underneath a run --
// the simulator a pool manager handed out can shut down inside the cache's
// TTL (see bootedSimulatorCacheTTL), and dispatching against it fails
// instantly with something like axe's "... as it is not booted", not by
// retrying anything. dispatchWithStaleTargetRetry (target.go) is what turns
// that into "re-resolve via target_command once, retry once" instead of a
// hard failure, using `simctl list devices booted` as the ground truth for
// "is this actually stale" -- never axe/idb's own stderr wording -- so a
// real, unrelated failure never wastes the retry's ~15s.
//
// newStaleTargetCommandRun mirrors newTargetCommandRun (target_command_test.go)
// but also primes the run's target-command cache with an already-resolved
// UDID, simulating a run that resolved target_command a couple of commands
// ago and is now dispatching against a simulator that quietly shut down in
// the meantime.
func newStaleTargetCommandRun(t *testing.T, cfg Config, cachedUDID string) (string, RunState) {
	t.Helper()
	root, runID := newTargetCommandRun(t, cfg)
	run, err := LoadRun(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	writeTargetCommandCache(run, cachedUDID, "", "")
	return root, run
}

func countCommandCalls(commands []string, want string) int {
	n := 0
	for _, c := range commands {
		if c == want {
			n++
		}
	}
	return n
}

// TestUITreeRetriesOnceWhenCachedSimulatorNoLongerBooted is the core repro
// from the bug report: `mav ui tree` dispatches against a target_command-
// cached UDID that has since shut down, fails instantly, and -- because
// target_command is configured and simctl confirms the cached UDID really
// isn't booted -- mav invalidates the cache, re-runs target_command (which
// is what actually reboots the simulator and waits for it, simpool-style),
// and retries the exact same `ui tree` once against the fresh UDID.
func TestUITreeRetriesOnceWhenCachedSimulatorNoLongerBooted(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "print-target"
	root, _ := newStaleTargetCommandRun(t, cfg, "OLD-UDID")

	bootedJSON := `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-3":[` +
		`{"udid":"NEW-UDID","name":"iPhone 17 Pro","state":"Booted"}]}}`
	targetKey := targetCommandKey(root, "print-target")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			"axe describe-ui --udid OLD-UDID": {
				Stderr: "Error: Cannot run accessibility commands against OLD-UDID as it is not booted",
				Code:   1, Err: fmt.Errorf("exit status 1"),
			},
		},
		out: map[string]string{
			targetKey:                             "NEW-UDID\n",
			"xcrun simctl list devices booted -j": bootedJSON,
			"axe describe-ui --udid NEW-UDID":     `[{"AXUniqueId":"HomeView","AXRole":"Application"}]`,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.HasPrefix(got, "fail ") {
		t.Fatalf("got %q, want ui tree to succeed after retrying against the re-resolved simulator", got)
	}
	if !strings.Contains(got, "udid=NEW-UDID") {
		t.Fatalf("got %q, want the retried udid reported", got)
	}
	if !strings.Contains(got, "target_command_restale=") {
		t.Fatalf("got %q, want target_command_restale flagging the re-resolve+retry", got)
	}
	if !strings.Contains(got, "OLD-UDID") || !strings.Contains(got, "NEW-UDID") {
		t.Fatalf("got %q, want the restale note to name both the stale and fresh udid", got)
	}
	if !containsCall(runner.commands, "axe describe-ui --udid OLD-UDID") {
		t.Fatalf("expected the first (failing) attempt against the stale udid; commands=%v", runner.commands)
	}
	if !containsCall(runner.commands, "axe describe-ui --udid NEW-UDID") {
		t.Fatalf("expected the retried attempt against the fresh udid; commands=%v", runner.commands)
	}
	if !containsCall(runner.commands, targetKey) {
		t.Fatalf("expected target_command to be re-invoked to resolve the fresh udid; commands=%v", runner.commands)
	}
}

// TestUITreeDoesNotRetryWhenSimulatorIsActuallyBooted guards against wasting
// the ~15s retry on a failure that has nothing to do with the simulator
// being down: when simctl confirms the cached udid really is booted, the
// original failure is reported as-is and target_command is never re-run.
func TestUITreeDoesNotRetryWhenSimulatorIsActuallyBooted(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "print-target"
	root, _ := newStaleTargetCommandRun(t, cfg, "LIVE-UDID")

	bootedJSON := `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-3":[` +
		`{"udid":"LIVE-UDID","name":"iPhone 17 Pro","state":"Booted"}]}}`
	targetKey := targetCommandKey(root, "print-target")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			"axe describe-ui --udid LIVE-UDID": {
				Stderr: "Error: something unrelated broke",
				Code:   1, Err: fmt.Errorf("exit status 1"),
			},
		},
		out: map[string]string{
			"xcrun simctl list devices booted -j": bootedJSON,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "fail ") {
		t.Fatalf("got %q, want the unrelated failure to still be reported as a failure", got)
	}
	if strings.Contains(got, "target_command_restale=") {
		t.Fatalf("got %q, want no retry when the simulator is confirmed booted", got)
	}
	if containsCall(runner.commands, targetKey) {
		t.Fatalf("target_command should never be re-invoked when the cached simulator is confirmed booted; commands=%v", runner.commands)
	}
}

// TestUITreeWithoutTargetCommandLabelsNotBootedClearly covers the case with
// nobody to re-ask: no target_command configured, so mav resolves through
// the plain booted-simulator fallback, and that simulator has since shut
// down. There's no retry (see the task description: only target_command has
// a pool manager on the other end), but the failure should say plainly that
// the simulator isn't booted instead of leaving the caller to guess from
// axe's raw stderr.
func TestUITreeWithoutTargetCommandLabelsNotBootedClearly(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	root, runID := newTargetCommandRun(t, cfg)
	_ = runID

	bootedJSON := `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-3":[` +
		`{"udid":"DEAD-UDID","name":"iPhone 17 Pro","state":"Booted"}]}}`
	emptyJSON := `{"devices":{}}`
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			"axe describe-ui --udid DEAD-UDID": {
				Stderr: "Error: Cannot run accessibility commands against DEAD-UDID as it is not booted",
				Code:   1, Err: fmt.Errorf("exit status 1"),
			},
		},
		seq: map[string][]string{
			"xcrun simctl list devices booted -j": {bootedJSON, emptyJSON},
		},
		calls: map[string]int{},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "fail ") {
		t.Fatalf("got %q, want ui tree to still fail -- nobody to re-ask without target_command", got)
	}
	if !strings.Contains(got, "reason=simulator_not_booted") {
		t.Fatalf("got %q, want a clear reason=simulator_not_booted field", got)
	}
	if strings.Contains(got, "target_command_restale=") {
		t.Fatalf("got %q, want no retry note -- there's no target_command to re-ask", got)
	}
}

// TestUITreeRetryNeverLoopsWhenReresolutionDoesNotHelp is the "exactly once"
// regression: if re-running target_command produces the same UDID again
// (e.g. a pool with no other slot to hand out), mav must not spin retrying
// -- it reports the original failure, now labeled, and never re-dispatches.
func TestUITreeRetryNeverLoopsWhenReresolutionDoesNotHelp(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "print-target"
	root, _ := newStaleTargetCommandRun(t, cfg, "OLD-UDID")

	emptyJSON := `{"devices":{}}`
	targetKey := targetCommandKey(root, "print-target")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			"axe describe-ui --udid OLD-UDID": {
				Stderr: "Error: Cannot run accessibility commands against OLD-UDID as it is not booted",
				Code:   1, Err: fmt.Errorf("exit status 1"),
			},
		},
		out: map[string]string{
			targetKey:                             "OLD-UDID\n", // same udid again: no slot freed up
			"xcrun simctl list devices booted -j": emptyJSON,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "fail ") {
		t.Fatalf("got %q, want the failure to persist when re-resolution changes nothing", got)
	}
	if !strings.Contains(got, "reason=simulator_not_booted") {
		t.Fatalf("got %q, want a clear reason field even though retrying didn't help", got)
	}
	if countCommandCalls(runner.commands, targetKey) != 1 {
		t.Fatalf("target_command invoked %d times, want exactly 1 (no retry loop); commands=%v",
			countCommandCalls(runner.commands, targetKey), runner.commands)
	}
	if countCommandCalls(runner.commands, "axe describe-ui --udid OLD-UDID") != 1 {
		t.Fatalf("axe describe-ui invoked %d times, want exactly 1 (no useless re-dispatch); commands=%v",
			countCommandCalls(runner.commands, "axe describe-ui --udid OLD-UDID"), runner.commands)
	}
}

// TestUITreeRetriesExactlyOnceEvenIfRetryAlsoFails covers the case where
// re-resolution does produce a different UDID, but the retried dispatch
// fails too (the freshly-leased simulator is somehow still unusable): mav
// must still stop after that single retry, not chain another one.
func TestUITreeRetriesExactlyOnceEvenIfRetryAlsoFails(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetCommand = "print-target"
	root, _ := newStaleTargetCommandRun(t, cfg, "OLD-UDID")

	bootedJSON := `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-3":[` +
		`{"udid":"SOMETHING-ELSE","name":"iPhone 17 Pro","state":"Booted"}]}}`
	targetKey := targetCommandKey(root, "print-target")
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"axe": true},
		err: map[string]CommandResult{
			"axe describe-ui --udid OLD-UDID": {
				Stderr: "Error: Cannot run accessibility commands against OLD-UDID as it is not booted",
				Code:   1, Err: fmt.Errorf("exit status 1"),
			},
			"axe describe-ui --udid NEW-UDID": {
				Stderr: "Error: Cannot run accessibility commands against NEW-UDID as it is not booted",
				Code:   1, Err: fmt.Errorf("exit status 1"),
			},
		},
		out: map[string]string{
			targetKey:                             "NEW-UDID\n",
			"xcrun simctl list devices booted -j": bootedJSON,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tree"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "fail ") {
		t.Fatalf("got %q, want the still-failing retry reported as a failure", got)
	}
	if !strings.Contains(got, "target_command_restale=") {
		t.Fatalf("got %q, want the retry attempt still flagged even though it also failed", got)
	}
	if countCommandCalls(runner.commands, targetKey) != 1 {
		t.Fatalf("target_command invoked %d times, want exactly 1; commands=%v",
			countCommandCalls(runner.commands, targetKey), runner.commands)
	}
	if countCommandCalls(runner.commands, "axe describe-ui --udid OLD-UDID") != 1 {
		t.Fatalf("first attempt invoked %d times, want exactly 1; commands=%v",
			countCommandCalls(runner.commands, "axe describe-ui --udid OLD-UDID"), runner.commands)
	}
	if countCommandCalls(runner.commands, "axe describe-ui --udid NEW-UDID") != 1 {
		t.Fatalf("retried attempt invoked %d times, want exactly 1 (no further retry loop); commands=%v",
			countCommandCalls(runner.commands, "axe describe-ui --udid NEW-UDID"), runner.commands)
	}
}
