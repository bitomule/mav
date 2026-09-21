package mav

import (
	"context"
	"fmt"
	"strings"
)

// `verify: { ask: "is the box on screen empty?" }` is for judgements of
// CONTENT that a selector cannot express. It reads the screen, puts the
// question, and records the verdict. That is all it does.
//
// WHAT IT MAY NEVER DO, and the reason this is a separate file with a test
// aimed at it: verify feeds no decision about progress and none about arrival.
// Handing a model the screen before and after and asking whether it changed is
// a judge with correlated errors - same model, same tree - and it is a solved
// problem in code: screenFingerprint is the sorted identity of (id, label,
// role), and it exists precisely because comparing frames reported changed
// on a gesture that moved nothing, and counting nodes reported unchanged on a
// screen that had entirely changed (80 before, 80 after).
//
// So: whether the screen changed, and whether a flow got where it was going,
// stay with screenFingerprint and with the selector. verify answers questions
// about what is on the screen, to whoever reads the run.

const (
	verifyYes     = "yes"
	verifyNo      = "no"
	verifyUnclear = "unclear"
)

// VerifyQuestion is the wording. Like find's, it makes declining a correct
// answer rather than a failure to be avoided.
func VerifyQuestion(ask string) string {
	return untrustedTextPreamble +
		"Below is what is currently on one screen of an iOS app, one element per line.\n" +
		"Answer this question about it: " + ask + "\n\n" +
		"Answer `yes` or `no`. Answer `unclear` if the screen does not settle the question; " +
		"that is a correct answer, and guessing is not."
}

// RenderScreenForVerify writes the screen as text. Everything carrying words
// goes in, not just what can be tapped: a question about content is usually a
// question about a label nobody can press.
func RenderScreenForVerify(elements []Element) string {
	var b strings.Builder
	for _, el := range elements {
		line := findElementText(el)
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(line)
		if el.Role != "" {
			b.WriteString(" [" + el.Role + "]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// runVerifyStep reads the screen and asks. It returns an error only when the
// question could not be put at all - never because of the answer, which is a
// record and not a gate.
func (c CLI) runVerifyStep(ctx context.Context, elements []Element, ask string) (map[string]string, error) {
	fields := map[string]string{"ask": ask}
	if strings.TrimSpace(ask) == "" {
		return fields, fmt.Errorf("verify_ask_missing")
	}
	if reason := findIsRefusedHere(); reason != "" {
		fields["find_reason"] = reason
		return fields, fmt.Errorf("verify_unavailable")
	}
	key, keySource, ok := ResolveJevKey()
	if !ok {
		fields["find_reason"] = ReasonNoKey
		fields["next"] = MissingJevKeyNext()
		return fields, fmt.Errorf("verify_unavailable")
	}
	fields["key_source"] = string(keySource)

	answer, err := askJevChoice(ctx, key, VerifyQuestion(ask),
		RenderScreenForVerify(elements), []string{verifyYes, verifyNo, verifyUnclear})
	if err != nil {
		fields["find_reason"] = ReasonNoNetwork
		return fields, fmt.Errorf("verify_unavailable")
	}
	fields["model_ms"] = fmt.Sprint(answer.LatencyMS)
	fields["verdict"] = normalizeVerifyVerdict(answer.Label)
	return fields, nil
}

func normalizeVerifyVerdict(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case verifyYes:
		return verifyYes
	case verifyNo:
		return verifyNo
	default:
		return verifyUnclear
	}
}
