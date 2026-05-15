package drivers

import "context"

// ExecResult is the captured outcome of running an external command. It mirrors
// the shape of the host process so drivers can decide on stdout/stderr/code
// independently. Kept in this package so concrete drivers depend only on
// `drivers`, not on the parent `mav` package (which would cause an import cycle
// because mav itself imports drivers).
type ExecResult struct {
	Stdout string
	Stderr string
	Code   int
	Err    error
}

// Executor is the surface drivers use to run external commands. It is a
// superset of Probe: Probe is enough for capability detection, Executor is
// needed once you actually want to do work.
//
// Production: drivers receive an adapter wrapping mav.Runner.
// Tests: drivers receive a fake constructed inline.
type Executor interface {
	Probe
	Run(ctx context.Context, name string, args ...string) ExecResult
	Start(ctx context.Context, logPath string, name string, args ...string) (int, error)
}
