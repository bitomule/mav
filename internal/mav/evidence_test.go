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
	path, err := GenerateReport(run)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if !strings.Contains(html, "MAV Evidence") || !strings.Contains(html, "hello log") || !strings.Contains(html, "screen.png") {
		t.Fatalf("report missing content:\n%s", html)
	}
}
