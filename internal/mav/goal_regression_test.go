package mav

import (
	"strings"
	"testing"
)

// Both arms receive the same correct model choice. The UI moves that control
// during planning. This reproduces the old executor's defect without model
// randomness, a busy machine, timers or an artificial load.
func TestGoalResolvesGotoStaleCoordinateRegression(t *testing.T) {
	fakeJev(t, "1", 10)
	c, cfg, runner := guardPointConfig(t)
	before := `[{"role":"heading","label":"Home"},{"identifier":"open","role":"button","label":"Details","frame":"{{0, 0}, {100, 40}}"}]`
	after := `[{"role":"heading","label":"Home"},{"identifier":"open","role":"button","label":"Details","frame":"{{100, 200}, {100, 40}}"}]`
	runner.seq["axe --output json describe-ui --udid "+guardPointUDID] = []string{before, after, after, after}
	criterion, _ := ParseArrivalCriterion(`title:"Details"`)
	old, err := c.runGotoLoop(t.Context(), cfg, GlobalOptions{}, "open Details", criterion, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	staleTap := false
	for _, cmd := range runner.commands {
		if strings.Contains(cmd, "tap") && strings.Contains(cmd, "-x 50 -y 20") {
			staleTap = true
		}
	}
	if !staleTap || old.Arrived == "true" {
		t.Fatalf("baseline defect did not reproduce: %+v %v", old, runner.commands)
	}
	f := &goalFixture{o: goalStart()}
	f.onPlan = func(f *goalFixture, _ []GoalOperation) GoalDecision {
		if f.plans == 1 {
			f.o.Elements = append([]Element{}, f.o.Elements...)
			f.o.Elements[1].Frame = "100,200,100,40"
		}
		return GoalDecision{Choice: 1, ModelCalls: 1}
	}
	f.onAct = func(f *goalFixture, _ GoalObservation, op GoalOperation) GoalReceipt {
		if op.Target.Frame != "100,200,100,40" {
			t.Fatal("candidate dispatched stale coordinates")
		}
		f.o.Elements = []Element{{Role: "heading", Label: "Details"}}
		return GoalReceipt{Delivery: "completed"}
	}
	result := runGoalFixture(t, f, `title:"Details"`)
	if result.Succeeded != "true" || result.Metrics.Discarded != 1 || f.actions != 1 {
		t.Fatalf("candidate failed to resolve baseline regression: %+v actions=%d", result, f.actions)
	}
}
