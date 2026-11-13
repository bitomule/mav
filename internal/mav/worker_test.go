package mav

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func workerTestSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mav-worker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "worker.sock")
}

type socketTestWorker struct {
	socket   string
	ready    chan struct{}
	done     chan error
	finished bool
}

func startSocketTestWorker(t *testing.T, socket string, chmod func(string, os.FileMode) error) *socketTestWorker {
	t.Helper()
	w := &socketTestWorker{socket: socket, ready: make(chan struct{}), done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	prepared := make(chan *workerSocketListener, 1)
	go func() {
		w.done <- (CLI{}).runInternalWorkerWithStartup(ctx, []string{"--socket", socket}, workerStartup{
			listen: func(socket string) (*workerSocketListener, error) {
				listener, err := listenWorkerSocket(socket, chmod)
				prepared <- listener
				return listener, err
			},
			ready: func() { close(w.ready) },
		})
	}()
	t.Cleanup(func() {
		cancel()
		if !w.finished {
			// Close the actual listener even when a regression announces readiness
			// too early or removes/replaces its pathname. Teardown must not depend
			// on the startup behaviour these tests are trying to disprove.
			select {
			case listener := <-prepared:
				if listener != nil {
					_ = listener.Close()
				}
			case <-time.After(2 * time.Second):
				t.Fatal("worker preparation did not finish during cleanup")
			}
			if err := w.waitDone(t); err != nil {
				t.Errorf("worker cleanup: %v", err)
			}
		}
	})
	return w
}

func (w *socketTestWorker) waitReady(t *testing.T) {
	t.Helper()
	select {
	case <-w.ready:
	case err := <-w.done:
		w.finished = true
		t.Fatalf("worker exited before readiness: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not become ready")
	}
}

func (w *socketTestWorker) waitDone(t *testing.T) error {
	t.Helper()
	select {
	case err := <-w.done:
		w.finished = true
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop")
		return nil
	}
}

func (w *socketTestWorker) request(command string) error {
	conn, err := net.DialTimeout("unix", w.socket, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	return socketTestRequest(conn, command)
}

func socketTestRequest(conn net.Conn, command string) error {
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(conn).Encode(workerRequest{Command: command}); err != nil {
		return err
	}
	var response workerResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return err
	}
	if !response.OK {
		return errors.New(response.Error)
	}
	return nil
}

func assertWorkerSocketMode(t *testing.T, socket string) {
	t.Helper()
	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode=%o want 600", info.Mode().Perm())
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatal("worker path is not a socket")
	}
}

func TestWorkerSocketIsPrivateAndStopsCleanly(t *testing.T) {
	socket := workerTestSocket(t)
	w := startSocketTestWorker(t, socket, os.Chmod)
	w.waitReady(t)
	assertWorkerSocketMode(t, socket)
	if err := w.request("ping"); err != nil {
		t.Fatal(err)
	}
	if err := w.request("stop"); err != nil {
		t.Fatal(err)
	}
	if err := w.waitDone(t); err != nil {
		t.Fatal(err)
	}
	if exists(socket) {
		t.Fatal("worker socket was not removed")
	}
}

