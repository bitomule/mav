package mav

import (
	"strings"
	"sync"
)

// The tree cache is not "I kept the tree". It is "I kept the identity, and I
// know per element whether it still holds".
//
// One read costs 630 ms - `axe describe-ui`, and there is nothing to shave off
// it: describe-ui takes --udid and --point and nothing else. So a run that
// finds, asserts and taps on one screen pays that three times for three reads
// of the same still screen.
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
	choice  *elementGuard
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

// rememberChoice stores the identity of the element a resolution chose, and
// consumeChoice takes it away. The decision is consumed BEFORE anything moves,
// so a retry cannot act on a choice that has already been acted on - it has to
// resolve again or fail.
func (t *treeCache) rememberChoice(guard elementGuard) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.choice = &guard
}

func (t *treeCache) consumeChoice() (elementGuard, bool) {
	if t == nil {
		return elementGuard{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.choice == nil {
		return elementGuard{}, false
	}
	guard := *t.choice
	t.choice = nil
	return guard, true
}

// elementGuard is the identity of a chosen element, re-checked live just
// before it is touched.
//
// The hole it closes is measured: a resolution reads a coordinate from one
// tree, asks a model about it - 550 ms - and then taps that coordinate having
// checked nothing. In those 550 ms the screen can finish drawing and move the
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
