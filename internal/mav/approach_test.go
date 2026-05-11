package mav

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApproachFileNameNormalisation(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{in: "Login Basic", want: "login-basic.json"},
		{in: "login_basic", want: "login-basic.json"},
		{in: "  cold launch  ", want: "cold-launch.json"},
		{in: "spaces & weird ✦ chars!", want: "spaces-weird-chars.json"},
		{in: "", want: "approach"},
	}
	for _, tc := range cases {
		if got := approachFileName(tc.in); got != tc.want {
			t.Fatalf("approachFileName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestApproachSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	a := Approach{
		Name:        "Login Basic",
		Description: "Fresh-install login sequence",
		Steps: []ApproachStep{
			{Anchor: "start", Text: "Acceptar todo", Driver: "appium", Wait: "1s"},
			{ID: "PreLoginActionsView.emailLoginButtonView", Driver: "axe"},
		},
		RecordedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := SaveApproach(root, a); err != nil {
		t.Fatal(err)
	}
	// Verify file lands at the canonical path.
	want := filepath.Join(root, MapApproachesDir, "login-basic.json")
	loaded, err := LoadApproach(root, "Login Basic")
	if err != nil {
		t.Fatalf("LoadApproach: %v (expected file at %s)", err, want)
	}
	if loaded.Name != a.Name || len(loaded.Steps) != 2 {
		t.Fatalf("round-trip mismatch: %+v", loaded)
	}
	if loaded.Steps[0].Anchor != "start" {
		t.Fatalf("anchor lost: %+v", loaded.Steps[0])
	}
	// Loading by the canonical name should also work.
	again, err := LoadApproach(root, "login-basic")
	if err != nil {
		t.Fatalf("canonical-name load: %v", err)
	}
	if again.Name != a.Name {
		t.Fatalf("canonical load mismatch: %+v", again)
	}
}

func TestLoadAllApproachesReturnsEmptyOnMissingDir(t *testing.T) {
	root := t.TempDir()
	got, err := LoadAllApproaches(root)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatalf("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d", len(got))
	}
}

func TestLoadAllApproachesSortedByName(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zulu", "alpha", "mike"} {
		if err := SaveApproach(root, Approach{Name: name, Steps: []ApproachStep{{Anchor: "start"}}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadAllApproaches(root)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	want := []string{"alpha", "mike", "zulu"}
	for i := range names {
		if names[i] != want[i] {
			t.Fatalf("order mismatch: got %v want %v", names, want)
		}
	}
}

func TestApproachAnchorReturnsFirstStepAnchor(t *testing.T) {
	if (Approach{}).Anchor() != "" {
		t.Fatal("empty approach should report empty anchor")
	}
	a := Approach{Steps: []ApproachStep{{Anchor: "cold-launch", ID: "x"}, {ID: "y"}}}
	if got := a.Anchor(); got != "cold-launch" {
		t.Fatalf("got %q", got)
	}
}

func TestApproachIsStaleHonoursLastSuccess(t *testing.T) {
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	stale := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	cases := []struct {
		name string
		a    Approach
		ttl  time.Duration
		want bool
	}{
		{name: "fresh", a: Approach{LastSuccessAt: fresh}, ttl: 14 * 24 * time.Hour, want: false},
		{name: "stale", a: Approach{LastSuccessAt: stale}, ttl: 14 * 24 * time.Hour, want: true},
		{name: "no timestamps", a: Approach{}, ttl: 14 * 24 * time.Hour, want: false},
		{name: "fallback to RecordedAt", a: Approach{RecordedAt: stale}, ttl: 14 * 24 * time.Hour, want: true},
		{name: "ttl disabled", a: Approach{LastSuccessAt: stale}, ttl: 0, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.IsStale(tc.ttl, now); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestMatchingApproachesIgnoresAnchorMismatchAtStartScreen(t *testing.T) {
	// `applyBlindStartApproach` reuses `MatchingApproaches`
	// passing the configured start screen as the current screen.
	// This locks the contract that matches are anchor-based, not
	// observation-based — exactly what the blind-launch path
	// relies on when there's no observation to compare against.
	now := time.Now().UTC()
	fresh := now.Add(-time.Hour).Format(time.RFC3339)
	approaches := []Approach{
		{Name: "anchored-on-start", LastSuccessAt: fresh, Steps: []ApproachStep{{Anchor: "start"}}},
		{Name: "anchored-on-home", LastSuccessAt: fresh, Steps: []ApproachStep{{Anchor: "home"}}},
	}
	got := MatchingApproaches(approaches, "start", 14*24*time.Hour, now)
	if len(got) != 1 || got[0].Name != "anchored-on-start" {
		t.Fatalf("expected only start-anchored approach, got %+v", got)
	}
}

func TestExtractApproachStepsReadsTapsAndTypeFromCommandsLog(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "commands.jsonl")
	contents := strings.Join([]string{
		`{"action":"tap","status":"ok","id":"first_btn","driver":"appium","time":"2026-05-11T10:00:00Z"}`,
		`{"action":"delay","status":"ok","duration":"1s"}`,
		`{"action":"tap","status":"ok","text":"Continue","prefer-driver":"appium"}`,
		`{"action":"tap","status":"ok","x":"120","y":"240"}`,
		`{"action":"tap","status":"fail","id":"will_not_show"}`,
		`{"action":"type","status":"ok","chars":"5","driver":"appium"}`,
	}, "\n")
	if err := osWrite(log, contents); err != nil {
		t.Fatal(err)
	}
	steps, err := extractApproachSteps(log)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 4 {
		t.Fatalf("expected 3 tap steps + 1 type step, got %+v", steps)
	}
	if steps[0].ID != "first_btn" || steps[0].Driver != "appium" {
		t.Fatalf("step 0 = %+v", steps[0])
	}
	if steps[1].Text != "Continue" || steps[1].Driver != "appium" {
		t.Fatalf("step 1 = %+v", steps[1])
	}
	if steps[2].X != "120" || steps[2].Y != "240" {
		t.Fatalf("step 2 = %+v", steps[2])
	}
	if !steps[3].IsType() || steps[3].Type != "" || steps[3].TypeChars != 5 || steps[3].Driver != "appium" {
		t.Fatalf("step 3 should be a type placeholder with chars=5: %+v", steps[3])
	}
}

func TestCountUnfilledTypeStepsCountsOnlyUnfilled(t *testing.T) {
	steps := []ApproachStep{
		{ID: "tap_btn"},
		{Type: "filled", TypeChars: 6},
		{TypeChars: 8}, // placeholder waiting for operator
		{TypeChars: 4}, // another placeholder
		{Type: "also_filled"},
	}
	if got := countUnfilledTypeSteps(steps); got != 2 {
		t.Fatalf("got %d want 2", got)
	}
}

func TestApproachStepIsTypeDistinguishesActions(t *testing.T) {
	if (ApproachStep{ID: "btn"}).IsType() {
		t.Fatal("tap step should not report IsType")
	}
	if !(ApproachStep{Type: "hello"}).IsType() {
		t.Fatal("filled type step should report IsType")
	}
	if !(ApproachStep{TypeChars: 5}).IsType() {
		t.Fatal("unfilled type placeholder should still report IsType")
	}
}

func osWrite(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o644)
}

func TestMatchingApproachesFiltersAndSorts(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-time.Hour).Format(time.RFC3339)
	stale := now.Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	approaches := []Approach{
		{Name: "a-fresh", LastSuccessAt: fresh, Steps: []ApproachStep{{Anchor: "start"}}},
		{Name: "b-stale", LastSuccessAt: stale, Steps: []ApproachStep{{Anchor: "start"}}},
		{Name: "c-different-anchor", LastSuccessAt: fresh, Steps: []ApproachStep{{Anchor: "home"}}},
		{Name: "d-broken", FailureCount: 3, Steps: []ApproachStep{{Anchor: "start"}}},
	}
	got := MatchingApproaches(approaches, "start", 14*24*time.Hour, now)
	if len(got) != 2 {
		t.Fatalf("expected 2 matches (a-fresh, b-stale), got %+v", got)
	}
	if got[0].Name != "a-fresh" || got[1].Name != "b-stale" {
		t.Fatalf("expected fresh before stale, got %+v", got)
	}
}
