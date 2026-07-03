package mav

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// runnerAdapter wraps a mav.Runner so drivers can consume it via the narrower
// drivers.Executor contract. This keeps the drivers package free of any
// dependency on `mav` and avoids the otherwise inevitable import cycle.
type runnerAdapter struct{ r Runner }

// NewExecutor adapts a mav.Runner to drivers.Executor. Drivers receive this in
// their constructors instead of the full Runner.
func NewExecutor(r Runner) drivers.Executor { return runnerAdapter{r: r} }

func (a runnerAdapter) LookPath(name string) (string, error) {
	return a.r.LookPath(name)
}

func (a runnerAdapter) RunInput(ctx context.Context, input string, name string, args ...string) drivers.ExecResult {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		code = 1
	}
	return drivers.ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), Code: code, Err: err}
}

func (a runnerAdapter) Run(ctx context.Context, name string, args ...string) drivers.ExecResult {
	res := runWithIDBCompanionRepair(ctx, a.r, name, args...)
	return drivers.ExecResult{Stdout: res.Stdout, Stderr: res.Stderr, Code: res.Code, Err: res.Err}
}

func (a runnerAdapter) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	return a.r.Start(ctx, logPath, name, args...)
}
