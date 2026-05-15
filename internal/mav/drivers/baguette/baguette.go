// Package baguette wraps the baguette CLI (https://github.com/tddworks/baguette)
// — a Swift host-side simulator driver built on private SimulatorKit symbols.
// Baguette is the canonical multitouch / system-UI / hardware-button path on
// simulator, replacing Appium for those operations.
//
// Sim-only: baguette has no device support. Provides() returns an empty set
// on device targets so the router never picks it; cli.go must surface a
// structured `gesture_unsupported_on_device` error in that case.
//
// CLI surface assumed (validated by mav setup against installed binary):
//
//	baguette --udid UDID tap (--x X --y Y | --id ID | --text TEXT)
//	baguette --udid UDID swipe --start-x X1 --start-y Y1 --end-x X2 --end-y Y2 [--duration MS]
//	baguette --udid UDID pinch --x X --y Y --scale S [--pan-x PX --pan-y PY] [--duration MS]
//	baguette --udid UDID rotate --x X --y Y --degrees D [--duration MS]
//	baguette --udid UDID two-finger-pan --x X --y Y --pan-x PX --pan-y PY [--hold MS] [--duration MS]
//	baguette --udid UDID type --text TEXT
//	baguette --udid UDID erase [--text TEXT] [--focused]
//	baguette --udid UDID hide-keyboard
//	baguette --udid UDID button (home|lock|volume-up|volume-down)
//	baguette --udid UDID tree --json [--include-system]
//	baguette --udid UDID actions --file PATH (W3C Actions JSON)
//	baguette --udid UDID probe (used for health check)
//
// If the real CLI differs, only buildArgs / shell-out helpers below change.
package baguette

import (
	"context"
	"fmt"
	"strconv"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key for this driver.
const ID = "baguette"

// Driver wraps the baguette CLI.
type Driver struct {
	exec drivers.Executor
	path string // resolved binary path, populated by Probe
}

// New constructs a Driver.
func New(exec drivers.Executor) *Driver { return &Driver{exec: exec} }

func (d *Driver) ID() string { return ID }

// Provides advertises the capabilities baguette covers on simulator targets.
// On device, returns empty: baguette does not support physical devices.
func (d *Driver) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.IsDevice() {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapTap,
		drivers.CapSemanticTap,
		drivers.CapCoordTap,
		drivers.CapSwipe,
		drivers.CapType,
		drivers.CapErase,
		drivers.CapHideKeyboard,
		drivers.CapPinch,
		drivers.CapRotate,
		drivers.CapTwoFingerPan,
		drivers.CapW3CActions,
		drivers.CapTreeSystem,
		drivers.CapHardwareBtn,
	)
}

// Cost favours baguette for the multitouch / system-UI capabilities it is the
// canonical owner of. Single-finger primitives (semantic tap, swipe, type) are
// flagged higher than AXe so AXe still wins those.
func (d *Driver) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapPinch, drivers.CapRotate, drivers.CapTwoFingerPan, drivers.CapW3CActions,
		drivers.CapErase, drivers.CapHideKeyboard, drivers.CapTreeSystem, drivers.CapHardwareBtn:
		return 0
	case drivers.CapType, drivers.CapSemanticTap, drivers.CapCoordTap, drivers.CapSwipe, drivers.CapTap:
		return 50
	default:
		return 100
	}
}

// Probe verifies baguette is on PATH and answers a sanity command. The sanity
// command is important: SimulatorKit private symbols can change shape across
// iOS majors (iOS 25 -> 26 added a 9th HID arg), so a clean PATH lookup is not
// sufficient evidence the driver works.
func (d *Driver) Probe(ctx context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("baguette")
	if err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "baguette not on PATH",
			Next:   "mav setup",
		}
	}
	d.path = path

	// Sanity probe: run `baguette probe` which exits 0 if SimulatorKit symbols
	// resolve. If this fails, the host iOS Simulator runtime is incompatible.
	res := d.exec.Run(ctx, "baguette", "probe")
	if res.Err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthDegraded,
			Detail: "baguette installed but probe failed: " + firstLine(res.Stderr),
			Next:   "check https://github.com/tddworks/baguette for iOS support",
			Tools:  map[string]string{"baguette": path},
		}
	}
	return drivers.HealthReport{
		State: drivers.HealthOK,
		Tools: map[string]string{"baguette": path},
	}
}

