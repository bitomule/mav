package mav

import (
	"context"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func runWithIDBCompanionRepair(ctx context.Context, runner Runner, name string, args ...string) CommandResult {
	result := runner.Run(ctx, name, args...)
	effectiveName, effectiveArgs := effectiveIDBCommand(name, args)
	if effectiveName != "idb" || isIDBCompanionRefreshCommand(effectiveArgs) || !isStaleIDBCompanionError(result) {
		return result
	}
	refresh := runner.Run(ctx, "idb", "list-targets", "--json")
	if refresh.Err != nil {
		return result
	}
	retry := runner.Run(ctx, name, args...)
	retry.IDBCompanionRefreshed = true
	return retry
}

// effectiveIDBCommand unwraps an `/usr/bin/env NAME=value... cmd args...`
// invocation to the tool it actually runs, so the stale-companion gate below
// still recognizes an env-prefixed idb launch (used to carry the app's own
// environment) the same way it recognizes a plain one. A non-env command is
// returned unchanged.
func effectiveIDBCommand(name string, args []string) (string, []string) {
	if name != drivers.EnvPrefixPath {
		return name, args
	}
	for i, arg := range args {
		if _, _, isAssignment := splitAssignment(arg); isAssignment {
			continue
		}
		return arg, args[i+1:]
	}
	return name, args
}

func (c CLI) runIDBCommand(ctx context.Context, args ...string) CommandResult {
	return runWithIDBCompanionRepair(ctx, c.Runner, "idb", args...)
}

func isIDBCompanionRefreshCommand(args []string) bool {
	return len(args) > 0 && args[0] == "list-targets"
}

func isStaleIDBCompanionError(result CommandResult) bool {
	if result.Err == nil {
		return false
	}
	text := strings.ToLower(result.Stderr + "\n" + result.Stdout + "\n" + result.Err.Error())
	return strings.Contains(text, "failed to connect to companion") ||
		strings.Contains(text, "failed to describe companioninfo") ||
		strings.Contains(text, "connection refused") && strings.Contains(text, "companion") ||
		strings.Contains(text, "connection lost") ||
		strings.Contains(text, "companion.sock")
}
