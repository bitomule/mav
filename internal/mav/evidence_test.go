package mav

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateReport(t *testing.T) {
	dir := t.TempDir()
	run := RunState{ID: "abc", Dir: dir, LogsPath: filepath.Join(dir, "logs.txt")}
	if err := os.WriteFile(run.LogsPath, []byte("hello log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "screen.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	stepFile := filepath.Join(dir, "steps", "01_notifications-before.png")
	if err := os.MkdirAll(filepath.Dir(stepFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stepFile, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvidenceStep(run, EvidenceStep{Name: "notifications-before", Note: "before toggling notifications", File: stepFile}); err != nil {
		t.Fatal(err)
	}
	path, err := GenerateReport(run)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{"MAV Evidence", "hello log", "screen.png", "notifications-before", "before toggling notifications", "max-width: 390px", "Verification Timeline"} {
		if !strings.Contains(html, want) {
			t.Fatalf("report missing %q:\n%s", want, html)
		}
	}
}

func TestSafeFileName(t *testing.T) {
	if got := safeFileName("Notifications: After Toggle"); got != "notifications-after-toggle" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadEvidenceSteps(t *testing.T) {
	dir := t.TempDir()
	run := RunState{ID: "abc", Dir: dir}
	if err := AppendEvidenceStep(run, EvidenceStep{Name: "one", File: filepath.Join(dir, "one.png")}); err != nil {
		t.Fatal(err)
	}
	steps := LoadEvidenceSteps(run)
	if len(steps) != 1 || steps[0].Name != "one" || steps[0].Kind != "screenshot" {
		t.Fatalf("steps=%+v", steps)
	}
}
