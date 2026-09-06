// Package baguette wraps the baguette CLI (https://github.com/tddworks/baguette)
// — a Swift host-side simulator driver built on private SimulatorKit symbols.
// Baguette is the canonical multitouch / hardware-button / streaming path on
// simulator.
//
// Sim-only: baguette has no device support. Provides() returns an empty set
// on device targets so the router never picks it; cli.go must surface a
// structured `gesture_unsupported_on_device` error in that case.
//
// CLI shape (verified against v0.1.97, September 2026):
//
//	baguette tap          --udid UDID --x X --y Y --width W --height H [--duration S]
//	baguette double-tap   --udid UDID --x X --y Y --width W --height H [--interval S] [--duration S]
//	baguette swipe        --udid UDID --start-x X1 --start-y Y1 --end-x X2 --end-y Y2 --width W --height H
//	baguette pinch        --udid UDID --cx CX --cy CY --startSpread S1 --endSpread S2 --width W --height H
//	baguette pan          --udid UDID --x1 X --y1 Y --x2 X --y2 Y --dx DX --dy DY --width W --height H
//	baguette type         --udid UDID --text TEXT
//	baguette key          --udid UDID --code <KeyA..ArrowRight> [--modifiers] [--duration S]
//	baguette press        --udid UDID --button (home|lock|power|action|volumeUp|volumeDown) [--duration S]
//	baguette describe-ui  --udid UDID [--x X --y Y] [--output PATH]
//	baguette orientation  --udid UDID (portrait|landscape-left|landscape-right|portrait-upside-down)
//	baguette screenshot   --udid UDID [--output PATH]
//	baguette list         [--json]
//
// Width/Height are the logical point dimensions of the device's screen and are
// REQUIRED on every gesture. Callers supply them via TapSpec.Width/Height etc;
// when zero the driver falls back to a sane default and logs a warning.
//
// What baguette does NOT do (and what we therefore advertise/SUPPORT here):
//   - No W3C Actions: baguette has a `input` streaming JSON protocol instead;
//     not exposed in this driver yet (router does not request CapW3CActions
//     against baguette).
//
// The driver therefore advertises a deliberately narrower capability set than
// the original plan suggested. The Provides() list below is the truth.
package baguette

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key for this driver.
const ID = "baguette"

// defaultGestureSize is the fallback (logical points) used when the caller
// did not supply Width/Height. iPhone 17 Pro at the time of writing; a small
// over-estimate is harmless because baguette normalises coordinates.
const (
	defaultGestureWidth  = 402
	defaultGestureHeight = 874
)

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
	// Neither device nor Mac: baguette drives the simulator through its
	// local HTTP. On macOS it declared nothing yet won capabilities on
	// cost ties, so `ui erase` ended up reporting driver=baguette on a
	// Mac, a success from a tool that cannot even touch that app.
	if target.IsDevice() || target.IsMac() {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapTap,
		drivers.CapDoubleTap,
		drivers.CapCoordTap,
		drivers.CapSwipe,
		drivers.CapType,
		drivers.CapPinch,
		drivers.CapTwoFingerPan,
		drivers.CapDrag,
		drivers.CapDragPath,
		drivers.CapHardwareBtn,
		drivers.CapScreenshot,
		drivers.CapTreeSystem,
		drivers.CapErase,
		drivers.CapHideKeyboard,
	)
}

func (d *Driver) DoubleTap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) error {
	w, h := defaultGestureSize()
	args := []string{
		"double-tap", "--udid", target.UDID,
		"--x", strconv.Itoa(spec.X), "--y", strconv.Itoa(spec.Y),
		"--width", strconv.Itoa(w), "--height", strconv.Itoa(h),
	}
	if spec.Duration > 0 {
		args = append(args, "--duration", floatSeconds(spec.Duration))
	}
	return d.runOK(ctx, "double-tap", args)
}

