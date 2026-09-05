package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A flow tap runs uiTap into a private buffer and discards its ok line, so
// the tree-fallback diagnosis (selector_via=tree, the DecodingError, the
// brew-upgrade hint) used to die inside that buffer: the run went green and
// nothing anywhere recorded that the selector path is broken on this
// machine. Revert the tapFallbackSink threading in the flow "tap" case and
// this test fails to find selector_via in the trail.
func TestRunFlowTapRecordsTreeFallbackContext(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	flow := "steps:\n  - tap: { text: \"Entendido\" }\n"
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui": `[{"AXLabel":"Entendido","AXUniqueId":"got_it","type":"Button","AXFrame":"{{100, 200}, {80, 40}}"},
			                     {"type":"Slider","AXValue":0.2,"AXFrame":"{{0, 0}, {300, 30}}"}]`,
		},
		err: map[string]CommandResult{
			"axe tap --label Entendido": {
				Stderr: "Error: DecodingError.typeMismatch: expected value of type Dictionary<String, Any>. Debug description: Expected to decode Dictionary<String, Any> but found an array instead.",
				Err:    os.ErrInvalid,
			},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	if !strings.Contains(out.String(), "ok cmd=run") {
		t.Fatalf("flow did not pass: %q", out.String())
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "idb ui tap 140 220") {
		t.Fatalf("no coordinate tap dispatched: %q", runner.commands)
	}
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := os.ReadFile(run.Commands)
	if err != nil {
		t.Fatal(err)
	}
	stepLine := ""
	for _, line := range strings.Split(strings.TrimSpace(string(trail)), "\n") {
		if strings.Contains(line, `"action":"tap"`) {
			stepLine = line
		}
	}
	if stepLine == "" {
		t.Fatalf("no tap step in the trail: %s", trail)
	}
	for _, want := range []string{`"selector_via":"tree"`, "DecodingError", "brew upgrade cameroncooke/axe/axe"} {
		if !strings.Contains(stepLine, want) {
			t.Fatalf("tap step record lost the fallback context, missing %q in %s", want, stepLine)
		}
	}
}
