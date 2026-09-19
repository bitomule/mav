package mav

import (
	"bytes"
	"strings"
	"testing"
)

// The hole `mav ui find` was built to fill, locked so it cannot silently
// reopen. `mav ui tree` prints at most 80 nodes. The line that announces the
// cut was unreachable, because the list was capped by Compact before it reached
// the printer that warns above 80 — so `i >= maxNodes` could never be true. On
// a real iOS Settings screen measured at 213 nodes, 133 of them went missing
// with nothing in the output saying so.

func manyElements(n int) []Element {
	out := make([]Element, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Element{ID: "id" + itoa(i), Label: "Fila " + itoa(i), Role: "button"})
	}
	return out
}

func TestALongListSaysHowMuchOfItselfIsMissing(t *testing.T) {
	var buf bytes.Buffer
	if err := writeElementLines(&buf, manyElements(213)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if got := strings.Count(out, "\nnode "); got+1 != 80 {
		t.Fatalf("expected 80 node lines, got %d", got+1)
	}
	if !strings.Contains(out, "node_more remaining=133") {
		t.Fatalf("a 213-element list was cut to 80 without saying so:\n%s", lastLines(out, 3))
	}
}

func TestTheWarningNamesTheCommandThatCanSeeTheRest(t *testing.T) {
	// A warning that only says "there is more" leaves the reader with no way
	// to get at it: nothing else in mav prints past the cap.
	var buf bytes.Buffer
	_ = writeElementLines(&buf, manyElements(100))
	if !strings.Contains(buf.String(), "mav ui find") {
		t.Fatalf("the truncation warning should point at the command that reads past the cap:\n%s", lastLines(buf.String(), 2))
	}
}

func TestAListThatFitsSaysNothing(t *testing.T) {
	var buf bytes.Buffer
	_ = writeElementLines(&buf, manyElements(12))
	if strings.Contains(buf.String(), "node_more") {
		t.Fatal("a list that fits must not carry a truncation warning")
	}
}

func TestObserveKeepsBothTheCappedAndTheWholeList(t *testing.T) {
	// The capped list is what every other reader of uiTreeState expects; the
	// whole one exists so the printer can count what it is dropping. Losing
	// either is how this regresses.
	raw := `[{"AXLabel":"A","role":"button"},{"AXLabel":"B","role":"button"}]`
	c := CLI{}
	state := c.observeUITree(Config{}, raw, "axe", false)
	if len(state.All) < len(state.Elements) {
		t.Fatalf("All must be the superset: all=%d elements=%d", len(state.All), len(state.Elements))
	}
	if len(state.All) == 0 {
		t.Fatal("All was never populated; the printer would show an empty tree")
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
