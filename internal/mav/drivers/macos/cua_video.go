package macos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

var _ drivers.VideoDriver = (*Cua)(nil)

// Video on macOS is the daemon's, and the daemon only records for as long
// as an MCP client stays connected. That one sentence explains the whole
// shape of this file, so it is worth spelling out what was measured rather
// than leaving the design looking arbitrary. Inside a real VM:
//
//   - `screencapture -v` is the obvious answer and cannot be used over SSH:
//     that process is not in the Aqua session and sees no display at all.
//   - `cua-driver recording start <dir>` DOES survive the process that
//     started it, but records per-action stills only; its `--video` has no
//     effect there and `recording render` refuses without an mp4 that only
//     the other path produces.
//   - `cua-driver call start_recording --record-video` starts real video and
//     tears it down the instant that one-shot CLI exits.
//   - the hypervisor's own desktop recording refuses macOS targets outright.
//
// What is left is to hold the MCP session open for the length of the
// recording, which is what videoSessionScript does. It maps onto the
// Runner's existing Start/Stop exactly: a long-lived process with a pid,
// stopped by a signal, which is the same shape the simulator's recorder
// already has.

// cuaVideoDir is where the daemon writes. It is derived from the path mav
// asked for rather than fixed, because the daemon names the file itself
// (`recording.mp4` inside the directory it was given) and mav names it
// `video.mp4` inside the run. Giving the daemon a directory of its own
// keeps its turn folders and cursor log out of the run directory's root.
func cuaVideoDir(outPath string) string {
	return strings.TrimSuffix(outPath, filepath.Ext(outPath))
}

// CuaVideoFile is the file the daemon produces inside cuaVideoDir. Exported
// because the caller has to move it into place: VideoStop is handed a pid
// and nothing else, so the driver has no way to know where the run wanted
// the finished file.
func CuaVideoFile(outPath string) string {
	return filepath.Join(cuaVideoDir(outPath), "recording.mp4")
}

// videoSessionScript is one shell pipeline that speaks just enough MCP to
// start the recording and then does nothing at all, forever, so the session
// stays up. The endless sleep is the point: close stdin and the daemon
// finalizes the mp4 and stops.
func videoSessionScript(dir string) string {
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"mav","version":"1"}}}`
	initialized := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	start := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"start_recording","arguments":{"output_dir":%s,"record_video":true}}}`,
		mustJSONString(dir))
	return "mkdir -p " + shellQuote(dir) + " && { printf '%s\\n%s\\n%s\\n' " +
		shellQuote(initialize) + " " + shellQuote(initialized) + " " + shellQuote(start) +
		"; while :; do sleep 3600; done; } | cua-driver mcp"
}

// VideoStart brings the daemon up, then holds a session open for it.
func (d *Cua) VideoStart(ctx context.Context, _ drivers.Target, spec drivers.VideoSpec) (drivers.VideoResult, error) {
	if spec.OutPath == "" {
		return drivers.VideoResult{}, errors.New("cua: video output path missing")
	}
	// The recording inherits the daemon's Screen Recording grant, so a
	// daemon that is not up is not a slow start, it is no video at all.
	if _, err := d.cuaCall(ctx, "get_recording_state", map[string]any{}); err != nil {
		return drivers.VideoResult{}, fmt.Errorf("cua: the CuaDriver daemon is not available for recording: %w", err)
	}
	dir := cuaVideoDir(spec.OutPath)
	pid, err := d.exec.Start(ctx, "", "sh", "-c", videoSessionScript(dir))
	if err != nil {
		return drivers.VideoResult{}, err
	}
	return drivers.VideoResult{PID: pid, OutPath: spec.OutPath}, nil
}

// VideoStop finalizes the mp4 before anything kills the session holding it.
//
// It asks the daemon to stop rather than just signalling the holder,
// because only the daemon can write the mp4's index; a file cut off without
// it is a plausible-looking mp4 no player will open, which is worse than no
// video. Stopping is documented as unconditional, so it does not matter
// which session started the recording.
func (d *Cua) VideoStop(ctx context.Context, _ drivers.Target, pid int) error {
	if pid <= 0 {
		return errors.New("cua: video pid missing")
	}
	payload, err := d.cuaCall(ctx, "stop_recording", map[string]any{})
	if err != nil {
		return err
	}
	var state struct {
		VideoPath string `json:"last_video_path"`
		LastError string `json:"last_error"`
	}
	if json.Unmarshal(payload, &state) == nil && state.LastError != "" {
		return fmt.Errorf("cua: recording failed: %s", state.LastError)
	}
	return nil
}

func mustJSONString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
