package mav

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEvidenceStepBackwardCompat asserts that an older JSON record (without
// the P4 tree/video fields) still unmarshals cleanly. The driver overhaul
// must not break previously written evidence.jsonl files.
func TestEvidenceStepBackwardCompat(t *testing.T) {
	old := []byte(`{"name":"open","file":"/tmp/x.png","kind":"screenshot","created_at":"2026-05-15T21:00:00Z"}`)
	var step EvidenceStep
	if err := json.Unmarshal(old, &step); err != nil {
		t.Fatal(err)
	}
	if step.Name != "open" || step.File != "/tmp/x.png" {
		t.Fatalf("unexpected step %+v", step)
	}
	// Optional fields must marshal back without leaking into the JSON.
	out, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"tree_path", "tree_hash", "monotonic_ms", "video_offset_ms"} {
		if strings.Contains(string(out), leaked) {
			t.Errorf("expected %s omitted when empty, got %s", leaked, out)
		}
	}
}

// TestEvidenceStepEmitsNewFields confirms a step populated with the new P4
// fields round-trips through JSON.
func TestEvidenceStepEmitsNewFields(t *testing.T) {
	step := EvidenceStep{
		Name:           "tap-go",
		File:           "/tmp/2.png",
		Kind:           "screenshot",
		CreatedAt:      "2026-05-15T21:00:00Z",
		TreePath:       "/tmp/trees/step-02_tap-go.json",
		TreeHash:       "deadbeef",
		MonotonicMs:    12345,
		VideoOffsetMs: 4500,
	}
	out, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"tree_path":"/tmp/trees/step-02_tap-go.json"`,
		`"tree_hash":"deadbeef"`,
		`"monotonic_ms":12345`,
		`"video_offset_ms":4500`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
}
