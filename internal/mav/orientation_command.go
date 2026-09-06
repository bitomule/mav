package mav

import (
	"context"
	"strconv"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// uiOrientation rotates the simulator AND records what it rotated it to.
//
// The recording is the point. Anything can turn a simulator, but only two
// things leave a trace MAV can read: Simulator.app writes a window angle to a
// user default, and this command writes its own declaration. A rotation
// applied by `baguette orientation`, a raw GSEvent or an app that is
// landscape-only leaves neither, and the accessibility tree alone cannot say
// which of the two landscapes is in effect -- they differ by 180 degrees, so
// a guess puts every subsequent tap in the diagonally opposite corner.
//
// So this is not a convenience wrapper over `baguette orientation`. It is how
// coordinate gestures stay correct on a headless simulator, where
// Simulator.app has no window to have an angle at all.
func (c CLI) uiOrientation(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
		return Fail("orientation_unsupported_on_target", map[string]string{
			"target_kind": targetKindLabel(targetKind(cfg)),
			"next":        "device orientation is a simulator feature; rotate a physical device by hand",
		}).Write(c.Stdout)
	}
	value := ""
	for _, arg := range args {
		if len(arg) > 0 && arg[0] == '-' {
			continue
		}
		value = arg
		break
	}
	if value == "" {
		return Fail("orientation_value_missing", map[string]string{
			"usage": "mav ui orientation " + orientationValueUsage(),
		}).Write(c.Stdout)
	}
	rotation, known := declaredOrientationRotation(value)
	if !known {
		return Fail("orientation_value_invalid", map[string]string{
			"value": value,
			"usage": "mav ui orientation " + orientationValueUsage(),
		}).Write(c.Stdout)
	}
	udid := cfg.SimulatorUDID
	if udid == "" {
		return Fail("orientation_target_missing", map[string]string{
			"next": "select a simulator with mav sim select first",
		}).Write(c.Stdout)
	}
	res := c.Runner.Run(ctx, "baguette", "orientation", "--udid", udid, value)
	if res.Err != nil {
		return Fail("orientation_failed", map[string]string{
			"stderr": firstLine(firstNonEmpty(res.Stderr, res.Stdout)),
			"tool":   "baguette",
			"next":   "mav setup --install baguette",
		}).Write(c.Stdout)
	}
	// The declaration is written only after the tool reported success, so a
	// failed rotation never leaves MAV believing a rotation is in effect --
	// which would be worse than not knowing, because every later tap would
	// be transformed into a space the device is not in.
	//
	// The window angle is sampled right now, at the moment the declaration
	// becomes true, so a LATER Simulator.app rotation can be told apart from
	// this one: resolveRotation compares a future live reading against it.
	windowAngle := simulatorRotationAngle(c.Runner, udid)
	if err := writeDeclaredOrientation(c.Root, udid, declaredOrientation{Value: value, Rotation: rotation, WindowAngle: &windowAngle}); err != nil {
		// The rotation genuinely happened but mav could not record it, so
		// any PREVIOUS declaration (and the angle-keyed screen cache that
		// goes with it) is now describing an orientation the device is no
		// longer in. Leaving them in place would silently apply the old
		// angle to every later gesture instead of the new one; discarding
		// them degrades to "no declaration", which falls back to the
		// window angle (or no compensation) instead of a confidently wrong
		// old one.
		clearDeclaredOrientation(c.Root, udid)
		clearScreenCache(c.Root, udid)
		return Fail("orientation_not_recorded", map[string]string{
			"error": err.Error(),
			"next":  "the simulator did rotate but mav could not record it; the previous declaration was discarded -- re-run mav ui orientation before dispatching coordinate gestures",
		}).Write(c.Stdout)
	}
	// The screen-size cache is keyed by the angle it was probed under, so a
	// stale entry from the previous orientation would be reused for one
	// gesture before anything re-probed. Dropping it here makes the next
	// gesture re-probe against the screen that is actually on display.
	clearScreenCache(c.Root, udid)
	fields := map[string]string{
		"value":  value,
		"driver": "baguette",
		"udid":   udid,
	}
	if rotation != 0 {
		fields["rotation"] = strconv.Itoa(rotation)
	} else {
		// portrait-upside-down resolves to no rotation on purpose; saying so
		// stops it reading as "MAV will compensate for this".
		fields["rotation"] = "0"
		if value == orientationUpsideDown {
			fields["note"] = "upside-down is indistinguishable from portrait in the accessibility tree, so coordinate gestures are dispatched unrotated"
		}
	}
	c.appendCurrentCommand("mav ui orientation "+value, res)
	return c.OK("ui.orientation", fields).Write(c.Stdout)
}
