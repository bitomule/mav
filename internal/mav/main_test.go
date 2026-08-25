package mav

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
)

// TestMain lets this test binary double as the mav executable itself: when
// launched with MAV_TEST_CHILD=1 it dispatches straight into Run and exits,
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
func TestMain(m *testing.M) {
	if os.Getenv("MAV_TEST_CHILD") == "1" {
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
