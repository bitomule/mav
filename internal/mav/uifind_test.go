package mav

import (
	"strings"
	"testing"
)

// A small screen that carries the three shapes that matter: an ordinary target,
// a destructive one, and a second element sharing a label with nothing.
func settingsScreen() []Element {
	return []Element{
		{Role: "text", Label: "Ajustes"},
		{ID: "com.apple.settings.camera", Label: "Cámara", Role: "button", Enabled: "true"},
		{ID: "com.apple.settings.battery", Label: "Batería", Role: "button", Enabled: "true"},
		{ID: "WIFI", Label: "Wi-Fi", Role: "cell", Enabled: "true"},
		{ID: "DELETE_ACCOUNT", Label: "Borrar cuenta", Role: "button", Enabled: "true"},
		{ID: "SIGN_OUT", Label: "Cerrar sesión", Role: "button", Enabled: "true"},
		{Role: "image"},
	}
}

func TestFindCandidatesKeepsOnlyActionableElementsWithText(t *testing.T) {
	got := FindCandidates(settingsScreen())
	if len(got) != 5 {
		t.Fatalf("expected the 5 actionable labelled elements, got %d", len(got))
	}
	for _, el := range got {
		if el.Role == "text" || el.Role == "image" {
			t.Fatalf("a non-actionable element survived: %+v", el)
		}
	}
}

func TestLiteralResolutionNeedsNoModel(t *testing.T) {
	// 14.4% of real searches are the element's own text. This is the path that
	// still works with no key and no network.
	el, ok := FindLiteral(FindCandidates(settingsScreen()), "Cámara")
	if !ok {
		t.Fatal("an exact label should resolve literally")
	}
	if el.ID != "com.apple.settings.camera" {
		t.Fatalf("resolved the wrong element: %+v", el)
	}
}

func TestLiteralResolutionFoldsAccentsAndCase(t *testing.T) {
	el, ok := FindLiteral(FindCandidates(settingsScreen()), "  camara ")
	if !ok || el.ID != "com.apple.settings.camera" {
		t.Fatalf("expected the accent-folded match, got ok=%v el=%+v", ok, el)
	}
}

func TestLiteralResolutionRefusesWhenTwoElementsShareTheText(t *testing.T) {
	// Two "Guardar" buttons is exactly the case where picking the first is
	// worse than not answering: it taps something at random.
	screen := []Element{
		{ID: "A", Label: "Guardar", Role: "button"},
		{ID: "B", Label: "Guardar", Role: "button"},
	}
	if _, ok := FindLiteral(FindCandidates(screen), "Guardar"); ok {
		t.Fatal("an ambiguous literal match must not resolve")
	}
}

// --- The control that has to come first --------------------------------------
//
// Before any of the "find is right" assertions mean anything, the instrument has
// to be shown capable of failing. These three tests are that: each one feeds the
// resolver an answer that is wrong in a different way and asserts it is caught.
// If they pass while the ones above also pass, the ones above are evidence.

func TestControlAWrongIndexIsCaughtNotReturned(t *testing.T) {
	batch := FindCandidates(settingsScreen())
	// The model answers yes on an index past the end of the list it was given.
	el, reason := InterpretFindAnswer("yes", "99", batch)
	if el != nil {
		t.Fatalf("an out-of-range index was returned as an element: %+v", el)
	}
	if reason != ReasonNotACandidate {
		t.Fatalf("expected %s, got %s", ReasonNotACandidate, reason)
	}
}

func TestControlBAConfidentButUnsureVerdictIsAnAbstention(t *testing.T) {
	// The measurement this encodes: correct picks score from 0.76 and wrong
	// ones reach 0.88, so no number separates them. The verdict does. Nothing
	// in this path reads a probability — the function takes no such argument.
	batch := FindCandidates(settingsScreen())
	for _, verdict := range []string{"unsure", "no", ""} {
		el, reason := InterpretFindAnswer(verdict, "2", batch)
		if el != nil {
			t.Fatalf("verdict %q returned an element: %+v", verdict, el)
		}
		if reason != ReasonAbstained {
			t.Fatalf("verdict %q gave reason %q, expected %s", verdict, reason, ReasonAbstained)
		}
	}
}

