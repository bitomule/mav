package axe

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type fakeMedia struct {
	duringRun func()
	fakeExec
	stdout, stderr    []byte
	streams           [][]string
	starts            [][]string
	paths             [][2]string
	stops             []int
	awaitErr, stopErr error
	streamResult      drivers.ExecResult
}

func (m *fakeMedia) RunStreams(_ context.Context, out, errout io.Writer, name string, args ...string) drivers.ExecResult {
	m.streams = append(m.streams, append([]string{name}, args...))
	if m.duringRun != nil {
		m.duringRun()
	}
	_, _ = out.Write(m.stdout)
	_, _ = errout.Write(m.stderr)
	return m.streamResult
}
func (m *fakeMedia) StartStreams(_ context.Context, out, errout, name string, args ...string) (int, error) {
	m.starts = append(m.starts, append([]string{name}, args...))
	m.paths = append(m.paths, [2]string{out, errout})
	return 99, nil
}
func (m *fakeMedia) StopProcess(_ context.Context, pid int) error {
	m.stops = append(m.stops, pid)
	return m.stopErr
}
func (m *fakeMedia) AwaitRecording(context.Context, int, string, string) error { return m.awaitErr }
func TestStreamKeepsBinaryFramesSeparateFromDiagnostics(t *testing.T) {
	media := &fakeMedia{stdout: []byte{0, 0, 0, 3, 0xff, 0xd8, 0xd9}, stderr: []byte("captured frame\n")}
	var out, diagnostics bytes.Buffer
	err := New(media).Stream(context.Background(), drivers.Target{Kind: drivers.KindDevice, UDID: "DEVICE-1"}, drivers.VideoStreamSpec{Format: "raw", FPS: 15, Quality: 70, Scale: 0.5}, &out, &diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), media.stdout) || diagnostics.String() != string(media.stderr) {
		t.Fatal(out.Bytes(), diagnostics.String())
	}
	args := strings.Join(media.streams[0], " ")
	if strings.Contains(args, "--output json") || !strings.Contains(args, "--format raw") {
		t.Fatal(args)
	}
}
func TestNativeH264RejectsIncompatibleOptionsBeforeStarting(t *testing.T) {
	for _, tc := range []struct {
		target drivers.Target
		spec   drivers.VideoStreamSpec
	}{{drivers.Target{Kind: drivers.KindSim}, drivers.VideoStreamSpec{Format: "h264"}}, {drivers.Target{Kind: drivers.KindDevice}, drivers.VideoStreamSpec{Format: "h264", FPS: 30}}} {
		m := &fakeMedia{}
		err := New(m).Stream(context.Background(), tc.target, tc.spec, io.Discard, io.Discard)
		if err == nil || len(m.streams) != 0 {
			t.Fatal(err, m.streams)
		}
	}
}
func TestBackgroundRecordingTracksNativeOptionsAndSeparateLogs(t *testing.T) {
	m := &fakeMedia{}
	path := filepath.Join(t.TempDir(), "capture.mp4")
	result, err := New(m).VideoStart(context.Background(), drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"}, drivers.VideoSpec{OutPath: path, FPS: 15, Quality: 90, Scale: 0.5})
	if err != nil || result.PID != 99 || result.OutPath != path {
		t.Fatal(result, err)
	}
	args := strings.Join(m.starts[0], " ")
	for _, part := range []string{"--output json record-video", "--fps 15", "--quality 90", "--scale 0.5"} {
		if !strings.Contains(args, part) {
			t.Fatal(args)
		}
	}
	if m.paths[0][0] == m.paths[0][1] || m.paths[0][0] != path+".log.result.json" {
		t.Fatal(m.paths)
	}
}
func TestRecordingReadinessFailureRetainsPIDAndAttemptsCleanup(t *testing.T) {
	m := &fakeMedia{awaitErr: errors.New("not ready"), stopErr: errors.New("still alive")}
	path := filepath.Join(t.TempDir(), "capture.mp4")
	result, err := New(m).VideoStart(context.Background(), drivers.Target{}, drivers.VideoSpec{OutPath: path})
	if err == nil || result.PID != 99 || len(m.stops) != 1 || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatal(result, err, m.stops)
	}
}
func TestForegroundRecordDecodesFinalEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.mp4")
	m := &fakeMedia{stdout: []byte(okOutput(`{"path":"capture.mp4","frames":15}`))}
	result, err := New(m).Record(context.Background(), drivers.Target{Kind: drivers.KindDevice}, drivers.VideoRecordSpec{OutPath: path})
	if err != nil || !strings.Contains(string(result.Data), `"frames":15`) {
		t.Fatal(result, err)
	}
}
func TestPinnedRecordingCannotAlsoFollowDisplay(t *testing.T) {
	m := &fakeMedia{}
	id := uint32(1)
	_, err := New(m).VideoStart(context.Background(), drivers.Target{InputContext: &drivers.InputContext{ScreenID: &id, ScreenUUID: "00000000-0000-4000-8000-000000000001"}}, drivers.VideoSpec{OutPath: filepath.Join(t.TempDir(), "capture.mp4"), FollowActiveDisplay: true})
	if err == nil || len(m.starts) != 0 {
		t.Fatal(err, m.starts)
	}
}

