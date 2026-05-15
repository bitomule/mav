package mav

import "testing"

func TestParseCrashNamesEmpty(t *testing.T) {
	if got := parseCrashNames(""); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
	if got := parseCrashNames("no crashes\n"); len(got) != 0 {
		t.Fatalf("expected empty on 'no crashes' line, got %v", got)
	}
}

func TestParseCrashNamesBasic(t *testing.T) {
	stdout := `Boxy-2026-05-15-210045
Boxy-2026-05-15-220012
`
	got := parseCrashNames(stdout)
	if len(got) != 2 {
		t.Fatalf("expected 2 names, got %d (%v)", len(got), got)
	}
	if got[0] != "Boxy-2026-05-15-210045" {
		t.Errorf("got[0]=%q", got[0])
	}
}

func TestParseCrashNamesTolerantOfDecoration(t *testing.T) {
	// Some idb versions print "* <name>" or "- <name>".
	stdout := "- Boxy-A\n* Boxy-B\n  Boxy-C\n"
	got := parseCrashNames(stdout)
	want := []string{"Boxy-A", "Boxy-B", "Boxy-C"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got=%q want=%q", i, got[i], want[i])
		}
	}
}
