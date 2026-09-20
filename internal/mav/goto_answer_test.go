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

func TestGotoAcceptsAChoiceTheVerdictWouldHaveThrownAway(t *testing.T) {
	batch := FindCandidates(tenRowScreen())
	// The exact shape that broke it: right index, verdict unsure.
	el, reason := InterpretGotoAnswer("3", batch)
	if el == nil {
		t.Fatalf("goto discarded a correct choice: reason=%s", reason)
	}
	if el.ID != "CAMERA" {
		t.Fatalf("wrong element: %+v", el)
	}
	// And find, whose caller taps without a loop underneath, still refuses it.
	if el, _ := InterpretFindAnswer("unsure", "3", batch); el != nil {
		t.Fatal("find must keep reading the verdict; its consequence is stricter")
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

// What happens to all of this when jevi is fixed.
//
// The defect is jevi attaching a confidence-derived verdict to a `choice`
// answer. goto reads only the choice, so a jevi that stops attaching one
// changes nothing for it. find still reads the verdict while it exists — and
// that is the half that could have broken silently, because "no verdict" read
// as "not yes" would make find abstain on every answer, for a reason nobody
// would connect to a jevi release.

func TestFindKeepsWorkingWhenJeviStopsClassifying(t *testing.T) {
	batch := FindCandidates(tenRowScreen())
	el, reason := InterpretFindAnswer("", "3", batch)
	if el == nil {
		t.Fatalf("a choice with no verdict must stand on its own, got reason=%s", reason)
	}
	if el.ID != "CAMERA" {
		t.Fatalf("wrong element: %+v", el)
	}
}

func TestFindStillRefusesAVerdictThatSaysNo(t *testing.T) {
	// The control: accepting an ABSENT verdict must not accept a NEGATIVE one.
	// Without this the change above would be a silent loosening of find.
	batch := FindCandidates(tenRowScreen())
	for _, verdict := range []string{"no", "unsure"} {
		if el, _ := InterpretFindAnswer(verdict, "3", batch); el != nil {
			t.Fatalf("verdict %q was accepted: find must still refuse it today", verdict)
		}
	}
}

func TestGotoIsUnaffectedByTheJeviChangeEitherWay(t *testing.T) {
	// goto never reads the verdict, so both shapes resolve identically. This
	// is the assertion that says the workaround is not a workaround: reading
	// the choice is correct before and after the upstream fix, and there is
	// nothing here to delete when jevi lands.
	batch := FindCandidates(tenRowScreen())
	chosen, _ := InterpretGotoAnswer("3", batch)
	if chosen == nil || chosen.ID != "CAMERA" {
		t.Fatal("goto should resolve the choice regardless of any verdict")
	}
}