func TestRecordingRefusesExistingFileAndDanglingSymlink(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "capture.mp4")
		var err error
		if symlink {
			err = os.Symlink(filepath.Join(dir, "missing"), path)
		} else {
			err = os.WriteFile(path, []byte("existing"), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		media := &fakeMedia{}
		_, err = New(media).VideoStart(context.Background(), drivers.Target{}, drivers.VideoSpec{OutPath: path})
		if err == nil || len(media.starts) != 0 {
			t.Fatal(err, media.starts)
		}
	}
}

func testMovie(includeIndex bool) []byte {
	atom := func(kind string, payload []byte) []byte {
		raw := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(raw, uint32(len(raw)))
		copy(raw[4:8], kind)
		copy(raw[8:], payload)
		return raw
	}
	out := atom("ftyp", []byte("isom0000"))
	out = append(out, atom("mdat", []byte{1, 2, 3, 4})...)
	if includeIndex {
		out = append(out, atom("moov", atom("mvhd", make([]byte, 100)))...)
	}
	return out
}
func TestRecordCancellationAcceptsOnlyProvenFinalization(t *testing.T) {
	for _, tc := range []struct {
		name         string
		processError error
		envelope     string
		finalized    bool
		wantSuccess  bool
	}{
		{"finalized", context.Canceled, `{"schema":"axe.output.command-result","schemaVersion":"1","command":"record-video","didError":false,"data":{"result":{"path":"capture.mp4","frames":10}}}`, true, true},
		{"missing movie index", context.Canceled, `{"schema":"axe.output.command-result","schemaVersion":"1","command":"record-video","didError":false,"data":{"result":{"frames":10}}}`, false, false},
		{"forced kill", errors.New("signal: killed"), `{"schema":"axe.output.command-result","schemaVersion":"1","command":"record-video","didError":false,"data":{"result":{"frames":10}}}`, true, false},
		{"error envelope", context.Canceled, `{"schema":"axe.output.command-result","schemaVersion":"1","command":"record-video","didError":true,"error":{"code":"captureFailed","message":"failed"},"data":{}}`, true, false},
		{"wrong command", context.Canceled, `{"schema":"axe.output.command-result","schemaVersion":"1","command":"screenshot","didError":false,"data":{}}`, true, false},
		{"deadline", context.DeadlineExceeded, `{"schema":"axe.output.command-result","schemaVersion":"1","command":"record-video","didError":false,"data":{}}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			path := filepath.Join(t.TempDir(), "capture.mp4")
			media := &fakeMedia{stdout: []byte(tc.envelope), streamResult: drivers.ExecResult{Code: 1, Err: tc.processError}}
			media.duringRun = func() {
				if err := os.WriteFile(path, testMovie(tc.finalized), 0600); err != nil {
					t.Fatal(err)
				}
				cancel()
			}
			_, err := New(media).Record(ctx, drivers.Target{}, drivers.VideoRecordSpec{OutPath: path})
			if (err == nil) != tc.wantSuccess {
				t.Fatalf("error=%v wantSuccess=%v", err, tc.wantSuccess)
			}
		})
	}
}

func TestBackgroundFollowRequiresForegroundAPI(t *testing.T) {
	m := &fakeMedia{}
	_, err := New(m).VideoStart(context.Background(), drivers.Target{Kind: drivers.KindSim}, drivers.VideoSpec{OutPath: filepath.Join(t.TempDir(), "capture.mp4"), FollowActiveDisplay: true})
	if err == nil || len(m.starts) != 0 || !strings.Contains(err.Error(), "foreground") {
		t.Fatal(err, m.starts)
	}
}
