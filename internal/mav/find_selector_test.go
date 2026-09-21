package mav

import (
	"strings"
	"testing"
)

// boxesScreen is the screen the written-step flow was designed against: a list
// of boxes with the button that CREATES one sitting among them.
func boxesScreen() []Element {
	return []Element{
		{Role: "text", Label: "Cajas"},
		{ID: "boxRow_1000", Label: "1000", Role: "cell", Enabled: "true", Frame: "{{0, 100}, {390, 44}}"},
		{ID: "boxRow_1001", Label: "1001", Role: "cell", Enabled: "true", Frame: "{{0, 144}, {390, 44}}"},
		{ID: "addBox", Label: "Agregar Caja", Role: "button", Enabled: "true", Frame: "{{300, 40}, {60, 40}}"},
	}
}

func TestFindSelectorTrimsStructurallyBeforeAsking(t *testing.T) {
	// The composition rule: the structural predicates cut the batch, and the
	// words only ever choose among what survived. The fake always answers "1",
	// so what changes between the two halves of this test is the batch, not
	// the answer.
	fakeJev(t, "1", 10)
	c := CLI{}

	open, _, err := c.resolveFindElement(t.Context(), boxesScreen(), Selector{Find: "the box"})
	if err != nil {
		t.Fatalf("unfiltered find failed: %v", err)
	}
	if open.ID != "boxRow_1000" {
		t.Fatalf("with no structural predicate the first candidate is the first row, got %q", open.ID)
	}

	button, _, err := c.resolveFindElement(t.Context(), boxesScreen(), Selector{Find: "the box", Role: "button"})
	if err != nil {
		t.Fatalf("filtered find failed: %v", err)
	}
	if button.ID != "addBox" {
		t.Fatalf("role=button had to trim the rows away before asking, got %q", button.ID)
	}
}

func TestFindSelectorAbstentionCutsWithFindAbstained(t *testing.T) {
	// `none` is an answer and it stops here. No second wording, no second
	// model, no ranking, and nothing numeric that could be relaxed.
	fakeJev(t, "none", 10)
	c := CLI{}

	_, _, err := c.resolveFindElement(t.Context(), boxesScreen(), Selector{Find: "the box nobody described"})
	if err == nil {
		t.Fatal("an abstention must fail the step, not return an element")
	}
	if err.Error() != "find_abstained" {
		t.Fatalf("expected find_abstained, got %q", err.Error())
	}
}

func TestFindSelectorSaysWhenItCouldNotAsk(t *testing.T) {
	// "I could not ask" is a different fact from "I asked and I am not sure",
	// and CI is where the first one is a guarantee rather than an accident.
	t.Setenv("CI", "1")
	c := CLI{}

	_, _, err := c.resolveFindElement(t.Context(), boxesScreen(), Selector{Find: "the first box"})
	if err == nil {
		t.Fatal("find must refuse to consult a model in CI")
	}
	if err.Error() != "find_unavailable" {
		t.Fatalf("expected find_unavailable, got %q", err.Error())
	}
	var selErr *findSelectorError
	if e, ok := err.(*findSelectorError); ok {
		selErr = e
	}
	if selErr == nil || selErr.Fields()["find_reason"] != ReasonCIRefused {
		t.Fatalf("the reason has to reach the step record, got %+v", err)
	}
}

func TestFindSelectorStructuralMissIsNotAnAbstention(t *testing.T) {
	t.Setenv("CI", "1")
	c := CLI{}
	_, _, err := c.resolveFindElement(t.Context(), boxesScreen(), Selector{Find: "anything", Role: "slider"})
	if err == nil || err.Error() != "selector_not_found" {
		t.Fatalf("a structural predicate that matches nothing is a selector miss, got %v", err)
	}
}

func TestFindReachesTheSelectorFromBothSides(t *testing.T) {
	selector, err := selectorFromCLI([]string{"--find", "la primera categoría", "--role", "cell"})
	if err != nil {
		t.Fatal(err)
	}
	if selector.Find != "la primera categoría" || selector.Role != "cell" {
		t.Fatalf("--find did not reach the selector: %+v", selector)
	}

	flow, err := ParseFlow([]byte("name: f\nsteps:\n  - tap: { where: { find: \"la primera categoría\" } }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if flow.Steps[0].Where.Find != "la primera categoría" {
		t.Fatalf("where.find did not reach the selector: %+v", flow.Steps[0].Where)
	}

	// And it survives the trip through the ui* flag parsers the flow steps
	// dispatch through, or a flow tap would quietly lose its words.
	round, err := selectorFromCLI(selectorCLIArgs(selector))
	if err != nil {
		t.Fatal(err)
	}
	if round.Find != selector.Find {
		t.Fatalf("find was dropped round-tripping through the CLI args: %+v", round)
	}
}

func TestMatchElementsRefusesToEvaluateFind(t *testing.T) {
	// Silently ignoring the words would make a selector meant to name one
	// element match the entire screen.
	_, err := MatchElements(boxesScreen(), Selector{Find: "the first box"})
	if err == nil || !strings.Contains(err.Error(), "selector_find_unsupported") {
		t.Fatalf("MatchElements must refuse a find, got %v", err)
	}
}
