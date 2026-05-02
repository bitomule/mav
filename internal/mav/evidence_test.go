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
	stepsDir := filepath.Join(dir, "maestro")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "mav_step_00_launch.png"), []byte("png"), 0o644); err != nil {
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
	if !strings.Contains(html, "MAV Evidence") || !strings.Contains(html, "hello log") || !strings.Contains(html, "screen.png") || !strings.Contains(html, "mav_step_00_launch.png") {
		t.Fatalf("report missing content:\n%s", html)
	}
}
