package mav

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The iPad status bar shows the DATE, and SpringBoard draws it in the
// SIMULATOR's language -- not the app's, which arrives as a launch argument
// and reaches one process. Boxy shipped several versions of English iPad
// screenshots reading "Viernes 18 de septiembre" because of that gap, and
// these tests are the gap closed.

func simLanguageRoot(t *testing.T) (string, *sequenceRecordingRunner) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.BundleID = "com.example.app"
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{tools: cfg.Tools, out: map[string]string{
		"xcrun simctl spawn SIM defaults read -g AppleLanguages":      "(\n    \"es-ES\",\n    \"en-ES\"\n)\n",
		"xcrun simctl spawn SIM defaults read -g AppleLocale":         "es_ES\n",
		"xcrun simctl spawn SIM launchctl list com.apple.SpringBoard": "{\n\t\"PID\" = 123;\n}\n",
	}}
	return root, runner
}

func TestSimLanguageSetWritesDefaultsAndRestartsSpringBoard(t *testing.T) {
	root, runner := simLanguageRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "language", "set", "--language", "fr-FR"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=sim.language.set") {
		t.Fatalf("output=%q", out.String())
	}
	for _, want := range []string{
		"xcrun simctl spawn SIM defaults write -g AppleLanguages -array fr-FR",
		// --locale defaults to the language tag rather than being left
		// alone: AppleLanguages picks the strings, AppleLocale picks the
		// date and time formats, and half of the pair set is a status bar
		// in French words with Spanish formats.
		"xcrun simctl spawn SIM defaults write -g AppleLocale -string fr_FR",
		"xcrun simctl spawn SIM launchctl stop com.apple.SpringBoard",
		"xcrun simctl spawn SIM launchctl list com.apple.SpringBoard",
	} {
		if !containsCall(runner.commands, want) {
			t.Fatalf("missing %q in %v", want, runner.commands)
		}
	}
}

// A capture matrix calls this once per language on a slot that is usually
// already correct, and the SpringBoard restart is the only expensive part.
func TestSimLanguageSetIsNoOpWhenAlreadyOnThatPair(t *testing.T) {
	root, runner := simLanguageRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "language", "set", "--language", "es-ES", "--locale", "es_ES"}); err != nil {
		t.Fatal(err)
	}
	if containsCall(runner.commands, "launchctl stop com.apple.SpringBoard") {
		t.Fatalf("restarted SpringBoard for a language it was already on: %v", runner.commands)
	}
}

// simctl accepts a bare subtag and iOS falls back to English without saying
// so, which is the same silent-wrong-language defect this command exists to
// remove. Measured on iOS 26.3: `fr` produced an English status bar.
func TestSimLanguageSetRejectsLanguageWithoutRegion(t *testing.T) {
	root, runner := simLanguageRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	_ = cli.Run(context.Background(), []string{"sim", "language", "set", "--language", "fr"})
	if !strings.Contains(out.String(), "sim_language_region_missing") {
		t.Fatalf("output=%q", out.String())
	}
	if containsCall(runner.commands, "defaults write -g AppleLanguages") {
		t.Fatalf("a rejected language must not reach the simulator: %v", runner.commands)
	}
}

func TestFlowLintRejectsSimLanguageWithoutRegion(t *testing.T) {
	hasCode := func(issues []flowLintIssue, code string) bool {
		for _, issue := range issues {
			if issue.Code == code {
				return true
			}
		}
		return false
	}
	cfg := DefaultConfig(t.TempDir())
	bare := lintFlowStep(1, FlowStep{Action: "sim.language.set", Params: map[string]string{"language": "de"}}, cfg)
	if !hasCode(bare, "sim_language_region_missing") {
		t.Fatalf("issues=%v", bare)
	}
	full := lintFlowStep(1, FlowStep{Action: "sim.language.set", Params: map[string]string{"language": "de-DE"}}, cfg)
	if hasCode(full, "sim_language_region_missing") {
		t.Fatalf("de-DE is valid: %v", full)
	}
}

// Every screenshot pipeline already carries the region in its locale --
// `--param language=de --param locale=de_DE` -- so a flow can pass its own
// params straight through and still get "de-DE" where iOS needs it.
func TestSimLanguageSetDerivesRegionFromLocale(t *testing.T) {
	root, runner := simLanguageRoot(t)
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"sim", "language", "set", "--language", "de", "--locale", "de_DE"}); err != nil {
		t.Fatal(err)
	}
	if !containsCall(runner.commands, "xcrun simctl spawn SIM defaults write -g AppleLanguages -array de-DE") {
		t.Fatalf("commands=%v", runner.commands)
	}
	if !strings.Contains(out.String(), "language=de-DE") {
		t.Fatalf("the tag actually used has to be echoed, got %q", out.String())
	}
}
