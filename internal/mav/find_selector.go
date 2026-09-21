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
// tree, spends 350 ms asking, and then acts on what it read - and in those
// 350 ms a screen that was still finishing drawing can move the row. So the
// identity of the chosen element is captured at resolution and re-checked
// against a fresh read before the caller acts on it.
//
// One re-read and one re-resolution. Not a loop: a screen that keeps moving
// under a step is a screen the step should fail on, not one to chase.
//
// The check itself is a read of ONE POINT - the coordinate the caller is about
// to tap - and not of the whole screen. Measured on iPhone 17 Pro / iOS 26.3,
// 10 reads each: 128 ms for the point against 287 ms for the tree. A guard
// that cost a whole tree turned every model-resolved step into two reads and
// handed back the time the flow saves by not settling on arrival.
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
	// The ledger, not the cache. Whoever comes to this next: the temptation is
	// to hang the spent-decision flag off c.trees, because the tree it was
	// resolved from is already there - and that is the v0.26.0 defect. The
	// cache is optional (nil outside `mav run`), so the flag was written
	// nowhere and read back as "already spent", and every model-resolved
	// selector failed find_decision_consumed at the CLI. choices() is the
	// run's ledger when there is a run and a private one when there is not,
	// so the interlock exists either way.
	ledger := c.trees.choices()
	ledger.remember(guardFor(chosen))

	if holds, asked := c.guardHoldsUnderPoint(ctx, cfg, prefer, chosen); asked && holds {
		// The decision is consumed before anything moves. A retry that
		// reached here without resolving again has nothing to act on, which
		// is the point: it cannot tap twice on one decision.
		if _, ok := ledger.consume(); !ok {
			return Element{}, fmt.Errorf("find_decision_consumed")
		}
		return chosen, nil
	}

	// The point said no, or could not be asked. Dirty first: this read has to
	// be a read, not the tree the decision was made from.
	c.trees.invalidate()
	fresh, err := c.readElementsForFind(ctx, cfg, prefer)
	if err != nil {
		return Element{}, err
	}
	guard, ok := ledger.consume()
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
	// The re-resolution is a decision of its own and goes through the same
	// interlock: written down, then spent before the caller is handed it.
	// Without this it was the one decision in the file that left the ledger
	// untouched, so the property held on one route and not the other.
	ledger.remember(guardFor(rechosen))
	if _, ok := ledger.consume(); !ok {
		return Element{}, fmt.Errorf("find_decision_consumed")
	}
	// The second and last check, and it is a live one. Checking the
	// re-resolution against `fresh` - the tree it was just resolved FROM - can
	// only ever answer yes, because it came out of that tree: that guard could
	// not fire on any screen, however fast it was moving. So the question is
	// put to the screen again, at the point the caller would touch.
	holds, asked := c.guardHoldsUnderPoint(ctx, cfg, prefer, rechosen)
	if asked && !holds {
		return Element{}, fmt.Errorf("element_moved")
	}
	return rechosen, nil
}

// guardHoldsUnderPoint asks what is under the coordinate the caller is about
// to touch and reports whether the chosen element is still there. `asked` is
// false when the question could not be put at all - no frame to aim at, a
// driver with no point read, a failed call - and then `holds` says nothing.
//
// The two are separate because the answers go opposite ways. A no on the FIRST
// check is not a failure: it sends the caller down the whole re-read, which is
// what decides. That matters because a point read answers with the element the
// hit test lands on and its descendants, so a choice that is an ANCESTOR of
// that element is absent from a payload describing a screen where nothing
// moved - and the re-read absorbs exactly that, since the guard still holds in
// the full tree. A no on the SECOND check, after the full tree has already
// said the element is gone, is the failure.
func (c CLI) guardHoldsUnderPoint(ctx context.Context, cfg Config, prefer string, chosen Element) (holds, asked bool) {
	x, y, ok := TapPoint(chosen)
	if !ok {
		return false, false
	}
	described, err := c.describeUITreeAtPoint(ctx, cfg, prefer, x, y)
	if err != nil || described.Result.Err != nil {
		return false, false
	}
	return guardFor(chosen).Holds(ExtractElements(described.Result.Stdout)), true
}

func (c CLI) readElementsForFind(ctx context.Context, cfg Config, prefer string) ([]Element, error) {
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil || described.Result.Err != nil {
		return nil, fmt.Errorf("tree_failed")
	}
	return ExtractElements(described.Result.Stdout), nil
}