func TestControlCAnElementNotInTheBatchIsVetoed(t *testing.T) {
	// The measured failure: asked for something absent from the screen, the
	// model chose an element anyway 13 times out of 26. The veto is what makes
	// that harmless.
	batch := FindCandidates(settingsScreen())
	stranger := Element{ID: "NOT_HERE", Label: "Exportar", Role: "button"}
	if got := VetoChoice(&stranger, "export the file", batch); got != ReasonNotACandidate {
		t.Fatalf("an element that was never on the screen passed the veto: %q", got)
	}
}

// --- The veto, in both directions --------------------------------------------

func TestDestructiveElementIsVetoedWhenTheGoalDidNotAskForIt(t *testing.T) {
	batch := FindCandidates(settingsScreen())
	var deleteBtn *Element
	for i := range batch {
		if batch[i].ID == "DELETE_ACCOUNT" {
			deleteBtn = &batch[i]
		}
	}
	if deleteBtn == nil {
		t.Fatal("fixture lost its destructive button")
	}
	if got := VetoChoice(deleteBtn, "abre los ajustes de la cuenta", batch); got != ReasonDestructiveGuard {
		t.Fatalf("a delete button passed a non-destructive goal: %q", got)
	}
}

func TestDestructiveElementIsAllowedWhenTheGoalSaysSo(t *testing.T) {
	// The guard removes a yes; it must not remove the one the caller asked for
	// in their own words, or the command cannot be used to test a delete flow.
	batch := FindCandidates(settingsScreen())
	var deleteBtn *Element
	for i := range batch {
		if batch[i].ID == "DELETE_ACCOUNT" {
			deleteBtn = &batch[i]
		}
	}
	if got := VetoChoice(deleteBtn, "el botón de borrar la cuenta", batch); got != "" {
		t.Fatalf("an explicitly requested destructive element was vetoed: %q", got)
	}
}

func TestTheVetoCanOnlyRemoveAYes(t *testing.T) {
	// The structural property, asserted rather than described: VetoChoice
	// returns either "" (leave the decision alone) or a reason (remove it). It
	// has no return value that produces an element, so no arrangement of
	// screen and goal can make it add one.
	batch := FindCandidates(settingsScreen())
	if got := VetoChoice(nil, "anything at all", batch); got == "" {
		t.Fatal("a nil choice must not be allowed through")
	}
}

func TestSignOutIsTreatedAsDestructive(t *testing.T) {
	for _, s := range []string{"Cerrar sesión", "Sign Out", "Log out", "Eliminar todo", "Reset Settings"} {
		if !IsDestructive(s) {
			t.Fatalf("%q should be destructive", s)
		}
	}
	for _, s := range []string{"Cámara", "Wi-Fi", "Guardar", "Ajustes"} {
		if IsDestructive(s) {
			t.Fatalf("%q should not be destructive", s)
		}
	}
}

// --- The batch, and never cutting in silence ---------------------------------

func TestABatchThatDoesNotFitReportsWhatWasLeftOut(t *testing.T) {
	// The whole reason this command exists is a list that was cut without
	// saying so. find must not do it too.
	many := make([]Element, 0, 200)
	for i := 0; i < 200; i++ {
		many = append(many, Element{ID: "id" + itoa(i), Label: "Fila " + itoa(i), Role: "button"})
	}
	batch, omitted := FindBatch(FindCandidates(many))
	if len(batch) != findCandidateCap {
		t.Fatalf("batch should be capped at %d, got %d", findCandidateCap, len(batch))
	}
	if omitted != 200-findCandidateCap {
		t.Fatalf("omitted should be %d, got %d", 200-findCandidateCap, omitted)
	}
}

func TestASmallScreenReportsNothingOmitted(t *testing.T) {
	batch, omitted := FindBatch(FindCandidates(settingsScreen()))
	if omitted != 0 {
		t.Fatalf("nothing should be omitted from a 5-candidate screen, got %d", omitted)
	}
	if len(batch) != 5 {
		t.Fatalf("expected the whole batch, got %d", len(batch))
	}
}

// --- The question, and the options it offers ---------------------------------

