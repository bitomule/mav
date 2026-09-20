package mav

import (
	"testing"
)

// A Settings list: the destination's NAME is on it as a row, which is the whole
// trap. "Notificaciones" is on screen before anything is tapped.
func settingsListScreen() []Element {
	return []Element{
		{Label: "Ajustes", Role: "application"},
		{Label: "Ajustes", Role: "heading"},
		{ID: "NOTIFICATIONS", Label: "Notificaciones", Role: "button", Enabled: "true"},
		{ID: "CAMERA", Label: "Cámara", Role: "button", Enabled: "true"},
		{ID: "DELETE", Label: "Borrar cuenta", Role: "button", Enabled: "true"},
	}
}

// Where tapping "Notificaciones" actually lands: the heading changes.
func notificationsScreen() []Element {
	return []Element{
		{Label: "Ajustes", Role: "application"},
		{Label: "Notificaciones", Role: "heading"},
		{ID: "ALLOW", Label: "Permitir notificaciones", Role: "switch", Enabled: "true"},
	}
}

// --- The step-zero bug, which is why this whole check exists -----------------

func TestACriterionAlreadyTrueOnTheStartingScreenIsNotArrival(t *testing.T) {
	// The defect an adversarial review found: checking whether the criterion
	// text is present ANYWHERE in the tree declares victory before tapping,
	// because the destination's name is a row on the list you start from.
	screen := settingsListScreen()
	criterion := ParseArrivalCriterion("Notificaciones")
	route := ExtractRoute(screen)

	if route.Title != "Ajustes" {
		t.Fatalf("the starting route should be the list's own heading, got %q", route.Title)
	}
	if criterion.MatchesRoute(route, screen) {
		t.Fatal("the criterion matched the screen we start on: this is the step-zero bug")
	}
}

func TestTheSameCriterionMatchesOnceTheScreenActuallyChanged(t *testing.T) {
	// The control for the test above: if the criterion never matched anything,
	// the assertion there would pass for the wrong reason.
	screen := notificationsScreen()
	criterion := ParseArrivalCriterion("Notificaciones")
	if !criterion.MatchesRoute(ExtractRoute(screen), screen) {
		t.Fatal("the criterion should match once the heading is the destination")
	}
}

func TestARowLabelIsNeverEnoughToArrive(t *testing.T) {
	// Titles are matched against the ROUTE's title only. A row, a tab label or
	// a Back button carrying the destination's name must not count.
	screen := []Element{
		{Label: "Ajustes", Role: "heading"},
		{Label: "Cámara", Role: "button"},             // a row
		{Label: "Cámara", Role: "tab"},                // a tab label
		{Label: "Cámara", Role: "button", ID: "back"}, // a Back button
	}
	if ParseArrivalCriterion("Cámara").MatchesRoute(ExtractRoute(screen), screen) {
		t.Fatal("a row, a tab and a Back button named the destination and that counted as arrival")
	}
}

// --- The route --------------------------------------------------------------

func TestTheRouteIsTheFirstHeadingNotAnyHeading(t *testing.T) {
	// Measured on a real screen: iOS Settings puts the screen name in the first
	// `heading` and its descriptive blurb in the second.
	screen := []Element{
		{Label: "General", Role: "heading"},
		{Label: "Gestiona la configuración y las preferencias generales…", Role: "heading"},
	}
	if got := ExtractRoute(screen).Title; got != "General" {
		t.Fatalf("expected the screen name, got %q", got)
	}
}

func TestAModalIsPartOfTheRoute(t *testing.T) {
	screen := []Element{
		{Label: "Ajustes", Role: "heading"},
		{Label: "¿Seguro?", Role: "alert"},
	}
	if got := ExtractRoute(screen).Modal; got != "¿Seguro?" {
		t.Fatalf("a modal on top should be in the route, got %q", got)
	}
}

func TestAScreenWithNothingIdentifyingSaysSo(t *testing.T) {
	// A real state, not an error: without a heading, a selected tab or a modal
	// there is nothing to compare, and goto must not invent one.
	screen := []Element{{Role: "group"}, {Role: "image"}}
	if !ExtractRoute(screen).IsZero() {
		t.Fatal("a screen with no identity should produce a zero route")
	}
}

// --- The criterion language --------------------------------------------------

func TestSeveralTermsAreAllRequired(t *testing.T) {
	// What makes a parameterised screen expressible: the same title with a
	// different order number is a different destination.
	screen := []Element{
		{Label: "Detalle del pedido", Role: "heading"},
		{Label: "Pedido 456", Role: "text"},
	}
	c := ParseArrivalCriterion(`title:"Detalle del pedido" text:"123"`)
	if c.MatchesRoute(ExtractRoute(screen), screen) {
		t.Fatal("the right screen with the wrong instance counted as arrival")
	}
	screen[1].Label = "Pedido 123"
	if !c.MatchesRoute(ExtractRoute(screen), screen) {
		t.Fatal("the right screen with the right instance did not count")
	}
}

func TestAQuotedScreenNameSurvivesItsSpaces(t *testing.T) {
	c := ParseArrivalCriterion(`title:"Idioma y región"`)
	if len(c.Titles) != 1 || c.Titles[0] != "Idioma y región" {
		t.Fatalf("a quoted title was split: %+v", c.Titles)
	}
}

