package mav

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindRecentIPSFilesFiltersByNameAndMtime(t *testing.T) {
	root := t.TempDir()
	recent := filepath.Join(root, "FoodLabel-2026-06-17.ips")
	old := filepath.Join(root, "FoodLabel-old.ips")
	other := filepath.Join(root, "Boxy-2026-06-17.ips")
	for _, path := range []string{recent, old, other} {
		if err := os.WriteFile(path, []byte("ips"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := time.Now().Add(-10 * time.Minute)
	oldTime := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	got := findRecentIPSFiles([]string{root}, []string{"foodlabel"}, cutoff)
	if len(got) != 1 || got[0] != recent {
		t.Fatalf("got %v, want [%s]", got, recent)
	}
}

func TestCrashNameNeedlesIncludeBundleComponents(t *testing.T) {
	got := crashNameNeedles(Config{BundleID: "com.davidcollado.undolly.debug"})
	if !containsString(got, "undolly") {
		t.Fatalf("needles=%v", got)
	}
	if containsString(got, "debug") {
		t.Fatalf("should skip build flavor needles: %v", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
