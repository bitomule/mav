package mav

import "testing"

func treeOf(stdout string) describedUITree {
	return describedUITree{Driver: "axe", Result: CommandResult{Stdout: stdout}}
}

func TestTreeCacheServesTheSameScreenUntilSomethingMoves(t *testing.T) {
	cache := newTreeCache()
	cache.store("auto", false, treeOf(`{"AXLabel":"Cajas"}`))
	if _, ok := cache.lookup("auto", false); !ok {
		t.Fatal("a clean cache must serve the tree it read")
	}
	cache.invalidate()
	if _, ok := cache.lookup("auto", false); ok {
		t.Fatal("a gesture makes the screen unknown; the cache must miss")
	}
}

func TestTreeCacheDropsAReadTakenDuringAGesture(t *testing.T) {
	// A gesture resolves what it is acting on by reading the screen, and that
	// read describes the screen BEFORE it acted. Invalidating on the way in
	// and on the way out is what makes it unusable afterwards.
	cache := newTreeCache()
	cache.invalidate() // entering the gesture
	cache.store("auto", false, treeOf(`{"AXLabel":"antes"}`))
	cache.invalidate() // leaving it
	if _, ok := cache.lookup("auto", false); ok {
		t.Fatal("the pre-gesture tree must not survive the gesture")
	}
}

func TestTreeCacheNeverKeepsANonScreen(t *testing.T) {
	cache := newTreeCache()
	cache.store("auto", false, describedUITree{Result: CommandResult{Err: errTreeForTest}})
	cache.store("auto", false, treeOf("   "))
	if _, ok := cache.lookup("auto", false); ok {
		t.Fatal("a failed or empty read is not a screen to hand to the next step")
	}
}

func TestANilTreeCacheIsAWorkingCacheThatNeverHits(t *testing.T) {
	var cache *treeCache
	cache.invalidate()
	cache.store("auto", false, treeOf(`{"AXLabel":"x"}`))
	if _, ok := cache.lookup("auto", false); ok {
		t.Fatal("a command that was never given a cache must always read")
	}
	// The interlock does NOT go away with the cache. Without this the whole
	// CLI - which never turns the cache on - could not spend a decision at
	// all, and every model-resolved selector failed before it touched
	// anything.
	ledger := cache.choices()
	ledger.remember(guardFor(Element{ID: "boxRow_1000", Label: "1000", Role: "cell"}))
	if _, ok := ledger.consume(); !ok {
		t.Fatal("a cacheless caller must still be able to spend its own decision")
	}
	if _, ok := ledger.consume(); ok {
		t.Fatal("and spending it twice must still be refused")
	}
}

func TestTheDecisionIsConsumedSoARetryCannotActTwice(t *testing.T) {
	cache := newTreeCache()
	cache.choices().remember(guardFor(Element{ID: "boxRow_1000", Label: "1000", Role: "cell"}))
	if _, ok := cache.choices().consume(); !ok {
		t.Fatal("the decision must be there to consume")
	}
	if _, ok := cache.choices().consume(); ok {
		t.Fatal("a second attempt must find nothing to act on")
	}
}

// A run shares ONE ledger, which is what makes "cannot act twice" hold across
// the steps of a run and not merely inside one call: the choices() a second
// step gets is the choices() the first step spent.
func TestOneRunSharesOneLedger(t *testing.T) {
	cache := newTreeCache()
	step := cache.choices()
	step.remember(guardFor(Element{ID: "boxRow_1000", Label: "1000", Role: "cell"}))
	if _, ok := cache.choices().consume(); !ok {
		t.Fatal("the run's ledger must be the same one every step writes to")
	}
	if _, ok := step.consume(); ok {
		t.Fatal("another holder of the same run's ledger must not find it again")
	}
}

func TestTheGuardIgnoresTheFrameAndNoticesTheIdentity(t *testing.T) {
	chosen := Element{ID: "boxRow_1000", Label: "1000", Role: "cell", Enabled: "true", Frame: "{{0, 100}, {390, 44}}"}
	guard := guardFor(chosen)

	drifted := chosen
	drifted.Frame = "{{0, 100.5}, {390, 43.5}}"
	if !guard.Holds([]Element{drifted}) {
		t.Fatal("two reads of a still screen disagree on coordinates; the guard must not fire on that")
	}

	relabelled := chosen
	relabelled.Label = "1001"
	if guard.Holds([]Element{relabelled}) {
		t.Fatal("the row under the finger changed; the guard must fire")
	}

	disabled := chosen
	disabled.Enabled = "false"
	if guard.Holds([]Element{disabled}) {
		t.Fatal("an element that went disabled is no longer the one that was chosen")
	}

	if guard.Holds([]Element{{ID: "addBox", Label: "Agregar Caja", Role: "button"}}) {
		t.Fatal("the chosen element is gone; the guard must fire")
	}
}

func TestOnlyReadsKeepTheCachedScreen(t *testing.T) {
	for _, action := range []string{"tree", "assert", "assertCount", "extract", "capture", "verify"} {
		if !isReadOnlyFlowAction(action) {
			t.Errorf("%s reads the screen and must not dirty the cache", action)
		}
	}
	for _, action := range []string{"tap", "type", "swipe", "press", "toggle", "longPress", "scrollUntil", "open", "doubleTap", "drag"} {
		if isReadOnlyFlowAction(action) {
			t.Errorf("%s can move the screen and must dirty the cache", action)
		}
	}
}

var errTreeForTest = errForTest("tree_failed")

type errForTest string

func (e errForTest) Error() string { return string(e) }
