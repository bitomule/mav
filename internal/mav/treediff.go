package mav

import "sort"

// TreeDelta is the difference between two Element snapshots. Agents consume
// it to reason about state transitions without re-reading the full tree on
// every step. The shape is intentionally JSON-stable: agents can rely on
// added/removed/changed keys.
type TreeDelta struct {
	Added   []Element       `json:"added,omitempty"`
	Removed []Element       `json:"removed,omitempty"`
	Changed []ElementChange `json:"changed,omitempty"`
}

// ElementChange records the per-field deltas between the same logical element
// (matched by ID, or by structural key when ID is empty) in two snapshots.
// Diffs maps field name -> "old → new".
type ElementChange struct {
	ID    string            `json:"id,omitempty"`
	Key   string            `json:"key,omitempty"` // populated when ID is empty
	Diffs map[string]string `json:"diffs"`
}

// TreeDiff computes the delta from prev to next. Elements are matched by
// non-empty ID first; for elements without ID, by a structural key (role +
// label + frame). Empty deltas (matched, no field difference) are omitted.
//
// Determinism: outputs are sorted by ID then Key so JSON serialisation is
// stable across runs — critical for golden tests and for the HTML report's
// diff renderer.
func TreeDiff(prev, next []Element) TreeDelta {
	prevIdx := indexByMatchKey(prev)
	nextIdx := indexByMatchKey(next)

	var delta TreeDelta
	seen := map[string]bool{}

	// Pass 1: items in prev — either Changed (also in next) or Removed.
	for key, p := range prevIdx {
		seen[key] = true
		n, ok := nextIdx[key]
		if !ok {
			delta.Removed = append(delta.Removed, p)
			continue
		}
		diffs := elementDiff(p, n)
		if len(diffs) == 0 {
			continue
		}
		change := ElementChange{Diffs: diffs}
		if p.ID != "" {
			change.ID = p.ID
		} else {
			change.Key = key
		}
		delta.Changed = append(delta.Changed, change)
	}

	// Pass 2: items only in next -> Added.
	for key, n := range nextIdx {
		if seen[key] {
			continue
		}
		delta.Added = append(delta.Added, n)
	}

	sortDelta(&delta)
	return delta
}

// indexByMatchKey builds a lookup keyed by matchKey(el). Last-wins on
// collision — rare in practice because dedupElements already removed
// duplicates upstream.
func indexByMatchKey(elements []Element) map[string]Element {
	out := make(map[string]Element, len(elements))
	for _, el := range elements {
		out[matchKey(el)] = el
	}
	return out
}

// matchKey decides identity for diffing. A non-empty ID wins outright; it
// is the only field that survives across screen transitions. Otherwise the
// structural triplet (role + label + frame) acts as a fallback so the same
// physical element on screen still pairs with its counterpart in the
// next snapshot.
func matchKey(el Element) string {
	if el.ID != "" {
		return "id:" + el.ID
	}
	return "s:" + el.Role + "|" + el.Label + "|" + el.Frame
}

// elementDiff returns a per-field map of differences. Empty result means
// the two elements are equal for diff purposes.
func elementDiff(a, b Element) map[string]string {
	out := map[string]string{}
	add := func(field, av, bv string) {
		if av != bv {
			out[field] = av + " → " + bv
		}
	}
	add("label", a.Label, b.Label)
	add("role", a.Role, b.Role)
	add("value", a.Value, b.Value)
	add("frame", a.Frame, b.Frame)
	add("enabled", a.Enabled, b.Enabled)
	add("subrole", a.Subrole, b.Subrole)
	add("title", a.Title, b.Title)
	add("focused", a.Focused, b.Focused)
	// PID intentionally excluded: changes when the app relaunches but
	// the screen identity stays the same; it would create noise.
	return out
}

func sortDelta(d *TreeDelta) {
	sort.SliceStable(d.Added, func(i, j int) bool { return matchKey(d.Added[i]) < matchKey(d.Added[j]) })
	sort.SliceStable(d.Removed, func(i, j int) bool { return matchKey(d.Removed[i]) < matchKey(d.Removed[j]) })
	sort.SliceStable(d.Changed, func(i, j int) bool {
		ki := d.Changed[i].ID
		if ki == "" {
			ki = d.Changed[i].Key
		}
		kj := d.Changed[j].ID
		if kj == "" {
			kj = d.Changed[j].Key
		}
		return ki < kj
	})
}
