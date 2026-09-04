package drivers

import (
	"context"
	"sort"
)

// EnvPrefixPath is the binary that carries a one-off environment into a
// child process. Drivers use it instead of mutating mav's own environment,
// which would leak the variables into every later invocation of the same
// tool.
const EnvPrefixPath = "/usr/bin/env"

// EnvArgs builds the leading arguments of an `/usr/bin/env NAME=value ... cmd`
// invocation: the variables (each with the tool's own prefix, SIMCTL_CHILD_
// for simctl, IDB_ for idb, none for a plain binary) followed by the command
// that was going to run. Sorted so the argv is deterministic and testable.
func EnvArgs(prefix string, env map[string]string, command string) []string {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)
	args := make([]string, 0, len(names)+1)
	for _, name := range names {
		args = append(args, prefix+name+"="+env[name])
	}
	return append(args, command)
}

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

// InputExecutor is an optional extension for tools with a streaming/stdin
// protocol, such as `baguette input`.
type InputExecutor interface {
	RunInput(ctx context.Context, input string, name string, args ...string) ExecResult
}
