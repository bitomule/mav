package mav

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain lets this test binary double as the mav executable itself: when
// it is started as a mav command -- MAV_TEST_CHILD=1, or a mav command line
// in argv (see mavCommandArgs) -- it dispatches straight into Run and exits,
// instead of running the test suite. The concurrent-run tests re-exec this
// same binary (os.Executable()) as real child/grandchild OS processes --
// real fork/exec, real PIDs, real process groups -- because the bug this
// change set fixes lives in cross-process state (files on disk, unix
// sockets, signals). A goroutine-only test cannot exercise any of that.
//
// Mirrors cmd/mav/main.go's error handling: CommandFailed means a structured
// failure line was already written to stdout, so nothing more to print;
// anything else is an unexpected error (e.g. a worker subprocess failing to
// start) and goes to stderr so a failing test's captured output shows it
// instead of a silent exit 1.
// mavCommandArgs reports whether this process was started as mav rather than
// as a test binary. The first argument decides it: `go test` passes only
// `-test.*` flags, and every mav command line starts with a subcommand.
//
// The marker env var alone is not enough, and that gap cost this machine its
// process table twice. mav starts its own run worker as `os.Executable()
// __worker ...` with whatever environment the starter happened to have
// (startRunWorker, worker.go). When the starter is this test binary running a
// CLI in-process with a real ExecRunner -- open_probe_logs_leak_test.go,
// evidence_start_after_steps_test.go, target_command_test.go all do -- that
// environment has no MAV_TEST_CHILD. The guard missed, the "worker" fell
// through to m.Run(), and re-ran the entire suite, which started more of
// them. Measured before this fix: 214 processes whose argv was `__worker` had
// forked a `mav run`, which a real worker cannot do -- it only listens on a
// socket. One `go test ./...` left ~400 of them, each holding a 15-minute
// lease, and took the machine to 2530 processes against a 2666 limit.
func mavCommandArgs() bool {
	return len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-")
}

func TestMain(m *testing.M) {
	if os.Getenv("MAV_TEST_CHILD") == "1" || mavCommandArgs() {
		code := 0
		if err := Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
			var failed CommandFailed
			if !errors.As(err, &failed) {
				fmt.Fprintln(os.Stderr, err)
			}
			code = 1
		}
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func TestExecRunnerStartDiscardsOutputWithoutALogPath(t *testing.T) {
	// Launching a macOS app has no log file to point at: its real channel
	// is OSLog, which mav captures separately. Before, this died with
	// "open : no such file or directory", which said nothing about the real
	// cause.
	pid, err := ExecRunner{}.Start(context.Background(), "", "/usr/bin/true")
	if err != nil {
		t.Fatalf("an empty logPath must mean discard, not fail: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid=%d", pid)
	}
}
