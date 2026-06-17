package mav

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type idbRepairRunner struct {
	tools    map[string]bool
	commands []string
	results  map[string][]CommandResult
}

func (r *idbRepairRunner) LookPath(file string) (string, error) {
	if r.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (r *idbRepairRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	if results := r.results[command]; len(results) > 0 {
		result := results[0]
		r.results[command] = results[1:]
		return result
	}
	return CommandResult{}
}

func (r *idbRepairRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	_ = name
	_ = args
	return 0, nil
}

func TestRunWithIDBCompanionRepairRefreshesAndRetriesStaleCompanion(t *testing.T) {
	runner := &idbRepairRunner{results: map[string][]CommandResult{
		"idb crash list --udid SIM-1": {
			{Stderr: "Failed to connect to companion at address DomainSocketAddress(path='/tmp/idb/SIM-1_companion.sock'): [Errno 61] Connection refused", Code: 1, Err: errors.New("exit status 1")},
			{Stdout: "no crashes\n"},
		},
		"idb list-targets --json": {
			{Stdout: "{}\n"},
		},
	}}
	result := runWithIDBCompanionRepair(context.Background(), runner, "idb", "crash", "list", "--udid", "SIM-1")
	if result.Err != nil {
		t.Fatalf("result=%+v", result)
	}
	if !result.IDBCompanionRefreshed {
		t.Fatalf("expected repaired result")
	}
	want := []string{
		"idb crash list --udid SIM-1",
		"idb list-targets --json",
		"idb crash list --udid SIM-1",
	}
	if strings.Join(runner.commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestRunWithIDBCompanionRepairDoesNotRetryUnrelatedIDBFailure(t *testing.T) {
	runner := &idbRepairRunner{results: map[string][]CommandResult{
		"idb crash list": {
			{Stderr: "permission denied", Code: 1, Err: errors.New("exit status 1")},
		},
	}}
	result := runWithIDBCompanionRepair(context.Background(), runner, "idb", "crash", "list")
	if result.Err == nil {
		t.Fatalf("expected original error")
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestRunWithIDBCompanionRepairRetriesConnectionLost(t *testing.T) {
	runner := &idbRepairRunner{results: map[string][]CommandResult{
		"idb crash list": {
			{Stderr: "('Connection lost',)", Code: 1, Err: errors.New("exit status 1")},
			{Stdout: "no crashes\n"},
		},
		"idb list-targets --json": {
			{Stdout: "{}\n"},
		},
	}}
	result := runWithIDBCompanionRepair(context.Background(), runner, "idb", "crash", "list")
	if result.Err != nil || !result.IDBCompanionRefreshed {
		t.Fatalf("result=%+v", result)
	}
	if len(runner.commands) != 3 {
		t.Fatalf("commands=%v", runner.commands)
	}
}

func TestCrashesReportsRepairedIDBCompanion(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.BundleID = "com.example.app"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &idbRepairRunner{
		tools: map[string]bool{"idb": true},
		results: map[string][]CommandResult{
			"idb crash list --udid REAL-1 --bundle-id com.example.app": {
				{Stderr: "Failed to connect to companion at address DomainSocketAddress(path='/tmp/idb/REAL-1_companion.sock'): [Errno 61] Connection refused", Code: 1, Err: errors.New("exit status 1")},
				{Stdout: "no crashes\n"},
			},
			"idb list-targets --json": {
				{Stdout: "{}\n"},
			},
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"crashes"}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"ok cmd=crashes", "count=0", "idb_repaired=true"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output %q missing %q", text, want)
		}
	}
}