func TestTheOptionsAlwaysIncludeAnAbstention(t *testing.T) {
	// Without somewhere to put a refusal, a model that has to pick one of the
	// options will pick a wrong one. `none` is the whole safety valve.
	opts := FindOptions(FindCandidates(settingsScreen()))
	if opts[len(opts)-1] != "none" {
		t.Fatalf("the last option must be none, got %v", opts)
	}
	if len(opts) != 6 {
		t.Fatalf("expected 5 indices plus none, got %v", opts)
	}
}

func TestTheQuestionTellsTheModelThatDecliningIsCorrect(t *testing.T) {
	q := FindQuestion("the camera row")
	for _, want := range []string{"none", "Do not guess", "correct and expected"} {
		if !strings.Contains(q, want) {
			t.Fatalf("the question should carry %q:\n%s", want, q)
		}
	}
}

func TestRenderedCandidatesCarryIdentityAndNotCoordinates(t *testing.T) {
	rendered := RenderFindCandidates(FindCandidates(settingsScreen()))
	if !strings.Contains(rendered, "label=Cámara") {
		t.Fatalf("rendering lost the label:\n%s", rendered)
	}
	if strings.Contains(rendered, "frame=") || strings.Contains(rendered, "{{") {
		t.Fatalf("rendering leaked frame coordinates the model could do arithmetic over:\n%s", rendered)
	}
	if !strings.HasPrefix(rendered, "1) ") {
		t.Fatalf("numbering must start at 1:\n%s", rendered)
	}
}

// --- The contract ------------------------------------------------------------

func TestNoCandidatesIsAnAnswerAndNotAnError(t *testing.T) {
	c := CLI{}
	got := c.resolveFind(t.Context(), []Element{{Role: "image"}}, "the save button")
	if got.ResolvedBy != ResolvedByNone {
		t.Fatalf("expected none, got %q", got.ResolvedBy)
	}
	if got.Reason != ReasonNoCandidates {
		t.Fatalf("expected %s, got %q", ReasonNoCandidates, got.Reason)
	}
	if got.Next == "" {
		t.Fatal("every none must say what to do next")
	}
}

func TestALiteralGoalResolvesWithoutTouchingTheModelPath(t *testing.T) {
	// Proven by the environment, not by inspection: with the model refused
	// outright, a literal goal still resolves. If this ever reached jev it
	// would come back none.
	t.Setenv("CI", "1")
	c := CLI{}
	got := c.resolveFind(t.Context(), settingsScreen(), "Wi-Fi")
	if got.ResolvedBy != ResolvedByLiteral {
		t.Fatalf("expected a literal resolution with the model refused, got %q reason=%q", got.ResolvedBy, got.Reason)
	}
	if got.Element == nil || got.Element.ID != "WIFI" {
		t.Fatalf("wrong element: %+v", got.Element)
	}
}

func TestFindRefusesToConsultAModelInCI(t *testing.T) {
	// "Not used in CI" has to be a guarantee and not a convention, so it is
	// enforced here rather than written in a README.
	t.Setenv("CI", "1")
	c := CLI{}
	got := c.resolveFind(t.Context(), settingsScreen(), "the row that opens the camera settings")
	if got.ResolvedBy != ResolvedByNone {
		t.Fatalf("expected none in CI, got %q", got.ResolvedBy)
	}
	if got.Reason != ReasonCIRefused {
		t.Fatalf("expected %s, got %q", ReasonCIRefused, got.Reason)
	}
}

func TestCouldNotAskAndAmNotSureAreDifferentAnswers(t *testing.T) {
	// The distinction the caller acts on: "no pude preguntar" leaves the screen
	// unjudged, "no estoy seguro" means it was read. Both fall back to the
	// tree, but only one of them says the screen was looked at.
	askFailures := map[string]bool{
		ReasonNoKey: true, ReasonNoNetwork: true, ReasonCIRefused: true,
	}
	judged := map[string]bool{
		ReasonAbstained: true, ReasonNotACandidate: true, ReasonDestructiveGuard: true,
	}
	for r := range askFailures {
		if judged[r] {
			t.Fatalf("%s cannot be both", r)
		}
	}
	if len(askFailures)+len(judged) != 6 {
		t.Fatal("a reason was added without deciding which side it is on")
	}
}
