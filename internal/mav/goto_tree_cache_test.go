package mav

import (
	"testing"
)

// goto's reads must never be served from the tree cache.
//
// The cache is armed for the length of a `mav run`, and a flow step dirties it
// on the way in and on the way out. Every other step reads, acts, and hands the
// screen on. goto taps and re-reads many times INSIDE one step, and nothing
// between its own tap and its own next read dirties the cache -- so without
// this every read after the first was handed the screen as it looked before the
// first tap.
//
// Measured, and it cost a whole 20-run batch on Boxy: the loop tapped the
// button that opens the create-category sheet, was handed back the pre-tap
// grid, recorded changed=false, tapped again, got the same tree again, and
// reported outcome=stuck with the sheet plainly open. 0/20, every run
// identical.
const gotoCacheGridTree = `[{"AXUniqueId":"categoriesView","AXLabel":"Categories","type":"Group","enabled":true,"AXFrame":"{{0, 0}, {390, 844}}"}]`

const gotoCacheSheetTree = `[{"AXUniqueId":"categoriesView","AXLabel":"Categories","type":"Group","enabled":true,"AXFrame":"{{0, 0}, {390, 844}}"},{"AXLabel":"Create Category","type":"Heading","enabled":true,"AXFrame":"{{0, 20}, {390, 40}}"}]`

func TestGotoReadScreenNeverServesACachedTree(t *testing.T) {
	c, cfg, runner := guardPointConfig(t)
	describe := "axe describe-ui --udid " + guardPointUDID
	runner.seq[describe] = []string{gotoCacheGridTree, gotoCacheSheetTree}

	first, err := c.gotoReadScreen(t.Context(), cfg, GlobalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ExtractRoute(first).Title; got != "" {
		t.Fatalf("first read should be the grid, got title=%q", got)
	}

	// Nothing invalidates the cache in between. That is the whole point: this
	// stands in for the tap goto makes inside its own loop.
	second, err := c.gotoReadScreen(t.Context(), cfg, GlobalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ExtractRoute(second).Title; got != "Create Category" {
		t.Fatalf("second read was served a stale tree: title=%q, want %q", got, "Create Category")
	}
	if screenFingerprint(first) == screenFingerprint(second) {
		t.Fatal("goto was handed the same screen twice; the cache served its own pre-tap read")
	}
	if n := countCalls(runner.commands, "describe-ui"); n != 2 {
		t.Fatalf("describe-ui calls=%d, want 2 -- goto must read every time", n)
	}
}

// The same defect, on the helper whose whole job is to decide a screen has
// stopped moving. Two reads of one cache entry are equal by construction, so a
// cached gotoSettle declares every screen settled at once -- including a
// half-drawn one, which is the single thing it exists to rule out.
func TestGotoSettleReadsTheScreenAgainRatherThanTheCache(t *testing.T) {
	c, cfg, runner := guardPointConfig(t)
	describe := "axe describe-ui --udid " + guardPointUDID
	runner.seq[describe] = []string{gotoCacheGridTree, gotoCacheSheetTree, gotoCacheSheetTree}

	settled, err := c.gotoSettle(t.Context(), cfg, GlobalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ExtractRoute(settled).Title; got != "Create Category" {
		t.Fatalf("settled on the wrong screen: title=%q", got)
	}
	if n := countCalls(runner.commands, "describe-ui"); n < 3 {
		t.Fatalf("describe-ui calls=%d, want at least 3 -- settling on one cached read is not settling", n)
	}
}

// `text:` matches by CONTAINS, and that is worth a test rather than a comment:
// on a fixture seeded with `Test Category 1` and `Test Category 2`, a criterion
// of text:"Test Category" already holds before anything happens. A flow step
// written that way fails for the wrong reason -- goto's ambiguous-criterion
// guard rather than the absence of the thing it was asked to create -- so the
// measurement it was meant to make silently measures nothing.
//
// Same family as the ordinal collision: a name that is a prefix of what is
// already on screen is not a name, it is a wildcard.
func TestArrivalTextCriterionMatchesBySubstring(t *testing.T) {
	seeded := []Element{
		{Label: "Test Category 1", Role: "button"},
		{Label: "Test Category 2", Role: "button"},
	}
	if !treeContainsText(seeded, "Test Category") {
		t.Fatal("text: is a contains match; a prefix of a seeded label must match")
	}
	if treeContainsText(seeded, "Kitchen Stuff") {
		t.Fatal("a name that collides with nothing on screen must not match")
	}
}
