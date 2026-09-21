package mav

import "testing"

// `outcome=no_route` used to cover two different facts: "there was no way to
// begin" and "I walked the route and this screen leads nowhere further". The
// second is what the destination looks like from the inside, and reporting it
// with the same label is what made `goto` look broken while standing exactly
// where it was sent — measured six takes of a video, all six `no_route`, five
// of them on the right screen.
func TestWalkingTheRouteAndStoppingIsNotTheSameAsNeverStarting(t *testing.T) {
	if GotoDeadEnd == GotoNoRoute {
		t.Fatal("the two outcomes are the same string again")
	}
	if !gotoMoved([]GotoStep{{Changed: false}, {Changed: true}}) {
		t.Fatal("a tap that changed the screen is movement")
	}
	if gotoMoved([]GotoStep{{Changed: false}}) {
		t.Fatal("a tap that changed nothing is not movement")
	}
	if gotoMoved(nil) {
		t.Fatal("no steps is not movement")
	}
}

// A criterion nobody wrote and one the caller wrote are different evidence, and
// the output now says which without the caller having to remember.
func TestTheOutputSaysWhereTheCriterionCameFrom(t *testing.T) {
	c := ParseArrivalCriterion(`title:"Order detail" text:"123"`)
	if c.String() != `title:"Order detail" text:"123"` {
		t.Fatalf("the criterion does not print back in the syntax the flag takes: %q", c.String())
	}
	if (ArrivalCriterion{}).String() != "" {
		t.Fatal("no criterion should print as nothing, not as an empty term")
	}
	if CriterionExplicit == CriterionNone {
		t.Fatal("the two sources are the same string")
	}
}
