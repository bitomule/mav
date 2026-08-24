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

func (ExecRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	cmd := exec.CommandContext(ctx, name, args...)
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

// Start lanza un proceso en segundo plano y devuelve su PID.
//
// Un logPath vacio significa "descarta la salida", no es un error. Lo necesita
// el lanzamiento de una app de macOS: ahi stdout y stderr no son el canal de
// logs de verdad -- lo es OSLog, que mav ya captura por su cuenta con
// `log stream` -- asi que obligar al driver a inventarse un fichero solo para
// tirarlo seria peor. Antes de esto, un logPath vacio moria con un
// `open : no such file or directory` que no decia nada de la causa.
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
