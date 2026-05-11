package mav

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Default discovery budgets. These are the safety net that prevents
// a `mav go missing-screen` from tapping forever — exceeding any
// one of them aborts discovery and surfaces a structured failure.
const (
	defaultDiscoverDepth    = 3
	defaultDiscoverBudget   = 90 * time.Second
	defaultDiscoverMaxTaps  = 15
	discoverObservePause    = 600 * time.Millisecond
	discoverTreeReadyMaxAge = 5 * time.Second
)

// DiscoverOptions packages the per-call knobs `mav go` uses to
// bound live discovery. Zero values fall through to the
// `defaultDiscover*` constants.
type DiscoverOptions struct {
	Depth    int
	Budget   time.Duration
	MaxTaps  int
	Disabled bool // operator opt-out via `--no-discover`
}

// effective returns the option struct with zero values replaced by
// the defaults — handy at the call boundary so the inner code can
// just read the fields.
func (o DiscoverOptions) effective() DiscoverOptions {
	out := o
	if out.Depth <= 0 {
		out.Depth = defaultDiscoverDepth
	}
	if out.Budget <= 0 {
		out.Budget = defaultDiscoverBudget
	}
	if out.MaxTaps <= 0 {
		out.MaxTaps = defaultDiscoverMaxTaps
	}
	return out
}

// DiscoverResult records what happened during a live-discovery run.
// `Reached == true` means the target screen was observed; the
// `Path` slice then carries the (selector, observed-screen) pairs
// that took us there — exactly the data the caller needs to
// promote them into permanent edges.
type DiscoverResult struct {
	Reached     bool
	Target      string
	StartScreen string
	Path        []DiscoverStep
	Aborted     string // "budget", "depth", "max_taps", "stuck"
}

// DiscoverStep mirrors `ApproachStep` but carries the post-tap
// screen so the caller can build map edges from a successful path.
type DiscoverStep struct {
	From    string
	To      string
	ID      string
	Text    string
	Driver  string
}

// scoreCandidate orders the tappable elements on the current screen
// so the highest-scoring (most-likely-progress-toward-target) one
// is tried first. The heuristic is intentionally simple:
//
//   - +3 for an id token that appears in the target name.
//   - +2 for a label/text token that appears in the target name.
//   - +1 for any element with a stable id (better than a coord-only
//     guess on a label-less generic widget).
//
// Tokens are compared in lower-case, non-alphanumeric characters
// stripped. So target `upload-form` matches an element with id
// `UploadFormView.continueButton` (both reduce to `uploadform...`).
func scoreCandidate(target string, el Element) int {
	want := normalizedRoleToken(target)
	if want == "" {
		return 0
	}
	score := 0
	idTokens := normalizedRoleToken(el.ID)
	labelTokens := normalizedRoleToken(el.Label)
	if idTokens != "" && strings.Contains(idTokens, want) {
		score += 3
	}
	if labelTokens != "" && strings.Contains(labelTokens, want) {
		score += 2
	}
	if score == 0 && el.ID != "" {
		score = 1
	}
	return score
}

// discoverCandidate is an internal record that pairs an element
// with its score so the planner can sort and de-duplicate them.
type discoverCandidate struct {
	element Element
	score   int
}

