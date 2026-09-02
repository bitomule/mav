package mav

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// longRunDir builds a run directory whose natural worker.sock path is over
// the platform's sun_path limit, mirroring the real layout that produced
// this bug: a git worktree under .claude/worktrees/<branch-name>, whose
// .mav/runs/<id>/worker.sock measured 106 bytes against macOS's 104.
func longRunDir(t *testing.T) RunState {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "Projects", "Boxy", ".claude", "worktrees",
		"screenshots-y-contraste", ".mav", "runs", "671e7e28")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "671e7e28", Dir: dir}
	if natural := filepath.Join(dir, "worker.sock"); len(natural) <= maxUnixSocketPath {
		t.Fatalf("test fixture is not long enough to exercise the bug: %d bytes", len(natural))
	}
	return run
}

// TestUnixListenRejectsOverlongPath is the ablation's control: it pins the
// operating system behaviour the fix exists for, so a future reader can see
// the limit is real and not a guess. Without workerSocket's fallback this is
// exactly the error worker.log recorded three times ("bind: invalid
// argument") in every run started from a worktree.
func TestUnixListenRejectsOverlongPath(t *testing.T) {
	run := longRunDir(t)
	natural := filepath.Join(run.Dir, "worker.sock")
	listener, err := net.Listen("unix", natural)
	if err == nil {
		listener.Close()
		t.Fatalf("expected %d-byte socket path to be rejected, but it bound", len(natural))
	}
	if !strings.Contains(err.Error(), "invalid argument") {
		t.Fatalf("unexpected error binding %d-byte path: %v", len(natural), err)
	}
}

func TestWorkerSocketFallsBackWhenPathTooLong(t *testing.T) {
	run := longRunDir(t)
	socket := workerSocket(run)
	if len(socket) > maxUnixSocketPath {
		t.Fatalf("fallback socket is still too long: %d bytes, %s", len(socket), socket)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("fallback socket did not bind: %v", err)
	}
	listener.Close()
	os.Remove(socket)
}

func TestWorkerSocketFallbackIsStableAndPerRun(t *testing.T) {
	run := longRunDir(t)
	if workerSocket(run) != workerSocket(run) {
		t.Fatal("fallback socket path is not deterministic; a second mav process would never find the worker")
	}
	other := run
	other.ID = "620b0b12"
	other.Dir = filepath.Join(filepath.Dir(run.Dir), other.ID)
	if err := os.MkdirAll(other.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if workerSocket(run) == workerSocket(other) {
		t.Fatal("two runs collapsed onto one fallback socket; concurrent agents would share a worker")
	}
}

func TestWorkerSocketKeepsRunDirWhenItFits(t *testing.T) {
	// Deliberately not t.TempDir(): Go derives that name from the test's
	// own name, which on its own can exceed the limit this test is asserting
	// we stay under.
	dir, err := os.MkdirTemp("/tmp", "mav-short-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	run := RunState{ID: "abc", Dir: dir}
	if got, want := workerSocket(run), filepath.Join(run.Dir, "worker.sock"); got != want {
		t.Fatalf("short path was relocated unnecessarily: got %s want %s", got, want)
	}
}
