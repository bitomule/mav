package mav

import (
	"strings"
	"testing"
)

// The menu is the screens the run stood on, and the chronology has to be gone
// from it: if the model can tell which entry is the endpoint, it is grading its
// own arrival rather than naming a destination among screens.
func TestObservedScreensAreAlphabeticalAndCarryNoOrder(t *testing.T) {
	var recorded []ObservedScreen
	recorded = RecordObservedScreen(recorded, Route{Screen: "categories-view"}, nil)
	recorded = RecordObservedScreen(recorded, Route{Screen: "boxes-view"}, nil)
	recorded = RecordObservedScreen(recorded, Route{Title: "Moving Boxes: Office cables"}, nil)

	names := ObservedNames(ObservedScreens(recorded))
	want := []string{"boxes-view", "categories-view", "Moving Boxes: Office cables"}
	if len(names) != len(want) {
		t.Fatalf("menu = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("menu = %v, want %v", names, want)
		}
	}
	rendered := RenderObservedMenu(names)
	if strings.Contains(rendered, "step") || strings.Contains(rendered, "last") {
		t.Errorf("the menu leaks where the run ended:\n%s", rendered)
	}
}

// A screen with neither a title nor a screen identity cannot be confirmed by
// anything, so offering it would be offering an answer nothing could check.
// A screen with only an identity IS offered: that fallback is what keeps the
// menu from collapsing to one entry on Boxy, where neither the category grid
// nor the box list has a heading.
func TestObservedScreensNameUntitledScreensByIdentity(t *testing.T) {
	var recorded []ObservedScreen
	recorded = RecordObservedScreen(recorded, Route{Tab: "Cajas"}, nil)
	recorded = RecordObservedScreen(recorded, Route{Screen: "boxes-view"}, nil)
	recorded = RecordObservedScreen(recorded, Route{Title: "Detalle"}, nil)
	names := ObservedNames(ObservedScreens(recorded))
	want := []string{"boxes-view", "Detalle"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("menu = %v, want %v (the tab-only screen has nothing to check a pick against)", names, want)
	}
}

// Two different screens that happen to share a title are one entry: the menu is
// of destinations, not of visits.
func TestObservedScreensDeduplicate(t *testing.T) {
	var recorded []ObservedScreen
	for _, r := range []Route{{Title: "Ajustes"}, {Title: "General"}, {Title: "Ajustes"}, {Title: "General"}} {
		recorded = RecordObservedScreen(recorded, r, nil)
	}
	names := ObservedNames(ObservedScreens(recorded))
	if len(names) != 2 {
		t.Fatalf("menu = %v, want 2 distinct titles", names)
	}
}

// The question must never ask about the journey, the run, or whether anything
// worked. It asks which screen the destination IS.
func TestObservedQuestionNeverAsksWhetherItArrived(t *testing.T) {
	q := strings.ToLower(GotoObservedQuestion("the camera settings screen"))
	for _, banned := range []string{"arrive", "did you", "did they", "succeed", "worked", "last screen", "ended", "step"} {
		if strings.Contains(q, banned) {
			t.Errorf("the question asks the model to grade the run (%q):\n%s", banned, q)
		}
	}
	if !strings.Contains(q, "`none`") {
		t.Errorf("the question has no abstention on the menu:\n%s", q)
	}
}

func TestGotoObservedOptionsAlwaysCarryNone(t *testing.T) {
	opts := GotoObservedOptions([]string{"Ajustes", "General"})
	if len(opts) != 3 || opts[2] != "none" {
		t.Fatalf("options = %v, want the names plus none", opts)
	}
}

func TestInterpretGotoObservedAnswer(t *testing.T) {
	menu := []ObservedScreen{{Name: "Ajustes", IsTitle: true}, {Name: "General", IsTitle: true}}
	for _, tc := range []struct {
		label string
		want  string
		ok    bool
	}{
		{"2", "General", true},
		{"none", "", false},
		{"", "", false},
		{"9", "", false},
		{"General", "", false},
	} {
		got, ok := InterpretGotoObservedAnswer(tc.label, menu)
		if got.Name != tc.want || ok != tc.ok {
			t.Errorf("label %q = (%q,%v), want (%q,%v)", tc.label, got.Name, ok, tc.want, tc.ok)
		}
	}
}