// rankCandidates returns the tappable elements on the supplied
// screen, sorted highest-score-first. Elements with score 0 are
// dropped — purely-decorative widgets aren't worth a discovery tap.
func rankCandidates(target string, elements []Element) []discoverCandidate {
	candidates := make([]discoverCandidate, 0, len(elements))
	seen := map[string]bool{}
	for _, el := range elements {
		if !isLikelyTappable(el) {
			continue
		}
		score := scoreCandidate(target, el)
		if score <= 0 {
			continue
		}
		key := el.ID + "\x00" + el.Label
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, discoverCandidate{element: el, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	return candidates
}

// isLikelyTappable filters out elements that the discovery walker
// shouldn't attempt to tap — purely textual nodes, application
// containers, system chrome. A `Button`/`Cell`/`Tab` role survives;
// a `StaticText`/`Heading` doesn't.
//
// The filter is intentionally permissive: false negatives waste a
// discovery tap, false positives just skip a candidate that might
// have worked anyway. Mav already handles "tap failed" gracefully.
func isLikelyTappable(el Element) bool {
	role := strings.ToLower(el.Role)
	switch {
	case strings.Contains(role, "button"):
		return true
	case strings.Contains(role, "cell"):
		return true
	case strings.Contains(role, "tab") && !strings.Contains(role, "tabbar"):
		return true
	case strings.Contains(role, "link"):
		return true
	case strings.Contains(role, "menuitem"):
		return true
	case strings.Contains(role, "actionsheet"):
		return false
	case strings.Contains(role, "statictext"), strings.Contains(role, "heading"):
		return false
	}
	// Stable accessibility id on an "Other" element often means a
	// custom hit-target the app exposes intentionally.
	if el.ID != "" && (strings.Contains(role, "other") || role == "") {
		return true
	}
	return false
}

// PersistDiscoveredPath promotes a successful discovery path into
// permanent map edges so the next `mav go` for the same target can
// follow the route via plain BFS instead of re-discovering. Each
// step becomes a high-confidence edge stamped with the current
// time; the existing `upsertEdge` machinery preserves any prior
// `FailureCount` if the edge already existed.
//
// Only called when `DiscoverResult.Reached == true`; failed
// branches are explicitly discarded (the persistence guard from
// the plan trade-offs table).
func PersistDiscoveredPath(root string, path []DiscoverStep) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, step := range path {
		screen, err := LoadScreen(root, step.From)
		if err != nil {
			continue
		}
		screen.Edges = upsertEdge(screen.Edges, Edge{
			From:          step.From,
			To:            step.To,
			ID:            step.ID,
			Text:          step.Text,
			Driver:        step.Driver,
			Source:        "discover",
			Confidence:    "high",
			RecordedAt:    now,
			LastSuccessAt: now,
			Wait:          "1s",
		})
		if err := SaveScreen(root, screen); err != nil {
			return err
		}
	}
	return nil
}

// Discover does the live BFS through the running app. It calls
// back into the provided `runner` (an abstraction over the CLI tap
// + observe pair) so this file stays unit-testable without a real
// simulator. The actual `goScreen` integration constructs an
// adapter that wraps `c.uiTap` + `c.waitForTreeReady` +
// `ObserveScreenDetailedWithDriver`.
type DiscoverRunner interface {
	// CurrentScreen returns the id and elements of the screen
	// the app is currently showing.
	CurrentScreen(ctx context.Context) (screen string, elements []Element, err error)
	// Tap fires a selector-based tap and returns the new
	// observed screen and its elements.
	Tap(ctx context.Context, sel ApproachStep) (screen string, elements []Element, err error)
	// Back attempts to go back one step (swipe-from-edge,
	// hardware back gesture) and returns the new screen.
	Back(ctx context.Context) (screen string, elements []Element, err error)
}

// Discover walks the live UI looking for the target screen. The
// algorithm is bounded BFS with the candidate-scoring heuristic
// above; failed branches are backtracked via the runner. Success
// returns a `DiscoverResult.Path` describing the taps that worked
// — the caller persists those as map edges.
func Discover(ctx context.Context, runner DiscoverRunner, target string, opts DiscoverOptions) (DiscoverResult, error) {
	o := opts.effective()
	deadline := time.Now().Add(o.Budget)
	start, _, err := runner.CurrentScreen(ctx)
	if err != nil {
		return DiscoverResult{Aborted: "current_screen_unavailable"}, err
	}
	result := DiscoverResult{
		Target:      target,
		StartScreen: start,
		Path:        []DiscoverStep{},
	}
	if start == target {
		result.Reached = true
		return result, nil
	}
	taps := 0
	current := start
	visited := map[string]bool{start: true}
	for depth := 0; depth < o.Depth; depth++ {
		if time.Now().After(deadline) {
			result.Aborted = "budget"
			return result, nil
		}
		if taps >= o.MaxTaps {
			result.Aborted = "max_taps"
			return result, nil
		}
		_, elements, err := runner.CurrentScreen(ctx)
		if err != nil {
			result.Aborted = "current_screen_unavailable"
			return result, nil
		}
		candidates := rankCandidates(target, elements)
		if len(candidates) == 0 {
			result.Aborted = "stuck"
			return result, nil
		}
		progressed := false
		for _, cand := range candidates {
			if time.Now().After(deadline) {
				result.Aborted = "budget"
				return result, nil
			}
			if taps >= o.MaxTaps {
				result.Aborted = "max_taps"
				return result, nil
			}
			step := ApproachStep{
				ID:   cand.element.ID,
				Text: cand.element.Label,
			}
			screen, _, err := runner.Tap(ctx, step)
			taps++
			if err != nil || screen == "" || screen == "unknown" || visited[screen] {
				// Did not progress — try to back out and pick
				// the next candidate.
				if backScreen, _, backErr := runner.Back(ctx); backErr == nil && backScreen != "" {
					current = backScreen
				}
				continue
			}
			result.Path = append(result.Path, DiscoverStep{
				From:   current,
				To:     screen,
				ID:     cand.element.ID,
				Text:   cand.element.Label,
				Driver: "",
			})
			visited[screen] = true
			current = screen
			if screen == target {
				result.Reached = true
				return result, nil
			}
			progressed = true
			time.Sleep(discoverObservePause)
			break
		}
		if !progressed {
			result.Aborted = "stuck"
			return result, nil
		}
	}
	result.Aborted = "depth"
	return result, nil
}
