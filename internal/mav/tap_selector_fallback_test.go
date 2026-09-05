package mav

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// axe below 1.7.0 cannot decode an accessibility tree that carries a numeric
// AXValue -- a slider anywhere on screen is enough -- so `axe tap --label` and
// `axe tap --id` die with a Swift decoding error for every element on that
// screen, while `describe-ui` reads the same tree fine. Revert
// tapSelectorViaTree and this test fails with ui_tap_failed.
func TestUITapFallsBackToTreeWhenSelectorResolutionFails(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
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
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--text", "Entendido"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"ok cmd=ui.tap", "selector_via=tree", "x=140", "y=220", "DecodingError"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "idb ui tap 140 220") {
		t.Fatalf("no coordinate tap dispatched: %q", runner.commands)
	}
}

// The fallback must not paper over a selector that genuinely matches nothing:
// when the tree cannot resolve it either, the tool's own failure is what the
// caller needs to read.
func TestUITapKeepsOriginalFailureWhenTreeCannotResolveEither(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out:   map[string]string{"axe describe-ui": `[{"AXLabel":"Otra cosa","type":"Button","AXFrame":"{{0, 0}, {10, 10}}"}]`},
		err: map[string]CommandResult{
			"axe tap --label Entendido": {Stderr: "Error: DecodingError.typeMismatch", Err: os.ErrInvalid},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"ui", "tap", "--text", "Entendido"}))
	got := out.String()
	if !strings.Contains(got, "fail code=ui_tap") {
		t.Fatalf("expected a failure, got %q", got)
	}
	if strings.Contains(got, "selector_via=tree") {
		t.Fatalf("fallback should not have reported success: %q", got)
	}
}
