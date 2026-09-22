package mav

import (
	"testing"
)

// The property the second safety belt rests on, pinned in code.
//
// The belt is: inside a flow, `unverified` fails the step and only a confirmed
// arrival passes. That is worth something only if a run given an explicit
// criterion CANNOT reach arrived=true by any route other than that criterion
// actually holding. The route that must stay shut is goto naming its own
// destination out of the screens it stood on -- which is precisely how goto
// came to report arrived=true after merely opening the create-category sheet
// without creating anything.
//
// Everything the observed-naming path needs is armed here on purpose: a key, a
// model that answers, and a menu of two distinct screens. The only thing
// standing between that and a named destination is finishGoto refusing to name
// one when a criterion was written.
//
// What this pins, exactly, and no more: with a criterion written, finishGoto
// never asks the model and never substitutes a name of its own, so the ONLY
// road to arrived=true is the written criterion actually holding on the settled
// screen. Ablated -- swap the guard for `if true` -- it names a destination and
// this fails.
//
// What it does NOT pin: that the live command refuses a real screen. That needs
// the simulator, and on the day this was written the machine was unusable (311
// leaked 20 Hz spinners from the repo's own concurrent-run tests, load ~125
// against a gate of 15), so the live run is still owed.
func TestExplicitCriterionNeverBecomesAnObservedArrival(t *testing.T) {
	fakeJev(t, "Create Category", 10)
	c, cfg, runner := guardPointConfig(t)
	// The screen the run stopped on: a sheet, with no sign anywhere of the
	// thing the goal asked to create. That is what "the action was never
	// performed" looks like in the tree.
	runner.out["axe describe-ui --udid "+guardPointUDID] = gotoCacheSheetTree

	criterion, err := ParseArrivalCriterion(`text:"Kitchen Stuff"`)
	if err != nil {
		t.Fatalf("ParseArrivalCriterion: %v", err)
	}
	if criterion.IsZero() {
		t.Fatal("the criterion under test must not be empty")
	}

	result := GotoResult{
		Arrived:         "false",
		Outcome:         GotoDeadEnd,
		Goal:            "create a category called Kitchen Stuff",
		CriterionSource: CriterionExplicit,
		Criterion:       criterion.String(),
		Steps:           []GotoStep{{Changed: true}},
	}
	// A second screen in the record, so the menu is not a yes/no about the one
	// screen it stopped on -- the gate nameObservedDestination applies before
	// it will ask anything at all.
	result.observed = RecordObservedScreen(result.observed,
		Route{Screen: "categories-view"}, nil)

	finished := c.finishGoto(t.Context(), cfg, GlobalOptions{}, result, criterion, nil)

	if finished.Arrived == "true" {
		t.Fatalf("an unmet explicit criterion must never report arrival: arrived=%q outcome=%q criterion_source=%q criterion=%q",
			finished.Arrived, finished.Outcome, finished.CriterionSource, finished.Criterion)
	}
	if finished.CriterionSource != CriterionExplicit {
		t.Fatalf("criterion_source=%q, want %q -- a written criterion must not be replaced by a named one",
			finished.CriterionSource, CriterionExplicit)
	}
	if len(finished.ObservedScreens) != 0 {
		t.Fatalf("no destination should have been named: %v", finished.ObservedScreens)
	}
	// And the verdict a flow step derives from that fails the step.
	if err := gotoFlowStepVerdict(finished.Arrived); err == nil {
		t.Fatal("a run that did not arrive must fail the flow step")
	}
}
