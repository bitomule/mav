package mav

import (
	"context"
	"time"
)

// Waiting for the screen to stop moving is not a thing a flow should have to
// ask for.
//
// It was declared, four words deep -- `after: { wait: { stable: true } }` --
// and it had to be, because without it two races showed up on Boxy, both
// measured, both caught with a screenshot:
//
//   - a save button that is DISABLED until the field it guards has text. A step
//     that reads the screen a moment early finds it disabled, `find` refuses to
//     offer a disabled button, and the step fails with nothing to tap.
//   - a form that arrives as a sheet sliding up. Typing into a screen that is
//     still moving loses the focus and the text goes nowhere.
//
// Neither is a thing the person writing the flow knows about, and neither is a
// thing they should have to. So it happens on its own, before any step that
// acts on an element.
//
// WHY THIS CANNOT HANG, which is the objection that matters. It is bounded:
// flowSettleReads reads and then it gives up and lets the step act on whatever
// is on screen. A screen that never settles -- a spinner, a running timer, an
// animation in a loop -- costs the budget and then carries on. It never fails a
// step and it never waits forever. That is the same shape goto has used since
// gotoSettle, for the same reason.
//
// WHAT IT COSTS, and it is not nothing. When the screen is already still it
// spends one read it would not otherwise have spent, plus the gap: measured at
// about 570 ms per acting step on Boxy. The second read is the one the step
// then uses -- it lands in the run's tree cache under the current generation --
// so the step's own read is free, and the declared form could never do that
// because the step re-read afterwards anyway.
const (
	flowSettleReads = 3
	flowSettleGap   = 250 * time.Millisecond
)

// settleBeforeFlowStep returns when two consecutive reads describe the same
// screen, or when the budget runs out. It has no error: a settle that could
// fail a step would be worse than the race it exists to close.
func (c CLI) settleBeforeFlowStep(ctx context.Context, step FlowStep, prefer string) {
	// Only steps that resolve an element. A delay, a capture, a simulator
	// setting and an app launch have no element to be wrong about, and making
	// them pay a read each would be a tax on the steps that do not need it.
	if flowStepSelector(step).IsZero() {
		return
	}
	cfg, err := c.mustLoadConfig()
	if err != nil {
		return
	}
	var last string
	for i := 0; i < flowSettleReads; i++ {
		described, readErr := c.describeUITree(ctx, cfg, prefer, false)
		if readErr != nil || described.Result.Err != nil {
			return
		}
		fingerprint := screenFingerprint(ExtractElements(described.Result.Stdout))
		if fingerprint != "" && fingerprint == last {
			// Settled, and this read stays in the cache for the step itself.
			return
		}
		last = fingerprint
		// The next read has to be a real one. Two reads of one cache entry are
		// equal by construction, which would declare every screen settled --
		// the exact bug gotoSettle had.
		c.trees.invalidate()
		select {
		case <-ctx.Done():
			return
		case <-time.After(flowSettleGap):
		}
	}
}
