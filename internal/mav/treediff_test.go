package mav

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTreeDiffDetectsAddedAndRemoved(t *testing.T) {
	prev := []Element{
		{ID: "btn-go", Label: "Go", Role: "Button"},
		{ID: "tf-email", Label: "Email", Role: "TextField"},
	}
	next := []Element{
		{ID: "btn-go", Label: "Go", Role: "Button"},
		{ID: "tf-password", Label: "Password", Role: "TextField"},
	}
	delta := TreeDiff(prev, next)
	if len(delta.Removed) != 1 || delta.Removed[0].ID != "tf-email" {
		t.Fatalf("expected tf-email removed, got %+v", delta.Removed)
	}
	if len(delta.Added) != 1 || delta.Added[0].ID != "tf-password" {
		t.Fatalf("expected tf-password added, got %+v", delta.Added)
	}
	if len(delta.Changed) != 0 {
		t.Fatalf("expected no changes, got %+v", delta.Changed)
	}
}

func TestTreeDiffDetectsFieldChanges(t *testing.T) {
	prev := []Element{
		{ID: "tf-email", Label: "Email", Value: "", Enabled: "true"},
	}
	next := []Element{
		{ID: "tf-email", Label: "Email", Value: "user@example.com", Enabled: "true"},
	}
	delta := TreeDiff(prev, next)
	if len(delta.Changed) != 1 {
		t.Fatalf("expected one change, got %+v", delta.Changed)
	}
	change := delta.Changed[0]
	if change.ID != "tf-email" {
		t.Fatalf("expected id=tf-email, got %q", change.ID)
	}
	if !strings.Contains(change.Diffs["value"], "→ user@example.com") {
		t.Fatalf("expected value diff, got %+v", change.Diffs)
	}
}

func TestTreeDiffPIDChangesIgnored(t *testing.T) {
	// PID changes (app relaunch) should not appear in the diff: they'd
	// generate noise on every cold start while the screen identity is
	// unchanged.
	prev := []Element{{ID: "root", Role: "Application", PID: "100"}}
	next := []Element{{ID: "root", Role: "Application", PID: "999"}}
	delta := TreeDiff(prev, next)
	if len(delta.Changed) != 0 {
		t.Fatalf("expected no changes (PID excluded), got %+v", delta.Changed)
	}
}

func TestTreeDiffFallsBackToStructuralKey(t *testing.T) {
	// Elements without ID are matched by role+label+frame.
	prev := []Element{{Role: "StaticText", Label: "Hola", Frame: "{0,0,100,40}"}}
	next := []Element{{Role: "StaticText", Label: "Hola", Frame: "{0,0,100,40}", Value: "after"}}
	delta := TreeDiff(prev, next)
	if len(delta.Changed) != 1 {
		t.Fatalf("expected one structural change, got %+v", delta.Changed)
	}
	if delta.Changed[0].Key == "" {
		t.Fatalf("expected Key set when ID empty, got %+v", delta.Changed[0])
	}
}

func TestTreeDiffStableOrdering(t *testing.T) {
	// Different insertion order should yield the same JSON.
	a := []Element{{ID: "z"}, {ID: "a"}, {ID: "m"}}
	b := []Element{}
	d1 := TreeDiff(b, a) // all Added
	d2 := TreeDiff(b, []Element{{ID: "m"}, {ID: "a"}, {ID: "z"}})
	j1, _ := json.Marshal(d1)
	j2, _ := json.Marshal(d2)
	if string(j1) != string(j2) {
		t.Fatalf("expected stable order; %s vs %s", j1, j2)
	}
}

func TestPersistTreeWritesCompactAndFull(t *testing.T) {
	dir := t.TempDir()
	// 90 elements -> compact caps at 80.
	raw := make([]Element, 0, 90)
	for i := 0; i < 90; i++ {
		raw = append(raw, Element{ID: id(i), Label: "L"})
	}
	got, err := PersistTree(dir, 1, "open-app", raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.CompactPath, "step-01_open-app.json") {
		t.Fatalf("unexpected compact path: %s", got.CompactPath)
	}
	if !strings.Contains(got.FullPath, "step-01_open-app.full.json") {
		t.Fatalf("unexpected full path: %s", got.FullPath)
	}
	if got.DeltaPath != "" {
		t.Fatalf("expected no delta when previous nil, got %s", got.DeltaPath)
	}
	if got.Hash == "" {
		t.Fatal("expected non-empty hash")
	}

	// Compact must have 80 entries; full must have 90.
	verifyLen(t, got.CompactPath, 80)
	verifyLen(t, got.FullPath, 90)
}

func TestPersistTreeWritesDeltaWhenPreviousProvided(t *testing.T) {
	dir := t.TempDir()
	prev := []Element{{ID: "a"}}
	next := []Element{{ID: "a"}, {ID: "b"}}
	got, err := PersistTree(dir, 2, "step two", next, prev)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeltaPath == "" {
		t.Fatal("expected delta path")
	}
	body, err := os.ReadFile(got.DeltaPath)
	if err != nil {
		t.Fatal(err)
	}
	var delta TreeDelta
	if err := json.Unmarshal(body, &delta); err != nil {
		t.Fatal(err)
	}
	if len(delta.Added) != 1 || delta.Added[0].ID != "b" {
		t.Fatalf("expected b added, got %+v", delta.Added)
	}
	// Filename slug must transform "step two" -> "step-two".
	if !strings.Contains(got.CompactPath, "step-02_step-two.json") {
		t.Fatalf("unexpected slug: %s", got.CompactPath)
	}
}

func TestLoadPersistedTreeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	raw := []Element{{ID: "btn", Label: "Go"}, {ID: "tf", Label: "Email"}}
	got, err := PersistTree(dir, 1, "x", raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPersistedTree(got.CompactPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded[0].ID != "btn" {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}
}

// --- helpers -------------------------------------------------------------

func id(i int) string {
	if i < 10 {
		return "id-0" + string(rune('0'+i))
	}
	return "id-" + string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func verifyLen(t *testing.T, path string, want int) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var elements []Element
	if err := json.Unmarshal(body, &elements); err != nil {
		t.Fatal(err)
	}
	if len(elements) != want {
		t.Fatalf("%s: got %d elements, want %d", filepath.Base(path), len(elements), want)
	}
}
