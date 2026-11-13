package mav

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func TestExecRunnerSeparatesBinaryOutputAndDiagnostics(t *testing.T) {
	var stdout, stderr bytes.Buffer
	result := (ExecRunner{}).RunStreams(context.Background(), &stdout, &stderr, "/bin/sh", "-c", `printf '\000\001\377'; printf 'diagnostic' >&2`)
	if result.Err != nil || !bytes.Equal(stdout.Bytes(), []byte{0, 1, 255}) || stderr.String() != "diagnostic" {
		t.Fatal(result, stdout.Bytes(), stderr.String())
	}
}
func TestExecRunnerStdinIsPassedToChild(t *testing.T) {
	result := (ExecRunner{}).RunInput(context.Background(), "--literal\nline two", "/bin/cat")
	if result.Err != nil || result.Stdout != "--literal\nline two" {
		t.Fatal(result)
	}
}
func TestGracefulMediaCancellationAllowsChildToFinalize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finalized := filepath.Join(t.TempDir(), "finalized")

	var stdout, stderr bytes.Buffer
	ready := make(chan struct{})
	output := &readyMediaWriter{buffer: &stdout, ready: ready}
	done := make(chan CommandResult, 1)
	go func() {
		done <- (ExecRunner{}).RunStreams(ctx, output, &stderr, "/usr/bin/env", "MAV_MEDIA_TEST_OUTPUT="+finalized, os.Args[0], "-test.run=^TestMediaRecordingChild$")
	}()
	select {
	case <-ready:
		cancel()
	case <-time.After(3 * time.Second):
		t.Fatal("recorder did not start")
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recording ignored cancellation")
	}
	data, err := os.ReadFile(finalized)
	if err != nil || string(data) != "finished" {
		t.Fatal(string(data), err, stderr.String())
	}
}
func TestMediaStartReportsExecFailure(t *testing.T) {
	_, err := (ExecRunner{}).StartStreams(context.Background(), filepath.Join(t.TempDir(), "out"), filepath.Join(t.TempDir(), "err"), "/nonexistent-mav-test-command")
	if err == nil {
		t.Fatal("missing executable accepted")
	}
}
func TestMediaReadinessRequiresMP4AndRunningChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.mp4")
	cmd := exec.CommandContext(ctx, "/bin/sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	if err := os.WriteFile(path, []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := (ExecRunner{}).AwaitRecording(ctx, cmd.Process.Pid, path, filepath.Join(dir, "err")); err != nil {
		t.Fatal(err)
	}
}
func TestAdapterNeverRunsRealChildForUnsupportedFakeStdin(t *testing.T) {
	runner := noInputRunner{Runner: &recordingRunner{}}
	result := NewExecutor(runner).(drivers.InputExecutor).RunInput(context.Background(), "sample", "/bin/cat")
	if result.Err == nil || !strings.Contains(result.Err.Error(), "input_stream_unsupported") {
		t.Fatal(result)
	}
}

type noInputRunner struct{ Runner }
type readyMediaWriter struct {
	buffer *bytes.Buffer
	ready  chan struct{}
	once   sync.Once
}

func (w *readyMediaWriter) Write(p []byte) (int, error) {
	n, err := w.buffer.Write(p)
	w.once.Do(func() { close(w.ready) })
	return n, err
}

// A real process that installs its signal handler before announcing readiness.
// This avoids a shell forking a new child after cancellation has been sent.
func TestMediaRecordingChild(t *testing.T) {
	output := os.Getenv("MAV_MEDIA_TEST_OUTPUT")
	if output == "" {
		return
	}
	received := make(chan os.Signal, 1)
	signal.Notify(received, syscall.SIGTERM)
	_, _ = os.Stdout.Write([]byte("ready"))
	<-received
	if err := os.WriteFile(output, []byte("finished"), 0600); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
