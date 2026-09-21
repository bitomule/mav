package mav

import (
	"strings"
	"testing"
)

// Deducing the criterion is the only way `goto` can say arrived=true without
// the caller writing --arrived-when. Everything here is about the deduction
// never being able to make the command worse than not deducing at all.

func categoryListScreen() []Element {
	return []Element{
		{Label: "Boxy", Role: "heading"},
		{Label: "Moving Boxes", Role: "button", Frame: "{{0, 100}, {320, 44}}"},
		{Label: "Moving Boxes", Role: "button", Frame: "{{0, 100}, {320, 44}}"},
		{Label: "Kitchen", Role: "button", Frame: "{{0, 150}, {320, 44}}"},
	}
}

func TestTitleCandidatesPutTheCallersQuotedWordsFirst(t *testing.T) {
	names := GotoTitleCandidates(categoryListScreen(), `the contents of the box "Office cables"`)
	if len(names) == 0 || names[0] != "Office cables" {
		t.Fatalf("a phrase the caller quoted must be offered, first: %v", names)
	}
}

func TestTitleCandidatesOfferEachNameOnce(t *testing.T) {
	// iOS renders a list row as two accessibility elements with the same
	// label. Two identical options read as two answers and are one name.
	names := GotoTitleCandidates(categoryListScreen(), "the first box")
	seen := 0
	for _, n := range names {
		if n == "Moving Boxes" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("the same name was offered %d times: %v", seen, names)
	}
}

func TestCriterionOptionsAlwaysCarryTheAbstention(t *testing.T) {
	// The service does not decline on its own. Take `none` off and every
	// screen returns a confident name that becomes a criterion nobody wrote.
	options := GotoCriterionOptions([]string{"a", "b"})
	if options[len(options)-1] != "none" {
		t.Fatalf("`none` is not on the menu: %v", options)
	}
}

func TestDecliningTheDeductionLeavesNoCriterion(t *testing.T) {
	names := []string{"Moving Boxes", "Kitchen"}
	for _, answer := range []string{"none", "", "  NONE ", "banana", "0", "9"} {
		if _, ok := InterpretGotoCriterionAnswer(answer, names); ok {
			t.Fatalf("answer %q was read as a usable criterion", answer)
		}
	}
	name, ok := InterpretGotoCriterionAnswer("2", names)
	if !ok || name != "Kitchen" {
		t.Fatalf("a plain pick was not read: %q %v", name, ok)
	}
}

// The test David asked for by name: a deduced criterion that already holds
// where the run starts is thrown away.
func TestADeducedCriterionThatAlreadyHoldsIsRefused(t *testing.T) {
	screen := categoryListScreen()
	route := ExtractRoute(screen)
	if _, ok := AcceptInferredCriterion("Boxy", route, screen); ok {
		t.Fatal("deduced the name of the screen it is standing on and accepted it; that is arrival at step zero")
	}
	criterion, ok := AcceptInferredCriterion("Moving Boxes: Office cables", route, screen)
	if !ok || criterion.Titles[0] != "Moving Boxes: Office cables" {
		t.Fatalf("a name that does not hold here should be accepted: %v %v", criterion, ok)
	}
}

// And it is DROPPED, not escalated: an explicit criterion that matches the
// start stops the run, a deduced one that matches the start just goes away and
// leaves goto behaving exactly as it does with no criterion at all.
func TestARefusedDeductionFallsBackToUnverified(t *testing.T) {
	screen := categoryListScreen()
	criterion, ok := AcceptInferredCriterion("Boxy", ExtractRoute(screen), screen)
	if ok || !criterion.IsZero() {
		t.Fatal("a refused deduction must leave a zero criterion, which is what reports unverified")
	}
}

func TestTheDeductionQuestionWarnsAboutScreensOnTheWay(t *testing.T) {
	// The whole risk of this feature: a name that titles an intermediate
	// screen declares arrival halfway, which is a false arrived=true and
	// strictly worse than the unverified it replaces.
	question := GotoCriterionQuestion("the contents of the first box")
	if !strings.Contains(question, "pass THROUGH") {
		t.Fatal("the question does not tell the model to skip screens on the way")
	}
	if !strings.Contains(question, "`none`") {
		t.Fatal("the question does not offer the abstention in its own words")
	}
}

func TestADeducedCriterionIsReportedAsDeduced(t *testing.T) {
	// A criterion the machine invented, presented as one a person wrote, is
	// exactly the thing this must not do.
	result := GotoResult{CriterionSource: CriterionInferred,
		Criterion: ArrivalCriterion{Titles: []string{"Office cables"}}.String()}
	if result.Criterion != `title:"Office cables"` {
		t.Fatalf("the deduced criterion is not printed in the syntax you could paste back: %q", result.Criterion)
	}
}

// Walking the route and finding nothing further is not the same fact as never
// finding a way to start, and they used to share the label no_route — which is
// what made goto look broken while standing on the destination.
func TestMovedThenAbstainedIsItsOwnOutcome(t *testing.T) {
	if !gotoMoved([]GotoStep{{Changed: false}, {Changed: true}}) {
		t.Fatal("a tap that changed the screen is movement")
	}
	if gotoMoved([]GotoStep{{Changed: false}}) {
		t.Fatal("a tap that changed nothing is not movement")
	}
	if gotoMoved(nil) {
		t.Fatal("no steps is not movement")
	}
	if GotoDeadEnd == GotoNoRoute {
		t.Fatal("the two outcomes are the same string again")
	}
}
