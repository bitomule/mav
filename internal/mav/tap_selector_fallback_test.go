package mav

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// AXe 2 is required. A failed selector is not replayed through the former
// AXe 1 coordinate workaround: its original input may already have landed.
func failingAXeSelectorCLI(t *testing.T) (CLI, *sequenceRecordingRunner, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools, out: map[string]string{
		"axe --output json describe-ui": `[{"AXLabel":"Continue","AXUniqueId":"continue","type":"Button","AXFrame":"{{100, 200}, {80, 40}}"}]`,
	}, err: map[string]CommandResult{
		"axe --output json tap --tap-style physical --label Continue": {Stderr: "input delivery could not be confirmed", Err: os.ErrInvalid},
		"axe --output json tap --tap-style physical --id continue":    {Stderr: "input delivery could not be confirmed", Err: os.ErrInvalid},
	}}
	out := &bytes.Buffer{}
	return CLI{Runner: runner, Root: root, Stdout: out, Stderr: &bytes.Buffer{}}, runner, out
}
func assertNoCoordinateReplay(t *testing.T, runner *sequenceRecordingRunner) {
	t.Helper()
	taps := 0
	for _, command := range runner.commands {
		if strings.Contains(command, " tap ") {
			taps++
			if strings.Contains(command, " -x ") {
				t.Fatalf("selector failure replayed at coordinates: %v", runner.commands)
			}
		}
	}
	if taps != 1 {
		t.Fatalf("expected one input attempt, got %d: %v", taps, runner.commands)
	}
}
func TestAXe2SelectorFailureKeepsItsErrorWithoutCoordinateReplay(t *testing.T) {
	for _, selector := range [][]string{{"--text", "Continue"}, {"--id", "continue"}} {
		cli, runner, out := failingAXeSelectorCLI(t)
		if err := cli.Run(context.Background(), append([]string{"ui", "tap"}, selector...)); err == nil {
			t.Fatal("uncertain input reported success")
		}
		for _, want := range []string{"fail code=axe_command_failed", "delivery=uncertain", "retry=unsafe", "input delivery could not be confirmed"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("missing %s: %s", want, out.String())
			}
		}
		assertNoCoordinateReplay(t, runner)
	}
}
