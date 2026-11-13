package mav

import (
	"context"
	"strings"
	"testing"
)

func TestGoalPlanningContextExplainsAppendCurrentValueAndPendingSave(t *testing.T) {
	field := Element{ID: "note", Label: "Note text", Role: "text field", Focused: "true", Value: "Sample"}
	op := GoalOperation{Kind: "type", Target: &field, Input: "note", Text: "Sample"}
	req := GoalRequest{Goal: "Create the declared note and save it", Mode: "action", Inputs: map[string]string{"note": "Sample"}, VerificationHint: `title:"Saved"`}
	history := []GoalStep{{Operation: op, Receipt: GoalReceipt{Delivery: "completed"}, Changed: true}}
	question, menu, _ := goalPlanningPrompt(req, GoalObservation{Elements: []Element{field}}, []GoalOperation{op}, history)
	for _, want := range []string{"inserts without clearing", `current value="Sample"`, "recent: type Note text Sample note input=note"} {
		if !strings.Contains(menu, want) {
			t.Errorf("planner cannot distinguish already-written text from replacement: missing %q in %s", want, menu)
		}
	}
	if !strings.Contains(question, `title:"Saved"`) || !strings.Contains(question, "not yet verified") {
		t.Fatalf("pending completion condition hidden from planner: %s", question)
	}
	// The input value itself is not sent merely because it was declared.
	req.Inputs["note"] = "synthetic-unwritten-secret"
	question, menu, _ = goalPlanningPrompt(req, GoalObservation{}, []GoalOperation{op}, nil)
	if strings.Contains(question+menu, "synthetic-unwritten-secret") {
		t.Fatal("planner received a declared value instead of choosing its name")
	}
}

type goalHintProbe struct {
	goalFixture
	hint string
}

func (p *goalHintProbe) Plan(_ context.Context, req GoalRequest, _ GoalObservation, _ []GoalOperation, _ []GoalStep) (GoalDecision, error) {
	p.hint = req.VerificationHint
	return GoalDecision{ReportedSuccess: true}, nil
}

func TestGoalPlanningHintComesFromVerifierAndCannotProveCompletion(t *testing.T) {
	for _, tc := range []struct{ spec, hint string }{
		{`title:"Saved"`, `title:"Saved"`}, {"", ""},
		{`value:"note=synthetic-unwritten-secret"`, `value:"note=<caller-declared expected value>"`},
	} {
		spec := tc.spec
		p := &goalHintProbe{goalFixture: goalFixture{o: goalStart()}}
		e := GoalEngine{Wait: func(context.Context) error { return nil }, Observer: p, Planner: p, Executor: p, Verifier: goalCondition(t, spec), Capabilities: map[string]bool{"tap": true}}
		r := e.Run(t.Context(), GoalRequest{Goal: "save", Mode: "action", VerificationHint: "invented caller hint"})
		if p.hint != tc.hint || r.Succeeded == "true" || !r.ReportedSuccess || p.actions != 0 {
			t.Fatalf("hint=%q spec=%q result=%+v actions=%d", p.hint, spec, r, p.actions)
		}
	}
}
