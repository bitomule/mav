package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Value of the `skipped=` field on a result line, or "" when absent.
// skipped_steps and skipped_children must not be mistaken for it.
func resultLineSkippedField(output string) string {
	match := regexp.MustCompile(`(?:^|\s)skipped=(\S*)`).FindStringSubmatch(output)
	if match == nil {
		return ""
	}
	return match[1]
}

// `skipped` on a fail line is documented as the number of run-level optional
// steps that were skipped, the same number run.json records. A whileNotVisible
// step that skipped an optional child and then timed out used to leak its own
// child count into that key, so stdout said skipped=1 while run.json said the
// run skipped nothing. Rename skipped_children back to skipped in the
// whileNotVisible executor and this test fails.
func TestFailLineSkippedAgreesWithRunJSON(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	flow := "steps:\n  - whileNotVisible:\n      text: \"You\"\n      timeout: 300ms\n      do:\n        - tap: { text: \"Entendido\", optional: true }\n"
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[]`},
		err: map[string]CommandResult{
			"axe tap --label Entendido": {Stderr: "tap_failed", Err: os.ErrInvalid},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"run", flowPath}))
	got := out.String()
	if !strings.Contains(got, "fail code=while_timeout") {
		t.Fatalf("expected the while loop to time out, got %q", got)
	}
	// Precondition: the child really was skipped, so the leak this test
	// guards against had something to leak.
	if leaked := resultLineSkippedField(got); leaked != "" {
		t.Fatalf("fail line carries a run-level skipped=%s but no step was skipped at run level: %q", leaked, got)
	}
	// Precondition: the child really was skipped, so the leak asserted
	// against above had something to leak. The loop runs an unpredictable
	// number of iterations in 300ms, so only the presence is fixed.
	if !regexp.MustCompile(`(?:^|\s)skipped_children=[1-9]`).MatchString(got) {
		t.Fatalf("expected the whileNotVisible step to report skipped children, got %q", got)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	runJSON, err := os.ReadFile(filepath.Join(run.Dir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runJSON), `"skipped": []`) {
		t.Fatalf("run.json disagrees with the fail line about run-level skips: %s", runJSON)
	}
}

// The same key, from the other side: when the run really did skip an optional
// step, the fail line must say so numerically, not only through skipped_steps.
func TestFailLineCountsRunLevelSkippedSteps(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	flow := "steps:\n" +
		"  - tap: { text: \"Entendido\", optional: true }\n" +
		"  - tap: { text: \"Required\" }\n"
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[]`},
		err: map[string]CommandResult{
			"axe tap --label Entendido": {Stderr: "Error: DecodingError.typeMismatch", Err: os.ErrInvalid},
			"axe tap --label Required":  {Stderr: "tap_failed", Err: os.ErrInvalid},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"run", flowPath}))
	got := out.String()
	if !strings.Contains(got, "fail code=tap_failed") {
		t.Fatalf("expected the required tap to fail, got %q", got)
	}
	if counted := resultLineSkippedField(got); counted != "1" {
		t.Fatalf("fail line should count the one run-level skipped step, got skipped=%q in %q", counted, got)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	runJSON, err := os.ReadFile(filepath.Join(run.Dir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runJSON), `"1:tap"`) {
		t.Fatalf("run.json disagrees with the fail line: %s", runJSON)
	}
}

// A `when` do-block that skipped an optional child used to report
// executed=len(do), counting the child it never ran, while whileNotVisible
// already counted honestly. Revert the counters in
// executeWhenFlowStepBoundWithOptions and this test fails.
func TestWhenReportsExecutedExcludingSkippedChildren(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	flow := "steps:\n" +
		"  - when: { id: Gate }\n" +
		"    do:\n" +
		"      - tap: { id: Gate, optional: true }\n" +
		"      - delay: 10ms\n"
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[{"AXUniqueId":"Gate","AXLabel":"Gate"}]`},
		err: map[string]CommandResult{
			"axe tap --id Gate": {Stderr: "tap_failed", Err: os.ErrInvalid},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := os.ReadFile(run.Commands)
	if err != nil {
		t.Fatal(err)
	}
	whenLine := ""
	skippedChild := false
	for _, line := range strings.Split(strings.TrimSpace(string(trail)), "\n") {
		if strings.Contains(line, `"action":"when"`) {
			whenLine = line
		}
		if strings.Contains(line, `"action":"when.tap"`) && strings.Contains(line, `"status":"skipped"`) {
			skippedChild = true
		}
	}
	if !skippedChild {
		t.Fatalf("precondition failed: the optional when child was not skipped: %s", trail)
	}
	if whenLine == "" {
		t.Fatalf("no when step in the trail: %s", trail)
	}
	if !strings.Contains(whenLine, `"executed":"1"`) {
		t.Fatalf("when step counted the skipped child as executed: %s", whenLine)
	}
	if !strings.Contains(whenLine, `"skipped_children":"1"`) {
		t.Fatalf("when step does not report its skipped child: %s", whenLine)
	}
}

// The `when` executor is one function used with and without exec bindings.
// The unbound path must still dispatch children through the plain executor:
// routing them through the bound one would newly subject them to retry,
// optional-skip and After semantics they never had.
func TestWhenWithoutBindingsStillRunsItsChildren(t *testing.T) {
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
		out:   map[string]string{"axe describe-ui": `[{"AXUniqueId":"Gate","AXLabel":"Gate"}]`},
	}
	cli := CLI{Runner: runner, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.executeWhenFlowStep(context.Background(), run, 1, FlowStep{
		Action: "when",
		Params: map[string]string{"id": "Gate"},
		Do:     []FlowStep{{Action: "tap", Params: map[string]string{"id": "Gate"}}},
	})
	if err != nil {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	if fields["matched"] != "true" || fields["executed"] != "1" {
		t.Fatalf("fields=%v", fields)
	}
	if fields["skipped_children"] != "" {
		t.Fatalf("nothing was skipped but fields=%v", fields)
	}
	if !containsCall(runner.commands, "axe tap --id Gate") {
		t.Fatalf("commands=%v", runner.commands)
	}

	// The discriminator between the two child executors: the bound one
	// applies onFailure/optional-skip, retry and After; the plain one does
	// not. An unbound `when` child marked optional must therefore still
	// propagate its failure. Collapse the two `when` executors by routing
	// nil bindings through the bound child executor and this fails.
	failing := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[{"AXUniqueId":"Gate","AXLabel":"Gate"}]`},
		err: map[string]CommandResult{
			"axe tap --id Gate": {Stderr: "tap_failed", Err: os.ErrInvalid},
		},
	}
	cli = CLI{Runner: failing, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err = cli.executeWhenFlowStep(context.Background(), run, 1, FlowStep{
		Action: "when",
		Params: map[string]string{"id": "Gate"},
		Do:     []FlowStep{{Action: "tap", Params: map[string]string{"id": "Gate", "optional": "true"}}},
	})
	if err == nil {
		t.Fatalf("unbound when child was silently skipped by a policy it does not have: fields=%v", fields)
	}
}
