package mav

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// When axe is the only accessibility tool installed (no idb), the tree
// fallback has no coordinate driver to dispatch through. It must bail
// before calling into a recursive uiTap that would report its own
// tool_missing, discarding the original DecodingError diagnosis the caller
// already has. Revert the CoordinateTap pre-check in tapSelectorViaTree and
// this test fails with fail code=tool_missing instead of the tap's own
// diagnosis.
func TestUITapPreservesOriginalFailureWhenNoCoordinateDriverExists(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui": `[{"AXLabel":"Entendido","AXUniqueId":"got_it","type":"Button","AXFrame":"{{100, 200}, {80, 40}}"}]`,
		},
		err: map[string]CommandResult{
			"axe tap --label Entendido": {
				Stderr: "Error: DecodingError.typeMismatch",
				Err:    os.ErrInvalid,
			},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"ui", "tap", "--text", "Entendido"}))
	got := out.String()
	if strings.Contains(got, "selector_via=tree") {
		t.Fatalf("no coordinate driver exists, the fallback should never have dispatched: %q", got)
	}
	if strings.Contains(got, "tool_missing") {
		t.Fatalf("the recursive fallback's own tool_missing replaced the original diagnosis: %q", got)
	}
	if !strings.Contains(got, "DecodingError") {
		t.Fatalf("original selector-tap diagnosis lost: %q", got)
	}
}

// A tree node with a degenerate ({0,0},{0,0}) frame is not a place: tapping
// it is tapping the corner of the screen and calling it a match. Revert the
// mw<=0||mh<=0 guard in tapSelectorViaTree and this test starts reporting
// ok x=0 y=0 instead of preserving the original failure.
func TestUITapDoesNotFallBackToDegenerateFrame(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui": `[{"AXLabel":"Entendido","AXUniqueId":"got_it","type":"Button","AXFrame":"{{0, 0}, {0, 0}}"}]`,
		},
		err: map[string]CommandResult{
			"axe tap --label Entendido": {
				Stderr: "Error: DecodingError.typeMismatch",
				Err:    os.ErrInvalid,
			},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"ui", "tap", "--text", "Entendido"}))
	got := out.String()
	if strings.Contains(got, "selector_via=tree") {
		t.Fatalf("a zero-sized frame must not be treated as a resolvable tap target: %q", got)
	}
	if strings.Contains(got, "ok cmd=ui.tap") {
		t.Fatalf("a degenerate frame produced a reported success: %q", got)
	}
}

// When the tree resolves the element but the coordinate tap itself then
// fails (idb error, bad route, whatever), the selector context the tree
// fallback gathered must survive onto that failure -- otherwise the agent
// sees a bare ui_tap_failed with no selector_error/next and no way to tell
// this ever went through the tree at all. Revert the c.withFallbackFields
// merge on the coordinate-failure path and this test fails.
func TestUITapKeepsSelectorContextWhenCoordinateTapAlsoFails(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui": `[{"AXLabel":"Entendido","AXUniqueId":"got_it","type":"Button","AXFrame":"{{100, 200}, {80, 40}}"}]`,
		},
		err: map[string]CommandResult{
			"axe tap --label Entendido": {
				Stderr: "Error: DecodingError.typeMismatch",
				Err:    os.ErrInvalid,
			},
			"idb ui tap 140 220": {
				Stderr: "idb: no booted simulator",
				Err:    os.ErrInvalid,
			},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"ui", "tap", "--text", "Entendido"}))
	got := out.String()
	if !strings.Contains(got, "fail code=ui_tap_failed") {
		t.Fatalf("expected the coordinate tap's own failure to surface, got %q", got)
	}
	for _, want := range []string{"selector_via=tree", "selector_error=", "DecodingError", "brew upgrade"} {
		if !strings.Contains(got, want) {
			t.Fatalf("selector context lost on the doubly-failing path, missing %q in %q", want, got)
		}
	}
}

// --verify must be honoured when a selector tap only reaches coordinates
// through the tree fallback -- that is exactly the path most likely to tap
// the wrong place, so losing verification there loses it where it matters
// most. Revert the --verify handling added to the coordinate branch of
// uiTap and this test fails to find verified= in the output.
func TestUITapVerifiesWhenTappingThroughTreeFallback(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"axe describe-ui": `[{"AXLabel":"Entendido","AXUniqueId":"got_it","type":"Button","AXFrame":"{{100, 200}, {80, 40}}"}]`,
		},
		err: map[string]CommandResult{
			"axe tap --label Entendido": {
				Stderr: "Error: DecodingError.typeMismatch",
				Err:    os.ErrInvalid,
			},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--text", "Entendido", "--verify"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "selector_via=tree") {
		t.Fatalf("expected the tree fallback to fire, got %q", got)
	}
	if !strings.Contains(got, "verified=") {
		t.Fatalf("--verify was silently dropped on the tree-fallback coordinate tap: %q", got)
	}
}
