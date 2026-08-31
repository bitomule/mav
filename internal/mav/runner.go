package mav

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Runner interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, name string, args ...string) CommandResult
	Start(ctx context.Context, logPath string, name string, args ...string) (int, error)
}

type CommandResult struct {
	Stdout                string
	Stderr                string
	Code                  int
	Err                   error
	IDBCompanionRefreshed bool
}

type ExecRunner struct{}

func (ExecRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// runWaitDelay is how long Run keeps waiting after the context is cancelled
// (or after the process itself exits) before it gives up on the output
// pipes and returns. Without it a ctx deadline does not actually bound Run:
// exec.CommandContext kills the direct child only, so a command that spawns
// its own children -- `/bin/bash -lc "simpool lease ..."`, whose grandchild
// inherits the pipe -- leaves cmd.Wait blocked on a pipe nobody will close,
// and mav waits out the grandchild instead of its own timeout. That is
// exactly the hang target_command's timeout exists to prevent, so the
// deadline has to reach the pipes too. Surviving grandchildren are left to
// whatever spawned them: killing a process group is not an option here,
// since Run's children share mav's own group.
const runWaitDelay = 2 * time.Second

func (ExecRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = runWaitDelay
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), Code: code, Err: err}
}

// Start launches a background process and returns its PID.
//
// An empty logPath means "discard the output", not an error. Launching a
// macOS app needs it: there stdout and stderr are not the real log
// channel, OSLog is, and mav already captures that on its own with
// `log stream`, so forcing the driver to invent a file just to throw it
// away would be worse. Before this, an empty logPath died with an
// `open : no such file or directory` that said nothing about the cause.
func (ExecRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	if logPath == "" {
		logPath = os.DevNull
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = file
	cmd.Stderr = file
	if err := cmd.Start(); err != nil {
		_ = file.Close()
		return 0, err
	}
	go func() {
		_ = cmd.Wait()
		_ = file.Close()
	}()
	if cmd.Process == nil {
		return 0, fmt.Errorf("process did not start: %s %s", name, strings.Join(args, " "))
	}
	return cmd.Process.Pid, nil
}
