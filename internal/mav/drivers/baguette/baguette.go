// Package baguette wraps the baguette CLI (https://github.com/tddworks/baguette)
// — a Swift host-side simulator driver built on private SimulatorKit symbols.
// Baguette is the canonical multitouch / hardware-button / streaming path on
// simulator.
//
// Sim-only: baguette has no device support. Provides() returns an empty set
// on device targets so the router never picks it; cli.go must surface a
// structured `gesture_unsupported_on_device` error in that case.
//
// CLI shape (verified against v0.2.1, October 2026):
//
//	baguette tap          --udid UDID --x X --y Y --width W --height H [--duration S]
//	baguette double-tap   --udid UDID --x X --y Y --width W --height H [--interval S] [--duration S]
//	baguette swipe        --udid UDID --start-x X1 --start-y Y1 --end-x X2 --end-y Y2 --width W --height H
//	baguette pinch        --udid UDID --cx CX --cy CY --start-spread S1 --end-spread S2 --width W --height H
//	baguette pan          --udid UDID --x1 X --y1 Y --x2 X --y2 Y --dx DX --dy DY --width W --height H
//	baguette type         --udid UDID --text TEXT
//	baguette key          --udid UDID --code <KeyA..ArrowRight> [--modifiers] [--duration S]
//	baguette press        --udid UDID --button (home|lock|power|action|volumeUp|volumeDown) [--duration S]
//	baguette describe-ui  --udid UDID [--x X --y Y] [--output PATH]
//	baguette orientation  --udid UDID (portrait|landscape-left|landscape-right|portrait-upside-down)
//	baguette screenshot   --udid UDID [--output PATH]
//	baguette list         [--json]
//	baguette chrome layout --udid UDID
//	baguette hinge        --udid UDID [--pose closed|open|flat] [--angle DEG]
//	baguette heal         --udid UDID
//
// Width/Height are the lit panel's size in points and are REQUIRED on every
// gesture: baguette divides each point by them. They come from `chrome layout`.
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
	"sync"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key for this driver.
const ID = "baguette"

// defaultGestureSize is the last resort when `chrome layout` cannot answer.
// It is NOT harmless, whatever this comment used to claim: baguette divides
// every point by width/height (`point.x / size.width` in IndigoHIDInput.swift)
// and multiplies back by the real panel, so a wrong size moves the gesture. On
// iPhone Duo's cover (466×678) the centre of a 402×874 screen lands at
// (233, 263), not (233, 339).
const (
	defaultGestureWidth  = 402
	defaultGestureHeight = 874
)

// Driver wraps the baguette CLI.
type Driver struct {
	exec drivers.Executor
	path string // resolved binary path, populated by Probe

	sizeMu sync.Mutex
	sizes  map[string][2]int
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
	// CapErase is deliberately absent, and its Erase method is gone with it.
	// baguette's HID keyboard delivers nothing into a focused simulator text
	// field: measured on 2026-09-22 against one Boxy search field holding
	// "a12345", `baguette key --code Backspace` left value= untouched twice
	// while `axe key 42` — the same HID usage, same simulator, same window —
	// took it to "a1234" and then "a123". `baguette key --code KeyZ` typed
	// nothing either, so no key name recovers it. Declaring the capability
	// only bought a `{"ok":true}` for a field that still held its text.
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
		drivers.CapHideKeyboard,
	)
}

