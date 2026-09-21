package mav

import "testing"

func inputsFlow(t *testing.T, step string) Flow {
	t.Helper()
	flow, err := ParseFlow([]byte(
		"name: f\ninputs:\n  nombre: \"Caja de herramientas\"\n  cantidad: \"12\"\nsteps:\n  - " + step + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

func TestTextFromCostsNoModel(t *testing.T) {
	// A named input is a map lookup. Nothing is asked, so this works with no
	// key and no network - and in CI, where asking is refused outright.
	t.Setenv("CI", "1")
	flow := inputsFlow(t, `type: { where: { id: name }, text: { from: nombre } }`)
	got, fields, err := CLI{}.resolveFlowStepText(t.Context(), flow.Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	if got != "Caja de herramientas" {
		t.Fatalf("expected the declared value, got %q", got)
	}
	if fields["text_from"] != "declared" {
		t.Fatalf("the record must say where the text came from: %+v", fields)
	}
}

func TestTextAskChoosesAKeyAndTheCodeSubstitutesTheValue(t *testing.T) {
	// The model names an input. It does not write one character of what gets
	// typed.
	fakeJev(t, "cantidad", 10)
	flow := inputsFlow(t, `type: { where: { id: qty }, text: { ask: "lo que toca escribir aquí" } }`)
	got, fields, err := CLI{}.resolveFlowStepText(t.Context(), flow.Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	if got != "12" {
		t.Fatalf("expected the value behind the chosen key, got %q", got)
	}
	if fields["input"] != "cantidad" || fields["text_from"] != "chosen" {
		t.Fatalf("the chosen input must reach the record: %+v", fields)
	}
}

func TestTextAskNeverTypesWhatTheModelSays(t *testing.T) {
	// The answer is read as a NAME. Free text - which is what an injected UI
	// label would try to produce - is an abstention, not something to type.
	fakeJev(t, "DROP TABLE cajas", 10)
	flow := inputsFlow(t, `type: { where: { id: qty }, text: { ask: "lo que toca escribir aquí" } }`)
	got, _, err := CLI{}.resolveFlowStepText(t.Context(), flow.Steps[0])
	if err == nil || err.Error() != "text_abstained" {
		t.Fatalf("free text must abstain, got %q err=%v", got, err)
	}
	if got != "" {
		t.Fatalf("nothing the model wrote may survive as text, got %q", got)
	}
}

func TestTextAskAbstentionStops(t *testing.T) {
	fakeJev(t, "none", 10)
	flow := inputsFlow(t, `type: { where: { id: qty }, text: { ask: "lo que toca escribir aquí" } }`)
	_, _, err := CLI{}.resolveFlowStepText(t.Context(), flow.Steps[0])
	if err == nil || err.Error() != "text_abstained" {
		t.Fatalf("expected text_abstained, got %v", err)
	}
}

func TestTextAskWithOneDeclaredInputAsksNobody(t *testing.T) {
	// One input is not a question, so it must resolve with the refusals on.
	t.Setenv("CI", "1")
	flow, err := ParseFlow([]byte(
		"name: f\ninputs:\n  nombre: \"Caja\"\nsteps:\n  - type: { where: { id: name }, text: { ask: \"lo que toca\" } }\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, fields, err := CLI{}.resolveFlowStepText(t.Context(), flow.Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	if got != "Caja" || fields["text_from"] != "only_input" {
		t.Fatalf("a single declared input resolves with no call: %q %+v", got, fields)
	}
}

func TestLiteralTextStillWorks(t *testing.T) {
	flow, err := ParseFlow([]byte("name: f\nsteps:\n  - type: { text: \"hola\", where: { id: name } }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if flow.Steps[0].Text != nil {
		t.Fatal("a literal is not a declared source")
	}
	got, _, err := CLI{}.resolveFlowStepText(t.Context(), flow.Steps[0])
	if err != nil || got != "hola" {
		t.Fatalf("literal text must survive untouched: %q %v", got, err)
	}
}

func TestTextSourceMustNameExactlyOneThing(t *testing.T) {
	if _, err := ParseFlow([]byte("name: f\nsteps:\n  - type: { text: { from: a, ask: b } }\n")); err == nil {
		t.Fatal("from and ask together must be rejected")
	}
	if _, err := ParseFlow([]byte("name: f\nsteps:\n  - type: { text: {} }\n")); err == nil {
		t.Fatal("an empty text source must be rejected")
	}
}

func TestTextFromMustNameADeclaredInput(t *testing.T) {
	_, err := LoadFlowFromBytes(t, "name: f\ninputs:\n  nombre: x\nsteps:\n  - type: { text: { from: apellido } }\n")
	if err == nil {
		t.Fatal("a from naming an undeclared input must fail at load time")
	}
}

// LoadFlowFromBytes parses and validates, which is what LoadFlow does to a
// file. The from/ask checks live in validation, not in parsing.
func LoadFlowFromBytes(t *testing.T, body string) (Flow, error) {
	t.Helper()
	flow, err := ParseFlow([]byte(body))
	if err != nil {
		return Flow{}, err
	}
	return flow, validateFlowSteps(flow.Steps)
}
