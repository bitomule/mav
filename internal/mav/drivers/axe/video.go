package axe

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/bitomule/mav/internal/mav/drivers"
)

var _ drivers.NativeVideoDriver = (*Driver)(nil)
var _ drivers.VideoDriver = (*Driver)(nil)

func mediaArgs(t drivers.Target, command string, fps, quality int, scale float64, follow bool) ([]string, error) {
	if t.Kind == drivers.KindMac {
		return nil, unsupported("AXe does not capture macOS targets")
	}
	if fps < 0 || fps > 30 || quality < 0 || quality > 100 || !finite(scale) || scale < 0 || scale > 1 || (scale > 0 && scale < 0.1) {
		return nil, invalid("video requires fps 1...30, quality 1...100 and scale 0.1...1")
	}
	args := []string{command}
	args = appendInt(args, "--fps", fps)
	args = appendInt(args, "--quality", quality)
	args = appendFloat(args, "--scale", scale)
	if follow {
		if t.Kind == drivers.KindDevice {
			return nil, unsupported("follow-active-display is simulator-only")
		}
		args = append(args, "--follow-active-display")
	}
	if in := t.InputContext; in != nil {
		if in.ExpectedPID != 0 || in.Selected {
			return nil, unsupported("video capture cannot enforce selected input or expected PID")
		}
		if (in.ScreenID == nil) != (in.ScreenUUID == "") {
			return nil, invalid("video screen ID and UUID must be supplied together")
		}
		if in.ScreenID != nil {
			if follow {
				return nil, invalid("video cannot follow displays and pin a screen together")
			}
			if t.Kind == drivers.KindDevice {
				return nil, unsupported("screen selection is simulator-only")
			}
			args = append(args, "--screen-id", strconv.FormatUint(uint64(*in.ScreenID), 10), "--screen-uuid", in.ScreenUUID)
		}
	}
	return args, nil
}
func recordArgs(t drivers.Target, s drivers.VideoRecordSpec) ([]string, error) {
	if s.OutPath == "" {
		return nil, invalid("recording output path is required")
	}
	args, err := mediaArgs(t, "record-video", s.FPS, s.Quality, s.Scale, s.FollowActiveDisplay)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(s.OutPath); err == nil {
		return nil, invalid("recording output already exists; choose a new path")
	} else if !os.IsNotExist(err) {
		return nil, invalid("cannot inspect recording output: " + err.Error())
	}
	return append(args, "--output", s.OutPath), nil
}
func (d *Driver) Record(ctx context.Context, t drivers.Target, s drivers.VideoRecordSpec) (drivers.NativeResult, error) {
	args, err := recordArgs(t, s)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	media, ok := d.exec.(drivers.MediaExecutor)
	if !ok {
		return drivers.NativeResult{}, unsupported("executor does not support graceful media capture")
	}
	var stdout, stderr bytes.Buffer
	args = append([]string{"--output", "json"}, targetArgs(t, args...)...)
	result := media.RunStreams(ctx, &stdout, &stderr, d.binary(), args...)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	// SIGINT/SIGTERM ends recording by design. CommandContext can report
	// context.Canceled even after AXe handled SIGTERM, finalized the movie and
	// returned success. Accept only that positively evidenced finalization;
	// a forced kill, timeout, error envelope or incomplete file remains failed.
	if errors.Is(result.Err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled) {
		var envelope commandEnvelope
		if json.Unmarshal([]byte(result.Stdout), &envelope) == nil && envelope.Command == "record-video" && envelope.DidError != nil && !*envelope.DidError && finalizedMP4(s.OutPath) {
			candidate := result
			candidate.Err = nil
			candidate.Code = 0
			if complete, err := decodeOutput(candidate, false); err == nil {
				return complete, nil
			}
		}
	}
	return decodeOutput(result, false)
}
func (d *Driver) VideoStart(ctx context.Context, t drivers.Target, s drivers.VideoSpec) (drivers.VideoResult, error) {
	// AXe's follow recorder creates an undisclosed temporary path until stop.
	// The foreground API can wait for its final receipt; a background start
	// cannot certify the final output header while it is still recording.
	if s.FollowActiveDisplay {
		return drivers.VideoResult{}, unsupported("follow-active-display recording requires foreground mav video record")
	}

	record := drivers.VideoRecordSpec{OutPath: s.OutPath, LogPath: s.LogPath, FPS: s.FPS, Quality: s.Quality, Scale: s.Scale, FollowActiveDisplay: s.FollowActiveDisplay}
	args, err := recordArgs(t, record)
	if err != nil {
		return drivers.VideoResult{}, err
	}
	media, ok := d.exec.(drivers.MediaExecutor)
	if !ok {
		return drivers.VideoResult{}, unsupported("executor does not support background media capture")
	}
	log := s.LogPath
	if log == "" {
		log = s.OutPath + ".log"
	}
	args = append([]string{"--output", "json"}, targetArgs(t, args...)...)
	pid, err := media.StartStreams(ctx, log+".result.json", log, d.binary(), args...)
	if err != nil {
		return drivers.VideoResult{}, err
	}
	result := drivers.VideoResult{PID: pid, OutPath: s.OutPath}
	if err := media.AwaitRecording(ctx, pid, s.OutPath, log); err != nil {
		// Start did create a child. Return its identity even when readiness
		// fails so the caller can keep tracking failed cleanup.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stopErr := media.StopProcess(cleanupCtx, pid)
		if stopErr != nil {
			return result, fmt.Errorf("%w; recording cleanup failed: %v", err, stopErr)
		}
		return result, err
	}
	return result, nil
}
func (d *Driver) VideoStop(ctx context.Context, _ drivers.Target, pid int) error {
	media, ok := d.exec.(drivers.MediaExecutor)
	if !ok {
		return unsupported("executor does not support graceful recorder stop")
	}
	return media.StopProcess(ctx, pid)
}
func (d *Driver) Stream(ctx context.Context, t drivers.Target, s drivers.VideoStreamSpec, stdout, stderr io.Writer) error {
	if stdout == nil || stderr == nil {
		return invalid("stream requires separate stdout and diagnostic writers")
	}
	format := s.Format
	if format == "" {
		format = "mjpeg"
	}
	switch format {
	case "mjpeg", "raw", "ffmpeg", "bgra":
	case "h264":
		if t.Kind != drivers.KindDevice {
			return unsupported("native H.264 streaming is available on physical devices only")
		}
		if s.FPS != 0 || s.Quality != 0 || s.Scale != 0 {
			return invalid("native H.264 stream does not accept fps, quality or scale")
		}
	default:
		return invalid("unknown video stream format")
	}
	args, err := mediaArgs(t, "stream-video", s.FPS, s.Quality, s.Scale, false)
	if err != nil {
		return err
	}
	args = append(args, "--format", format)
	media, ok := d.exec.(drivers.MediaExecutor)
	if !ok {
		return unsupported("executor does not support separate media streams")
	}
	// Never request the command JSON envelope on binary stdout.
	result := media.RunStreams(ctx, stdout, stderr, d.binary(), targetArgs(t, args...)...)
	if result.Err != nil || result.Code != 0 {
		return &drivers.DriverError{Driver: ID, Code: "video_stream_failed", Message: failureText(result), Delivery: drivers.DeliveryNotSent}
	}
	return nil
}