func (d *Driver) DoubleTap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) error {
	w, h := d.gestureSize(ctx, target)
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
	w, h := d.gestureSize(ctx, target)
	args := []string{
		"swipe", "--udid", target.UDID,
		"--start-x", strconv.Itoa(spec.StartX), "--start-y", strconv.Itoa(spec.StartY),
		"--end-x", strconv.Itoa(spec.EndX), "--end-y", strconv.Itoa(spec.EndY),
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
	w, h := d.gestureSize(ctx, target)
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
	case drivers.CapPinch, drivers.CapTwoFingerPan, drivers.CapHardwareBtn, drivers.CapTreeSystem, drivers.CapHideKeyboard:
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

	// `--version` is both the sanity call (the binary runs) and the floor. It
	// costs what the `list --json` it replaced cost: 140ms on a loaded machine.
	res := d.exec.Run(ctx, "baguette", "--version")
	if res.Err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthDegraded,
			Detail: "baguette installed but `--version` failed: " + firstLine(res.Stderr),
			Next:   "brew upgrade baguette",
			Tools:  map[string]string{"baguette": path},
		}
	}
	got := strings.TrimSpace(res.Stdout)
	if got != "" && got[0] >= '0' && got[0] <= '9' && !drivers.VersionAtLeast(got, MinVersion) {
		return drivers.HealthReport{
			State: drivers.HealthBroken,
			Detail: fmt.Sprintf("baguette %s is installed and mav needs %s or newer: older builds "+
				"have no `heal`, so on Xcode 27 every gesture acks once Device Hub attaches and lands "+
				"nowhere, and no `hinge` or lit-panel input for iPhone Duo", got, MinVersion),
			Next:  "brew upgrade baguette",
			Tools: map[string]string{"baguette": path},
		}
	}
	return drivers.HealthReport{
		State: drivers.HealthOK,
		Tools: map[string]string{"baguette": path},
	}
}

// MinVersion is the oldest baguette mav drives. 0.2.1 is the first with all of
// `heal` (Device Hub's input shadowing), `hinge`, and taps bound to the lit
// panel of a foldable.
const MinVersion = "0.2.1"

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
	w, h := d.gestureSize(ctx, target)
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
	w, h := d.gestureSize(ctx, target)
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
	w, h := d.gestureSize(ctx, target)
	args := []string{
		"pinch",
		"--udid", target.UDID,
		"--cx", strconv.Itoa(spec.X),
		"--cy", strconv.Itoa(spec.Y),
		"--start-spread", strconv.FormatFloat(startSpread, 'f', 1, 64),
		"--end-spread", strconv.FormatFloat(endSpread, 'f', 1, 64),
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
	w, h := d.gestureSize(ctx, target)
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
// gestureSize is the lit panel's size in points, from the same `chrome layout`
// baguette itself binds input to. It is read once per simulator per process:
// on a loaded machine one read took 3.6s, because it asks devicectl for the
// hinge. ForgetScreenSize drops it when mav moves the hinge.
func (d *Driver) gestureSize(ctx context.Context, target drivers.Target) (int, int) {
	d.sizeMu.Lock()
	defer d.sizeMu.Unlock()
	if s, ok := d.sizes[target.UDID]; ok {
		return s[0], s[1]
	}
	w, h, ok := d.readScreenSize(ctx, target)
	if !ok {
		return defaultGestureWidth, defaultGestureHeight
	}
	if d.sizes == nil {
		d.sizes = map[string][2]int{}
	}
	d.sizes[target.UDID] = [2]int{w, h}
	return w, h
}

// ForgetScreenSize drops the cached panel size, for when the lit panel changes.
func (d *Driver) ForgetScreenSize(udid string) {
	d.sizeMu.Lock()
	defer d.sizeMu.Unlock()
	delete(d.sizes, udid)
}

func (d *Driver) readScreenSize(ctx context.Context, target drivers.Target) (int, int, bool) {
	res := d.exec.Run(ctx, "baguette", "chrome", "layout", "--udid", target.UDID)
	if res.Err != nil {
		return 0, 0, false
	}
	var layout struct {
		Screen struct {
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		} `json:"screen"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &layout); err != nil {
		return 0, 0, false
	}
	if layout.Screen.Width <= 0 || layout.Screen.Height <= 0 {
		return 0, 0, false
	}
	return int(layout.Screen.Width + 0.5), int(layout.Screen.Height + 0.5), true
}

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
