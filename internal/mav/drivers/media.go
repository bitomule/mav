package drivers

import (
	"context"
	"io"
)

// MediaExecutor preserves binary stdout separately from diagnostic stderr.
// Cancellation requests graceful termination so recorders can finish their file.
type MediaExecutor interface {
	RunStreams(context.Context, io.Writer, io.Writer, string, ...string) ExecResult
	StartStreams(context.Context, string, string, string, ...string) (int, error)
	StopProcess(context.Context, int) error
	AwaitRecording(context.Context, int, string, string) error
}
type VideoRecordSpec struct {
	OutPath, LogPath    string
	FPS, Quality        int
	Scale               float64
	FollowActiveDisplay bool
}
type VideoStreamSpec struct {
	Format       string
	FPS, Quality int
	Scale        float64
}
type NativeVideoDriver interface {
	Driver
	Record(context.Context, Target, VideoRecordSpec) (NativeResult, error)
	Stream(context.Context, Target, VideoStreamSpec, io.Writer, io.Writer) error
}