// finalizedMP4 checks the bounded top-level ISO BMFF container. A live writer
// may have emitted ftyp/mdat; a completed movie must also have its moov index.
// Playback/codec validation remains the evidence layer's responsibility.
func finalizedMP4(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return false
	}
	size := stat.Size()
	var offset int64
	ftyp, moov := false, false
	for count := 0; offset < size && count < 1_000_000; count++ {
		var header [16]byte
		if size-offset < 8 {
			return false
		}
		if _, err = file.ReadAt(header[:8], offset); err != nil {
			return false
		}
		atomSize := uint64(binary.BigEndian.Uint32(header[:4]))
		headerSize := uint64(8)
		if atomSize == 1 {
			if size-offset < 16 {
				return false
			}
			if _, err = file.ReadAt(header[8:], offset+8); err != nil {
				return false
			}
			atomSize = binary.BigEndian.Uint64(header[8:])
			headerSize = 16
		}
		if atomSize == 0 {
			atomSize = uint64(size - offset)
		}
		if atomSize < headerSize || atomSize > uint64(size-offset) {
			return false
		}
		switch string(header[4:8]) {
		case "ftyp":
			ftyp = atomSize >= headerSize+8
		case "moov":
			moov = atomSize > headerSize
		}
		offset += int64(atomSize)
	}
	return offset == size && ftyp && moov
}
