package mav

import "testing"

// The candidate filter is a STATE filter. Both halves of that are load-bearing
// and both are fixed here: what cannot be acted on is dropped, and what the
// words say is never read.

func TestCandidatesDropWhatCannotBeActedOn(t *testing.T) {
	screen := []Element{
		{ID: "save", Label: "Guardar", Role: "button", Enabled: "true"},
		{ID: "submit", Label: "Enviar", Role: "button", Enabled: "false"},
		{ID: "hidden", Label: "Oculto", Role: "button", Enabled: "true", Visible: "false"},
		{ID: "collapsed", Label: "Plegado", Role: "button", Enabled: "true", Frame: "{{0, 0}, {0, 0}}"},
	}
	got := FindCandidates(screen)
	if len(got) != 1 || got[0].ID != "save" {
		t.Fatalf("only the visible, enabled element can be executed; got %+v", got)
	}
}

func TestAgregarCajaStaysACandidate(t *testing.T) {
	// The rule the state filter must not quietly become: `Agregar Caja` is the
	// button that CREATES a box, sitting on a screen of boxes. It is visible
	// and enabled, so it is offerable, and it stays on the menu. Filtering it
	// out by what its label says would be deciding the question in code while
	// pretending to prepare it - and the guard against picking it is the
	// model's own `none` plus the destructive veto, not a word list here.
	got := FindCandidates(boxesScreen())
	found := false
	for _, el := range got {
		if el.ID == "addBox" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Agregar Caja must remain a candidate; got %+v", got)
	}
	if len(got) != 3 {
		t.Fatalf("every visible, enabled, labelled element is a candidate; got %d", len(got))
	}
}

func TestCandidatesKeepElementsWhoseStateIsNotReported(t *testing.T) {
	// A driver that reports neither `enabled` nor a frame says nothing about
	// state. Reading that silence as "disabled" would empty the batch on every
	// tree that omits those fields.
	screen := []Element{{ID: "row", Label: "Una fila", Role: "cell"}}
	if got := FindCandidates(screen); len(got) != 1 {
		t.Fatalf("absent is not false; got %+v", got)
	}
}
