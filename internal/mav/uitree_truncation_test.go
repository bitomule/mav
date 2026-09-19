package mav

import (
	"bytes"
	"strings"
	"testing"
)

// `mav ui tree` used to print at most 80 nodes. The line that announced the
// cut was unreachable, so the first fix made it fire; this file is what
// replaced that fix when the cap itself was removed. A warning is what you
// need when something is missing, and the answer to "elements are missing" is
// to stop dropping them, not to describe the drop.
//
// These assertions are deliberately of the shape "printed == extracted". A cap
// creeping back in fails the build, where a warning would only print a line
// into a log nobody reads — which is exactly how the original hole survived.

func manyElements(n int) []Element {
	out := make([]Element, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Element{ID: "id" + itoa(i), Label: "Fila " + itoa(i), Role: "button"})
	}
	return out
}

func countNodeLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "node ") {
			n++
		}
	}
	return n
}

func TestEveryElementIsPrintedHoweverManyThereAre(t *testing.T) {
	// 213 is the real measurement this was found on: an iOS Settings screen,
	// where 80 printed and 133 disappeared without a word.
	for _, n := range []int{1, 12, 79, 80, 81, 213, 500} {
		var buf bytes.Buffer
		if err := writeElementLines(&buf, manyElements(n)); err != nil {
			t.Fatal(err)
		}
		if got := countNodeLines(buf.String()); got != n {
			t.Fatalf("gave the printer %d elements and it printed %d", n, got)
		}
	}
}

func TestTheLastElementOfALongScreenIsVisible(t *testing.T) {
	// The consequence that mattered, and stated no larger than it is: an
	// element past the old cap could not be SEEN. `mav ui tap --id` reads the
	// driver, not this list, so such an element was always tappable by someone
	// who already knew its id — and the only command that hands out ids is the
	// one that was hiding them.
	var buf bytes.Buffer
	_ = writeElementLines(&buf, manyElements(213))
	if !strings.Contains(buf.String(), "id=id212") {
		t.Fatal("the 213th element is not in the output; something is still capping")
	}
}

func TestNothingAnnouncesATruncationBecauseNothingTruncates(t *testing.T) {
	var buf bytes.Buffer
	_ = writeElementLines(&buf, manyElements(213))
	if strings.Contains(buf.String(), "node_more") {
		t.Fatal("a truncation marker survived the removal of the truncation")
	}
}

func TestExtractionKeepsEveryElementItParsed(t *testing.T) {
	// The other half: the cap used to live in extraction too, so a capless
	// printer alone would still have shown 80.
	var b strings.Builder
	b.WriteString("[")
	const n = 213
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"AXIdentifier":"id`)
		b.WriteString(itoa(i))
		b.WriteString(`","AXLabel":"Fila `)
		b.WriteString(itoa(i))
		b.WriteString(`","role":"button"}`)
	}
	b.WriteString("]")

	got := ExtractElements(b.String())
	if len(got) != n {
		t.Fatalf("parsed %d elements out of %d in the tree", len(got), n)
	}
}

func TestObserveExposesTheWholeScreen(t *testing.T) {
	raw := `[{"AXLabel":"A","role":"button"},{"AXLabel":"B","role":"button"}]`
	c := CLI{}
	state := c.observeUITree(Config{}, raw, "axe", false)
	if len(state.Elements) != 2 {
		t.Fatalf("expected both elements, got %d", len(state.Elements))
	}
}
