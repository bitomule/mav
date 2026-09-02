package mav

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// straddlingRunDir returns two spellings of one physical run directory: a
// short one whose worker.sock path fits in sun_path, and its canonical form
// which does not. That band is only a handful of bytes wide (macOS's
// /tmp -> /private/tmp is an 8-byte delta), and it is exactly where a
// workerSocket that measures the caller's raw spelling picks a different
// branch per process.
func straddlingRunDir(t *testing.T) (short string, canonical string) {
	t.Helper()
	deep := t.TempDir()
	for {
		resolved, err := filepath.EvalSymlinks(deep)
		if err != nil {
			t.Fatal(err)
		}
		if len(filepath.Join(resolved, "worker.sock")) > maxUnixSocketPath {
			canonical = resolved
			break
		}
		deep = filepath.Join(deep, "runs")
		if err := os.Mkdir(deep, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join("/tmp", fmt.Sprintf("mav-s%d", os.Getpid()))
	if err := os.Symlink(deep, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(link) })
	if got := len(filepath.Join(link, "worker.sock")); got > maxUnixSocketPath {
		t.Fatalf("fixture does not straddle the limit: short spelling is %d bytes", got)
	}
	return link, canonical
}

// TestWorkerSocketConvergesWhenSpellingsStraddleTheLimit is the case the
// fallback-only canonicalization could not reach: one process spells the run
// directory short enough to fit, another spells it long. If the length gate
// runs on the raw spelling they land on different sockets, so two workers
// serve one run and the idle one reaps it as abandoned.
func TestWorkerSocketConvergesWhenSpellingsStraddleTheLimit(t *testing.T) {
	short, canonical := straddlingRunDir(t)
	viaShort := workerSocket(RunState{ID: "671e7e28", Dir: short})
	viaCanonical := workerSocket(RunState{ID: "671e7e28", Dir: canonical})
	if viaShort != viaCanonical {
		t.Fatalf("spellings of one run dir diverged: %s vs %s", viaShort, viaCanonical)
	}
	if len(viaShort) > maxUnixSocketPath {
		t.Fatalf("socket path is unbindable: %d bytes, %s", len(viaShort), viaShort)
	}
	if strings.HasPrefix(viaShort, short+string(os.PathSeparator)) {
		t.Fatalf("socket was placed inside a spelling whose canonical form is over the limit: %s", viaShort)
	}
}

// TestWorkerSocketFallbackBaseIsPrivate pins that the fallback never lands
// directly on world-writable /tmp, where another local account could squat
// the derived path and force every worker start into direct mode.
func TestWorkerSocketFallbackBaseIsPrivate(t *testing.T) {
	run := longRunDir(t)
	socket := workerSocket(run)
	base := filepath.Dir(socket)
	if base == "/tmp" {
		t.Fatal("fallback socket sits directly in world-writable /tmp")
	}
	info, err := os.Lstat(base)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("fallback base is not private: %s mode %v", base, info.Mode())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		t.Fatalf("fallback base is not owned by this uid: %s", base)
	}
}
