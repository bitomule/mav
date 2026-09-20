package mav

import "testing"

// The three-option permission alert, as it really comes off the tree. This is
// the fixture the whole design turns on: two of these three buttons GRANT, the
// one that does not is last, and there is no kTCCService marker anywhere on it.
func threeOptionPermissionAlert() []Element {
	return []Element{
		{Label: "¿Permitir que la app Mapas use tu ubicación?", Role: "sheet"},
		{Label: "Permitir una vez", Role: "button", Frame: "{{20, 500}, {300, 44}}"},
		{Label: "Permitir al usarse la app", Role: "button", Frame: "{{20, 550}, {300, 44}}"},
		{Label: "No permitir", Role: "button", Frame: "{{20, 600}, {300, 44}}"},
	}
}

func TestTheDeclaredButtonIsFoundWhateverItsPosition(t *testing.T) {
	// Last here, first on other alerts. Position is not the rule; the caller's
	// own words are.
	btn := FindDismissButton(threeOptionPermissionAlert(), "No permitir")
	if btn == nil {
		t.Fatal("the declared button was not found")
	}
	if btn.Label != "No permitir" {
		t.Fatalf("found the wrong button: %+v", btn)
	}
}

func TestNothingIsDismissedWhenNothingWasDeclared(t *testing.T) {
	// The default is unchanged: goto stops at a modal. The door only opens for
	// a caller who named the button.
	if btn := FindDismissButton(threeOptionPermissionAlert(), ""); btn != nil {
		t.Fatalf("a modal was answered with no authorisation: %+v", btn)
	}
}

func TestADeclaredLabelThatIsNotThereFindsNothing(t *testing.T) {
	// Fail closed, and this is the assertion that matters most. With two of
	// three buttons granting, a near-miss that fell through to "something else"
	// would grant a permission. There is no near-miss: no prefix, no substring,
	// no closest match.
	for _, declared := range []string{
		"Don't Allow", // right idea, wrong language — the alert came back
		"No permit",   // in Spanish even from an app launched in English
		"permitir",    // a substring of two GRANTING buttons
		"No",          // a prefix
		"No permitir nunca",
	} {
		if btn := FindDismissButton(threeOptionPermissionAlert(), declared); btn != nil {
			t.Fatalf("%q matched %q — a near-miss on a permission alert grants it",
				declared, btn.Label)
		}
	}
}

func TestAccentsAndCaseStillFold(t *testing.T) {
	// The one latitude allowed, and it cannot reach another button: folding
	// case and accents never turns "No permitir" into "Permitir una vez".
	if btn := FindDismissButton(threeOptionPermissionAlert(), "  NO PERMITIR "); btn == nil {
		t.Fatal("case and spacing should fold, like every other label match here")
	}
}

func TestADestructiveButtonIsRefusedEvenWhenDeclared(t *testing.T) {
	// The one place goto overrules an explicit instruction. find would return
	// it — its caller reads the answer before anything is tapped — and goto has
	// nobody between the decision and the finger.
	screen := []Element{
		{Label: "¿Borrar todos tus datos?", Role: "sheet"},
		{Label: "Borrar todo", Role: "button", Frame: "{{0, 0}, {10, 10}}"},
		{Label: "Cancelar", Role: "button", Frame: "{{0, 20}, {10, 10}}"},
	}
	if btn := FindDismissButton(screen, "Borrar todo"); btn != nil {
		t.Fatal("goto would have tapped a destructive button because it was declared")
	}
	// And the harmless one on the same sheet still works, so the guard is not
	// simply refusing everything on a scary-looking screen.
	if btn := FindDismissButton(screen, "Cancelar"); btn == nil {
		t.Fatal("the non-destructive option should still be available")
	}
}

func TestOnlyButtonsAreEligible(t *testing.T) {
	// A label that matches on a static text is not a button, and tapping where
	// it sits is tapping something unknown.
	screen := []Element{
		{Label: "No permitir", Role: "text", Frame: "{{0, 0}, {10, 10}}"},
	}
	if btn := FindDismissButton(screen, "No permitir"); btn != nil {
		t.Fatalf("a non-button matched: %+v", btn)
	}
}

func TestThereIsACapOnHowManyDialogsOneRunAnswers(t *testing.T) {
	// An app that asks again after every tap would otherwise let the loop spend
	// its whole budget answering dialogs, which is a different failure from the
	// one the step budget is for.
	if gotoMaxDismissals < 1 || gotoMaxDismissals > 5 {
		t.Fatalf("the dismissal cap is %d, which is not a bounded number of dialogs",
			gotoMaxDismissals)
	}
}
