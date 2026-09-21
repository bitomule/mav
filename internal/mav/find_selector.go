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

// resolveFindForAction is resolveFindElement with the guard around it: the
// answer is checked against the screen as it is NOW, immediately before
// anything touches it.
//
// The window it closes is the model's own round trip. A resolution reads a
// tree, spends 550 ms asking, and then acts on what it read - and in those
// 550 ms a screen that was still finishing drawing can move the row. So the
// identity of the chosen element is captured at resolution and re-checked
// against a fresh read before the caller acts on it.
//
// One re-read and one re-resolution. Not a loop: a screen that keeps moving
// under a step is a screen the step should fail on, not one to chase.
//
// The re-check is skipped for a literal resolution, which asked nobody and so
// has no window to close - that is the route the price is not paid on.
func (c CLI) resolveFindForAction(ctx context.Context, cfg Config, selector Selector, prefer string) (Element, error) {
	elements, err := c.readElementsForFind(ctx, cfg, prefer)
	if err != nil {
		return Element{}, err
	}
	chosen, result, err := c.resolveFindElement(ctx, elements, selector)
	if err != nil {
		return Element{}, err
	}
	if result.ResolvedBy != ResolvedByModel {
		return chosen, nil
	}
	c.trees.rememberChoice(guardFor(chosen))

	// Dirty first: this read has to be a read, not the tree the decision was
	// made from.
	c.trees.invalidate()
	fresh, err := c.readElementsForFind(ctx, cfg, prefer)
	if err != nil {
		return Element{}, err
	}
	// The decision is consumed before anything moves. A retry that reached
	// here without resolving again has nothing to act on, which is the point:
	// it cannot tap twice on one decision.
	guard, ok := c.trees.consumeChoice()
	if !ok {
		return Element{}, fmt.Errorf("find_decision_consumed")
	}
	if guard.Holds(fresh) {
		return chosen, nil
	}
	rechosen, _, err := c.resolveFindElement(ctx, fresh, selector)
	if err != nil {
		return Element{}, err
	}
	if !guardFor(rechosen).Holds(fresh) {
		return Element{}, fmt.Errorf("element_moved")
	}
	return rechosen, nil
}

func (c CLI) readElementsForFind(ctx context.Context, cfg Config, prefer string) ([]Element, error) {
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil || described.Result.Err != nil {
		return nil, fmt.Errorf("tree_failed")
	}
	return ExtractElements(described.Result.Stdout), nil
}
