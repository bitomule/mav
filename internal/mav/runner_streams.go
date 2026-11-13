package mav

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func streamCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 10 * time.Second
	return cmd
}
func commandResult(err error) CommandResult {
	code := 0
	if err != nil {
		code = 1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
	}
	return CommandResult{Code: code, Err: err}
}
func (ExecRunner) RunInput(ctx context.Context, input, name string, args ...string) CommandResult {
	cmd := streamCommand(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	result := commandResult(cmd.Run())
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	return result
}
func (ExecRunner) RunStreams(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) CommandResult {
	cmd := streamCommand(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return commandResult(cmd.Run())
}
func openMediaLog(path string) (*os.File, error) {
	if path == "" {
		path = os.DevNull
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
}
func (ExecRunner) StartStreams(ctx context.Context, stdoutPath, stderrPath, name string, args ...string) (int, error) {
	if stdoutPath != "" && stdoutPath == stderrPath {
		return 0, fmt.Errorf("media stdout and stderr must use separate paths")
	}
	stdout, err := openMediaLog(stdoutPath)
	if err != nil {
		return 0, err
	}
	stderr, err := openMediaLog(stderrPath)
	if err != nil {
		_ = stdout.Close()
		return 0, err
	}
	cmd := streamCommand(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return 0, err
	}
	go func() { _ = cmd.Wait(); _ = stdout.Close(); _ = stderr.Close() }()
	return cmd.Process.Pid, nil
}
func (ExecRunner) StopProcess(ctx context.Context, pid int) error {
	if pid <= 1 {
		return fmt.Errorf("invalid recording PID")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("recording did not finish within 10 seconds; process remains tracked")
		case <-tick.C:
		}
	}
}
func (ExecRunner) AwaitRecording(ctx context.Context, pid int, path, stderrPath string) error {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			diagnostic, _ := os.ReadFile(stderrPath)
			if len(diagnostic) > 4096 {
				diagnostic = diagnostic[:4096]
			}
			return fmt.Errorf("video_recorder_failed: %s", strings.TrimSpace(string(diagnostic)))
		}
		file, err := os.Open(path)
		if err == nil {
			header := make([]byte, 12)
			n, _ := io.ReadFull(file, header)
			_ = file.Close()
			// ftyp identifies the MP4 writer initialized from a captured frame.
			if n >= 8 && string(header[4:8]) == "ftyp" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("video_recorder_not_ready after 30s")
		case <-tick.C:
		}
	}
}
func (a runnerAdapter) RunStreams(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) drivers.ExecResult {
	r, ok := a.r.(interface {
		RunStreams(context.Context, io.Writer, io.Writer, string, ...string) CommandResult
	})
	if !ok {
		return drivers.ExecResult{Code: 1, Err: fmt.Errorf("media_stream_unsupported: runner does not support separate streams")}
	}
	res := r.RunStreams(ctx, stdout, stderr, name, args...)
	return drivers.ExecResult{Code: res.Code, Err: res.Err, Stdout: res.Stdout, Stderr: res.Stderr}
}
func (a runnerAdapter) StartStreams(ctx context.Context, stdout, stderr, name string, args ...string) (int, error) {
	r, ok := a.r.(interface {
		StartStreams(context.Context, string, string, string, ...string) (int, error)
	})
	if !ok {
		return 0, fmt.Errorf("media_stream_unsupported: runner does not support separate streams")
	}
	return r.StartStreams(ctx, stdout, stderr, name, args...)
}
func (a runnerAdapter) StopProcess(ctx context.Context, pid int) error {
	r, ok := a.r.(interface {
		StopProcess(context.Context, int) error
	})
	if !ok {
		return fmt.Errorf("media_process_unsupported")
	}
	return r.StopProcess(ctx, pid)
}
func (a runnerAdapter) AwaitRecording(ctx context.Context, pid int, path, stderr string) error {
	r, ok := a.r.(interface {
		AwaitRecording(context.Context, int, string, string) error
	})
	if !ok {
		return fmt.Errorf("media_readiness_unsupported")
	}
	return r.AwaitRecording(ctx, pid, path, stderr)
}
