package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// liveProbeLogs returns the recorded probe-logs PIDs, across every run under
// root, that are still alive. Counting PROCESSES is the measure here: the
// defect is a log stream nobody reaps, and a record in processes.jsonl says
// nothing about whether the process behind it is still running.
func liveProbeLogs(root string) []int {
	var live []int
	entries, err := os.ReadDir(filepath.Join(root, MavDir, "runs"))
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, MavDir, "runs", entry.Name())
		run := RunState{ID: entry.Name(), Dir: dir, Processes: filepath.Join(dir, "processes.jsonl")}
		for _, record := range loadProcessRecords(run) {
			if record.Kind != "probe-logs" || record.PID <= 0 {
				continue
			}
			if syscall.Kill(record.PID, 0) == nil {
				live = append(live, record.PID)
			}
		}
	}
	return live
}

// The defect, measured on this test before the fix: four `mav open
// --no-relaunch` calls left four live `log stream` processes, one per open,
// all recorded against the same reused run.
//
//	after open 1: [48293]
//	after open 2: [48293 50125]
//	after open 3: [48293 50125 53005]
//	after open 4: [48293 50125 53005 56774]
//
// Nothing between opens reaped them: --no-relaunch has no bound run (so the
// stopProbeLogs call in open() was skipped) and supersedes no previous run
// (so nothing stopped the old stream either). They only went away when the
// lease was released.
//
// Plain `mav open` was measured the same way and never grew past one, before
// or after the fix: it records the previous run and stops it on the way in.
func TestRepeatedOpenLeavesOneLogStreamAlive(t *testing.T) {
	root := mkShortRoot(t)
	// Its own UDID, not the shared "SIM" the other open tests use. The
	// simulator lock is a GLOBAL file keyed by UDID, so sharing one means the
	// second open here can be refused with sim_locked because a different
	// test's root got there first -- a failure that says nothing about log
	// streams.
	udid := "SIMLEAK-" + filepath.Base(root)
	t.Cleanup(func() { killRunProcesses(root); removeSimulatorLock(udid, root) })
	writeConcurrencyConfig(t, root)
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SimulatorUDID = udid
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", writeFakeXcrun(t)+":"+os.Getenv("PATH"))

	for i := 0; i < 4; i++ {
		var out bytes.Buffer
		cli := CLI{Runner: ExecRunner{}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
		if err := cli.Run(context.Background(), []string{"open", "--no-relaunch"}); err != nil {
			t.Fatalf("open %d: %v (%s)", i, err, out.String())
		}
		t.Logf("after open %d: live probe-logs = %v", i+1, liveProbeLogs(root))
	}
	if live := liveProbeLogs(root); len(live) != 1 {
		t.Fatalf("after 4 opens, %d log streams are alive (%v); only the newest should be", len(live), live)
	}
}
