package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A flow that captures its way into position and only then starts recording
// is the documented shape of an evidence run, and it used to fail at the
// recording step: the earlier `capture: {name: ...}` steps left evidence.jsonl
// and steps/ behind, existingEvidenceIssue read those as leftovers, and
// evidence start refused the run. Through the flow runner that surfaced as a
// bare video_start_failed in under a millisecond, so no agent could record
// video inside a flow that captured anything first.
func TestEvidenceStartAcceptsRunWithEarlierScreenshotSteps(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	step := EvidenceStep{Name: "settings-import-row", File: filepath.Join(run.Dir, "steps", "01_settings-import-row.png"), Kind: "screenshot"}
	if err := os.MkdirAll(filepath.Join(run.Dir, "steps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(step.File, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvidenceStep(run, step); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"evidence", "start", "--run", run.ID}); err != nil {
		t.Fatalf("evidence start: %v (%s)", err, out.String())
	}
	if !strings.Contains(out.String(), "ok cmd=evidence.start") {
		t.Fatalf("got %q", out.String())
	}
	if !fileExists(filepath.Join(run.Dir, "video.pid")) {
		t.Fatalf("no recorder registered: %q", out.String())
	}
}

// The same run through the flow runner, which is where it was reported.
func TestFlowVideoStartAcceptsRunWithEarlierScreenshotSteps(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(run.Dir, "steps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvidenceStep(run, EvidenceStep{Name: "before", File: filepath.Join(run.Dir, "steps", "01_before.png"), Kind: "screenshot"}); err != nil {
		t.Fatal(err)
	}

	cli := CLI{Runner: &sequenceRecordingRunner{tools: cfg.Tools}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if _, err := cli.executeFlowStepBound(context.Background(), run, 2, FlowStep{Action: "video.start"}, flowExecBindings{}); err != nil {
		t.Fatalf("video.start: %v", err)
	}
}

// A recorder already running for this run still blocks a second one: the
// simulator has one recording slot, and starting anyway leaves a video.pid
// pointing at a process that died on arrival.
func TestEvidenceStartStillRejectsRunWithLiveRecorder(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, "video.pid"), []byte("123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: &sequenceRecordingRunner{tools: cfg.Tools}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), []string{"evidence", "start", "--run", run.ID}))
	if !strings.Contains(out.String(), "fail code=evidence_run_not_clean") {
		t.Fatalf("got %q", out.String())
	}
}

// The flow step used to discard the command's own fail line and flatten every
// cause into the same opaque code, which cost a reported run three retries
// against a message that named nothing.
func TestFlowEvidenceStepsCarryTheInnerFailure(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = "SIM"
	cfg.Tools = map[string]bool{"xcrun": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, "video.pid"), []byte("123\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cli := CLI{Runner: &sequenceRecordingRunner{tools: cfg.Tools}, Root: root, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	fields, err := cli.executeFlowStepBound(context.Background(), run, 1, FlowStep{Action: "video.start"}, flowExecBindings{})
	if err == nil {
		t.Fatal("expected video.start to fail")
	}
	if err.Error() != "video_start_failed" {
		t.Fatalf("code=%q", err.Error())
	}
	if !strings.Contains(fields["detail"], "evidence_run_not_clean") {
		t.Fatalf("detail=%q", fields["detail"])
	}
}

// simctl always opens its log with a "Note: No display specified" preamble,
// so reporting the log's first line named that note as the error of a
// recording that actually died on an occupied slot.
func TestVideoLogFailureNamesTheOffendingLine(t *testing.T) {
	const log = `Note: No display specified. Defaulting to display: 885505F4-0000-0000-0000-000000000000 (screenID: 1, name: LCD)
Error starting video recorder: Error Domain=NSPOSIXErrorDomain Code=16 "Resource busy" UserInfo={NSLocalizedFailureReason=Host recording is already in progress}.
`
	failure := videoLogFailure(log)
	if !strings.Contains(failure, "Host recording is already in progress") {
		t.Fatalf("failure=%q", failure)
	}
	if strings.HasPrefix(failure, "Note:") {
		t.Fatalf("reported the display note as the failure: %q", failure)
	}
	if videoLogFailure("Note: No display specified.\nRecording started\n") != "" {
		t.Fatal("a healthy log reported a failure")
	}
}

// Start() only reports that the recorder was forked; xcrun reaches simctl
// through a wrapper that can take seconds to resolve on a loaded machine, and
// everything the flow did in that window was recorded by nobody.
func TestAwaitVideoRecordingWaitsForTheRecorderToStart(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "video.log")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(400 * time.Millisecond)
		_ = os.WriteFile(logPath, []byte("Note: No display specified.\nRecording started\n"), 0o644)
	}()

	cli := CLI{Runner: ExecRunner{}}
	started := time.Now()
	if err := cli.awaitVideoRecording(logPath); err != nil {
		t.Fatalf("await: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 300*time.Millisecond {
		t.Fatalf("returned before the recorder started, after %s", elapsed)
	}
}

func TestAwaitVideoRecordingReportsARecorderThatNeverStarts(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "video.log")
	if err := os.WriteFile(logPath, []byte("Note: No display specified.\nError starting video recorder: Host recording is already in progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: ExecRunner{}}
	err := cli.awaitVideoRecording(logPath)
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), "Host recording is already in progress") {
		t.Fatalf("err=%v", err)
	}
}