// The one pick that would be actively wrong is the screen the run started on.
// Same origin negation an explicit --arrived-when goes through; this one is
// dropped rather than stopping the command, because it is goto's mistake.
func TestAcceptObservedCriterionRefusesTheStartingScreen(t *testing.T) {
	start := Route{Screen: "categories-view", Title: "Moving Boxes"}
	if _, ok := AcceptObservedCriterion(ObservedScreen{Name: "Moving Boxes", IsTitle: true}, start); ok {
		t.Error("a pick naming the title of the screen the run started on was accepted")
	}
	if _, ok := AcceptObservedCriterion(ObservedScreen{Name: "categories-view"}, start); ok {
		t.Error("a pick naming the identity of the screen the run started on was accepted")
	}
	c, ok := AcceptObservedCriterion(ObservedScreen{Name: "Moving Boxes: Office cables", IsTitle: true}, start)
	if !ok {
		t.Fatal("a pick naming a screen other than the start was refused")
	}
	if c.String() != `title:"Moving Boxes: Office cables"` {
		t.Errorf("criterion = %q", c.String())
	}
	c, ok = AcceptObservedCriterion(ObservedScreen{Name: "boxes-view"}, start)
	if !ok || c.String() != `screen:"boxes-view"` {
		t.Errorf("an untitled screen must come back as a screen: criterion, got %q", c.String())
	}
}

// A screen id is an identity and is compared whole; a title is a name and is
// contained. `screen:"boxes"` must not match `boxes-view`.
func TestScreenCriterionIsWholeNotSubstring(t *testing.T) {
	route := Route{Screen: "boxes-view"}
	if (ArrivalCriterion{Screens: []string{"boxes"}}).MatchesRoute(route, nil) {
		t.Error("a partial screen id matched")
	}
	if !(ArrivalCriterion{Screens: []string{"boxes-view"}}).MatchesRoute(route, nil) {
		t.Error("the screen id did not match itself")
	}
	if ParseArrivalCriterion(`screen:"boxes-view"`).String() != `screen:"boxes-view"` {
		t.Error("--arrived-when does not round-trip a screen: term")
	}
}

// The property the whole design rests on: a pick that is not where the run
// stopped leaves arrived=unverified, never false. Naming among observed screens
// can only ever turn an unverified into a true.
func TestObservedCriterionOnlyEverConfirmsTheScreenItStoppedOn(t *testing.T) {
	final := Route{Title: "Moving Boxes: Office cables"}
	confirmed := ArrivalCriterion{Titles: []string{"Moving Boxes: Office cables"}}
	if !confirmed.MatchesRoute(final, nil) {
		t.Error("the destination's own title does not match the route it names")
	}
	elsewhere := ArrivalCriterion{Titles: []string{"Vista de cajas vacía"}}
	if elsewhere.MatchesRoute(final, nil) {
		t.Error("a title from another screen matched the final route")
	}
}

// The text beside each name is what makes a positional goal answerable, and it
// has to arrive in tree order — roughly top to bottom — or "the first category"
// is not in the menu at all.
func TestScreenShowsKeepsTreeOrderAndDropsWhatAddsNothing(t *testing.T) {
	shows := ScreenShows([]Element{
		{Label: "bBOXY", Role: "application"},
		{Label: "Moving Boxes", Role: "button"},
		{Label: "Moving Boxes", Role: "text"},
		{Label: "Test Category 1", Role: "button"},
		{Label: "Office cables, Código 8993", Role: "button"},
		{Label: "Office cables", Role: "text"},
		{ID: "voice_record_button", Role: "button"},
	})
	want := []string{"Moving Boxes", "Test Category 1", "Office cables, Código 8993"}
	// the id-only element contributes nothing: identifiers are written for code
	if len(shows) != len(want) {
		t.Fatalf("shows = %v, want %v", shows, want)
	}
	for i := range want {
		if shows[i] != want[i] {
			t.Fatalf("shows = %v, want %v", shows, want)
		}
	}
}

// A menu line is the name and then what the screen shows, so the model is given
// the evidence rather than asked to supply it.
func TestObservedScreenLineCarriesWhatTheScreenShows(t *testing.T) {
	line := ObservedScreen{Name: "boxes-view", Shows: []string{"Moving Boxes", "Office cables"}}.Line()
	if line != "boxes-view — Moving Boxes, Office cables" {
		t.Errorf("line = %q", line)
	}
	if bare := (ObservedScreen{Name: "boxes-view"}).Line(); bare != "boxes-view" {
		t.Errorf("a screen showing nothing must be its bare name, got %q", bare)
	}
}
