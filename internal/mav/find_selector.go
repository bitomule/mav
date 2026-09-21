package mav

import (
	"context"
	"fmt"
)

// The `find` selector: `--find "the first category"` on the command line,
// `where: { find: "..." }` in a flow. One field on Selector, read from the one
// place both sides already read every other predicate from.
//
// Nothing here re-implements `mav ui find`. The ladder is the same one, called
// as a function: FindCandidates picks what could be meant, FindBatch bounds it,
// RenderFindCandidates writes it, FindQuestion and FindOptions ask it,
// InterpretFindAnswer reads the answer and VetoChoice can still take the yes
// away. What this file adds is the composition rule and the failure.

// findSelectorError is a find that did not produce an element. The code on the
// wire is the distinction the caller has to act on:
//
//   - find_abstained: the screen was read and asked about, and no element came
//     back. The model answered `none`, or a veto removed its answer. This is a
//     real answer and it CUTS - there is no second attempt with other words, no
//     second model, no ranking, and no confidence anywhere that could be
//     lowered to make it go through.
//   - find_unavailable: the question was never put - no key, no network, or the
//     refusal that keeps this out of CI. Nothing was judged.
type findSelectorError struct {
	code   string
	reason string
	next   string
}

func (e *findSelectorError) Error() string { return e.code }

// Fields carries the why onto the step record, since the code alone says only
// which of the two halves it was.
func (e *findSelectorError) Fields() map[string]string {
	fields := map[string]string{}
	if e.reason != "" {
		fields["find_reason"] = e.reason
	}
	if e.next != "" {
		fields["next"] = e.next
	}
	return fields
}

func newFindSelectorError(result FindResult) *findSelectorError {
	code := "find_abstained"
	switch result.Reason {
	case ReasonNoKey, ReasonNoNetwork, ReasonCIRefused, ReasonNoCandidates:
		code = "find_unavailable"
	}
	return &findSelectorError{code: code, reason: result.Reason, next: result.Next}
}

// resolveFindElement answers a selector carrying words. The structural
// predicates run first and cut the tree down; the words only ever choose among
// what survived.
//
// Trimming is done against the WHOLE tree rather than against a pre-filtered
// slice, because `near` and `parentOf` are relationships and a selector that
// has lost its neighbours cannot evaluate them.
func (c CLI) resolveFindElement(ctx context.Context, elements []Element, selector Selector) (Element, FindResult, error) {
	pool := elements
	if structural := selector.Structural(); !structural.IsZero() {
		matched, err := MatchElements(elements, structural)
		if err != nil {
			return Element{}, FindResult{}, err
		}
		if len(matched) == 0 {
			return Element{}, FindResult{}, fmt.Errorf("selector_not_found")
		}
		pool = matched
	}
	result := c.resolveFind(ctx, pool, selector.Find)
	if result.Element == nil {
		return Element{}, result, newFindSelectorError(result)
	}
	return *result.Element, result, nil
}
