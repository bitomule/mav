package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func TestGoalStaleRecoveryReverifiesBeforeActing(t *testing.T) {
	f := &goalFixture{o: goalStart()}
	f.onAct = func(f *goalFixture, _ GoalObservation, _ GoalOperation) GoalReceipt {
		f.o.Elements = append([]Element{}, f.o.Elements...)
		f.o.Elements[0].Label = "Details"
		return GoalReceipt{Delivery: "not_sent", Stale: true}
	}
	r := runGoalFixture(t, f, `title:"Details"`)
	if f.actions != 1 || len(r.Steps) != 0 || r.Succeeded != "true" {
		t.Fatalf("actions=%d steps=%d result=%+v", f.actions, len(r.Steps), r)
	}
}

func TestGoalModalDoesNotConsumeNavigationBudget(t *testing.T) {
	f := &goalFixture{o: goalStart()}
	f.o.Elements = append(f.o.Elements, Element{Role: "alert", Label: "Permission"}, Element{Role: "button", Label: "Cancel", Frame: "0,50,100,40"})
	f.onAct = func(f *goalFixture, _ GoalObservation, op GoalOperation) GoalReceipt {
		if op.Target.Label == "Cancel" {
			f.o.Elements = append([]Element{}, f.o.Elements[:2]...)
		} else {
			f.o.Elements = []Element{{Role: "heading", Label: "Details"}}
		}
		return GoalReceipt{Delivery: "completed"}
	}
	r := (GoalEngine{Wait: func(context.Context) error { return nil }, Observer: f, Planner: f, Executor: f, Verifier: goalCondition(t, `title:"Details"`), Capabilities: map[string]bool{"tap": true}}).Run(t.Context(), GoalRequest{Goal: "open Details", Mode: "navigation", MaxSteps: 1, DismissPermission: "Cancel"})
	if r.Succeeded != "true" || f.actions != 2 {
		t.Fatalf("actions=%d result=%+v", f.actions, r)
	}
}

func TestGoalNavigationKeepsUnchangedPolicyDespiteGeometryJitter(t *testing.T) {
	f := &goalFixture{o: goalStart()}
	f.onAct = func(f *goalFixture, _ GoalObservation, _ GoalOperation) GoalReceipt {
		f.o.Elements = append([]Element{}, f.o.Elements...)
		f.o.Elements[1].Frame = fmt.Sprintf("0,%d,100,40", f.actions)
		return GoalReceipt{Delivery: "completed"}
	}
	r := runGoalFixture(t, f, `title:"Details"`)
	if f.actions != gotoMaxUnchanged || r.Outcome != "stuck" {
		t.Fatalf("geometry-only changes counted as progress: %+v actions=%d", r, f.actions)
	}
}

type goalReferenceProbe struct{ drivers.Driver }

func (goalReferenceProbe) ID() string { return "refs" }
func (goalReferenceProbe) Tree(context.Context, drivers.Target, drivers.TreeSpec) (drivers.TreeResult, error) {
	return drivers.TreeResult{JSON: []byte(`[{"identifier":"content","role":"AXScrollArea","role_description":"Group","type":"ScrollArea","frame":"0,0,300,700"}]`)}, nil
}
func (goalReferenceProbe) ReferenceSnapshot(context.Context, drivers.Target) (drivers.ReferenceSnapshot, error) {
	return drivers.ReferenceSnapshot{ID: "s1", PID: 101, Context: []byte(`{"screenID":1}`), Elements: []drivers.ReferenceElement{{Ref: "e6", Action: "pinchWithin", Role: "scroll-view", ID: "content"}, {Ref: "e6", Action: "swipe", Role: "scroll-view", ID: "content"}}}, nil
}
func (goalReferenceProbe) ExecuteReference(context.Context, drivers.Target, drivers.ReferenceSnapshot, drivers.ReferenceOperation) (drivers.ReferenceResult, error) {
	panic("unused")
}
func TestGoalSameReferenceInTargetsAndScrollRemainsUsable(t *testing.T) {
	a := goalDriverAdapter{driver: goalReferenceProbe{}, target: drivers.Target{Kind: drivers.KindSim}}
	o, e := a.Observe(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	ops := GoalOperations(GoalRequest{Mode: "navigation"}, o, map[string]bool{"scroll": true})
	if len(ops) != 2 || o.Refs[0] != "e6" {
		t.Fatalf("refs=%v operations=%v", o.Refs, ops)
	}
}

type goalSwitchDriver struct {
	portableTapDriver
	reads int
}

func (*goalSwitchDriver) Provides(drivers.Target) drivers.CapabilitySet {
	return drivers.NewSet(drivers.CapTreeAX, drivers.CapCoordTap)
}
func (d *goalSwitchDriver) Tree(context.Context, drivers.Target, drivers.TreeSpec) (drivers.TreeResult, error) {
	d.reads++
	els := []Element{{Role: "application", ID: "original", Label: "Original"}, {Role: "heading", Label: "Home"}, {ID: "open", Role: "button", Label: "Details", Frame: "0,0,100,40"}}
	if d.reads >= 4 {
		els = []Element{{Role: "application", ID: "other", Label: "Other"}, {Role: "heading", Label: "Details"}}
	}
	b, _ := json.Marshal(els)
	return drivers.TreeResult{JSON: b}, nil
}
func TestGoalInferenceCannotSucceedInDifferentApp(t *testing.T) {
	fakeJev(t, "1", 20)
	d := &goalSwitchDriver{}
	reg := drivers.NewRegistry()
	reg.Register(d)
	c := CLI{driverSession: &driverSession{registry: reg}}
	r := c.runGoal(t.Context(), GlobalOptions{PreferDriver: "portable"}, Config{}, GoalRequest{Goal: "open Details", Mode: "navigation", MaxSteps: 1}, SuccessCondition{})
	if r.Succeeded == "true" {
		t.Fatalf("foreign app accepted: reads=%d result=%+v", d.reads, r)
	}
}

type goalNamingProbe struct {
	*goalFixture
	named    int
	deadline bool
}

func (n *goalNamingProbe) NameDestination(ctx context.Context, _ string, _ Route, _ []ObservedScreen) (SuccessCondition, GoalDecision) {
	n.named++
	_, n.deadline = ctx.Deadline()
	c, _ := ParseSuccessCondition(`title:"Details"`)
	return c, GoalDecision{ModelCalls: 1}
}
func TestGoalInferenceUsesOriginalContextAndDeadline(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		f := &goalFixture{o: goalStart()}
		f.onAct = func(f *goalFixture, _ GoalObservation, _ GoalOperation) GoalReceipt {
			f.o.Elements = append([]Element{}, f.o.Elements...)
			f.o.Elements[0].Label = "Details"
			return GoalReceipt{Delivery: "completed"}
		}
		if foreign {
			f.onRead = func(f *goalFixture) error {
				if f.reads >= 4 {
					f.o.Context = "other:202"
				}
				return nil
			}
		}
		n := &goalNamingProbe{goalFixture: f}
		r := (GoalEngine{Wait: func(context.Context) error { return nil }, Observer: f, Planner: n, Executor: f, Verifier: SuccessCondition{}, Capabilities: map[string]bool{"tap": true}}).Run(t.Context(), GoalRequest{Goal: "open Details", Mode: "navigation", MaxSteps: 1})
		if n.named != 1 || !n.deadline || (r.Succeeded == "true") == foreign {
			t.Fatalf("foreign=%t result=%+v named=%d deadline=%t", foreign, r, n.named, n.deadline)
		}
	}
}
