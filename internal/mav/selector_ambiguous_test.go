package mav

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// iOS Settings, reduced to the shape that stalled four separate agents on
// 2026-09-19: the same label four times, none of the cells carrying an
// accessibility id. Refusing to choose between them is correct. Refusing
// without naming --index is what cost the day -- the advice on offer was
// "use --id, or a longer --text", and neither exists on this screen.
const fourIdenticalRowsTree = `[{"AXLabel":"Ajustes","role":"window","AXFrame":"{{0, 0}, {390, 844}}","children":[
{"AXLabel":"Pantalla y tamaño del texto","role":"cell","AXFrame":"{{0, 100}, {390, 44}}"},
{"AXLabel":"Pantalla y tamaño del texto","role":"cell","AXFrame":"{{0, 144}, {390, 44}}"},
{"AXLabel":"Pantalla y tamaño del texto","role":"cell","AXFrame":"{{0, 188}, {390, 44}}"},
{"AXLabel":"Pantalla y tamaño del texto","role":"cell","AXFrame":"{{0, 232}, {390, 44}}"}]}]`

func TestAmbiguousSelectorSaysHowToChooseBetweenTheMatches(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	cfg.SimulatorUDID = "SIM-AMBIGUOUS"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	runner := fakeRunner{tools: cfg.Tools, out: map[string]string{
		"axe describe-ui --udid SIM-AMBIGUOUS": fourIdenticalRowsTree,
	}}
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	_ = cli.Run(context.Background(), []string{"ui", "tap",
		"--text", "Pantalla y tamaño del texto", "--role", "cell"})

	got := out.String()
	if !strings.Contains(got, "code=selector_ambiguous") {
		t.Fatalf("four matches must still be refused, not guessed:\n%s", got)
	}
	if !strings.Contains(got, "matches=4") {
		t.Fatalf("the refusal must say how many things matched:\n%s", got)
	}
	if !strings.Contains(got, "--index") {
		t.Fatalf("the refusal must name the flag that resolves it:\n%s", got)
	}
}

// The way out the message now points at has to actually work, or the
// remediation is just a nicer dead end.
func TestIndexPicksOneOfSeveralIdenticalMatches(t *testing.T) {
	elements := ExtractElements(fourIdenticalRowsTree)
	selector := Selector{Text: "Pantalla y tamaño del texto", Role: "cell"}

	all, err := MatchElements(elements, selector)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("the fixture must reproduce the four matches; got %d", len(all))
	}

	third := 2
	selector.Index = &third
	picked, err := MatchElements(elements, selector)
	if err != nil {
		t.Fatal(err)
	}
	if len(picked) != 1 {
		t.Fatalf("--index must resolve to exactly one element; got %d", len(picked))
	}
	if picked[0].Frame != all[2].Frame {
		t.Fatalf("--index 2 must pick the third match in tree order; got frame %q want %q",
			picked[0].Frame, all[2].Frame)
	}
}
