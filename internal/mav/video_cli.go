package mav

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type recordingIdentity struct {
	Driver string
	Target drivers.Target
	Path   string
}

func (c CLI) videoCommand(ctx context.Context, opts GlobalOptions, args []string) error {
	failc := c
	if len(args) > 0 && args[0] == "stream" {
		stderr := c.Stderr
		if stderr == nil {
			stderr = io.Discard
		}
		failc = c.withStdout(stderr)
	}
	if len(args) == 0 {
		return failc.automationFailure(fmt.Errorf("usage: mav video record --out PATH | stream --format FORMAT"))
	}
	cfg, err := c.mustLoadConfig()
	if err != nil {
		return failc.failTargetCommand(err)
	}
	if err = c.requireAXe2(ctx, cfg); err != nil {
		return failc.automationFailure(err)
	}
	binding, rest, err := splitInputContext(args[1:])
	if err != nil {
		return failc.automationFailure(err)
	}
	req, err := automationCLI(args[0], rest)
	if err != nil {
		return failc.automationFailure(err)
	}
	target := targetFromConfig(cfg)
	target.InputContext = binding
	if e := validateOperationContext("video", binding); e != nil {
		return failc.automationFailure(e)
	}
	cap := drivers.CapVideo
	if args[0] == "stream" {
		cap = drivers.CapVideoStream
	}
	d, _, err := c.router().Route(ctx, cap, target, routerPrefer(opts.PreferDriver))
	if err != nil {
		return failc.automationFailure(err)
	}
	if e := validateContextDriver(d, target); e != nil {
		return failc.automationFailure(e)
	}
	video, ok := d.(drivers.NativeVideoDriver)
	if !ok {
		return failc.automationFailure(fmt.Errorf("driver %s does not support this video operation", d.ID()))
	}
	if raw, ok := req.Options["out"]; ok {
		req.Options["OutPath"] = raw
		delete(req.Options, "out")
	}
	switch args[0] {
	case "record":
		var spec drivers.VideoRecordSpec
		if err = decodeOptions(req.Options, &spec); err != nil {
			return failc.automationFailure(err)
		}
		if spec.OutPath == "" {
			return failc.automationFailure(fmt.Errorf("record needs --out PATH"))
		}
		if !filepath.IsAbs(spec.OutPath) {
			spec.OutPath = filepath.Join(c.Root, spec.OutPath)
		}
		result, err := video.Record(ctx, target, spec)
		if err != nil {
			return failc.automationFailure(err)
		}
		if err = c.OK("video.record", map[string]string{"driver": d.ID(), "file": spec.OutPath}).Write(c.Stdout); err != nil {
			return err
		}
		if len(result.Data) > 0 {
			_, err = fmt.Fprintln(c.Stdout, string(result.Data))
		}
		return err
	case "stream":
		var spec drivers.VideoStreamSpec
		if err = decodeOptions(req.Options, &spec); err != nil {
			return failc.automationFailure(err)
		}
		stderr := c.Stderr
		if stderr == nil {
			stderr = io.Discard
		}
		if err = video.Stream(ctx, target, spec, c.Stdout, stderr); err != nil {
			return c.withStdout(stderr).automationFailure(err)
		}
		return nil // stdout is exclusively the requested media format.
	default:
		return failc.automationFailure(fmt.Errorf("unknown video operation %q", args[0]))
	}
}

// stopAbandonedVideo uses the same recorded driver identity as explicit stop.
// A failed shutdown must leave the process reachable by the run's watchdog.
func (c CLI) stopAbandonedVideo(ctx context.Context, run RunState) {
	if !fileExists(filepath.Join(run.Dir, "video-driver.json")) {
		stopVideoRecording(c.Runner, run)
		return
	}
	pid, err := readPID(filepath.Join(run.Dir, "video.pid"))
	if err != nil {
		return
	}
	if warning := c.stopVideoThroughDriver(ctx, Config{}, run, pid); warning != "" {
		return
	}
	_ = os.Remove(filepath.Join(run.Dir, "video.pid"))
	removeProcess(run, pid)
}
