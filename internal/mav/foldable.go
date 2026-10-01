package mav

import (
	"context"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

const simHingeUsage = `Usage:
  mav sim hinge                       read the hinge angle
  mav sim hinge closed|open|flat      fold to a pose: 0°, 130° (Device Hub's book pose), 180°
  mav sim hinge --angle DEG           any angle from 0 to 180
  [--duration SECONDS]                how long the sweep takes (default 0.8, as Device Hub)

Folds a foldable simulator (iPhone Duo, Xcode 27.1 / iOS 27.1). The hinge
decides which panel is lit: the 466×678 cover while folded, the 669×951 inner
panel once open, and SpringBoard turns landscape when it opens. Every later
gesture and capture follows the lit panel.

Book, laptop and tent are an angle plus how the device is held in space; no
CLI can turn the device in space yet, so they are reachable only as an angle.

angle=unknown is normal for a few seconds after mav sim heal: the hinge
reading goes silent until the hinge next moves.`

const simHealUsage = `Usage: mav sim heal [--force]

Xcode 27's Device Hub attaches its own input daemon to every booted simulator,
and the iOS 27 runtime then tears down the input services every driver uses:
taps, swipes and button presses ack and land nowhere. heal restarts backboardd
to bring them back. It restarts SpringBoard too, so it kills the app under
test; relaunch it after. mav sim boot heals unasked, while nothing is running.

It does nothing when the simulator is not shadowed; --force heals anyway.
Relaunching Device Hub shadows the simulator again.`

var hingePoses = map[string]bool{"closed": true, "open": true, "flat": true}

type hingeArgsFailure struct {
	code   string
	fields map[string]string
}

// hingeSpecFromArgs is the one parser for `mav sim hinge`, the sim.hinge flow
// step and its lint, so none of them can accept what another refuses.
func hingeSpecFromArgs(args []string) (drivers.HingeSpec, *hingeArgsFailure) {
	spec := drivers.HingeSpec{}
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		if !hingePoses[args[0]] {
			return spec, &hingeArgsFailure{"hinge_pose_invalid", map[string]string{"pose": args[0], "usage": "mav sim hinge closed|open|flat | --angle DEG"}}
		}
		spec.Pose = args[0]
	}
	if raw := flagValue(args, "--angle"); raw != "" {
		angle, err := strconv.ParseFloat(raw, 64)
		if err != nil || angle < 0 || angle > 180 {
			return spec, &hingeArgsFailure{"hinge_angle_invalid", map[string]string{"angle": raw, "next": "pass an angle from 0 to 180"}}
		}
		if spec.Pose != "" {
			return spec, &hingeArgsFailure{"hinge_pose_and_angle", map[string]string{"next": "pass a pose or --angle, not both"}}
		}
		spec.Angle = &angle
	}
	if raw := flagValue(args, "--duration"); raw != "" {
		duration, err := strconv.ParseFloat(raw, 64)
		if err != nil || duration < 0 {
			return spec, &hingeArgsFailure{"hinge_duration_invalid", map[string]string{"duration": raw}}
		}
		spec.Duration = duration
	}
	return spec, nil
}

// hingeFlowArgs turns a sim.hinge step into the command's arguments:
//
//   - sim.hinge: { pose: open }
//   - sim.hinge: { angle: "95", duration: "1.2" }
func hingeFlowArgs(params map[string]string) []string {
	var args []string
	if pose := params["pose"]; pose != "" {
		args = append(args, pose)
	}
	if angle := params["angle"]; angle != "" {
		args = append(args, "--angle", angle)
	}
	if duration := params["duration"]; duration != "" {
		args = append(args, "--duration", duration)
	}
	return args
}

func hingeFlowProblem(params map[string]string, bound bool) string {
	if params["pose"] == "" && params["angle"] == "" && !bound {
		return "sim.hinge needs pose: closed|open|flat or angle: 0-180"
	}
	if _, failure := hingeSpecFromArgs(hingeFlowArgs(params)); failure != nil {
		return failure.code + " " + strings.TrimSpace(failure.fields["pose"]+failure.fields["angle"]+failure.fields["duration"]+" "+failure.fields["next"])
	}
	return ""
}

