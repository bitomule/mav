package mav

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type goalFixture struct {
	o                     GoalObservation
	reads, actions, plans int
	onRead                func(*goalFixture) error
	onPlan                func(*goalFixture, []GoalOperation) GoalDecision
	onAct                 func(*goalFixture, GoalObservation, GoalOperation) GoalReceipt
}

func (f *goalFixture) Observe(context.Context) (GoalObservation, error) {
	f.reads++
	if f.onRead != nil {
		if e := f.onRead(f); e != nil {
			return GoalObservation{}, e
		}
	}
	return f.o, nil
}
func (f *goalFixture) Plan(_ context.Context, _ GoalRequest, _ GoalObservation, m []GoalOperation, _ []GoalStep) (GoalDecision, error) {
	f.plans++
	if f.onPlan != nil {
		return f.onPlan(f, m), nil
	}
	return GoalDecision{Choice: 1, ModelCalls: 1}, nil
}
func (f *goalFixture) Execute(_ context.Context, o GoalObservation, op GoalOperation) GoalReceipt {
	f.actions++
	if f.onAct != nil {
		return f.onAct(f, o, op)
	}
	return GoalReceipt{Delivery: "completed"}
}
func goalStart() GoalObservation {
	return GoalObservation{Driver: "fixture", Target: "sim:test", Context: "app:101", Elements: []Element{{Role: "heading", Label: "Home"}, {ID: "open", Role: "button", Label: "Details", Frame: "0,0,100,40"}}}
}
func goalCondition(t *testing.T, s string) SuccessCondition {
	t.Helper()
	c, e := ParseSuccessCondition(s)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func runGoalFixture(t *testing.T, f *goalFixture, spec string) GoalResult {
	t.Helper()
	return (GoalEngine{Wait: func(context.Context) error { return nil }, Observer: f, Planner: f, Executor: f, Verifier: goalCondition(t, spec), Capabilities: map[string]bool{"tap": true, "type": true, "scroll": true}}).Run(t.Context(), GoalRequest{Goal: "open Details", Mode: "navigation", MaxSteps: 12})
}

func TestGoalVerifiesAndAlreadySatisfiedDoesNotAct(t *testing.T) {
	f := &goalFixture{o: goalStart()}
	r := runGoalFixture(t, f, `title:"Home"`)
	if r.Outcome != "already_satisfied" || r.Succeeded != "true" || f.actions != 0 || f.plans != 0 || f.reads != 2 {
		t.Fatalf("result=%+v fixture=%+v", r, f)
	}
	f = &goalFixture{o: goalStart(), onAct: func(f *goalFixture, _ GoalObservation, _ GoalOperation) GoalReceipt {
		f.o.Elements = []Element{{Role: "heading", Label: "Details"}}
		return GoalReceipt{Delivery: "completed"}
	}}
	r = runGoalFixture(t, f, `title:"Details"`)
	if r.Succeeded != "true" || r.Outcome != "succeeded" || f.actions != 1 || f.reads != 4 || r.Metrics.Reads != f.reads {
		t.Fatalf("%+v reads=%d", r, f.reads)
	}
}

func TestGoalDiscardsDecisionWhenGeometryFocusOrValueChanges(t *testing.T) {
	for _, mutation := range []string{"frame", "value", "focus", "enabled", "selected"} {
		t.Run(mutation, func(t *testing.T) {
			f := &goalFixture{o: goalStart()}
			f.onPlan = func(f *goalFixture, _ []GoalOperation) GoalDecision {
				if f.plans == 1 {
					e := f.o.Elements[1]
					switch mutation {
					case "frame":
						e.Frame = "100,200,100,40"
					case "value":
						e.Value = "different"
					case "focus":
						e.Focused = "true"
					case "enabled":
						e.Enabled = "false"
					case "selected":
						e.Selected = "true"
					}
					f.o.Elements = append([]Element{f.o.Elements[0]}, e)
					return GoalDecision{Choice: 1}
				}
				return GoalDecision{ReportedSuccess: true}
			}
			r := runGoalFixture(t, f, `title:"Details"`)
			if f.actions != 0 || r.Metrics.Discarded != 1 || r.Succeeded == "true" {
				t.Fatalf("stale decision acted: %+v actions=%d", r, f.actions)
			}
		})
	}
}

func TestGoalModelDoneCannotVerifyAndNoCriterionStaysUnverified(t *testing.T) {
	for _, criterion := range []string{`title:"Details"`, ""} {
		f := &goalFixture{o: goalStart(), onPlan: func(*goalFixture, []GoalOperation) GoalDecision {
			return GoalDecision{ReportedSuccess: true, ModelCalls: 1}
		}}
		r := runGoalFixture(t, f, criterion)
		if r.Succeeded == "true" || !r.ReportedSuccess || r.Outcome != "unverified" || f.actions != 0 {
			t.Fatalf("%+v", r)
		}
	}
}

func TestGoalNeverRepeatsPartialUnknownOrPostInputReadFailure(t *testing.T) {
	for _, delivery := range []string{"partial", "unknown", "completed"} {
		t.Run(delivery, func(t *testing.T) {
			f := &goalFixture{o: goalStart(), onAct: func(*goalFixture, GoalObservation, GoalOperation) GoalReceipt { return GoalReceipt{Delivery: delivery} }}
			if delivery == "completed" {
				f.onRead = func(f *goalFixture) error {
					if f.actions > 0 {
						return errors.New("capture failed")
					}
					return nil
				}
			}
			r := runGoalFixture(t, f, `title:"Details"`)
			if r.Outcome != "uncertain" || f.actions != 1 || len(r.Steps) != 1 || r.Steps[0].Receipt.Delivery != delivery {
				t.Fatalf("%+v actions=%d", r, f.actions)
			}
		})
	}
}

func TestGoalRejectsForegroundChangeBeforeAndAfterInput(t *testing.T) {
	for _, after := range []bool{false, true} {
		f := &goalFixture{o: goalStart()}
		f.onRead = func(f *goalFixture) error {
			if (!after && f.reads == 2) || (after && f.actions > 0) {
				f.o.Context = "other-app:202"
			}
			return nil
		}
		r := runGoalFixture(t, f, `title:"Details"`)
		if r.Outcome != "blocked" || r.Succeeded == "true" || (!after && f.actions != 0) || (after && f.actions != 1) {
			t.Fatalf("after=%t %+v actions=%d", after, r, f.actions)
		}
	}
}

func TestGoalSettlingFailureDoesNotReuseSuccessfulTree(t *testing.T) {
	f := &goalFixture{o: goalStart(), onRead: func(f *goalFixture) error {
		if f.reads == 2 {
			return errors.New("read failed")
		}
		return nil
	}}
	r := runGoalFixture(t, f, `title:"Home"`)
	if r.Outcome != "uncertain" || r.Succeeded == "true" || f.actions != 0 {
		t.Fatalf("%+v", r)
	}
	f = &goalFixture{o: goalStart(), onRead: func(f *goalFixture) error {
		if f.reads > 1 {
			f.o.Elements = append([]Element{}, f.o.Elements...)
			f.o.Elements[0].Value = fmt.Sprint(f.reads)
		}
		return nil
	}, onPlan: func(*goalFixture, []GoalOperation) GoalDecision { return GoalDecision{ReportedSuccess: true} }}
	r = runGoalFixture(t, f, `title:"Home"`)
	if r.Succeeded == "true" {
		t.Fatalf("unstable successful tree passed: %+v", r)
	}
}

func TestGoalKeepsAbstentionLoopStuckModalAndDestructivePolicies(t *testing.T) {
	for _, scenario := range []string{"abstain", "stuck", "loop", "modal", "destructive", "offmenu"} {
		t.Run(scenario, func(t *testing.T) {
			f := &goalFixture{o: goalStart()}
			want := ""
			switch scenario {
			case "abstain":
				want = "no_route"
				f.onPlan = func(*goalFixture, []GoalOperation) GoalDecision { return GoalDecision{} }
			case "stuck":
				want = "stuck"
			case "loop":
				want = "looping"
				f.onAct = func(f *goalFixture, _ GoalObservation, _ GoalOperation) GoalReceipt {
					label := "Home"
					if f.actions%2 == 1 {
						label = "Intermediate"
					}
					f.o.Elements = append([]Element{}, f.o.Elements...)
					f.o.Elements[0].Label = label
					return GoalReceipt{Delivery: "completed"}
				}
			case "modal":
				want = "blocked"
				f.o.Elements = append(f.o.Elements, Element{Role: "alert", Label: "Allow access?"})
			case "destructive":
				want = "blocked"
				f.o.Elements[1].Label = "Delete account" // absent from menu
			case "offmenu":
				want = "blocked"
				f.onPlan = func(*goalFixture, []GoalOperation) GoalDecision { return GoalDecision{Choice: 999} }
			}
			r := runGoalFixture(t, f, `title:"Details"`)
			if r.Outcome != want || r.Succeeded == "true" {
				t.Fatalf("want=%s %+v", want, r)
			}
			if (scenario == "modal" || scenario == "destructive" || scenario == "offmenu") && f.actions != 0 {
				t.Fatal("unsafe input")
			}
		})
	}
}

func TestGoalMenuUsesDeclaredInputsAndRestrictsNavigation(t *testing.T) {
	o := goalStart()
	o.Elements = append(o.Elements, Element{ID: "name", Role: "textfield", Label: "Name", Focused: "true", Frame: "0,50,200,40"}, Element{ID: "list", Role: "scroll view", Frame: "0,0,300,700"})
	caps := map[string]bool{"tap": true, "type": true, "scroll": true}
	for _, mode := range []string{"action", "navigation"} {
		ops := GoalOperations(GoalRequest{Mode: mode, Inputs: map[string]string{"name": "Sample"}}, o, caps)
		typed, scrolled := false, false
		for _, op := range ops {
			if op.Kind == "type" {
				typed = true
				if op.Text != "Sample" || op.Input != "name" || op.Target.ID != "name" {
					t.Fatalf("%+v", op)
				}
			}
			if op.Kind == "scroll" {
				scrolled = true
			}
		}
		if typed != (mode == "action") || !scrolled {
			t.Fatalf("mode=%s %+v", mode, ops)
		}
	}
	if ops := GoalOperations(GoalRequest{Mode: "action"}, o, map[string]bool{}); len(ops) != 0 {
		t.Fatalf("unsupported operations offered: %+v", ops)
	}
	o.Elements[2].Focused = "false"
	for _, op := range GoalOperations(GoalRequest{Mode: "action", Inputs: map[string]string{"name": "Sample"}}, o, caps) {
		if op.Kind == "type" {
			t.Fatal("typing without focus")
		}
	}
}

func TestGoalArgumentsConditionsAndFlowValidation(t *testing.T) {
	r, c, e := parseGoalArgs([]string{`type "Sample" in Name`, "--succeeded-when", `value:"name=Sample"`, "--input", "other=Declared"})
	if e != nil || r.Inputs["literal_1"] != "Sample" || r.Inputs["other"] != "Declared" || !c.Declared() {
		t.Fatalf("%+v %+v %v", r, c, e)
	}
	o := goalStart()
	o.Elements = append(o.Elements, Element{ID: "name", Value: "Sample", Visible: "true"})
	if !c.Satisfied(o) {
		t.Fatal("exact value not matched")
	}
	o.Elements = append(o.Elements, o.Elements[2])
	if c.Satisfied(o) {
		t.Fatal("ambiguous field verified")
	}
	for _, args := range [][]string{{"x", "--arrived-when", "Home"}, {"x", "--max-steps", "13"}, {"x", "--timeout", "91s"}, {"x", "--mode", "unknown"}, {"x", "--succeeded-when", `value:"bad"`}, {"x", "--input", "a=1", "--input", "a=2"}} {
		if _, _, e := parseGoalArgs(args); e == nil {
			t.Fatalf("accepted %+v", args)
		}
	}
	for _, spec := range []string{`title:"Settings" text:"Saved"`, `screen:"details-view"`, `value:"name="`} {
		c := goalCondition(t, spec)
		if !c.Declared() || c.String() == "" {
			t.Fatalf("%+v", c)
		}
	}
	step := FlowStep{Action: "goal", Params: map[string]string{"goal": "open Details", "succeededWhen": `title:"Details"`}}
	if e := validateGoalFlowStep(0, step); e != nil {
		t.Fatal(e)
	}
	delete(step.Params, "succeededWhen")
	if e := validateGoalFlowStep(0, step); e == nil || !strings.Contains(e.Error(), "succeededWhen") {
		t.Fatalf("%v", e)
	}
}

func TestGoalFlowFailureNeverReplaysDeliveredInput(t *testing.T) {
	for _, delivery := range []string{"completed", "partial", "unknown", "not_sent"} {
		fields := map[string]string{}
		err := goalFlowFailure(GoalResult{Outcome: "uncertain", Steps: []GoalStep{{Receipt: GoalReceipt{Delivery: delivery}}}}, fields)
		if safe := inputRetrySafe(err) && fields["retry"] != "unsafe"; safe != (delivery == "not_sent") {
			t.Fatalf("delivery=%s err=%v fields=%v", delivery, err, fields)
		}
	}
}

func TestGoalCancellationDuringPlanningPreventsInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := &goalFixture{o: goalStart(), onPlan: func(*goalFixture, []GoalOperation) GoalDecision { cancel(); return GoalDecision{Choice: 1} }}
	r := (GoalEngine{Observer: f, Planner: f, Executor: f, Verifier: goalCondition(t, `title:"Details"`), Capabilities: map[string]bool{"tap": true}}).Run(ctx, GoalRequest{Goal: "open Details", Mode: "navigation"})
	if f.actions != 0 || r.Outcome != "timeout" {
		t.Fatalf("%+v actions=%d", r, f.actions)
	}
}

