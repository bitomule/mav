package mav

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkerSocketIsPrivateAndStopsCleanly(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "mav-worker-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "worker.sock")
	cli := CLI{}
	done := make(chan error, 1)
	go func() { done <- cli.runInternalWorker(context.Background(), []string{"--socket", socket}) }()
	deadline := time.Now().Add(2 * time.Second)
	for !exists(socket) && time.Now().Before(deadline) {
		select {
		case workerErr := <-done:
			if workerErr != nil && strings.Contains(workerErr.Error(), "operation not permitted") {
				t.Skip("sandbox does not permit Unix listeners")
			}
			t.Fatalf("worker exited before creating socket: %v", workerErr)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode=%o want 600", info.Mode().Perm())
	}
	run := RunState{Dir: dir}
	if _, err := sendWorkerRequest(run, workerRequest{Command: "stop"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop")
	}
	if exists(socket) {
		t.Fatal("worker socket was not removed")
	}
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
