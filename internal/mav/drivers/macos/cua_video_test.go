package macos

import (
	"context"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// TestVideoSessionOutlivesNothingLess: the daemon records only while a
// client stays connected, so a recorder that returns as soon as it has
// started produces a video the length of the command that started it.
// Measured: a one-shot start gave 0 useful seconds, the held session gave
// the whole run. The endless wait in the script is what makes the
// difference, so it is pinned here rather than left to look like a mistake.
func TestVideoSessionOutlivesNothingLess(t *testing.T) {
	script := videoSessionScript("/runs/abc/video")
	if !strings.Contains(script, "cua-driver mcp") {
		t.Fatalf("the recording must run inside a held session: %s", script)
	}
	if !strings.Contains(script, "sleep") {
		t.Fatalf("nothing holds the session open, so the video ends with the command: %s", script)
	}
	if !strings.Contains(script, `"record_video":true`) {
		t.Fatalf("the session records stills only, not video: %s", script)
	}
	if !strings.Contains(script, "/runs/abc/video") {
		t.Fatalf("the recording does not land in the run: %s", script)
	}
}

// TestVideoIsCollectedFromWhereTheDaemonWritesIt: the daemon names the file
// itself, inside the directory it was given. A caller that expects the path
// it asked for finds nothing and reports a run with no video when the video
// exists.
func TestVideoIsCollectedFromWhereTheDaemonWritesIt(t *testing.T) {
	if got := CuaVideoFile("/runs/abc/video.mp4"); got != "/runs/abc/video/recording.mp4" {
		t.Fatalf("got %q", got)
	}
}

// TestVideoStopAsksTheDaemonBeforeAnybodyKillsIt: only the daemon can write
// the mp4's index. Cutting the session off without it leaves a
// plausible-looking file no player opens, which is worse than no video.
func TestVideoStopAsksTheDaemonBeforeAnybodyKillsIt(t *testing.T) {
	f := &fakeExec{results: map[string]drivers.ExecResult{
		"stop_recording": {Stdout: `{"enabled":false,"last_error":null,"last_video_path":"/runs/abc/video/recording.mp4"}`},
	}}
	d := NewCua(f)
	if err := d.VideoStop(context.Background(), drivers.Target{Kind: drivers.KindMac}, 4321); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) == 0 || !strings.Contains(f.commands[0], "stop_recording") {
		t.Fatalf("the daemon was never asked to finalize the file: %v", f.commands)
	}
}

// TestARecordingThatFailedIsNotReportedAsAVideo: the daemon carries the
// reason in the same payload that says it stopped. Ignoring it hands back a
// run whose report points at a file that was never written.
func TestARecordingThatFailedIsNotReportedAsAVideo(t *testing.T) {
	f := &fakeExec{results: map[string]drivers.ExecResult{
		"stop_recording": {Stdout: `{"enabled":false,"last_error":"capture stream ended","last_video_path":null}`},
	}}
	d := NewCua(f)
	err := d.VideoStop(context.Background(), drivers.Target{Kind: drivers.KindMac}, 4321)
	if err == nil || !strings.Contains(err.Error(), "capture stream ended") {
		t.Fatalf("err=%v", err)
	}
}

// TestCuaOutranksScreencaptureForVideo: screencapture records only when mav
// already runs inside the graphical session, which is never true over SSH,
// which is every run against a VM. If it ever wins the route, VM runs go
// back to having no video at all.
func TestCuaOutranksScreencaptureForVideo(t *testing.T) {
	mac := drivers.Target{Kind: drivers.KindMac}
	cua := NewCua(&fakeExec{})
	if !cua.Provides(mac).Has(drivers.CapVideo) {
		t.Fatal("cua does not offer video, so nothing records inside a VM")
	}
	screen := NewScreencapture(&fakeExec{})
	if cua.Cost(drivers.CapVideo, mac) >= screen.Cost(drivers.CapVideo, mac) {
		t.Fatalf("cua=%d screencapture=%d", cua.Cost(drivers.CapVideo, mac), screen.Cost(drivers.CapVideo, mac))
	}
}