func TestNoCriterionMatchesNothing(t *testing.T) {
	// The property the whole "unverified" contract rests on: with nothing
	// declared, there is no arrival to assert.
	screen := notificationsScreen()
	var empty ArrivalCriterion
	if empty.MatchesRoute(ExtractRoute(screen), screen) {
		t.Fatal("an empty criterion must never match; goto would claim arrival it cannot verify")
	}
}

// --- Stopping ----------------------------------------------------------------

func TestARevisitedScreenIsDetected(t *testing.T) {
	seen := NewSeenRoutes()
	a := screenFingerprint(settingsListScreen())
	b := screenFingerprint(notificationsScreen())
	if seen.Visit(a) {
		t.Fatal("the first visit is not a revisit")
	}
	if seen.Visit(b) {
		t.Fatal("a different screen is not a revisit")
	}
	if !seen.Visit(a) {
		t.Fatal("coming back to the first screen should be detected as looping")
	}
}

func TestTheFingerprintIgnoresFrameAndValue(t *testing.T) {
	// Coordinates drift by fractions of a point between two reads of a still
	// screen, and a clock or a spinner moves on its own. A fingerprint that
	// moves by itself detects nothing.
	a := []Element{{ID: "X", Label: "L", Role: "button", Frame: "{{0, 0}, {10, 10}}", Value: "12:01"}}
	b := []Element{{ID: "X", Label: "L", Role: "button", Frame: "{{0.3, 0.1}, {10, 10}}", Value: "12:02"}}
	if screenFingerprint(a) != screenFingerprint(b) {
		t.Fatal("the fingerprint moved on a screen that did not")
	}
}

func TestNodeCountWouldNotHaveCaughtThat(t *testing.T) {
	// Measured on a real simulator: 80 nodes before a tap and 80 after, with
	// the screen entirely different. This is why the detector is identity and
	// not arithmetic, asserted so nobody swaps it back for a counter.
	before := []Element{{ID: "A", Role: "button"}, {ID: "B", Role: "button"}}
	after := []Element{{ID: "C", Role: "button"}, {ID: "D", Role: "button"}}
	if len(before) != len(after) {
		t.Fatal("fixture broken: the two screens must have the same node count")
	}
	if screenFingerprint(before) == screenFingerprint(after) {
		t.Fatal("two different screens with the same node count share a fingerprint")
	}
}

// --- The destructive guard ---------------------------------------------------

func TestGotoNeverTapsSomethingDestructive(t *testing.T) {
	// Stricter than find's guard and with no escape hatch. find returns a
	// destructive element when the caller's own words ask for it, because the
	// caller reads the answer first. Here nobody reads anything between the
	// decision and the finger.
	del := Element{ID: "DELETE", Label: "Borrar cuenta", Role: "button"}
	if !GotoRefusesDestructive(&del) {
		t.Fatal("goto would have tapped a delete button")
	}
}

func TestTheDestructiveGuardHasNoEscapeHatchInGoto(t *testing.T) {
	// The difference from find, asserted rather than described: even a goal
	// that asks for it in the caller's own words does not unlock it, because
	// GotoRefusesDestructive does not take the goal at all.
	del := Element{ID: "DELETE", Label: "Borrar cuenta", Role: "button"}
	if !GotoRefusesDestructive(&del) {
		t.Fatal("the guard must not depend on what was asked for")
	}
	ordinary := Element{ID: "CAMERA", Label: "Cámara", Role: "button"}
	if GotoRefusesDestructive(&ordinary) {
		t.Fatal("the guard fired on an ordinary row; it would refuse every run")
	}
}

// --- Tapping the point it already has ----------------------------------------

func TestTheTapPointIsTheCentreOfTheElement(t *testing.T) {
	// goto taps the point it resolved rather than a selector, because a
	// selector tap re-reads the tree: 277ms by coordinates against 1,480ms by
	// text, measured, and that read is the whole saving.
	el := Element{Frame: "{{16, 571.33333333333326}, {370, 52}}"}
	x, y, ok := TapPoint(el)
	if !ok {
		t.Fatal("a well-formed frame should yield a point")
	}
	if x != 201 || y != 597 {
		t.Fatalf("expected the centre (201, 597), got (%d, %d)", x, y)
	}
}

func TestAnElementWithNoFrameCannotBeTapped(t *testing.T) {
	if _, _, ok := TapPoint(Element{}); ok {
		t.Fatal("an element with no frame must not produce a point to tap")
	}
}

func TestEveryOutcomeIsDistinct(t *testing.T) {
	// A loop that stops has to say which way it stopped; two outcomes sharing
	// a string would silently merge two situations that need different action.
	all := []string{
		GotoArrived, GotoExhausted, GotoTimeout, GotoStuck, GotoLooping,
		GotoNoRoute, GotoRefused, GotoOutOfApp, GotoAmbiguousCriterion, GotoCIRefused,
	}
	seen := map[string]bool{}
	for _, o := range all {
		if seen[o] {
			t.Fatalf("duplicate outcome %q", o)
		}
		seen[o] = true
	}
}
