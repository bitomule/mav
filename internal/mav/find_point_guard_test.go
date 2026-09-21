package mav

import (
	"strings"
	"testing"
)

// The guard around a model-resolved choice is a read of ONE POINT, not of the
// whole screen. These tests pin the three things that makes true: what gets
// asked, what a no on the point costs, and that the failure at the end of the
// slow path is reachable at all.

const guardPointUDID = "GUARD-UDID"

// One button, big enough that its centre is a round number: {{0,100},{200,50}}
// centres on 100,125.
const guardPointTree = `[{"AXUniqueId":"openBox","AXLabel":"Abrir caja","type":"Button","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"}]`

// What the same button looks like when axe is asked about the point alone: a
// single object rather than an array.
const guardPointHit = `{"AXUniqueId":"openBox","AXLabel":"Abrir caja","type":"Button","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"}`

// The screen after the row has gone, with something else under the same point.
const guardPointMovedTree = `[{"AXUniqueId":"otherBox","AXLabel":"Otra caja","type":"Button","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"}]`

const guardPointMovedHit = `{"AXUniqueId":"somethingElse","AXLabel":"Nada que ver","type":"Button","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"}`

func guardPointConfig(t *testing.T) (CLI, Config, *sequenceRecordingRunner) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	cfg.SimulatorUDID = guardPointUDID
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		seq:   map[string][]string{},
		calls: map[string]int{},
		out:   map[string]string{},
	}
	return CLI{Runner: runner, Root: root}.withTreeCache(), cfg, runner
}

func countCalls(commands []string, substr string) int {
	n := 0
	for _, command := range commands {
		if strings.Contains(command, substr) {
			n++
		}
	}
	return n
}

func TestGuardChecksOnePointAndNotTheWholeTree(t *testing.T) {
	fakeJev(t, "1", 10)
	c, cfg, runner := guardPointConfig(t)
	runner.out["axe describe-ui --udid "+guardPointUDID] = guardPointTree
	runner.out["axe describe-ui --point 100,125 --udid "+guardPointUDID] = guardPointHit

	el, err := c.resolveFindForAction(t.Context(), cfg, Selector{Find: "la caja"}, "auto")
	if err != nil {
		t.Fatalf("the guard held, so this had to succeed: %v", err)
	}
	if el.ID != "openBox" {
		t.Fatalf("wrong element came back: %+v", el)
	}
	if got := countCalls(runner.commands, "describe-ui --point 100,125"); got != 1 {
		t.Fatalf("the guard must be one point read, got %d: %v", got, runner.commands)
	}
	// This is the whole point of the change: ONE whole-screen read per
	// resolution, the one the decision was made from.
	if got := countCalls(runner.commands, "describe-ui --udid"); got != 1 {
		t.Fatalf("a held guard must not re-read the screen, got %d whole-tree reads: %v", got, runner.commands)
	}
}

func TestGuardFallsBackToTheTreeWhenThePointDisagrees(t *testing.T) {
	// The point read answers with what the hit test lands on, so it can say no
	// about a screen where nothing moved. That no buys a whole-tree read, and
	// the tree - which still has the element - is what decides.
	fakeJev(t, "1", 10)
	c, cfg, runner := guardPointConfig(t)
	runner.out["axe describe-ui --udid "+guardPointUDID] = guardPointTree
	runner.out["axe describe-ui --point 100,125 --udid "+guardPointUDID] = guardPointMovedHit

	el, err := c.resolveFindForAction(t.Context(), cfg, Selector{Find: "la caja"}, "auto")
	if err != nil {
		t.Fatalf("the element never left the screen, so the step must not fail: %v", err)
	}
	if el.ID != "openBox" {
		t.Fatalf("wrong element came back: %+v", el)
	}
	if got := countCalls(runner.commands, "describe-ui --udid"); got != 2 {
		t.Fatalf("a point that disagrees costs exactly one re-read, got %d: %v", got, runner.commands)
	}
}

