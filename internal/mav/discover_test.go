package mav

import (
	"context"
	"errors"
	"testing"
	"time"
)

// scriptedDiscoverRunner is a tiny in-memory `DiscoverRunner` used
// by every test in this file. Each invocation pops the next entry
// off the scripted queue, so a test can model "tap A reveals screen
// B, tap C reveals target" by listing those screen transitions in
// order.
type scriptedDiscoverRunner struct {
	current  string
	screens  map[string][]Element
	transitions []scriptedTap
	backStack   []string
	calls       int
}

type scriptedTap struct {
	from     string
	matchID  string
	to       string
	err      error
}

func (r *scriptedDiscoverRunner) CurrentScreen(ctx context.Context) (string, []Element, error) {
	return r.current, r.screens[r.current], nil
}

func (r *scriptedDiscoverRunner) Tap(ctx context.Context, sel ApproachStep) (string, []Element, error) {
	r.calls++
	for i, tr := range r.transitions {
		if tr.from != r.current {
			continue
		}
		if tr.matchID != "" && tr.matchID != sel.ID {
			continue
		}
		r.transitions = append(r.transitions[:i], r.transitions[i+1:]...)
		if tr.err != nil {
			return "", nil, tr.err
		}
		r.backStack = append(r.backStack, r.current)
		r.current = tr.to
		return r.current, r.screens[r.current], nil
	}
	// Unscripted tap → no progress, returns same screen.
	return r.current, r.screens[r.current], nil
}

func (r *scriptedDiscoverRunner) Back(ctx context.Context) (string, []Element, error) {
	if len(r.backStack) == 0 {
		return r.current, r.screens[r.current], nil
	}
	r.current = r.backStack[len(r.backStack)-1]
	r.backStack = r.backStack[:len(r.backStack)-1]
	return r.current, r.screens[r.current], nil
}

func TestDiscoverFindsTargetByIdToken(t *testing.T) {
	runner := &scriptedDiscoverRunner{
		current: "home",
		screens: map[string][]Element{
			"home": {
				{ID: "settings_button", Label: "Settings", Role: "Button"},
				{ID: "noise_button", Label: "Other", Role: "Button"},
			},
			"settings": {
				{ID: "profile_link", Label: "Profile", Role: "Cell"},
			},
		},
		transitions: []scriptedTap{
			{from: "home", matchID: "settings_button", to: "settings"},
		},
	}
	got, err := Discover(context.Background(), runner, "settings", DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Reached {
		t.Fatalf("expected target reached, result=%+v", got)
	}
	if len(got.Path) != 1 || got.Path[0].ID != "settings_button" {
		t.Fatalf("path=%+v", got.Path)
	}
}

func TestDiscoverHonoursDepthBudget(t *testing.T) {
	// home → A → B → target. Depth budget = 2 should abort
	// before reaching target.
	runner := &scriptedDiscoverRunner{
		current: "home",
		screens: map[string][]Element{
			"home":   {{ID: "to_a_button", Label: "A", Role: "Button"}},
			"a":      {{ID: "to_b_button", Label: "B", Role: "Button"}},
			"b":      {{ID: "target_button", Label: "Target", Role: "Button"}},
			"target": {},
		},
		transitions: []scriptedTap{
			{from: "home", matchID: "to_a_button", to: "a"},
			{from: "a", matchID: "to_b_button", to: "b"},
			{from: "b", matchID: "target_button", to: "target"},
		},
	}
	got, _ := Discover(context.Background(), runner, "target", DiscoverOptions{Depth: 2})
	if got.Reached {
		t.Fatalf("expected NOT reached at depth=2, got %+v", got)
	}
	if got.Aborted != "depth" {
		t.Fatalf("expected aborted=depth, got %q", got.Aborted)
	}
}

func TestDiscoverHonoursMaxTaps(t *testing.T) {
	runner := &scriptedDiscoverRunner{
		current: "home",
		screens: map[string][]Element{
			"home": {{ID: "noise_button", Role: "Button"}},
		},
		transitions: []scriptedTap{
			{from: "home", err: errors.New("doomed")},
			{from: "home", err: errors.New("doomed")},
		},
	}
	got, _ := Discover(context.Background(), runner, "ghost", DiscoverOptions{MaxTaps: 1, Depth: 5})
	// The single allowed tap fails → no progress → marked stuck.
	if got.Reached {
		t.Fatal("did not expect reach")
	}
	if got.Aborted != "max_taps" && got.Aborted != "stuck" {
		t.Fatalf("expected max_taps or stuck, got %q", got.Aborted)
	}
}

func TestDiscoverHonoursTimeBudget(t *testing.T) {
	runner := &scriptedDiscoverRunner{
		current: "home",
		screens: map[string][]Element{
			"home": {{ID: "doomed_button", Role: "Button"}},
		},
	}
	got, _ := Discover(context.Background(), runner, "ghost", DiscoverOptions{Budget: 1 * time.Millisecond, Depth: 10, MaxTaps: 10})
	if got.Reached {
		t.Fatal("did not expect reach")
	}
	if got.Aborted != "budget" && got.Aborted != "stuck" {
		t.Fatalf("expected budget or stuck, got %q", got.Aborted)
	}
}

func TestScoreCandidatePrefersIdMatchOverLabelMatch(t *testing.T) {
	idMatch := Element{ID: "uploadFormView", Role: "Other"}
	labelMatch := Element{Label: "Upload Form", Role: "Other"}
	if scoreCandidate("upload-form-view", idMatch) <= scoreCandidate("upload-form-view", labelMatch) {
		t.Fatalf("id match should outscore label match")
	}
}

func TestIsLikelyTappableAcceptsButtonsAndCells(t *testing.T) {
	yes := []Element{
		{Role: "Button"},
		{Role: "XCUIElementTypeButton"},
		{Role: "Cell"},
		{Role: "Link"},
		{Role: "MenuItem"},
		{Role: "AXOther", ID: "some.button"},
	}
	for _, el := range yes {
		if !isLikelyTappable(el) {
			t.Fatalf("expected tappable: %+v", el)
		}
	}
	no := []Element{
		{Role: "StaticText"},
		{Role: "Heading"},
		{Role: "AXTabBar"},
	}
	for _, el := range no {
		if isLikelyTappable(el) {
			t.Fatalf("did not expect tappable: %+v", el)
		}
	}
}