// Warm has no async work to do.
func (d *Driver) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// --- functional methods --------------------------------------------------

// Tap dispatches a tap. When Selector is non-zero, baguette resolves it
// semantically (system UI inclusive). Otherwise it taps the coordinate.
func (d *Driver) Tap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	args := udid(target, "tap")
	switch {
	case spec.Selector.ID != "":
		args = append(args, "--id", spec.Selector.ID)
	case spec.Selector.Text != "":
		args = append(args, "--text", spec.Selector.Text)
	case spec.Selector.Value != "":
		args = append(args, "--value", spec.Selector.Value)
	default:
		args = append(args, "--x", strconv.Itoa(spec.X), "--y", strconv.Itoa(spec.Y))
	}
	if spec.Duration > 0 {
		args = append(args, "--duration", strconv.Itoa(spec.Duration))
	}
	if err := d.runOK(ctx, "tap", args); err != nil {
		return drivers.TapResult{}, err
	}
	return drivers.TapResult{
		MatchedID:    spec.Selector.ID,
		MatchedText:  spec.Selector.Text,
		MatchedValue: spec.Selector.Value,
		X:            spec.X,
		Y:            spec.Y,
	}, nil
}

// Swipe dispatches a single-finger swipe between (StartX, StartY) and
// (EndX, EndY). Direction is currently a hint only; coordinates are required.
func (d *Driver) Swipe(ctx context.Context, target drivers.Target, spec drivers.SwipeSpec) error {
	args := udid(target, "swipe",
		"--start-x", strconv.Itoa(spec.StartX),
		"--start-y", strconv.Itoa(spec.StartY),
		"--end-x", strconv.Itoa(spec.EndX),
		"--end-y", strconv.Itoa(spec.EndY),
	)
	if spec.DurationMs > 0 {
		args = append(args, "--duration", strconv.Itoa(spec.DurationMs))
	}
	return d.runOK(ctx, "swipe", args)
}

// Pinch dispatches a two-finger pinch centred at (X, Y).
func (d *Driver) Pinch(ctx context.Context, target drivers.Target, spec drivers.PinchSpec) error {
	args := udid(target, "pinch",
		"--x", strconv.Itoa(spec.X),
		"--y", strconv.Itoa(spec.Y),
		"--scale", strconv.FormatFloat(spec.Scale, 'f', -1, 64),
	)
	if spec.PanX != 0 || spec.PanY != 0 {
		args = append(args, "--pan-x", strconv.Itoa(spec.PanX), "--pan-y", strconv.Itoa(spec.PanY))
	}
	if spec.DurationMs > 0 {
		args = append(args, "--duration", strconv.Itoa(spec.DurationMs))
	}
	return d.runOK(ctx, "pinch", args)
}

// Rotate dispatches a two-finger rotation.
func (d *Driver) Rotate(ctx context.Context, target drivers.Target, spec drivers.RotateSpec) error {
	args := udid(target, "rotate",
		"--x", strconv.Itoa(spec.X),
		"--y", strconv.Itoa(spec.Y),
		"--degrees", strconv.FormatFloat(spec.Degrees, 'f', -1, 64),
	)
	if spec.DurationMs > 0 {
		args = append(args, "--duration", strconv.Itoa(spec.DurationMs))
	}
	return d.runOK(ctx, "rotate", args)
}

