package mav

import (
	"encoding/json"
	"strings"
	"testing"
)

func actionSpaceFixture(t *testing.T) ActionSpace {
	t.Helper()
	batch, _ := FindBatch(FindCandidates(loadFixtureScreen(t, fixtureCategoriesThree)))
	return ActionSpace{Goal: "la primera categoría", Batch: batch}
}

// The guard on the one thing that must not drift. jevi's `--options` shorthand
// builds the single question as `{"instructions": …, "type": "choice",
// "criteria": {…}}` under the name `answer`, in that key order, with the
// criteria in the order the options were given — and jevi forwards each object
// to the model in the order it was written, so the order IS the request.
//
// The whole action space is only safe to wire in if the tap head is still the
// head production sends. This asserts the exact bytes rather than the parsed
// shape, because a parsed comparison is blind to the thing that matters.
func TestTheTapHeadIsExactlyWhatProductionSends(t *testing.T) {
	space := actionSpaceFixture(t)
	set := ActionSpaceQuestions(space)

	var criteria strings.Builder
	criteria.WriteString("{")
	for i, option := range FindOptions(space.Batch) {
		if i > 0 {
			criteria.WriteString(",")
		}
		criteria.WriteString(jsonString(option) + ":" + jsonString(option))
	}
	criteria.WriteString("}")

	want := `"answer":{"instructions":` + jsonString(GotoStepQuestion(space.Goal)) +
		`,"type":"choice","criteria":` + criteria.String() + `}`
	if !strings.Contains(set, want) {
		t.Fatalf("the tap head is no longer byte-identical to jevi's shorthand.\nwant to contain:\n%s\ngot:\n%s", want, set)
	}
	if !strings.HasPrefix(set, `{"version":1,"questions":{`+want) {
		t.Errorf("the tap head must come first in the set, as it is the only head in production")
	}
}

func TestTheQuestionSetIsValidJSONWithOneHeadPerThingAsked(t *testing.T) {
	space := actionSpaceFixture(t)
	space.InputKeys = []string{"color", "nombre"}

	var doc struct {
		Version   int                        `json:"version"`
		Questions map[string]json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal([]byte(ActionSpaceQuestions(space)), &doc); err != nil {
		t.Fatalf("the set jevi is handed does not parse: %v", err)
	}
	if doc.Version != 1 {
		t.Errorf("version 1 is the only one jevi understands, got %d", doc.Version)
	}
	for _, head := range []string{actionHeadTap, actionHeadOperation, actionHeadTypeTo, actionHeadTypeWhat} {
		if _, ok := doc.Questions[head]; !ok {
			t.Errorf("head %q is missing from the set", head)
		}
	}
}

// Typing is only on the menu when there is something declared to type. An
// operation whose only possible outcome is an abstention is an option that can
// only take accuracy off the heads that matter.
func TestTypingIsOffTheMenuWhenNobodyDeclaredAnything(t *testing.T) {
	space := actionSpaceFixture(t)
	if space.OffersTyping() {
		t.Fatal("a space with no declared inputs must not offer typing")
	}
	set := ActionSpaceQuestions(space)
	for _, head := range []string{actionHeadTypeTo, actionHeadTypeWhat} {
		if strings.Contains(set, `"`+head+`":`) {
			t.Errorf("head %q must not be sent when there is nothing to type", head)
		}
	}
	if strings.Contains(set, `"`+ActionType+`":"`+ActionType+`"`) {
		t.Errorf("`type` must not be an option on the operation head when there is nothing to type")
	}
}

// The rule this piece exists to keep: the model picks a NAME and the code holds
// the characters. Anything that is not a declared name — free text above all —
// is an abstention, so there is no branch on which what the model wrote reaches
// a keyboard.
func TestNothingTheModelWritesCanEverBeTyped(t *testing.T) {
	keys := []string{"nombre", "cantidad"}
	for _, label := range []string{
		"Test category",
		"none",
		"NOMBRE",
		"",
		"nombre; rm -rf /",
		"3",
	} {
		key, reason := InterpretActionInput(label, keys)
		if key != "" {
			t.Errorf("%q resolved to the declared key %q; only an exact declared name may", label, key)
		}
		if reason == "" {
			t.Errorf("%q produced neither a key nor a reason", label)
		}
	}
	if key, reason := InterpretActionInput("nombre", keys); key != "nombre" || reason != "" {
		t.Errorf("a declared name must resolve: got %q %q", key, reason)
	}
}

func TestTheValueComesFromWhatTheCallerDeclared(t *testing.T) {
	inputs := map[string]string{"nombre": "Test category"}
	got, ok := ActionSpaceText(ActionChoice{Operation: ActionType, InputKey: "nombre"}, inputs)
	if !ok || got != "Test category" {
		t.Errorf("expected the declared value, got %q %v", got, ok)
	}
	if _, ok := ActionSpaceText(ActionChoice{Operation: ActionType, InputKey: "inventado"}, inputs); ok {
		t.Error("a key nobody declared must not resolve to anything")
	}
	if _, ok := ActionSpaceText(ActionChoice{Operation: ActionTap, InputKey: "nombre"}, inputs); ok {
		t.Error("a tap has no text; resolving one is a bug waiting to type on a button")
	}
}

// goto's destructive guard is not relaxed by any of this, and a space that can
// write makes that stricter rather than looser: a field is easier to fill with
// something irreversible than a button is to press by accident.
func TestTheDestructiveGuardStillCoversWhatTheSpaceReturns(t *testing.T) {
	deleteButton := Element{ID: "deleteAll", Label: "Borrar todo", Role: "button"}
	if !GotoRefusesDestructive(&deleteButton) {
		t.Fatal("goto's guard must still refuse a destructive element reached through the action space")
	}
}
