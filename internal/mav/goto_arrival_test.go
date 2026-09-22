package mav

import "testing"

// goto walked two steps into Boxy, landed on the destination, and reported
// arrived=false. The heading `label="Test Category 2: 1000" role=heading` was on
// screen afterwards and both a title: and a text: criterion naming exactly that
// came back denied — measured 3 runs out of 3 that reached it.
//
// The cause was an asymmetry rather than anything about matching. Arrival was
// tested on the ONE read taken right after a tap, and the loop settles only when
// that read already matches, so a screen that finished drawing a moment later
// was missed and nothing looked again: the loop went round, found nothing left
// to tap, abstained twice and reported no_route while standing on the
// destination.
//
// The fix restores the symmetry — the loop already refuses to declare arrival on
// a half-drawn screen, and now equally refuses to declare failure on one. What
// keeps that from becoming leniency is that it re-asks the SAME question: a
// criterion that does not hold still does not hold.

func boxContentsScreen() []Element {
	return []Element{
		{ID: "Test Category 2: 1000", Role: "Barra de navegación"},
		{Label: "Test Category 2: 1000", Role: "heading"},
		{Label: "Item 4", Role: "text"},
		{ID: "backButton", Label: "Back", Role: "button", Frame: "{{0, 0}, {40, 40}}"},
	}
}

func TestTheDestinationsCriterionMatchesThatScreen(t *testing.T) {
	// The premise the whole defect rests on: the criterion was always correct.
	// If this fails, the bug was never where we thought it was.
	screen := boxContentsScreen()
	c := mustCriterion(t, `title:"Test Category 2: 1000"`)
	if !c.MatchesRoute(ExtractRoute(screen), screen) {
		t.Fatal("the criterion does not match its own destination; the diagnosis is wrong")
	}
}

func TestACriterionThatDoesNotHoldStillDoesNotHold(t *testing.T) {
	// The other side, and the one that matters: looking a second time must not
	// turn a journey that did not arrive into one that did. A fix that makes
	// everything arrive is worse than the defect it replaces.
	screen := boxContentsScreen()
	for _, spec := range []string{
		`title:"Print Shipping Label"`,
		`title:"Test Category 1: 1000"`,
		`title:"Test Category 2: 1001"`,
		`title:"Test Category 2: 1000" text:"Item 5"`, // right screen, absent content
	} {
		c := mustCriterion(t, spec)
		if c.MatchesRoute(ExtractRoute(screen), screen) {
			t.Fatalf("%s matched a screen it should not", spec)
		}
	}
}

func TestTheNavigationBarIdIsNotMistakenForTheTitle(t *testing.T) {
	// That screen carries the same string twice: once as a navigation bar's id
	// and once as a heading's label. Only the heading is the route's title, and
	// a criterion must not pass on the other one — otherwise a screen could
	// "arrive" on an identifier that happens to read like a title.
	screen := []Element{
		{ID: "Test Category 2: 1000", Role: "Barra de navegación"},
		{Label: "Something Else", Role: "heading"},
	}
	c := mustCriterion(t, `title:"Test Category 2: 1000"`)
	if c.MatchesRoute(ExtractRoute(screen), screen) {
		t.Fatal("a navigation bar id was accepted as the screen's title")
	}
}

func TestArrivalIsStillImpossibleWithoutACriterion(t *testing.T) {
	// The second look changes nothing here: with nothing declared there is no
	// question to re-ask, and goto still cannot assert arrival at all.
	screen := boxContentsScreen()
	var none ArrivalCriterion
	if none.MatchesRoute(ExtractRoute(screen), screen) {
		t.Fatal("an empty criterion matched; goto would claim an arrival it cannot verify")
	}
}
