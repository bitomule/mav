package mav

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAttachStepTreePersistsAndDiffs verifies that attachStepTree on two
// sequential calls writes step-01 / step-02 trees plus a delta against the
// previous step, and populates the EvidenceStep TreePath/TreeHash/DeltaPath.
func TestAttachStepTreePersistsAndDiffs(t *testing.T) {
	root := t.TempDir()

	tree1 := `[{"AXIdentifier":"btn-go","AXLabel":"Go","role":"Button"}]`
	tree2 := `[{"AXIdentifier":"btn-back","AXLabel":"Back","role":"Button"}]`

	cli := CLI{
		Runner: fakeRunner{
			tools: map[string]bool{"axe": true},
			seq: map[string][]string{
				"axe describe-ui --udid TEST-UDID": {tree1, tree2},
			},
			calls: map[string]int{},
		},
		Root:   root,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	cfg := Config{
		Root:          root,
		SimulatorUDID: "TEST-UDID",
		Tools:         map[string]bool{"axe": true},
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: no previous tree -> no delta expected.
	s1 := EvidenceStep{Name: "open", File: "/tmp/1.png", Kind: "screenshot"}
	attachStepTree(context.Background(), cli, cfg, run, &s1, 1, "open")
	if s1.TreePath == "" || !strings.Contains(s1.TreePath, "step-01_open.json") {
		t.Fatalf("step 1 TreePath=%q", s1.TreePath)
	}
	if s1.DeltaPath != "" {
		t.Fatalf("step 1 should have no delta, got %q", s1.DeltaPath)
	}
	if s1.TreeHash == "" {
		t.Fatalf("step 1 should have a tree hash")
	}
	if err := AppendEvidenceStep(run, s1); err != nil {
		t.Fatal(err)
	}

	// Step 2: previous tree present -> delta expected.
	s2 := EvidenceStep{Name: "navigate", File: "/tmp/2.png", Kind: "screenshot"}
	attachStepTree(context.Background(), cli, cfg, run, &s2, 2, "navigate")
	if s2.TreePath == "" || !strings.Contains(s2.TreePath, "step-02_navigate.json") {
		t.Fatalf("step 2 TreePath=%q", s2.TreePath)
	}
	if s2.DeltaPath == "" {
		t.Fatalf("step 2 should have a delta against step 1")
	}
	if s2.TreeHash == s1.TreeHash {
		t.Fatalf("step 2 hash should differ from step 1 (trees differ)")
	}

	// Inspect the delta JSON: btn-back added, btn-go removed.
	body, err := os.ReadFile(s2.DeltaPath)
	if err != nil {
		t.Fatal(err)
	}
	var delta TreeDelta
	if err := json.Unmarshal(body, &delta); err != nil {
		t.Fatal(err)
	}
	if len(delta.Added) != 1 || delta.Added[0].ID != "btn-back" {
		t.Fatalf("expected btn-back added, got %+v", delta.Added)
	}
	if len(delta.Removed) != 1 || delta.Removed[0].ID != "btn-go" {
		t.Fatalf("expected btn-go removed, got %+v", delta.Removed)
	}
}

func TestAttachStepTimingsRecordsMonotonic(t *testing.T) {
	root := t.TempDir()
	run, _ := NewProjectRunState(root)

	step := EvidenceStep{}
	before := time.Now().UnixMilli()
	attachStepTimings(run, &step)
	after := time.Now().UnixMilli()

	if step.MonotonicMs < before || step.MonotonicMs > after {
		t.Fatalf("MonotonicMs=%d not in [%d,%d]", step.MonotonicMs, before, after)
	}
	if step.VideoOffsetMs != 0 {
		t.Fatalf("expected VideoOffsetMs=0 when no video.start.ms, got %d", step.VideoOffsetMs)
	}
}

func TestAttachStepTimingsComputesVideoOffset(t *testing.T) {
	root := t.TempDir()
	run, _ := NewProjectRunState(root)
	videoStart := time.Now().UnixMilli() - 1500
	mustWrite(t, filepath.Join(run.Dir, "video.start.ms"), itoaWithNewline(videoStart))

	step := EvidenceStep{}
	attachStepTimings(run, &step)
	if step.VideoOffsetMs < 1500 || step.VideoOffsetMs > 2500 {
		t.Fatalf("expected VideoOffsetMs ~1500, got %d", step.VideoOffsetMs)
	}
}

func itoaWithNewline(n int64) string {
	return formatInt(n) + "\n"
}

func formatInt(n int64) string {
	// Hand-rolled to avoid importing strconv just for the helper. Negative
	// values aren't expected here but handled for completeness.
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
