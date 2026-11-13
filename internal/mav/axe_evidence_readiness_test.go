package mav

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAXeEvidenceStartRequiresRecorderReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, want   string
		setup        func(*sequenceRecordingRunner)
		childStarted bool
	}{
		{"fork failure", "recorder fork failed", func(r *sequenceRecordingRunner) { r.mediaStartErr = errors.New("recorder fork failed") }, false},
		{"missing recording", "no such file", func(r *sequenceRecordingRunner) { r.mediaNoArtifact = true }, true},
		{"error log", "recording permission denied", func(r *sequenceRecordingRunner) { r.mediaLog = "Error: recording permission denied\n" }, true},
		{"readiness failure", "recorder exited before startup", func(r *sequenceRecordingRunner) { r.mediaReadyErr = errors.New("recorder exited before startup") }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := DefaultConfig(root)
			cfg.SimulatorUDID = "SIM"
			cfg.Tools = map[string]bool{"axe": true}
			if err := SaveConfig(root, cfg); err != nil {
				t.Fatal(err)
			}
			run, err := NewProjectRunState(root)
			if err != nil {
				t.Fatal(err)
			}
			if err = SaveCurrentRun(root, run); err != nil {
				t.Fatal(err)
			}
			runner := &sequenceRecordingRunner{tools: cfg.Tools}
			tc.setup(runner)
			var out bytes.Buffer
			cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
			err = cli.Run(context.Background(), []string{"evidence", "start", "--run", run.ID})
			if err == nil || !strings.Contains(out.String(), "fail code=evidence_start_failed") || !strings.Contains(out.String(), tc.want) {
				t.Fatalf("err=%v output=%s", err, out.String())
			}
			if strings.Contains(out.String(), "ok cmd=evidence.start") {
				t.Fatalf("premature success: %s", out.String())
			}
			for _, file := range []string{"video.pid", "video-driver.json", "video.start.ms"} {
				if _, err := os.Stat(filepath.Join(run.Dir, file)); !os.IsNotExist(err) {
					t.Fatalf("failed recorder left accepted state %s: %v", file, err)
				}
			}
			if got := containsCall(runner.commands, "kill -TERM 123"); got != tc.childStarted {
				t.Fatalf("cleanup=%t started=%t commands=%v", got, tc.childStarted, runner.commands)
			}
		})
	}
}
