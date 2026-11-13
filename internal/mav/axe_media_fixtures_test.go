package mav

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// A fake recorder becomes ready only after it has produced a real MP4 header.
// Keep stderr separate so a recorder failure cannot become successful JSON.
func awaitFixtureRecording(ctx context.Context, path, stderrPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	log, err := os.ReadFile(stderrPath)
	if err != nil {
		return err
	}
	if failure := videoLogFailure(string(log)); failure != "" {
		return fmt.Errorf("%s", failure)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) < 12 || string(data[4:8]) != "ftyp" {
		return fmt.Errorf("recording has no MP4 header")
	}
	return nil
}

func writeFixtureStreams(stdoutPath, stderrPath, log string) error {
	for _, path := range []string{stdoutPath, stderrPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(stdoutPath, nil, 0o644); err != nil {
		return err
	}
	return os.WriteFile(stderrPath, []byte(log), 0o644)
}

func (r *sequenceRecordingRunner) StartStreams(ctx context.Context, stdoutPath, stderrPath, name string, args ...string) (int, error) {
	if r.mediaStartErr != nil {
		return 0, r.mediaStartErr
	}
	if err := writeFixtureStreams(stdoutPath, stderrPath, r.mediaLog); err != nil {
		return 0, err
	}
	if r.mediaNoArtifact {
		r.commands = append(r.commands, axeFixtureLookup(name, args))
		if r.startPID == 0 {
			r.startPID = 123
		}
		pid := r.startPID
		r.startPID++
		return pid, nil
	}
	return r.Start(ctx, stderrPath, axeFixtureName(name), args...)
}
func (r *sequenceRecordingRunner) AwaitRecording(ctx context.Context, pid int, path, stderrPath string) error {
	if r.mediaReadyErr != nil {
		return r.mediaReadyErr
	}
	return awaitFixtureRecording(ctx, path, stderrPath)
}
func (r *sequenceRecordingRunner) StopProcess(ctx context.Context, pid int) error {
	result := r.Run(ctx, "kill", "-TERM", strconv.Itoa(pid))
	return result.Err
}
func (r *sequenceRecordingRunner) RunStreams(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) CommandResult {
	result := r.Run(ctx, name, args...)
	_, _ = io.WriteString(stdout, result.Stdout)
	_, _ = io.WriteString(stderr, result.Stderr)
	return result
}

func (f fakeRunner) StartStreams(ctx context.Context, stdoutPath, stderrPath, name string, args ...string) (int, error) {
	if err := writeFixtureStreams(stdoutPath, stderrPath, ""); err != nil {
		return 0, err
	}
	writeStartedArtifact(axeFixtureName(name), args)
	return 123, nil
}
func (f fakeRunner) AwaitRecording(ctx context.Context, pid int, path, stderrPath string) error {
	return awaitFixtureRecording(ctx, path, stderrPath)
}
func (f fakeRunner) StopProcess(ctx context.Context, pid int) error {
	return f.Run(ctx, "kill", "-TERM", strconv.Itoa(pid)).Err
}
