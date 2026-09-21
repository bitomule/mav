package mav

import "testing"

// iOS renders a list row as TWO accessibility elements — a container button and
// an inner one — with the same label, role and id. Measured on Settings >
// General: `Idioma y región` appears four times as a button and twice more as
// text, and there is not one unique label on the whole screen.
//
// Sent as separate options they read as indistinguishable candidates, and a
// model told to decline when two are equally plausible declines on every row of
// every list. That is not a hypothetical: `mav goto` stopped with no_route on a
// destination one visible tap away, and find's literal path could never resolve
// anything on such a screen.

func duplicatedListScreen() []Element {
	return []Element{
		{Label: "General", Role: "heading"},
		{ID: "INTERNATIONAL", Label: "Idioma y región", Role: "button", Enabled: "true"},
		{ID: "INTERNATIONAL", Label: "Idioma y región", Role: "button", Enabled: "true"},
		{ID: "INTERNATIONAL", Label: "Idioma y región", Role: "button", Enabled: "true"},
		{ID: "FONT_SETTING", Label: "Tipos de letra", Role: "button", Enabled: "true"},
		{ID: "FONT_SETTING", Label: "Tipos de letra", Role: "button", Enabled: "true"},
	}
}

func TestARowTheTreeMentionsTwiceIsOneCandidate(t *testing.T) {
	got := FindCandidates(duplicatedListScreen())
	if len(got) != 2 {
		t.Fatalf("expected the two distinct rows, got %d: %+v", len(got), got)
	}
}

func TestDeduplicationRestoresTheLiteralPathOnAListScreen(t *testing.T) {
	// Before the de-duplication this returned nothing: three identical entries
	// made every literal match "ambiguous", so the path that needs no key and
	// no network was dead on exactly the screens it is most useful on.
	el, ok := FindLiteral(FindCandidates(duplicatedListScreen()), "Idioma y región")
	if !ok {
		t.Fatal("a row duplicated by the tree should still resolve literally")
	}
	if el.ID != "INTERNATIONAL" {
		t.Fatalf("resolved the wrong row: %+v", el)
	}
}

func TestTwoGenuinelyDifferentRowsStayTwoCandidates(t *testing.T) {
	// The control: de-duplication must not collapse rows that differ. If it
	// did, the test above would pass while find silently lost half a screen.
	screen := []Element{
		{ID: "A", Label: "Guardar", Role: "button"},
		{ID: "B", Label: "Guardar", Role: "button"},
	}
	if got := FindCandidates(screen); len(got) != 2 {
		t.Fatalf("two different elements sharing a label are still two candidates, got %d", len(got))
	}
	// And they are still genuinely ambiguous to a literal match, which is the
	// case de-duplication must leave exactly as it was.
	if _, ok := FindLiteral(FindCandidates(screen), "Guardar"); ok {
		t.Fatal("two distinct rows with one label must not resolve literally")
	}
}

func TestTheLoopAsksAWiderQuestionThanFind(t *testing.T) {
	// find asks "which element IS it" and goto needs "IS it, or LEADS to it".
	// Measured both ways round: find's question made goto stop at the Settings
	// root with the destination two taps away, and a first attempt at the wider
	// one ("they are not there yet, what gets them closer") made it hesitate on
	// the row that IS the destination — find resolved it and goto did not, same
	// screen, same minute. So both cases are named explicitly.
	q := GotoStepQuestion("the language screen")
	for _, want := range []string{"IS what they are looking for", "LEADS towards it", "none", "Do not guess"} {
		if !contains(q, want) {
			t.Fatalf("the step question should carry %q:\n%s", want, q)
		}
	}
	// And it must NOT carry find's "two or more could equally be it": on an
	// iOS list every row is rendered twice, and that clause abstained on all
	// of them.
	if contains(q, "could equally be it") {
		t.Fatal("the clause that abstained on every duplicated row is back")
	}
	// The goal is named as something a USER DESCRIBED, not as a destination
	// somebody is trying to reach. That single sentence is what the 40-run
	// table in GotoStepQuestion's comment moved: with "Someone is trying to
	// reach", "la primera categoría" on Boxy's grid came back as the row
	// literally named "Test Category 1" 6 to 11 times in 40; with this line it
	// is the first category 40 times in 40, and the multi-step cell the LEADS
	// clause protects stays at 40/40 either way.
	if !contains(q, "A user described where they want to get to as:") {
		t.Fatalf("the step question must name the goal as the user's own description:\n%s", q)
	}
	if contains(q, "Someone is trying to reach") {
		t.Fatal("the naming line measured to lose the first-category cell is back")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
