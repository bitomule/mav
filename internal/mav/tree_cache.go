package mav

import (
	"strings"
	"sync"
)

// The tree cache is not "I kept the tree". It is "I kept the identity, and I
// know per element whether it still holds".
//
// One read costs 320 ms on the 21 sep remeasurement - `axe describe-ui`, and
// there is nothing to shave off it: describe-ui takes --udid and --point and
// nothing else. So a run that finds, asserts and taps on one screen pays that
// three times for three reads of the same still screen.
//
// Two pieces, and they answer different questions.

// treeCache is the run's tree plus a dirty bit. There is NO TTL, deliberately:
// a cache that expires on a clock is a cache that hands back a screen that is
// no longer there, and one that survives a clock is a cache that hands back a
// screen that is no longer there. What decides is whether anything could have
// moved, not how long ago it was read.
//
// Dirty by default in the face of any gesture. A nil *treeCache is a working
// cache that never hits, so every CLI that was never given one behaves exactly
// as it did before.
type treeCache struct {
	mu sync.Mutex
	// gen rises on every invalidation. An entry carries the gen it was stored
	// under and is only served while that is still current, which is what
	// makes a read taken DURING a gesture unusable afterwards: the gesture
	// invalidates on the way in and on the way out, so the tree it read in the
	// middle is already two generations old by the time anyone asks for it.
	gen     uint64
	entries map[string]cachedTree
	// ledger is the run's spent-decision interlock. Reached through
	// choices(), which works on a nil cache too - see choiceLedger.
	ledger choiceLedger
}

type cachedTree struct {
	gen  uint64
	tree describedUITree
}

func newTreeCache() *treeCache {
	return &treeCache{entries: map[string]cachedTree{}}
}

// withTreeCache turns the cache on for this invocation. It is on the CLI and
// not global because what it caches is one run's screen.
func (c CLI) withTreeCache() CLI {
	c.trees = newTreeCache()
	return c
}

func treeCacheKey(prefer string, includeSystem bool) string {
	if includeSystem {
		return prefer + "\x1fsystem"
	}
	return prefer + "\x1fapp"
}

func (t *treeCache) lookup(prefer string, includeSystem bool) (describedUITree, bool) {
	if t == nil {
		return describedUITree{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[treeCacheKey(prefer, includeSystem)]
	if !ok || entry.gen != t.gen {
		return describedUITree{}, false
	}
	return entry.tree, true
}

func (t *treeCache) store(prefer string, includeSystem bool, tree describedUITree) {
	if t == nil {
		return
	}
	// A failed or empty read is not a screen. Caching one would hand the same
	// nothing back to every step after it.
	if tree.Result.Err != nil || strings.TrimSpace(tree.Result.Stdout) == "" || isEmptyAXTree(tree.Result.Stdout) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries[treeCacheKey(prefer, includeSystem)] = cachedTree{gen: t.gen, tree: tree}
}

// invalidate marks everything read so far as no longer describing the screen.
func (t *treeCache) invalidate() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gen++
}

// generation is how many times anything has dirtied this cache. It answers one
// question and only one: has something already moved in this run? A flow's
// first acting step is standing on a screen nobody has touched since the app
// finished launching, so there is nothing for it to wait to settle.
func (t *treeCache) generation() uint64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gen
}

// choiceLedger holds the element a find chose and has not acted on yet.
// remember writes it, consume takes it away, and consume answers no the second
// time: the decision is spent BEFORE anything moves, so nothing can act twice
// on one choice - it has to resolve again or fail.
//
// It is SEPARATE FROM THE CACHE, and that is the whole point of the type. The
// cache is an optimisation a caller opts into (withTreeCache) and a nil one is
// a cache that never hits; the ledger is an interlock, and an interlock that
// is absent whenever the optimisation is off is not an interlock. It was one
// field on treeCache until v0.26.x, and every `mav ui ...` outside `mav run` -
// the whole CLI, which never turns the cache on - had nothing to remember the
// decision IN, so the consume that follows found nothing and every
// model-resolved selector died `find_decision_consumed` before touching the
// screen. Measured on a simpool slot: `mav ui tap --find "..."` failed 1/1
// that way, and `mav ui type --find` inherited it through the tap it runs.
type choiceLedger struct {
	mu     sync.Mutex
	choice *elementGuard
}

func (l *choiceLedger) remember(guard elementGuard) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.choice = &guard
}

func (l *choiceLedger) consume() (elementGuard, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.choice == nil {
		return elementGuard{}, false
	}
	guard := *l.choice
	l.choice = nil
	return guard, true
}

// choices hands back where this resolution's decision is written.
//
// With a run's cache it is the RUN's ledger, so every step of that run shares
// one and a decision spent by one actor is gone for the next. Without one it
// is private to the caller that asked, which is the same guarantee at the only
// scope a single `mav ui` invocation has: the decision is still spent before
// the screen is touched, and still cannot be acted on twice.
func (t *treeCache) choices() *choiceLedger {
	if t == nil {
		return &choiceLedger{}
	}
	return &t.ledger
}

// elementGuard is the identity of a chosen element, re-checked live just
// before it is touched.
//
// The hole it closes is measured: a resolution reads a coordinate from one
// tree, asks a model about it - 350 ms - and then taps that coordinate having
// checked nothing. In those 350 ms the screen can finish drawing and move the
// row.
//
// NO FRAME. The same reason screenFingerprint leaves it out: two reads of a
// perfectly still screen disagree on coordinates by fractions of a point, so a
// guard that included the frame would fire on screens where nothing happened
// and would teach everyone to ignore it. These are the fields sameElement
// compares, minus the geometry.
type elementGuard struct {
	ID      string
	Label   string
	Role    string
	Value   string
	Enabled string
}

func guardFor(el Element) elementGuard {
	return elementGuard{
		ID: el.ID, Label: el.Label, Role: el.Role,
		Value: el.Value, Enabled: el.Enabled,
	}
}

// Holds reports whether the element this guard was taken from is still on the
// screen, unchanged in the ways that decide what tapping it does.
func (g elementGuard) Holds(elements []Element) bool {
	for _, el := range elements {
		if guardFor(el) == g {
			return true
		}
	}
	return false
}

// Unique is Holds plus the element itself, and it answers no when the identity
// matches more than once.
//
// The element matters because the guard is deliberately blind to the frame,
// so a caller that has just confirmed its choice is still on the screen still
// does not know WHERE it is - and the copy it is holding came from an older
// read. The one it gets back here came from the read that just answered.
//
// Two matches is not an answer: nothing in the identity separates them, so
// there is no saying which one the choice was, and a caller that needs a
// coordinate has to resolve again rather than pick.
func (g elementGuard) Unique(elements []Element) (Element, bool) {
	var found Element
	seen := 0
	for _, el := range elements {
		if guardFor(el) == g {
			found = el
			seen++
		}
	}
	return found, seen == 1
}

// isReadOnlyFlowAction names the steps that cannot move the screen. Everything
// else dirties the cache, which is the safe direction: a gesture wrongly
// called read-only serves a stale tree to the step after it, while a read
// wrongly called a gesture costs one re-read.
func isReadOnlyFlowAction(action string) bool {
	switch action {
	case "tree", "assert", "assertCount", "extract", "capture", "verify",
		"logs", "crashes", "report", "network.status", "time.status",
		"app.list", "clipboard.read", "evidence.step":
		return true
	}
	return false
}
