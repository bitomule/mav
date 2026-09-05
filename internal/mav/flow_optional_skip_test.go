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
