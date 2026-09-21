package mav

import "testing"

// Why goto reads the model's CHOICE where find reads its VERDICT.
//
// goto would not arrive. The abstention it stopped on turned out to be the
// model picking the RIGHT element and jevi marking the answer `unsure` because
// its confidence sat at 0.37, under jevi's own default cut. mav read the
// verdict, so a correct answer was thrown away — mav inheriting someone else's
// numeric threshold, which is the thing it is not supposed to have.
//
// Measured on one ten-row screen, eight goals whose answer was on it and twelve
// whose answer was not:
//
//	                     picks the right row   declines when it should
//	reading the verdict        4/8                    10/12
//	reading the label          8/8                     9/12
//
// The verdict cost half the correct answers and bought almost nothing: two of
// the three wrong picks carried `verdict: yes` anyway.

func tenRowScreen() []Element {
	rows := []struct{ id, label string }{
		{"GENERAL", "General"}, {"A11Y", "Accesibilidad"}, {"CAMERA", "Cámara"},
		{"SEARCH", "Búsqueda"}, {"BATTERY", "Batería"},
	}
	out := make([]Element, 0, len(rows))
	for _, r := range rows {
		out = append(out, Element{ID: r.id, Label: r.label, Role: "button", Enabled: "true"})
	}
	return out
}

func TestGotoResolvesTheChoiceItWasGiven(t *testing.T) {
	batch := FindCandidates(tenRowScreen())
	el, reason := InterpretGotoAnswer("3", batch)
	if el == nil {
		t.Fatalf("goto discarded a correct choice: reason=%s", reason)
	}
	if el.ID != "CAMERA" {
		t.Fatalf("wrong element: %+v", el)
	}
	// find reads the same answer the same way, now that neither reads the
	// verdict. What still differs is the consequence: find's caller taps what
	// it is handed, so find keeps its vetoes and its stricter question.
	if el, _ := InterpretFindAnswer("3", batch); el == nil || el.ID != "CAMERA" {
		t.Fatalf("find must resolve the same choice: %+v", el)
	}
}

func TestTheAbstentionStillLivesInTheChoice(t *testing.T) {
	// The safety argument, and it is not a number: `none` is an option the
	// model can pick, and it picked it 9 times out of 12 when nothing fitted.
	batch := FindCandidates(tenRowScreen())
	for _, label := range []string{"none", "NONE", " none ", ""} {
		if el, reason := InterpretGotoAnswer(label, batch); el != nil || reason != ReasonAbstained {
			t.Fatalf("%q should abstain, got el=%v reason=%s", label, el, reason)
		}
	}
}

func TestAChoiceOutsideTheBatchIsStillRefused(t *testing.T) {
	// The veto that can only remove a yes is unchanged by reading the label:
	// an index that was never offered is discarded rather than looked up.
	batch := FindCandidates(tenRowScreen())
	for _, label := range []string{"99", "0", "-1", "three", "3x"} {
		if el, reason := InterpretGotoAnswer(label, batch); el != nil {
			t.Fatalf("%q produced an element: %+v (reason %s)", label, el, reason)
		}
	}
}

func TestReadingTheChoiceCannotProduceAnArrival(t *testing.T) {
	// Why the looser reading is safe HERE and not in find: a wrong lead costs
	// goto one step. It can never be reported as arrival, because arrival is
	// decided by code against the route and a criterion the caller declared.
	screen := tenRowScreen()
	var noCriterion ArrivalCriterion
	if noCriterion.MatchesRoute(ExtractRoute(screen), screen) {
		t.Fatal("no criterion must never match, whatever the model chose")
	}
}

// Neither command reads the verdict any more, and that is a measurement and
// not a preference: over 40 runs of `mav ui find`, jevi answered
// verdict "yes" 40 times out of 40 — on the 15 abstentions too. Every
// abstention in those 40 runs was the model answering `none`. There is nothing
// left here that a jevi release can change.

func TestBothCommandsReadTheSameAnswerTheSameWay(t *testing.T) {
	batch := FindCandidates(tenRowScreen())
	gotoEl, _ := InterpretGotoAnswer("3", batch)
	findEl, _ := InterpretFindAnswer("3", batch)
	if gotoEl == nil || findEl == nil || gotoEl.ID != findEl.ID {
		t.Fatalf("the two readings diverged: goto=%+v find=%+v", gotoEl, findEl)
	}
}