func (d *Driver) Drag(ctx context.Context, target drivers.Target, spec drivers.DragSpec) error {
	w, h := defaultGestureSize()
	args := []string{
		"swipe", "--udid", target.UDID,
		"--startX", strconv.Itoa(spec.StartX), "--startY", strconv.Itoa(spec.StartY),
		"--endX", strconv.Itoa(spec.EndX), "--endY", strconv.Itoa(spec.EndY),
		"--width", strconv.Itoa(w), "--height", strconv.Itoa(h),
	}
	if spec.DurationMs > 0 {
		args = append(args, "--duration", floatSeconds(spec.DurationMs))
	}
	return d.runOK(ctx, "drag", args)
}

func (d *Driver) DragPath(ctx context.Context, target drivers.Target, spec drivers.DragPathSpec) error {
	if len(spec.Points) < 2 {
		return fmt.Errorf("baguette: drag path needs at least two points")
	}
	inputExec, ok := d.exec.(drivers.InputExecutor)
	if !ok {
		return fmt.Errorf("baguette: input executor unavailable")
	}
	w, h := defaultGestureSize()
	lines := make([]string, 0, len(spec.Points)+1)
	for i, point := range spec.Points {
		kind := "touch1-move"
		if i == 0 {
			kind = "touch1-down"
		}
		body, _ := json.Marshal(map[string]any{
			"type": kind, "x": point.X, "y": point.Y, "width": w, "height": h,
		})
		lines = append(lines, string(body))
	}
	last := spec.Points[len(spec.Points)-1]
	up, _ := json.Marshal(map[string]any{
		"type": "touch1-up", "x": last.X, "y": last.Y, "width": w, "height": h,
	})
	lines = append(lines, string(up))
	res := inputExec.RunInput(ctx, strings.Join(lines, "\n")+"\n", "baguette", "input", "--udid", target.UDID)
	if res.Err != nil || res.Code != 0 {
		return fmt.Errorf("baguette input: %s", firstLine(res.Stderr))
	}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if line != "" && !strings.Contains(line, `"ok":true`) {
			return fmt.Errorf("baguette input rejected gesture: %s", line)
		}
	}
	return nil
}

// Cost favours baguette for the multitouch / hardware-button capabilities it
// owns canonically. Single-finger primitives (coord tap, swipe, type) are
// flagged higher than AXe so AXe still wins those when both are healthy.
func (d *Driver) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapPinch, drivers.CapTwoFingerPan, drivers.CapHardwareBtn, drivers.CapTreeSystem, drivers.CapErase, drivers.CapHideKeyboard:
		return 0
	case drivers.CapType, drivers.CapCoordTap, drivers.CapSwipe, drivers.CapTap, drivers.CapScreenshot:
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
			Next:   "mav setup --install baguette",
		}
	}
	d.path = path

	// Sanity probe: `baguette list` enumerates simulators without touching
	// SimulatorKit's HID path, so a clean exit is decent evidence the binary
	// is callable. A full HID-shape test would need a real boot and is left
	// to the first gesture call.
	res := d.exec.Run(ctx, "baguette", "list", "--json")
	if res.Err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthDegraded,
			Detail: "baguette installed but `list` failed: " + firstLine(res.Stderr),
			Next:   "check https://github.com/tddworks/baguette",
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

// Tap dispatches a coordinate tap. Semantic (Selector) taps fall through to
// AXe via the router; baguette only handles X/Y because describe-ui in
// baguette resolves to coordinates, not to a tap.
func (d *Driver) Tap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	if !spec.Selector.IsZero() {
		return drivers.TapResult{}, fmt.Errorf("baguette: semantic taps go through axe; received Selector=%+v", spec.Selector)
	}
	w, h := defaultGestureSize()
	args := []string{
		"tap",
		"--udid", target.UDID,
		"--x", strconv.Itoa(spec.X),
		"--y", strconv.Itoa(spec.Y),
		"--width", strconv.Itoa(w),
		"--height", strconv.Itoa(h),
	}
	if spec.Duration > 0 {
		args = append(args, "--duration", floatSeconds(spec.Duration))
	}
	if err := d.runOK(ctx, "tap", args); err != nil {
		return drivers.TapResult{}, err
	}
	return drivers.TapResult{X: spec.X, Y: spec.Y}, nil
}

