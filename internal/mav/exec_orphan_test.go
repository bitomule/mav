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
	done := make(chan error, 1)
	go func() {
		_, err := cli.execFlowShell(context.Background(), run, 1,
			map[string]string{"cmd": "sleep 120 & echo started", "timeout": "2s"})
		done <- err
	}()
	select {
	case <-done:
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
