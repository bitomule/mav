package mav

import (
	"context"
	"strings"
)

func runWithIDBCompanionRepair(ctx context.Context, runner Runner, name string, args ...string) CommandResult {
	result := runner.Run(ctx, name, args...)
	if name != "idb" || isIDBCompanionRefreshCommand(args) || !isStaleIDBCompanionError(result) {
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
