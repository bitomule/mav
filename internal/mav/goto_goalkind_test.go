package mav

import (
	"os"
	"strings"
	"testing"
)

// The question is put before anything is read or tapped, and it has to stay
// that way in its wording too: the moment it mentions a journey, an outcome or
// a screen this run is standing on, it stops being a question about a sentence
// and becomes the self-assessment the whole design refuses.
func TestGoalKindQuestionAsksAboutTheSentenceOnly(t *testing.T) {
	q := strings.ToLower(GotoGoalKindQuestion())
	for _, leak := range []string{"arrive", "did you", "this run", "step", "you tapped", "succeed"} {
		if strings.Contains(q, leak) {
			t.Errorf("the goal-kind question leaks the journey (%q):\n%s", leak, q)
		}
	}
	if !strings.Contains(q, "place") || !strings.Contains(q, "action") {
		t.Errorf("the question does not name both options:\n%s", q)
	}
}

// Anything that is not one of the two options leaves the goal unclassified,
// and unclassified is exactly today's behaviour: nothing is withheld that used
// to be given.
func TestInterpretGoalKindReadsOnlyTheTwoOptions(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  string
		ok    bool
	}{
		{"place", GoalKindPlace, true},
		{"  ACTION ", GoalKindAction, true},
		{"none", "", false},
		{"", "", false},
		{"yes, it is an action", "", false},
	} {
		got, ok := InterpretGoalKind(tc.label)
		if got != tc.want || ok != tc.ok {
			t.Errorf("InterpretGoalKind(%q) = %q,%v want %q,%v", tc.label, got, ok, tc.want, tc.ok)
		}
	}
}

// There is no abstention on this menu on purpose: not answering and answering
// "I don't know" have the same consequence here, and a third option would only
// be a second spelling of the failure path.
func TestGoalKindOptionsCarryNoAbstention(t *testing.T) {
	opts := GotoGoalKindOptions()
	if len(opts) != 2 || opts[0] != GoalKindPlace || opts[1] != GoalKindAction {
		t.Fatalf("options = %v, want [place action]", opts)
	}
}

// The measured defect: goto opened the sheet on which a category is created and
// named that screen its destination, reporting arrived=true 20 runs out of 20
// with nothing created. The gate that stops it is an ORDERING inside finishGoto
// — the action goal returns before the destination is ever named — so the
// ordering is what is guarded. A refactor that moves the naming call above the
// gate reinstates the defect in silence, and this fails when it does.
func TestActionGoalsReturnBeforeTheDestinationIsNamed(t *testing.T) {
	src, err := os.ReadFile("goto_cmd.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func (c CLI) finishGoto(")
	if start < 0 {
		t.Fatal("finishGoto is gone; this guard needs rewriting, not deleting")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("cannot delimit finishGoto")
	}
	fn := body[start : start+end]

	gate := strings.Index(fn, "GoalKindAction")
	naming := strings.Index(fn, "nameObservedDestination")
	if gate < 0 {
		t.Fatal("finishGoto no longer checks for an action goal; an action goal can be reported as arrived again")
	}
	if naming < 0 {
		t.Fatal("finishGoto no longer names a destination; this guard needs rewriting")
	}
	if gate > naming {
		t.Error("the action gate runs AFTER the destination is named; an action goal can be reported as arrived again")
	}
}

// Arrival is asserted in exactly two places, and only one of them can be
// reached without the caller having written --arrived-when. If a third appears,
// the gate above stops being sufficient and nobody would notice.
func TestArrivalIsAssertedOnlyWhereItIsGated(t *testing.T) {
	src, err := os.ReadFile("goto_cmd.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), `result.Arrived = "true"`); n != 2 {
		t.Errorf("arrived=true is asserted in %d places, want 2 (the explicit-criterion loop check and finishGoto); "+
			"a new one is not covered by the action gate", n)
	}
}
