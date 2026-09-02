package mav

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWorkerSocketFallbackIgnoresTMPDIR pins the part of C2's fix that a
// process-local $TMPDIR must no longer steer the fallback path: two
// cooperating mav processes on the same run directory but with different
// TMPDIR values (a real scenario — one launched from a shell with an
// exported TMPDIR, one without) must still converge on the same socket, or
// the second one starts a redundant worker that nobody renews and that
// later gets reaped as "abandoned" out from under the first.
func TestWorkerSocketFallbackIgnoresTMPDIR(t *testing.T) {
	run := longRunDir(t)
	before := workerSocket(run)

	altTMP := t.TempDir()
	t.Setenv("TMPDIR", altTMP)

	after := workerSocket(run)
	if before != after {
		t.Fatalf("fallback socket changed with TMPDIR: before=%s after=%s", before, after)
	}
	if filepath.Dir(after) == altTMP {
		t.Fatalf("fallback socket followed TMPDIR into %s instead of staying on a fixed base", altTMP)
	}
}

// TestWorkerSocketFallbackIgnoresSymlinkSpelling pins the other half: two
// processes that reach the same physical run directory through different
// spellings (one via a symlink, one via the resolved path — exactly what
// os.Getwd's $PWD kludge can hand back) must compute the same socket.
// Before the fix, hashing run.Dir directly let the two spellings diverge.
func TestWorkerSocketFallbackIgnoresSymlinkSpelling(t *testing.T) {
	real := longRunDir(t)

	linkBase := t.TempDir()
	link := filepath.Join(linkBase, "via-symlink")
	if err := os.Symlink(real.Dir, link); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	viaSymlink := real
	viaSymlink.Dir = link

	realSocket := workerSocket(real)
	symlinkSocket := workerSocket(viaSymlink)
	if realSocket != symlinkSocket {
		t.Fatalf("fallback socket diverged by spelling: resolved=%s symlink=%s", realSocket, symlinkSocket)
	}
}