func TestGuardFailsWithElementMovedWhenTheScreenKeepsMoving(t *testing.T) {
	// Ablation of the guard itself. The element the model chose is gone from
	// the re-read, and the point under the re-resolution disagrees too: the
	// screen is still moving, and the step has to fail rather than tap.
	fakeJev(t, "1", 10)
	c, cfg, runner := guardPointConfig(t)
	runner.seq["axe describe-ui --udid "+guardPointUDID] = []string{guardPointTree, guardPointMovedTree}
	runner.out["axe describe-ui --point 100,125 --udid "+guardPointUDID] = guardPointMovedHit

	_, err := c.resolveFindForAction(t.Context(), cfg, Selector{Find: "la caja"}, "auto")
	if err == nil {
		t.Fatal("a screen still moving under the step must fail it, not tap on a guess")
	}
	if err.Error() != "element_moved" {
		t.Fatalf("expected element_moved, got %q", err.Error())
	}
	// One re-read and one re-resolution. Not a loop.
	if got := countCalls(runner.commands, "describe-ui --udid"); got != 2 {
		t.Fatalf("the guard must re-read once and stop, got %d: %v", got, runner.commands)
	}
}

// A CLI with no tree cache is every `mav ui ...` there is: the cache is turned
// on by `mav run` alone. v0.26.0 kept the spent-decision flag ON the cache, so
// with no cache there was nowhere to write it, the consume that follows found
// nothing, and `mav ui tap --find` died find_decision_consumed without
// touching the screen. Measured on a simpool slot before the fix, 1/1.
func TestACacheLessCLIStillResolvesAFind(t *testing.T) {
	fakeJev(t, "1", 10)
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true}
	cfg.SimulatorUDID = guardPointUDID
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		seq:   map[string][]string{},
		calls: map[string]int{},
		out:   map[string]string{},
	}
	runner.out["axe describe-ui --udid "+guardPointUDID] = guardPointTree
	runner.out["axe describe-ui --point 100,125 --udid "+guardPointUDID] = guardPointHit

	c := CLI{Runner: runner, Root: root}
	el, err := c.resolveFindForAction(t.Context(), cfg, Selector{Find: "la caja"}, "auto")
	if err != nil {
		t.Fatalf("a find with no cache around it must still resolve: %v", err)
	}
	if el.ID != "openBox" {
		t.Fatalf("wrong element came back: %+v", el)
	}
}

// The other half, and the one that must not be traded away for the half
// above: once a resolution has handed its element to the caller, the decision
// is spent. Nothing holding the run's ledger can act on it a second time
// without resolving again.
func TestASpentDecisionCannotBeActedOnTwice(t *testing.T) {
	fakeJev(t, "1", 10)
	c, cfg, runner := guardPointConfig(t)
	runner.out["axe describe-ui --udid "+guardPointUDID] = guardPointTree
	runner.out["axe describe-ui --point 100,125 --udid "+guardPointUDID] = guardPointHit

	if _, err := c.resolveFindForAction(t.Context(), cfg, Selector{Find: "la caja"}, "auto"); err != nil {
		t.Fatalf("the first resolution had to succeed: %v", err)
	}
	if _, ok := c.trees.choices().consume(); ok {
		t.Fatal("the decision was already acted on; a retry must find nothing left to act on")
	}
}

// Same on the slow route: the point disagreed, the tree was re-read, the
// element was re-resolved -- and that second decision is spent too.
func TestTheReResolvedDecisionIsSpentAsWell(t *testing.T) {
	fakeJev(t, "1", 10)
	c, cfg, runner := guardPointConfig(t)
	runner.out["axe describe-ui --udid "+guardPointUDID] = guardPointTree
	runner.seq["axe describe-ui --point 100,125 --udid "+guardPointUDID] = []string{guardPointMovedHit, guardPointHit}

	if _, err := c.resolveFindForAction(t.Context(), cfg, Selector{Find: "la caja"}, "auto"); err != nil {
		t.Fatalf("the element never left the screen, so this had to succeed: %v", err)
	}
	if _, ok := c.trees.choices().consume(); ok {
		t.Fatal("the re-resolution is a decision too, and it has to be spent before the caller acts")
	}
}

func TestLiteralResolutionPaysNoGuard(t *testing.T) {
	// Nobody was asked, so there is no round trip for the screen to move in.
	t.Setenv("CI", "1")
	c, cfg, runner := guardPointConfig(t)
	runner.out["axe describe-ui --udid "+guardPointUDID] = guardPointTree

	el, err := c.resolveFindForAction(t.Context(), cfg, Selector{Find: "Abrir caja"}, "auto")
	if err != nil {
		t.Fatalf("a literal find must resolve with no model and no key: %v", err)
	}
	if el.ID != "openBox" {
		t.Fatalf("wrong element came back: %+v", el)
	}
	if got := countCalls(runner.commands, "--point"); got != 0 {
		t.Fatalf("the literal route must not pay for a guard, got %d point reads: %v", got, runner.commands)
	}
}