func TestWorkerSocketWaitsForPermissionsBeforeListening(t *testing.T) {
	socket := workerTestSocket(t)
	bound := make(chan struct{})
	proceed := make(chan struct{})
	release := sync.OnceFunc(func() { close(proceed) })
	w := startSocketTestWorker(t, socket, func(path string, mode os.FileMode) error {
		close(bound)
		<-proceed
		return os.Chmod(path, mode)
	})
	// Unblock startup before the worker cleanup, including on assertion failures.
	t.Cleanup(release)
	select {
	case <-bound:
	case err := <-w.done:
		w.finished = true
		t.Fatalf("worker exited before chmod: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not bind")
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("socket must exist at the barrier: %v", err)
	}
	select {
	case <-w.ready:
		t.Error("worker announced readiness before chmod")
	default:
	}
	// The owner can write the socket even with a restrictive inherited umask.
	// ECONNREFUSED therefore proves it is not listening, rather than depending
	// on permissions to hide an already active listener.
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err == nil {
		_ = conn.Close()
		t.Error("socket accepted a connection before chmod")
	} else if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("connect before chmod: got %v, want ECONNREFUSED", err)
	}
	release()
	w.waitReady(t)
	assertWorkerSocketMode(t, socket)
	if err := w.request("ping"); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerSocketChmodFailureCleansUp(t *testing.T) {
	socket := workerTestSocket(t)
	wantErr := errors.New("injected chmod failure")
	w := startSocketTestWorker(t, socket, func(path string, mode os.FileMode) error {
		if _, err := os.Stat(path); err != nil {
			return err
		}
		return wantErr
	})
	if err := w.waitDone(t); !errors.Is(err, wantErr) {
		t.Fatalf("startup error=%v, want chmod failure", err)
	}
	select {
	case <-w.ready:
		t.Error("worker announced readiness after chmod failure")
	default:
	}
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remains after chmod failure: %v", err)
	}
	// A failed start must leave the same address available to a new worker.
	retry := startSocketTestWorker(t, socket, os.Chmod)
	retry.waitReady(t)
	assertWorkerSocketMode(t, socket)
	if err := retry.request("ping"); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerSocketStopKeepsReplacement(t *testing.T) {
	socket := workerTestSocket(t)
	original := startSocketTestWorker(t, socket, os.Chmod)
	original.waitReady(t)
	// Keep a connection to the old listener so it can be stopped after the
	// pathname belongs to a different worker.
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	replacement := startSocketTestWorker(t, socket, os.Chmod)
	replacement.waitReady(t)
	if err := socketTestRequest(conn, "stop"); err != nil {
		t.Fatal(err)
	}
	if err := original.waitDone(t); err != nil {
		t.Fatal(err)
	}
	assertWorkerSocketMode(t, socket)
	if err := replacement.request("ping"); err != nil {
		t.Fatalf("stopping original worker removed replacement: %v", err)
	}
}

func TestWorkerSocketChmodFailureKeepsReplacement(t *testing.T) {
	socket := workerTestSocket(t)
	wantErr := errors.New("injected chmod failure after replacement")
	var replacement net.Listener
	_, err := listenWorkerSocket(socket, func(path string, mode os.FileMode) error {
		if err := os.Remove(path); err != nil {
			return err
		}
		var err error
		replacement, err = net.Listen("unix", path)
		if err != nil {
			return err
		}
		return wantErr
	})
	if replacement != nil {
		defer replacement.Close()
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("startup error=%v, want chmod failure", err)
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatalf("failed startup removed replacement: %v", err)
	}
	_ = conn.Close()
}

func TestWorkerSocketReplacementDuringStartupIsRejected(t *testing.T) {
	socket := workerTestSocket(t)
	var replacement net.Listener
	w := startSocketTestWorker(t, socket, func(path string, mode os.FileMode) error {
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		var err error
		replacement, err = net.Listen("unix", path)
		return err
	})
	err := w.waitDone(t)
	if replacement != nil {
		defer replacement.Close()
	}
	if err == nil || err.Error() != "worker_socket_replaced" {
		t.Fatalf("startup error=%v, want worker_socket_replaced", err)
	}
	select {
	case <-w.ready:
		t.Error("replaced worker announced readiness")
	default:
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatalf("rejected startup removed replacement: %v", err)
	}
	_ = conn.Close()
}

func TestAcquireWorkerLockStealsStaleLock(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "mav-worker-lock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	lockPath := filepath.Join(dir, "worker.starting")
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-(workerLockStaleAge + time.Second))
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireWorkerLock(lockPath)
	if err != nil {
		t.Fatalf("expected stale lock to be stolen, got err: %v", err)
	}
	defer lock.Close()
}