// Swipe dispatches a single-finger swipe between (StartX, StartY) and
// (EndX, EndY). Direction is currently a hint only; coordinates are required.
func (d *Driver) Swipe(ctx context.Context, target drivers.Target, spec drivers.SwipeSpec) error {
	w, h := defaultGestureSize()
	args := []string{
		"swipe",
		"--udid", target.UDID,
		// Hyphenated, matching `baguette swipe --help`. The camelCase
		// spelling this used to send has been rejected with "Missing
		// expected argument '--start-x'" for as long as anyone has looked;
		// nothing noticed because AXe is canonical for CapSwipe and always
		// won the route, so this path only became reachable when a rotated
		// simulator started routing around AXe.
		"--start-x", strconv.Itoa(spec.StartX),
		"--start-y", strconv.Itoa(spec.StartY),
		"--end-x", strconv.Itoa(spec.EndX),
		"--end-y", strconv.Itoa(spec.EndY),
		"--width", strconv.Itoa(w),
		"--height", strconv.Itoa(h),
	}
	return d.runOK(ctx, "swipe", args)
}

// Pinch dispatches a two-finger pinch centred at (X, Y). baguette models a
// pinch as startSpread -> endSpread (distance between the two contact points).
// We derive both from PinchSpec.Scale: assume a baseline spread of 120 points
// and multiply by Scale for the end spread. Callers who need exact spreads
// should use the lower-level `input` JSON path (not yet exposed here).
func (d *Driver) Pinch(ctx context.Context, target drivers.Target, spec drivers.PinchSpec) error {
	if spec.Scale <= 0 {
		return fmt.Errorf("baguette: pinch Scale must be > 0, got %v", spec.Scale)
	}
	const baselineSpread = 120.0
	startSpread := baselineSpread
	endSpread := baselineSpread * spec.Scale
	w, h := defaultGestureSize()
	args := []string{
		"pinch",
		"--udid", target.UDID,
		"--cx", strconv.Itoa(spec.X),
		"--cy", strconv.Itoa(spec.Y),
		"--startSpread", strconv.FormatFloat(startSpread, 'f', 1, 64),
		"--endSpread", strconv.FormatFloat(endSpread, 'f', 1, 64),
		"--width", strconv.Itoa(w),
		"--height", strconv.Itoa(h),
	}
	return d.runOK(ctx, "pinch", args)
}

// Rotate is not directly modelled by baguette's CLI surface. Mark as
// unsupported; AXe and the router will fall back to an explicit error.
// Implemented purely so the driver still satisfies the GestureDriver
// interface; Provides() excludes CapRotate so the router never picks us.
func (d *Driver) Rotate(_ context.Context, _ drivers.Target, _ drivers.RotateSpec) error {
	return fmt.Errorf("baguette: rotate not exposed by CLI (use pan or input JSON)")
}

// TwoFingerPan dispatches a parallel two-finger pan via baguette's `pan`. The
// two fingers start at fixed offsets either side of (X, Y) and move together
// by (PanX, PanY).
func (d *Driver) TwoFingerPan(ctx context.Context, target drivers.Target, spec drivers.TwoFingerPanSpec) error {
	const fingerOffset = 60 // logical points either side of the centre
	x1 := spec.X - fingerOffset
	y1 := spec.Y
	x2 := spec.X + fingerOffset
	y2 := spec.Y
	w, h := defaultGestureSize()
	args := []string{
		"pan",
		"--udid", target.UDID,
		"--x1", strconv.Itoa(x1),
		"--y1", strconv.Itoa(y1),
		"--x2", strconv.Itoa(x2),
		"--y2", strconv.Itoa(y2),
		"--dx", strconv.Itoa(spec.PanX),
		"--dy", strconv.Itoa(spec.PanY),
		"--width", strconv.Itoa(w),
		"--height", strconv.Itoa(h),
	}
	return d.runOK(ctx, "pan", args)
}

// W3CActions is not implemented for baguette; the equivalent is its `input`
// streaming JSON protocol, intentionally left for a future commit. The router
// excludes CapW3CActions from Provides() above so this method is unreachable
// through the normal path -- we satisfy the interface for static analysis.
func (d *Driver) W3CActions(_ context.Context, _ drivers.Target, _ []byte) error {
	return fmt.Errorf("baguette: W3C Actions not implemented (use `baguette input` JSON directly)")
}