func TestGoalCancellationDuringPreflightPreventsInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := &goalFixture{o: goalStart(), onRead: func(f *goalFixture) error {
		if f.reads == 2 {
			cancel()
		}
		return nil
	}}
	r := (GoalEngine{Observer: f, Planner: f, Executor: f, Verifier: goalCondition(t, `title:"Details"`), Capabilities: map[string]bool{"tap": true}}).Run(ctx, GoalRequest{Goal: "open Details", Mode: "navigation"})
	if f.actions != 0 || r.Outcome != "timeout" {
		t.Fatalf("%+v actions=%d", r, f.actions)
	}
}

func TestGoalCancelledModelDoneReportsTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := &goalFixture{o: goalStart(), onPlan: func(*goalFixture, []GoalOperation) GoalDecision { cancel(); return GoalDecision{ReportedSuccess: true} }}
	r := (GoalEngine{Observer: f, Planner: f, Executor: f, Verifier: goalCondition(t, `title:"Details"`), Capabilities: map[string]bool{"tap": true}}).Run(ctx, GoalRequest{Goal: "open Details", Mode: "navigation"})
	if r.Outcome != "timeout" || f.actions != 0 {
		t.Fatalf("%+v actions=%d", r, f.actions)
	}
}

func TestGoalLiteralInputsIgnoreApostrophesAndNeverOverwriteDeclarations(t *testing.T) {
	values := goalLiteralInputs(`don't open Kate's note; type "Sample"`)
	if len(values) != 1 || values[0] != "Sample" {
		t.Fatal(values)
	}
	if _, _, err := parseGoalArgs([]string{`type "Sample"`, "--input", "literal_1=Other"}); err == nil {
		t.Fatal("literal overwrote declared input")
	}
}