func TestAcquireWorkerLockRespectsFreshLock(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "mav-worker-lock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	lockPath := filepath.Join(dir, "worker.starting")
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireWorkerLock(lockPath); err == nil {
		t.Fatal("expected fresh lock to be respected, got no error")
	}
}

func TestWorkerLeaseRenewsThenExpires(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "mav-worker-lease-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM-LEASE"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeSimulatorLock(cfg.SimulatorUDID, run, root, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	appendProcess(run, "worker", os.Getpid(), "test worker")
	socket := workerSocket(run)
	cli := CLI{Root: root, Stdout: io.Discard}
	done := make(chan error, 1)
	go func() {
		done <- cli.runInternalWorker(context.Background(), []string{
			"--socket", socket,
			"--root", root,
			"--run", run.ID,
			"--lease", "150ms",
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !workerPing(run) && time.Now().Before(deadline) {
		select {
		case workerErr := <-done:
			if workerErr != nil && strings.Contains(workerErr.Error(), "operation not permitted") {
				t.Skip("sandbox does not permit Unix listeners")
			}
			t.Fatalf("worker exited before lease test: %v", workerErr)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !workerPing(run) {
		t.Fatal("worker did not start")
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := sendWorkerRequest(run, workerRequest{Command: "renew"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if !workerPing(run) {
		t.Fatal("worker expired before the renewed lease")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not expire")
	}
	if exists(socket) {
		t.Fatal("worker socket remains after lease expiration")
	}
	if !exists(filepath.Join(run.Dir, "lease.expired")) {
		t.Fatal("lease expiration was not recorded")
	}
	if _, ok := readSimulatorLock(cfg.SimulatorUDID); ok {
		t.Fatal("simulator lock remains after lease expiration")
	}
	if data, err := os.ReadFile(run.LogsPath); err != nil || !strings.Contains(string(data), "lease expired") {
		t.Fatalf("lease expiration missing from logs: %q err=%v", string(data), err)
	}
}

// TestWorkerLeaseExpiryStopsAbandonedVideoRecording reproduces the leak
// reported for `mav run`: its parent tree (an external wrapper plus the mav
// process itself) gets SIGKILLed mid-flight, so nobody is ever left to call
// `mav evidence stop`. The recorder survives the kill because Setpgid gives
// it its own process group -- that's necessary for stopProcess's group-kill
// to work on a clean stop, not a bug -- so the run's worker lease expiry is
// the only reaper left. It must actually kill the recorder, not just the
// other bookkeeping.
func TestWorkerLeaseExpiryStopsAbandonedVideoRecording(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "mav-worker-video-lease-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}

	// Stand-in for `xcrun simctl io <udid> recordVideo ...`, started exactly
	// the way ExecRunner.Start launches every auxiliary process mav owns
	// (own process group, pid registered in processes.jsonl, video.pid
	// marking it as still recording).
	pid, err := ExecRunner{}.Start(context.Background(), filepath.Join(run.Dir, "video.log"), "sleep", "30")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	appendProcess(run, "video", pid, "sleep 30")
	if err := os.WriteFile(filepath.Join(run.Dir, "video.pid"), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !processAlive(pid) {
		t.Fatal("stand-in recorder did not start")
	}

	appendProcess(run, "worker", os.Getpid(), "test worker")
	socket := workerSocket(run)
	cli := CLI{Root: root, Stdout: io.Discard}
	done := make(chan error, 1)
	go func() {
		done <- cli.runInternalWorker(context.Background(), []string{
			"--socket", socket,
			"--root", root,
			"--run", run.ID,
			"--lease", "150ms",
		})
	}()

	// No renew: this models the run's owner being gone, not a busy gap
	// between commands.
	select {
	case workerErr := <-done:
		if workerErr != nil {
			if strings.Contains(workerErr.Error(), "operation not permitted") {
				t.Skip("sandbox does not permit Unix listeners")
			}
			t.Fatal(workerErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not expire and exit")
	}

	deadline := time.Now().Add(2 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("video recorder pid=%d is still running after its run was abandoned and the worker lease expired", pid)
	}
}
