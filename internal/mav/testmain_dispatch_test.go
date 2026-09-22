package mav

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSpawnedWorkerArgvRunsMavNotTheSuite is the regression test for the
// self-replicating test binary.
//
// mav starts its run worker as `os.Executable() __worker ...` and hands it
// whatever environment the starter had (startRunWorker, worker.go). A test
// that drives a CLI in-process with a real ExecRunner is therefore the
// starter, and its environment carries no MAV_TEST_CHILD -- so a TestMain
// that keys only on that variable lets the "worker" fall through to m.Run()
// and re-run the whole suite, which starts more of them. One `go test ./...`
// left ~400 such processes, each holding a 15-minute lease, and filled the
// machine's process table twice.
//
// `__worker` with no --socket is the cheapest probe there is: a real mav
// refuses it instantly with worker_socket_missing, while a test binary that
// missed the guard starts running tests and does not exit at all.
func TestSpawnedWorkerArgvRunsMavNotTheSuite(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(self, "__worker")
	// Deliberately without MAV_TEST_CHILD: that is exactly the environment
	// mav's own startRunWorker would hand it.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	// Its own process group, so a binary that did fall through to the suite
	// takes the clones it managed to start down with it below.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		if !strings.Contains(out.String(), "worker_socket_missing") {
			t.Fatalf("spawned `__worker` exited without refusing the missing socket, so it did not dispatch into mav; output=%q", out.String())
		}
	case <-time.After(15 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatalf("spawned `__worker` never exited: it fell through to m.Run() and is re-running the suite. Output so far:\n%s", out.String())
	}
}
