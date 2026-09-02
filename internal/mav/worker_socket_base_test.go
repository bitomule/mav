package mav

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWorkerSocketBaseRevertSkipsTempDir pins R3-C1: when the primary
// /tmp/mav-<uid>-style base fails verification (here: squatted by a plain
// file so MkdirAll fails), the revert must land on the deterministic
// secondary base, not on os.TempDir() -- os.TempDir is $TMPDIR-or-/tmp,
// which is exactly the per-caller / world-writable pair the function's own
// doc comment disqualifies.
func TestWorkerSocketBaseRevertSkipsTempDir(t *testing.T) {
	uid := os.Getuid()
	root := t.TempDir()

	primary := filepath.Join(root, "primary")
	if err := os.WriteFile(primary, []byte("squatter"), 0o600); err != nil {
		t.Fatal(err)
	}
	secondary := filepath.Join(root, "secondary", "sock")

	got := workerSocketBaseFrom(uid, primary, secondary)
	if got != secondary {
		t.Fatalf("revert from failed primary landed on %s, want secondary %s", got, secondary)
	}
	info, err := os.Lstat(secondary)
	if err != nil || !info.IsDir() {
		t.Fatalf("secondary base was not created as a directory: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("secondary base created with group/other bits: %o", perm)
	}
}

// TestWorkerSocketBaseRevertRejectsLoosePrimary covers the verification
// branch (dir exists but is group/other-accessible): the revert must again
// prefer the secondary base over os.TempDir().
func TestWorkerSocketBaseRevertRejectsLoosePrimary(t *testing.T) {
	uid := os.Getuid()
	root := t.TempDir()

	primary := filepath.Join(root, "primary")
	if err := os.Mkdir(primary, 0o755); err != nil {
		t.Fatal(err)
	}
	secondary := filepath.Join(root, "secondary")

	got := workerSocketBaseFrom(uid, primary, secondary)
	if got != secondary {
		t.Fatalf("loose-perm primary reverted to %s, want secondary %s", got, secondary)
	}
}

// TestWorkerSocketBaseLastResortIsTempDir pins the documented last resort:
// only when every candidate fails does os.TempDir() get returned.
func TestWorkerSocketBaseLastResortIsTempDir(t *testing.T) {
	uid := os.Getuid()
	root := t.TempDir()

	primary := filepath.Join(root, "primary")
	secondary := filepath.Join(root, "secondary")
	for _, p := range []string{primary, secondary} {
		if err := os.WriteFile(p, []byte("squatter"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got := workerSocketBaseFrom(uid, primary, secondary)
	if got != os.TempDir() {
		t.Fatalf("with all candidates failing got %s, want os.TempDir()=%s", got, os.TempDir())
	}
}

// TestWorkerSocketBaseSecondaryIsHomeDerived pins that the production
// secondary candidate is uid-deterministic (home-derived) rather than
// TMPDIR-derived: workerSocketBase must not change when TMPDIR does, even
// when /tmp/mav-<uid> is healthy or unhealthy alike.
func TestWorkerSocketBaseSecondaryIsHomeDerived(t *testing.T) {
	before := workerSocketBase()
	t.Setenv("TMPDIR", t.TempDir())
	after := workerSocketBase()
	if before != after {
		t.Fatalf("workerSocketBase moved with TMPDIR: before=%s after=%s", before, after)
	}
}
