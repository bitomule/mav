package mav

import (
	"context"
	"fmt"
	"strings"
)

// `verify: { ask: "is the box on screen empty?" }` is for judgements of
// CONTENT that a selector cannot express. It reads the screen, puts the
// question, and the answer decides whether the step passed.
//
// There are two distinctions here and they are easy to run together. Written
// out, because losing either of them is how this turns into something it must
// not be.
//
// FIRST: what verify is allowed to decide.
//
//   - ALLOWED - a judgement of content. "Is the box on screen empty?" has no
//     structural answer: there is nothing to select, nothing to count, nothing
//     in the tree that settles it. A verifier that cannot fail is not a
//     verifier, it is a log line, so a `no` fails the step.
//   - FORBIDDEN - whether the screen CHANGED, or whether the flow ARRIVED.
//     Asking a model that is a judge whose errors correlate with the thing
//     being judged, same model over the same tree, and it is already solved in
//     code: screenFingerprint is the sorted identity of (id, label, role), and
//     it exists because comparing frames reported changed on a gesture that
//     moved nothing, and counting nodes reported unchanged on a screen that
//     had entirely changed (80 nodes before, 80 after). Progress and arrival
//     stay with screenFingerprint and the selector, and nothing in this file
//     may reach for them.
//
// SECOND: "the model says no" is not "the model does not know", and only the
// first one is a failure.
//
//   - `no` fails the step, with its own code, verify_rejected.
//   - `unclear` does not. Declining is a correct answer and not a negative
//     verdict - the same rule find lives by, where the abstention is on the
//     menu precisely so the model has somewhere to put "I am not sure" other
//     than a wrong answer. A step that failed on `unclear` would punish the
//     honest answer and reward the guess.

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

// runVerifyStep reads the screen and asks. The verdict is always on the
// record; a `no` also fails the step, and an `unclear` does not.
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
	verdict := normalizeVerifyVerdict(answer.Label)
	fields["verdict"] = verdict
	if verdict == verifyNo {
		return fields, fmt.Errorf("verify_rejected")
	}
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
