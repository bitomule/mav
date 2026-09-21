package mav

import "testing"

// The whole shape, loaded as one document: words in the selector, declared
// inputs, a named one and a chosen one, a content judgement, and a structural
// assertion that is still the thing deciding whether the flow got there.
func TestWrittenStepFlowLoads(t *testing.T) {
	body := `name: abrir-caja
inputs:
  nombre: "Caja de herramientas"
  cantidad: "12"
steps:
  - tap: { where: { find: "la primera categoría" } }
  - tap: { where: { find: "la primera caja", role: "cell" } }
  - type: { where: { find: "el campo del nombre" }, text: { from: nombre } }
  - type: { where: { find: "el campo de cantidad" }, text: { ask: "lo que toca escribir aquí" } }
  - verify: { ask: "¿la caja que se ve abierta está vacía?" }
  - assert: { id: boxes-view }
`
	flow, err := LoadFlowFromBytes(t, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Steps) != 6 {
		t.Fatalf("got %d steps", len(flow.Steps))
	}
	if flow.Steps[1].Where.Find != "la primera caja" || flow.Steps[1].Where.Role != "cell" {
		t.Fatalf("the composed selector did not survive the load: %+v", flow.Steps[1].Where)
	}
	if flow.Steps[4].Params["ask"] == "" {
		t.Fatal("verify lost its question")
	}
	if flow.Steps[3].Inputs["cantidad"] != "12" {
		t.Fatal("the step did not carry the declared inputs")
	}
}