func (c CLI) simHinge(ctx context.Context, args []string) error {
	spec, failure := hingeSpecFromArgs(args)
	if failure != nil {
		return Fail(failure.code, failure.fields).Write(c.Stdout)
	}
	target, err := c.simTarget("hinge", "only a foldable simulator has a hinge; select one with mav sim select --device 'iPhone Duo'")
	if err != nil {
		return err
	}
	driver, _, routeErr := c.router().Route(ctx, drivers.CapHinge, target, "")
	if routeErr != nil {
		return Fail("hinge_unsupported", map[string]string{"stderr": firstLine(routeErr.Error()), "next": "brew upgrade baguette"}).Write(c.Stdout)
	}
	hinge, ok := driver.(drivers.HingeDriver)
	if !ok {
		return Fail("hinge_unsupported", map[string]string{"driver": driver.ID()}).Write(c.Stdout)
	}
	state, hingeErr := hinge.Hinge(ctx, target, spec)
	if hingeErr != nil {
		return Fail("hinge_failed", map[string]string{"stderr": firstLine(hingeErr.Error()), "next": "the simulator must be a booted iPhone Duo on an iOS 27.1 runtime"}).Write(c.Stdout)
	}
	if spec.Pose != "" || spec.Angle != nil {
		clearScreenCache(c.Root, target.UDID)
		declared := poseAngle(spec.Pose)
		if spec.Angle != nil {
			declared = *spec.Angle
		}
		writeDeclaredHinge(c.Root, target.UDID, declared)
	}
	fields := map[string]string{"udid": target.UDID, "driver": driver.ID(), "angle": "unknown"}
	if state.Angle != nil {
		fields["angle"] = strconv.FormatFloat(*state.Angle, 'f', -1, 64)
	}
	if spec.Pose != "" {
		fields["pose"] = spec.Pose
	}
	return c.OK("sim.hinge", fields).Write(c.Stdout)
}

// simHeal is the manual form of what boot does unasked: undo Device Hub's
// shadowing of the simulator's input. It restarts SpringBoard, so it kills the
// app under test, which is why a gesture never does it on its own.
func (c CLI) simHeal(ctx context.Context, args []string) error {
	target, err := c.simTarget("heal", "Device Hub only shadows simulator input")
	if err != nil {
		return err
	}
	before := inputShadowed(ctx, c.Runner, target.UDID)
	if before == shadowNo && !hasFlag(args, "--force") {
		return c.OK("sim.heal", map[string]string{"udid": target.UDID, "shadowed": "false", "healed": "false"}).Write(c.Stdout)
	}
	driver, healErr := c.healInput(ctx, target)
	if healErr != nil {
		return Fail("heal_failed", map[string]string{"stderr": firstLine(healErr.Error()), "next": "brew upgrade baguette"}).Write(c.Stdout)
	}
	clearScreenCache(c.Root, target.UDID)
	return c.OK("sim.heal", map[string]string{
		"udid":     target.UDID,
		"driver":   driver,
		"shadowed": string(before),
		"healed":   "true",
		"after":    string(inputShadowed(ctx, c.Runner, target.UDID)),
		"note":     "SpringBoard restarted; relaunch the app under test",
	}).Write(c.Stdout)
}

func (c CLI) healInput(ctx context.Context, target drivers.Target) (string, error) {
	driver, _, routeErr := c.router().Route(ctx, drivers.CapInputHeal, target, "")
	if routeErr != nil {
		return "", routeErr
	}
	healer, ok := driver.(drivers.InputHealDriver)
	if !ok {
		return "", &drivers.ErrNoDriver{Capability: drivers.CapInputHeal, Target: target}
	}
	return driver.ID(), healer.HealInput(ctx, target)
}

type shadowState string

const (
	shadowYes     shadowState = "true"
	shadowNo      shadowState = "false"
	shadowUnknown shadowState = "unknown"
)

// inputShadowed asks the guest whether Device Hub's dtuhidd attached during
// this backboardd's life, which is exactly when taps start acking and landing
// nowhere. Xcode 26 runtimes never publish the key and read as 0.
func inputShadowed(ctx context.Context, runner Runner, udid string) shadowState {
	res := runner.Run(ctx, "xcrun", "simctl", "spawn", udid, "notifyutil", "-g", "com.apple.coredevice.dtuhidd.active")
	if res.Err != nil {
		return shadowUnknown
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) == 0 {
		return shadowUnknown
	}
	switch fields[len(fields)-1] {
	case "0":
		return shadowNo
	case "1":
		return shadowYes
	}
	return shadowUnknown
}

// shadowedInputNext names Device Hub as the cause when a verified gesture
// changed nothing, because then no selector or retry will help: every driver's
// input is gone until the simulator is healed.
func (c CLI) shadowedInputNext(ctx context.Context, cfg Config, fields map[string]string) {
	if targetKind(cfg) != drivers.KindSim || cfg.SimulatorUDID == "" {
		return
	}
	if inputShadowed(ctx, c.Runner, cfg.SimulatorUDID) != shadowYes {
		return
	}
	fields["input"] = "shadowed"
	fields["next"] = "Xcode 27's Device Hub has taken this simulator's input, so every gesture acks and lands nowhere; run `mav sim heal` (restarts SpringBoard, relaunch the app), then repeat"
}
