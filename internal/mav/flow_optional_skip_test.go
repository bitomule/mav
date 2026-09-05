package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A flow that declares a tap optional and then fails it used to finish green
// with nothing but a bare skipped=true in the trail: no reason, status "ok",
// and a pass line that counted the step as done. That is a flow claiming work
// it never did. Revert the skip bookkeeping and this test fails.
func TestRunFlowOptionalTapSkipIsVisibleAndCarriesItsReason(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	flow := "steps:\n  - tap: { text: \"Entendido\", optional: true }\n"
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[]`},
		err: map[string]CommandResult{
			"axe tap --label Entendido": {Stderr: "Error: DecodingError.typeMismatch", Err: os.ErrInvalid},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"ok cmd=run", "skipped=1", "skipped_steps=1:tap"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := os.ReadFile(run.Commands)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(trail), `"status":"skipped"`) {
		t.Fatalf("skipped step recorded as done: %s", trail)
	}
	if !strings.Contains(string(trail), `"error":"tap_failed"`) {
		t.Fatalf("skip reason missing from the trail: %s", trail)
	}
	runJSON, err := os.ReadFile(filepath.Join(run.Dir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runJSON), `"1:tap"`) {
		t.Fatalf("run.json does not name the skipped step: %s", runJSON)
	}
}

// A whileNotVisible do-block child that fails an optional tap used to be
// logged into the trail with a hardcoded status "ok", so the trail line for
// the very child that was skipped claimed it ran to completion. Revert the
// child status derivation in executeWhileNotVisibleFlowStepBoundWithOptions
// and this test fails.
func TestWhileNotVisibleOptionalChildSkipIsVisibleInTrail(t *testing.T) {
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
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := os.ReadFile(run.Commands)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(trail)), "\n") {
		if strings.Contains(line, `"action":"whileNotVisible.tap"`) {
			if !strings.Contains(line, `"status":"skipped"`) {
				t.Fatalf("whileNotVisible.tap child was skipped but not recorded with status=skipped: %s", line)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no whileNotVisible.tap entry in the trail: %s", trail)
	}
}

// A flow that skips an optional step and then fails a later, required one
// used to report zero skip evidence on the fail line and in run.json,
// indistinguishable from a run where the earlier step fully ran. Revert the
// failure-branch bookkeeping in runFlow and this test fails.
func TestRunFlowFailureStillNamesEarlierSkippedSteps(t *testing.T) {
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
	if !strings.Contains(got, "skipped_steps=1:tap") {
		t.Fatalf("the fail line dropped the earlier skipped step: %q", got)
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
		t.Fatalf("failed run.json does not name the skipped step: %s", runJSON)
	}
}