func TestGoalHiddenTextAndHeadingCannotSatisfyCondition(t *testing.T) {
	for _, spec := range []string{`title:"Hidden completion"`, `text:"Hidden completion"`} {
		o := goalStart()
		o.Elements = append([]Element{{Role: "heading", Label: "Hidden completion", Visible: "false"}}, o.Elements...)
		if goalCondition(t, spec).Satisfied(o) {
			t.Fatalf("hidden content satisfied %s", spec)
		}
	}
}

func TestGoalTracksProviderCostWithoutInventingUnknownBilling(t *testing.T) {
	for _, known := range []bool{true, false} {
		cost := 0.00042
		f := &goalFixture{o: goalStart(), onPlan: func(*goalFixture, []GoalOperation) GoalDecision {
			d := GoalDecision{Choice: 1, ModelCalls: 1, ModelMS: 7}
			if known {
				d.CostUSD = &cost
			}
			return d
		}, onAct: func(f *goalFixture, _ GoalObservation, _ GoalOperation) GoalReceipt {
			f.o.Elements = []Element{{Role: "heading", Label: "Details"}}
			return GoalReceipt{Delivery: "completed"}
		}}
		r := runGoalFixture(t, f, `title:"Details"`)
		if r.Metrics.ModelCalls != 1 || r.Metrics.ModelMS != 7 || known && (r.Metrics.CostUSD == nil || *r.Metrics.CostUSD != cost) || !known && r.Metrics.CostUSD != nil {
			t.Fatalf("known=%t %+v", known, r.Metrics)
		}
	}
}