// TwoFingerPan dispatches a parallel two-finger pan.
func (d *Driver) TwoFingerPan(ctx context.Context, target drivers.Target, spec drivers.TwoFingerPanSpec) error {
	args := udid(target, "two-finger-pan",
		"--x", strconv.Itoa(spec.X),
		"--y", strconv.Itoa(spec.Y),
		"--pan-x", strconv.Itoa(spec.PanX),
		"--pan-y", strconv.Itoa(spec.PanY),
	)
	if spec.DurationMs > 0 {
		args = append(args, "--duration", strconv.Itoa(spec.DurationMs))
	}
	if spec.HoldMs > 0 {
		args = append(args, "--hold", strconv.Itoa(spec.HoldMs))
	}
	return d.runOK(ctx, "two-finger-pan", args)
}

// W3CActions forwards a W3C Actions JSON body to baguette's translator. The
// JSON is written to a temp file and passed via --file so the CLI can stream it.
func (d *Driver) W3CActions(ctx context.Context, target drivers.Target, body []byte) error {
	path, cleanup, err := writeTemp(body)
	if err != nil {
		return fmt.Errorf("baguette w3c: %w", err)
	}
	defer cleanup()
	args := udid(target, "actions", "--file", path)
	return d.runOK(ctx, "actions", args)
}

// Type sends text via the on-screen keyboard.
func (d *Driver) Type(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	args := udid(target, "type", "--text", spec.Text)
	if spec.Selector.ID != "" {
		args = append(args, "--id", spec.Selector.ID)
	}
	if spec.Focused {
		args = append(args, "--focused")
	}
	return d.runOK(ctx, "type", args)
}

// Erase clears the focused field (or one identified by Selector).
func (d *Driver) Erase(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	args := udid(target, "erase")
	if spec.Selector.ID != "" {
		args = append(args, "--id", spec.Selector.ID)
	} else if spec.Text != "" {
		args = append(args, "--text", spec.Text)
	}
	if spec.Focused {
		args = append(args, "--focused")
	}
	return d.runOK(ctx, "erase", args)
}

// HideKeyboard dismisses the on-screen keyboard.
func (d *Driver) HideKeyboard(ctx context.Context, target drivers.Target) error {
	args := udid(target, "hide-keyboard")
	return d.runOK(ctx, "hide-keyboard", args)
}

// Tree returns the system+app accessibility tree as JSON. The caller (cli.go)
// feeds the bytes straight into ExtractElements.
func (d *Driver) Tree(ctx context.Context, target drivers.Target, spec drivers.TreeSpec) (drivers.TreeResult, error) {
	args := udid(target, "tree", "--json")
	if spec.IncludeSystem {
		args = append(args, "--include-system")
	}
	res := d.exec.Run(ctx, "baguette", args...)
	if res.Err != nil {
		return drivers.TreeResult{}, fmt.Errorf("baguette tree: %w (%s)", res.Err, firstLine(res.Stderr))
	}
	return drivers.TreeResult{JSON: []byte(res.Stdout)}, nil
}

// PressButton dispatches a hardware button press.
func (d *Driver) PressButton(ctx context.Context, target drivers.Target, btn drivers.HardwareButton) error {
	switch btn {
	case drivers.BtnHome, drivers.BtnLock, drivers.BtnVolumeUp, drivers.BtnVolumeDown:
	default:
		return fmt.Errorf("baguette: unsupported hardware button %q", btn)
	}
	args := udid(target, "button", string(btn))
	return d.runOK(ctx, "button", args)
}

// --- helpers ------------------------------------------------------------

// udid prepends `--udid UDID <op>` to the trailing args.
func udid(target drivers.Target, op string, extra ...string) []string {
	out := []string{"--udid", target.UDID, op}
	return append(out, extra...)
}

// runOK runs baguette with args and wraps non-zero exits in a structured error.
func (d *Driver) runOK(ctx context.Context, op string, args []string) error {
	res := d.exec.Run(ctx, "baguette", args...)
	if res.Err != nil {
		return fmt.Errorf("baguette %s: %w (%s)", op, res.Err, firstLine(res.Stderr))
	}
	if res.Code != 0 {
		return fmt.Errorf("baguette %s: exit %d (%s)", op, res.Code, firstLine(res.Stderr))
	}
	return nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
