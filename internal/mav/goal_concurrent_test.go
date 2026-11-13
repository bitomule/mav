package mav

import (
	"context"
	"sync/atomic"
	"testing"
)

type goalParallelProbe struct {
	reads    atomic.Int32
	planning chan struct{}
	actions  int
}

func (*goalParallelProbe) SafeDuringPlanning() bool { return true }
func (f *goalParallelProbe) Observe(ctx context.Context) (GoalObservation, error) {
	n := f.reads.Add(1)
	if n == 1 {
		return goalStart(), nil
	}
	if n == 2 {
		select {
		case <-f.planning:
		case <-ctx.Done():
			return GoalObservation{}, ctx.Err()
		}
	}
	o := goalStart()
	o.Elements = []Element{{Role: "heading", Label: "Details"}}
	return o, nil
}
func (f *goalParallelProbe) Plan(context.Context, GoalRequest, GoalObservation, []GoalOperation, []GoalStep) (GoalDecision, error) {
	close(f.planning)
	return GoalDecision{Choice: 1, ModelCalls: 1}, nil
}
func (f *goalParallelProbe) Execute(context.Context, GoalObservation, GoalOperation) GoalReceipt {
	f.actions++
	return GoalReceipt{Delivery: "completed"}
}
func TestGoalSafeConcurrentReadDiscardsAndVerifiesWithoutActing(t *testing.T) {
	f := &goalParallelProbe{planning: make(chan struct{})}
	r := (GoalEngine{Wait: func(context.Context) error { return nil }, Observer: f, Planner: f, Executor: f, Verifier: goalCondition(t, `title:"Details"`), Capabilities: map[string]bool{"tap": true}}).Run(t.Context(), GoalRequest{Goal: "open Details", Mode: "navigation"})
	if r.Succeeded != "true" || f.actions != 0 || r.Metrics.Discarded != 1 || r.Metrics.Reads != 3 {
		t.Fatalf("%+v actions=%d", r, f.actions)
	}
}
