package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func execStepCLI(t *testing.T) (CLI, RunState) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.AllowShell = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run := RunState{ID: "abc", Dir: filepath.Join(t.TempDir(), "run"), LogsPath: filepath.Join(t.TempDir(), "logs.txt")}
	if err := os.MkdirAll(run.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return CLI{Runner: fakeRunner{}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}, run
}

// TestExecStepDoesNotBlockOnOrphanHoldingThePipe reproduces the hang that
// stranded a screenshot pipeline for 6h46m against 4.46s of CPU: the shell
// exits promptly, but a grandchild it started inherits the step's stdout and
// stderr pipes and keeps them open. With Stdout set to a buffer and no
// WaitDelay, Wait reads those pipes until EOF, so the step outlives the shell
// by however long the grandchild lives — measured in the wild as a bazel
// client that had been orphaned to launchd and was going nowhere.
func TestExecStepDoesNotBlockOnOrphanHoldingThePipe(t *testing.T) {
	cli, run := execStepCLI(t)
	type result struct {
		fields map[string]string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		fields, err := cli.execFlowShell(context.Background(), run, 1,
			map[string]string{"cmd": "sleep 120 & echo started", "timeout": "2s"})
		done <- result{fields, err}
	}()
	select {
	case r := <-done:
		// The shell itself exited successfully; only an orphan grandchild
		// kept the pipes open. The step's verdict must reflect that success,
		// not the outer context deadline that only the orphan overran.
		if r.err != nil {
			t.Fatalf("expected the shell's success to stand, got err=%v fields=%v", r.err, r.fields)
		}
		if r.fields["exit_code"] != "0" {
			t.Fatalf("expected exit_code 0, got fields=%v", r.fields)
		}
		data, readErr := os.ReadFile(r.fields["stdout"])
		if readErr != nil {
			t.Fatalf("could not read captured stdout: %v", readErr)
		}
		if !strings.Contains(string(data), "started") {
			t.Fatalf("expected captured stdout to contain %q, got %q", "started", string(data))
		}
	case <-time.After(20 * time.Second):
		t.Fatal("exec step is still waiting on a pipe its own child no longer holds")
	}
}

// TestExecStepKillsTheWholeProcessGroupOnTimeout covers the other half of the
// same leak: when the step times out, signalling only the direct shell leaves
// its children reparented to launchd (`ppid=1`) with nothing left to collect
// them. The real pipelines left `make` and `bazelisk` behind exactly this way.
func TestExecStepKillsTheWholeProcessGroupOnTimeout(t *testing.T) {
	cli, run := execStepCLI(t)
	pidFile := filepath.Join(run.Dir, "grandchild.pid")
	fields, err := cli.execFlowShell(context.Background(), run, 1, map[string]string{
		"cmd":     "sh -c 'echo $$ > " + pidFile + "; exec sleep 120' & wait",
		"timeout": "2s",
	})
	if err == nil || err.Error() != "exec_timeout" {
		t.Fatalf("expected exec_timeout, got fields=%v err=%v", fields, err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("grandchild never recorded its pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("grandchild %d survived the step's timeout; it would now be reparented to launchd", pid)
}

// TestExecStepShellDoesNotSurviveMav is the third leak of the same family,
// and the one that actually filled this machine: every guard on the step
// (its timeout, its Cancel, its WaitDelay) runs inside MAV, so SIGKILLing
// MAV mid-step leaves the shell running with nothing left to stop it. The
// flow here waits on a file that is never created, exactly like the
// `until [ -f ... ]; do sleep 0.05; done` barriers in concurrent_run_test.go
// whose orphans were found still polling long after the temp directory
// holding the file they wait for had been deleted.
func TestExecStepShellDoesNotSurviveMav(t *testing.T) {
	root := mkShortRoot(t)
	writeConcurrencyConfig(t, root)
	binDir := writeFakeXcrun(t)

	coord := t.TempDir()
	shellPIDPath := filepath.Join(coord, "shell.pid")
	never := filepath.Join(coord, "never")
	flowPath := filepath.Join(root, "waiter.yaml")
	flow := "name: waiter\nsteps:\n  - exec:\n      cmd: 'echo $$ > \"" + shellPIDPath +
		"\"; until [ -f \"" + never + "\" ]; do sleep 0.05; done'\n      timeout: 120s\n"
	if err := os.WriteFile(flowPath, []byte(flow), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd, stdout, stderr := startChild(t, ctx, root, flowPath, binDir, t.TempDir())

	var shellPID int
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if pid, err := readPID(shellPIDPath); err == nil && processAlive(pid) {
			shellPID = pid
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if shellPID == 0 {
		t.Fatalf("exec step's shell never reported its pid\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	// Belt and braces: a failing assertion below must not leave behind the
	// very orphan this test exists to forbid.
	t.Cleanup(func() { _ = syscall.Kill(-shellPID, syscall.SIGKILL) })

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill mav child: %v", err)
	}
	// Deliberately not reaped: a harness that kills MAV and walks away
	// (concurrent_run_test.go's own t.Cleanup does exactly this) leaves it a
	// zombie for as long as the harness lives, and a zombie still answers
	// `kill -0`. That is the state the first version of this fix got wrong,
	// so the test has to reproduce it rather than tidy it away with a Wait.

	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(shellPID) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("exec step's shell (pid %d) outlived the mav process that started it; it is now reparented to launchd, polling forever for %s", shellPID, never)
}