func TestGoalMenuRespectsNativeReferenceActionSet(t *testing.T) {
	o := goalStart()
	o.Refs = map[int]string{1: "e1"}
	o.RefActions = map[int]map[string]bool{1: {}}
	if ops := GoalOperations(GoalRequest{Mode: "action"}, o, map[string]bool{"tap": true}); len(ops) != 0 {
		t.Fatalf("offered unsupported native action: %+v", ops)
	}
	o.RefActions[1]["tap"] = true
	if ops := GoalOperations(GoalRequest{Mode: "action"}, o, map[string]bool{"tap": true}); len(ops) != 1 {
		t.Fatalf("lost supported native action: %+v", ops)
	}
}

func TestGoalNavigationScrollTracksViewportWithoutCountingGeometryJitter(t *testing.T) {
	f := &goalFixture{o: goalStart()}
	f.o.Elements = []Element{{Role: "heading", Label: "Home"}, {ID: "content", Role: "scroll-view", Frame: "0,0,300,700"}, {ID: "details", Label: "Details", Role: "button", Frame: "0,0,100,40"}, {ID: "a", Label: "Row A", Role: "button", Frame: "0,0,100,40"}, {ID: "b", Label: "Row B", Role: "button", Frame: "0,0,100,40"}, {ID: "c", Label: "Row C", Role: "button", Frame: "0,0,100,40"}}
	viewport := func(index int) {
		f.o.Refs = map[int]string{1: "scroll", index: "visible"}
		f.o.RefActions = map[int]map[string]bool{1: {"scroll": true}, index: {"tap": true}}
	}
	viewport(3)
	f.onPlan = func(_ *goalFixture, ops []GoalOperation) GoalDecision {
		for i, op := range ops {
			if op.Kind == "tap" && op.Target.ID == "details" {
				return GoalDecision{Choice: i + 1}
			}
		}
		for i, op := range ops {
			if op.Kind == "scroll" && op.Direction == "up" {
				return GoalDecision{Choice: i + 1}
			}
		}
		return GoalDecision{}
	}
	f.onAct = func(f *goalFixture, _ GoalObservation, op GoalOperation) GoalReceipt {
		if op.Kind == "scroll" {
			switch f.actions {
			case 1:
				viewport(4)
			case 2:
				viewport(5)
			case 3:
				viewport(2)
			}
		} else {
			f.o.Elements = append([]Element{}, f.o.Elements...)
			f.o.Elements[0].Label = "Details"
		}
		return GoalReceipt{Delivery: "completed"}
	}
	r := runGoalFixture(t, f, `title:"Details"`)
	if r.Succeeded != "true" || f.actions != 4 {
		t.Fatalf("viewport movement treated as no progress: %+v actions=%d", r, f.actions)
	}
}
