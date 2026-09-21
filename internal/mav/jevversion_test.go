package mav

import "testing"

// The floor is what makes removing mav's own off-menu check safe rather than
// merely tidy, so it is pinned here: get the comparison wrong in the permissive
// direction and a jevi without the check sails through, which is the exact hole
// the removal opened.
func TestTheFloorRefusesAJeviOlderThanTheCheckItReliesOn(t *testing.T) {
	for _, tc := range []struct {
		installed string
		allowed   bool
		why       string
	}{
		{"0.4.0", true, "the version that added off_menu_answer is the floor itself"},
		{"0.4.1", true, "a later patch still has it"},
		{"0.5.0", true, "a later minor still has it"},
		{"1.0.0", true, "a later major still has it"},
		{"0.3.9", false, "the last release before the check"},
		{"0.3.0", false, "older minor"},
		{"0.1.3", false, "what was on this machine before tonight"},
		// A release candidate of the floor carries the fix. Refusing it would
		// stop the one person testing the thing we depend on, which is the wrong
		// way round.
		{"0.4.0-rc1", true, "a pre-release of the floor still carries the fix"},
	} {
		if got := versionAtLeast(tc.installed, jevMinVersion); got != tc.allowed {
			t.Errorf("jevi %s: allowed=%v, want %v (%s)", tc.installed, got, tc.allowed, tc.why)
		}
	}
}

func TestTheVersionIsReadOutOfJeviOwnOutput(t *testing.T) {
	// `jevi --version` prints "jevi 0.4.0". Anything unparseable must come back
	// empty so the caller lets it through: blocking a working install over a
	// string we failed to read is worse than not checking.
	for out, want := range map[string]string{
		"jevi 0.4.0\n":   "0.4.0",
		"jevi v0.4.0":    "0.4.0",
		"jevi 0.4.0-rc1": "0.4.0-rc1",
		"":               "",
		"jevi":           "",
		"not a version":  "",
	} {
		if got := parseJevVersion(out); got != want {
			t.Errorf("parseJevVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

// The other half of the removal: the destructive guard is mav's alone and stays.
// It is about what this tool is willing to tap, not about whether an answer is
// well formed, so no upstream validation can take it over.
func TestTheDestructiveGuardSurvivedTheRemoval(t *testing.T) {
	destructive := Element{Label: "Borrar todo", Role: "button"}

	if veto := VetoChoice(&destructive, "algo inocente", []Element{destructive}); veto != ReasonDestructiveGuard {
		t.Fatalf("a destructive element must be vetoed when the goal did not ask for it, got %q", veto)
	}
	// And the direction that matters: the veto can only ever remove a yes. Asked
	// for exactly that, it is returned.
	if veto := VetoChoice(&destructive, "borrar todo", []Element{destructive}); veto != "" {
		t.Fatalf("the caller's own words asked to delete; the guard must let it through, got %q", veto)
	}
}

// What the removal actually removed, asserted so nobody puts it back by reflex:
// an element that was not in the batch is no longer vetoed here, because jevi
// withholds an off-menu label and it can no longer arrive.
func TestTheOffMenuCheckIsGoneBecauseJeviHoldsItBack(t *testing.T) {
	onScreen := Element{Label: "Guardar", Role: "button"}
	notOnScreen := Element{Label: "Nunca estuvo aquí", Role: "button"}

	if veto := VetoChoice(&notOnScreen, "guardar", []Element{onScreen}); veto != "" {
		t.Fatalf("mav no longer rechecks the menu -- jevi 0.4.0 returns no label at all "+
			"for an off-menu answer, and jevMinVersion is what keeps that true. Got %q", veto)
	}
}
