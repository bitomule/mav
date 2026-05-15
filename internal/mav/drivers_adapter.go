package mav

import (
	"context"

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

func (a runnerAdapter) Run(ctx context.Context, name string, args ...string) drivers.ExecResult {
	res := a.r.Run(ctx, name, args...)
	return drivers.ExecResult{Stdout: res.Stdout, Stderr: res.Stderr, Code: res.Code, Err: res.Err}
}

func (a runnerAdapter) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	return a.r.Start(ctx, logPath, name, args...)
}