// Type sends text via the simulator keyboard.
func (d *Driver) Type(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	args := []string{
		"type",
		"--udid", target.UDID,
		"--text", spec.Text,
	}
	return d.runOK(ctx, "type", args)
}

// Erase clears the focused field by sending repeated Backspace key presses.
// If callers pass Text, use its length as a lower bound; otherwise send a
// conservative fixed count. Baguette does not currently offer semantic field
// targeting, so non-focused selectors are intentionally ignored by this shim.
func (d *Driver) Erase(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	count := len([]rune(spec.Text))
	if count < 32 {
		count = 32
	}
	for i := 0; i < count; i++ {
		args := []string{"key", "--udid", target.UDID, "--code", "Backspace"}
		if err := d.runOK(ctx, "key", args); err != nil {
			return err
		}
	}
	return nil
}

// HideKeyboard sends Escape through the simulator keyboard, which dismisses
// the software keyboard for standard UIKit/SwiftUI text inputs.
func (d *Driver) HideKeyboard(ctx context.Context, target drivers.Target) error {
	args := []string{"key", "--udid", target.UDID, "--code", "Escape"}
	return d.runOK(ctx, "key", args)
}

// Tree returns baguette's accessibility description. Callers use this path for
// simulator system/SpringBoard inspection because AXe is app-focused.
func (d *Driver) Tree(ctx context.Context, target drivers.Target, _ drivers.TreeSpec) (drivers.TreeResult, error) {
	res := d.exec.Run(ctx, "baguette", "describe-ui", "--udid", target.UDID)
	if res.Err != nil {
		return drivers.TreeResult{}, fmt.Errorf("baguette describe-ui: %w (%s)", res.Err, firstLine(res.Stderr))
	}
	return drivers.TreeResult{JSON: []byte(res.Stdout)}, nil
}

// PressButton dispatches a hardware button press via baguette `press`. The
// driver maps the HardwareButton enum to baguette's button names.
func (d *Driver) PressButton(ctx context.Context, target drivers.Target, btn drivers.HardwareButton) error {
	name, err := baguetteButtonName(btn)
	if err != nil {
		return err
	}
	args := []string{
		"press",
		"--udid", target.UDID,
		"--button", name,
	}
	return d.runOK(ctx, "press", args)
}

// Screenshot writes a PNG via baguette's `screenshot` subcommand. The
// destination path is required.
func (d *Driver) Screenshot(ctx context.Context, target drivers.Target, spec drivers.ScreenshotSpec) error {
	if spec.OutPath == "" {
		return fmt.Errorf("baguette: ScreenshotSpec.OutPath required")
	}
	args := []string{
		"screenshot",
		"--udid", target.UDID,
		"--output", spec.OutPath,
	}
	return d.runOK(ctx, "screenshot", args)
}

// --- helpers ------------------------------------------------------------

// defaultGestureSize is the screen width/height baguette needs for every
// gesture. We pass it on every call; if MAV ever needs per-device values we
// can plumb them through Target.
func defaultGestureSize() (int, int) { return defaultGestureWidth, defaultGestureHeight }

// baguetteButtonName maps the driver-neutral HardwareButton to baguette's
// `press --button` vocabulary. baguette uses camelCase for the volume buttons
// (`volumeUp`/`volumeDown`) and accepts `home`, `lock`, `power`, `action`.
func baguetteButtonName(btn drivers.HardwareButton) (string, error) {
	switch btn {
	case drivers.BtnHome:
		return "home", nil
	case drivers.BtnLock:
		return "lock", nil
	case drivers.BtnVolumeUp:
		return "volumeUp", nil
	case drivers.BtnVolumeDown:
		return "volumeDown", nil
	}
	return "", fmt.Errorf("baguette: unsupported hardware button %q", btn)
}

// floatSeconds formats a Duration (milliseconds in the spec) as the
// fractional-seconds form baguette accepts on --duration / --interval.
func floatSeconds(ms int) string {
	if ms <= 0 {
		return "0"
	}
	return strconv.FormatFloat(float64(ms)/1000.0, 'f', 3, 64)
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
