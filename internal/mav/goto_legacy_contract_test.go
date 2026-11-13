package mav

import (
	"os"
	"strings"
	"testing"
)

// These tests preserve the frozen goto baseline; they do not exercise the public goal command.
func TestLegacyGotoFlowStepVerdictFailsUnverified(t *testing.T) {
	if err := gotoFlowStepVerdict("true"); err != nil {
		t.Fatalf("arrived=true must pass, got %v", err)
	}
	err := gotoFlowStepVerdict("unverified")
	if err == nil {
		t.Fatal("arrived=unverified must fail the step")
	}
	if err.Error() != "goto_arrival_unverified" {
		t.Fatalf("code=%q", err.Error())
	}
	if err := gotoFlowStepVerdict("false"); err == nil || err.Error() != "goto_did_not_arrive" {
		t.Fatalf("err=%v", err)
	}
}

// Why the second belt is not made redundant by the action-goal gate.
//
// The gate that stops `goto` claiming it did a THING is asked only when the
// caller named no criterion -- a caller who wrote one has already said what
// arriving means. Inside a flow a criterion is mandatory, so the goal is never
// classified there and that gate never fires. Whatever guards a flow against a
// false arrival, it is not that gate.
//
// Measured, 5 runs out of 5 on the fixture app: a flow whose goto asks for an action and
// demands the effect dies on `goto_did_not_arrive`, never on the action gate.
func TestLegacyGotoActionGoalGateIsNeverReachedWithACriterion(t *testing.T) {
	src, err := os.ReadFile("goto_legacy_test.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)

	guard := strings.Index(body, "if !criterion.IsZero() {")
	if guard < 0 {
		t.Fatal("the criterion branch is gone; this guard needs rewriting, not deleting")
	}
	call := strings.Index(body, "result.GoalKind = c.classifyGoal")
	if call < 0 {
		t.Fatal("the goal is no longer classified; this guard needs rewriting")
	}
	if call < guard {
		t.Fatal("the goal is classified before the criterion is looked at; a flow's goal would be classified too")
	}
	between := body[guard:call]
	if !strings.Contains(between, "} else {") {
		t.Error("classifyGoal is no longer in the else of `!criterion.IsZero()`; with a criterion written -- which a flow always has -- the goal must not be classified at all")
	}
	if strings.Count(between, "\n\tif ") != 0 {
		t.Error("another branch has appeared between the criterion check and classifyGoal; this guard can no longer tell which one reaches it")
	}
}

// The other half of the same argument, and the reason the live measurement of
// the second belt had to be an ambiguous criterion rather than an unreachable
// one: with a criterion written, the ONLY road left to `unverified` is the
// guard that refuses a criterion already holding where the run starts. Every
// other one is behind "no criterion was given".
//
// If a new unverified appears outside both, the belt stops being fully measured
// and nobody would notice.
func TestLegacyGotoEveryUnverifiedIsEitherUncriterionedOrTheAmbiguousGuard(t *testing.T) {
	src, err := os.ReadFile("goto_legacy_test.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	const assign = `result.Arrived = "unverified"`

	found := 0
	for at := 0; ; {
		i := strings.Index(body[at:], assign)
		if i < 0 {
			break
		}
		i += at
		at = i + len(assign)
		found++

		lookback := i - 600
		if lookback < 0 {
			lookback = 0
		}
		context := body[lookback:i]
		// CriterionObserved counts as "no criterion was given": it is the
		// source set when goto had to name the destination itself, which it
		// only ever does after finding the criterion empty.
		guarded := strings.Contains(context, "criterion.IsZero()") ||
			strings.Contains(context, "GotoAmbiguousCriterion") ||
			strings.Contains(context, "CriterionSource == CriterionObserved")
		if !guarded {
			t.Errorf("an unverified at offset %d is reachable with a criterion written and is not the ambiguous-criterion guard; a flow can now reach an unverified the belt was never measured against", i)
		}
	}
	if found == 0 {
		t.Fatal("no unverified assignments found; this guard needs rewriting")
	}
}
