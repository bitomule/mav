package mav

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestVerifyRecordsTheVerdict(t *testing.T) {
	fakeJev(t, "no", 12)
	fields, err := CLI{}.runVerifyStep(t.Context(), boxesScreen(), "¿la caja está vacía?")
	if err != nil {
		t.Fatal(err)
	}
	if fields["verdict"] != verifyNo {
		t.Fatalf("the verdict has to reach the record: %+v", fields)
	}
}

func TestVerifyCannotDecideProgressOrArrival(t *testing.T) {
	// THE HARD RULE. verify answers questions about content. Whether the
	// screen changed, and whether the flow arrived, are decided in code by
	// screenFingerprint - a model asked "did this change?" is a judge with
	// errors correlated to the one that acted, and the code answer is both
	// free and better.
	//
	// Three things are checked, because the rule can be broken in three
	// places: in what verify returns, in what verify reads, and in what the
	// progress and arrival code reads.

	// 1. A verdict is a record, not a gate. `no` does not fail the step and
	//    does not produce any field an advance decision consumes.
	fakeJev(t, "no", 12)
	fields, err := CLI{}.runVerifyStep(t.Context(), boxesScreen(), "¿la caja está vacía?")
	if err != nil {
		t.Fatalf("a verdict must never fail a step: %v", err)
	}
	for _, forbidden := range []string{"changed", "verified", "arrived", "fingerprint", "outcome"} {
		if _, found := fields[forbidden]; found {
			t.Fatalf("verify must not produce %q, which is read as a decision: %+v", forbidden, fields)
		}
	}

	// 2. verify must not reach for the progress machinery itself.
	source, err := os.ReadFile("flow_verify.go")
	if err != nil {
		t.Fatal(err)
	}
	body := stripGoComments(string(source))
	for _, forbidden := range []string{"screenFingerprint", "verifyTapChangedSomething", "snapshotForVerification", "ArrivalCriterion"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("verify must not touch %s: judging change is not its job", forbidden)
		}
	}

	// 3. And the progress and arrival code must not reach for a model. This
	//    is the direction that would be easiest to break later and hardest to
	//    notice: screenFingerprint quietly gaining a question.
	cli, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"func screenFingerprint(", "func (c CLI) verifyTapChangedSomething("} {
		start := strings.Index(string(cli), fn)
		if start < 0 {
			t.Fatalf("%s not found; if it was renamed, this test has to follow it", fn)
		}
		end := strings.Index(string(cli)[start+len(fn):], "\nfunc ")
		if end < 0 {
			t.Fatalf("end of %s not found", fn)
		}
		fnBody := stripGoComments(string(cli)[start : start+len(fn)+end])
		for _, forbidden := range []string{"askJevChoice", "runVerifyStep", "VerifyQuestion", "resolveFind"} {
			if strings.Contains(fnBody, forbidden) {
				t.Fatalf("%s must decide in code, and it reached for %s", fn, forbidden)
			}
		}
	}
}

func TestVerifyIsAReadAndKeepsTheCachedScreen(t *testing.T) {
	if !isReadOnlyFlowAction("verify") {
		t.Fatal("verify only reads; dirtying the tree would charge it a 320 ms re-read it never earned")
	}
}

func TestVerifyRefusesToAskInCI(t *testing.T) {
	t.Setenv("CI", "1")
	fields, err := CLI{}.runVerifyStep(t.Context(), boxesScreen(), "¿la caja está vacía?")
	if err == nil || err.Error() != "verify_unavailable" {
		t.Fatalf("expected verify_unavailable, got %v", err)
	}
	if fields["verdict"] != "" {
		t.Fatalf("a question that was never put has no verdict: %+v", fields)
	}
}

func TestVerifyReadsAnUnexpectedAnswerAsUnclear(t *testing.T) {
	fakeJev(t, "maybe later", 12)
	fields, err := CLI{}.runVerifyStep(t.Context(), boxesScreen(), "¿la caja está vacía?")
	if err != nil {
		t.Fatal(err)
	}
	if fields["verdict"] != verifyUnclear {
		t.Fatalf("anything that is not yes or no is unclear, got %q", fields["verdict"])
	}
}

var goCommentPattern = regexp.MustCompile(`(?s)//[^\n]*|/\*.*?\*/`)

func stripGoComments(source string) string {
	return goCommentPattern.ReplaceAllString(source, "")
}
