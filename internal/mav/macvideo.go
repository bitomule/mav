package mav

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bitomule/mav/internal/mav/drivers"
	"github.com/bitomule/mav/internal/mav/drivers/macos"
)

// stopVideoThroughDriver gives the recorder a chance to finish its file
// before the process holding it is signalled, and then puts that file where
// the run expects it. Returns a warning rather than an error: by the time
// this runs the recording either exists or does not, and refusing to
// complete `evidence stop` would leave the run with a live recorder and a
// pid file nobody will clean up.
//
// Simulator targets skip it entirely. There the recorder IS the process,
// SIGINT is how it finalizes, and routing a capability just to send the
// same signal would add a failure mode to a path that works.
func (c CLI) stopVideoThroughDriver(ctx context.Context, cfg Config, run RunState, pid int) string {
	if targetKind(cfg) != drivers.KindMac {
		return ""
	}
	target := targetFromConfig(cfg)
	driver, _, err := c.router().Route(ctx, drivers.CapVideo, target, "")
	if err != nil {
		return "video_recorder_unavailable: no driver could finish the recording"
	}
	recorder, ok := driver.(drivers.VideoDriver)
	if !ok {
		return "video_recorder_unavailable: " + driver.ID() + " cannot finish a recording"
	}
	if stopErr := recorder.VideoStop(ctx, target, pid); stopErr != nil {
		return "video_stop_failed: " + firstLine(stopErr.Error())
	}
	return c.collectMacVideo(ctx, cfg, run, driver.ID())
}

// collectMacVideo moves the recorder's own output to the run's video path.
//
// It goes through the Runner and not the os package because in VM mode the
// file is on the guest: moving it here would find nothing, and the evidence
// sync that runs afterwards would bring back a run directory whose report
// points at a video that never arrived.
func (c CLI) collectMacVideo(ctx context.Context, cfg Config, run RunState, driverID string) string {
	if driverID != macos.CuaID {
		return ""
	}
	path := evidenceVideoPath(cfg, run)
	source := macos.CuaVideoFile(path)
	if result := c.Runner.Run(ctx, "mv", source, path); result.Err != nil {
		return "video_not_collected: " + filepath.Base(source) + " stayed where the recorder wrote it"
	}
	c.Runner.Run(ctx, "rm", "-rf", filepath.Dir(source))
	return ""
}

// pruneMacVideoLeftovers removes the recorder's per-action trail from THIS
// machine, after the evidence sync has run.
//
// Deleting it on the guest is not enough and the reason is worth naming:
// the trail is written while the run is in flight, so the per-command sync
// has already copied it here long before the video is collected. Nothing in
// report.json points at those folders, and a run directory carrying
// hundreds of files no reader is told about is not evidence, it is noise
// that makes the real evidence harder to find.
func (c CLI) pruneMacVideoLeftovers(cfg Config, run RunState) {
	if targetKind(cfg) != drivers.KindMac {
		return
	}
	_ = os.RemoveAll(filepath.Dir(macos.CuaVideoFile(evidenceVideoPath(cfg, run))))
}
