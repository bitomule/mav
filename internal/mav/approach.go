package mav

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MapApproachesDir is where named approach paths live, one JSON
// file per approach (filename derived from the approach name).
const MapApproachesDir = ".mav/map/approaches"

// ApproachStep is a single move inside an approach path. Most
// steps are taps (id / text / value / x+y selectors); a step can
// alternatively be a `type` action (`Type` non-empty) — useful for
// approaches that have to fill in a login form, search box, etc.
// before the route engine can move on.
//
// `Anchor` is non-empty only on the FIRST step of the sequence — it
// names the screen the engine must be on for the approach to fire.
// Subsequent steps assume the previous step landed correctly; they
// just describe the next tap or text input.
//
// `Wait` is optional and follows the same convention as `Edge.Wait`
// (a duration string like "1s", "500ms"). Empty falls back to the
// engine default.
//
// `TypeChars` is a metadata-only hint left by `approach extract`
// when it sees a `type` action in the run log but the actual text
// isn't recorded (commands.jsonl stores `chars=<N>` only, by design
// — operator credentials and other secrets must not leak to disk).
// Operators see the chars hint when running `approach show`, fill
// in `Type` by hand, and re-save. Playback errors out cleanly if
// `Type` is still empty when a step claims to be a type action.
type ApproachStep struct {
	Anchor    string `json:"anchor,omitempty"`
	ID        string `json:"id,omitempty"`
	Text      string `json:"text,omitempty"`
	Value     string `json:"value,omitempty"`
	X         string `json:"x,omitempty"`
	Y         string `json:"y,omitempty"`
	Wait      string `json:"wait,omitempty"`
	Driver    string `json:"driver,omitempty"`
	Type      string `json:"type,omitempty"`       // text to type; mutually exclusive with tap selectors
	TypeChars int    `json:"type_chars,omitempty"` // metadata-only hint from extract; ignored by playback
}

// IsType reports whether the step is a text-entry action rather
// than a tap. A type step has the `Type` payload filled in (after
// extract: also `TypeChars` non-zero meaning "extract saw a type
// action but the text needs to be supplied").
func (s ApproachStep) IsType() bool {
	return s.Type != "" || s.TypeChars > 0
}

// Approach is a named, ordered sequence the route engine replays as
// a single atomic unit before BFS engages when the engine is on the
// approach's anchor screen.
//
// This is the local equivalent of Firebase Robo Test's "Robo
// Scripts" — a way to short-circuit a long deterministic chain
// (login, onboarding dismissal, paywall, locale picker) that would
// otherwise have to be discovered or BFS'd every time.
type Approach struct {
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Steps         []ApproachStep `json:"steps"`
	RecordedAt    string         `json:"recorded_at,omitempty"`
	LastSuccessAt string         `json:"last_success_at,omitempty"`
	LastFailureAt string         `json:"last_failure_at,omitempty"`
	FailureCount  int            `json:"failure_count,omitempty"`
}

// Anchor returns the screen id the approach is bound to. Empty if
// the approach has no steps or its first step is malformed.
func (a Approach) Anchor() string {
	if len(a.Steps) == 0 {
		return ""
	}
	return a.Steps[0].Anchor
}

// IsStale reports whether the approach is past its TTL — same rule
// as `IsEdgeStale`, just at the approach granularity. Stale
// approaches are demoted in `MatchingApproaches` so the route
// engine prefers fresh ones; if no fresh approach matches, the
// stale ones are still tried (better than nothing).
func (a Approach) IsStale(ttl time.Duration, now time.Time) bool {
	if ttl <= 0 {
		return false
	}
	stamp := a.LastSuccessAt
	if stamp == "" {
		stamp = a.RecordedAt
	}
	if stamp == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return false
	}
	return now.Sub(t) > ttl
}

// approachFileName returns the canonical filename for an approach
// — lower-case, alphanumeric, dashes for word breaks. Mirrors the
// `safeFileName` policy used for screens.
//
// Non-alphanumeric characters (other than the dashes/underscores/
// spaces we explicitly map to "-") are dropped silently; consecutive
// dashes that result from those drops collapse to a single dash so
// "weird & chars" doesn't become "weird---chars".
func approachFileName(name string) string {
	var b strings.Builder
	lastDash := true // suppress a leading dash
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '_' || r == '-':
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		default:
			// Any other rune is treated as a word-break trigger
			// — but we suppress the dash until we see a real
			// alphanumeric run, so "&" between words becomes a
			// single dash, not two.
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "approach"
	}
	return out + ".json"
}

// LoadApproach reads one approach from disk by canonical name. The
// returned `Approach` is zero-valued if the file does not exist;
// callers test `err` via `os.IsNotExist` for the absence case.
func LoadApproach(root, name string) (Approach, error) {
	path := filepath.Join(root, MapApproachesDir, approachFileName(name))
	data, err := os.ReadFile(path)
	if err != nil {
		return Approach{}, err
	}
	var a Approach
	if err := json.Unmarshal(data, &a); err != nil {
		return Approach{}, fmt.Errorf("approach_invalid: %w", err)
	}
	return a, nil
}

// LoadAllApproaches reads every approach in `.mav/map/approaches/`.
// Returns an empty slice (not nil) when the directory is missing —
// approaches are an optional augmentation of the map.
func LoadAllApproaches(root string) ([]Approach, error) {
	dir := filepath.Join(root, MapApproachesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Approach{}, nil
		}
		return nil, err
	}
	out := make([]Approach, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var a Approach
		if err := json.Unmarshal(data, &a); err != nil {
			return nil, fmt.Errorf("approach_invalid %s: %w", entry.Name(), err)
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// SaveApproach writes an approach to disk. The directory is created
// if missing; existing files are overwritten so callers control
// merge semantics (almost always: load → mutate → save).
func SaveApproach(root string, a Approach) error {
	if a.Name == "" {
		return fmt.Errorf("approach_name_required")
	}
	dir := filepath.Join(root, MapApproachesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, approachFileName(a.Name))
	return os.WriteFile(path, data, 0o644)
}

// MatchingApproaches returns the approaches whose anchor matches
// the supplied current screen id, freshest first. Stale approaches
// land at the end of the slice so callers can prefer fresh ones
// without losing the option to fall back when none are fresh.
//
// An approach with a `low`-style failure history (`FailureCount >=
// 2`) is filtered out entirely — same gate the edge route engine
// uses for `Confidence: "low"` edges.
func MatchingApproaches(approaches []Approach, currentScreen string, ttl time.Duration, now time.Time) []Approach {
	type ranked struct {
		approach Approach
		stale    bool
	}
	var matches []ranked
	for _, a := range approaches {
		if a.Anchor() != currentScreen {
			continue
		}
		if a.FailureCount >= 2 {
			continue
		}
		matches = append(matches, ranked{approach: a, stale: a.IsStale(ttl, now)})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].stale != matches[j].stale {
			return !matches[i].stale // fresh before stale
		}
		return matches[i].approach.Name < matches[j].approach.Name
	})
	out := make([]Approach, len(matches))
	for i, r := range matches {
		out[i] = r.approach
	}
	return out
}
