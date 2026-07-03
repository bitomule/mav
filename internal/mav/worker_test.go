package mav

import (
	"context"
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
