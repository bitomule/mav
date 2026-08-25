package macos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// CuaID is the driver's registry key.
const CuaID = "cua"

// Cua wraps cua-driver (trycua/cua, MIT), mav's canonical driver on macOS.
//
// It is here for a structural reason, not preference: on macOS the
// Accessibility and Screen Recording permissions are granted ONLY to
// interactive GUI processes. A CLI cannot hold them no matter how much you
// grant them to the terminal. The only architecture that works is a broker,
// an app holding the permissions plus a socket, and cua-driver ships it
// built in: the binary we invoke lives inside /Applications/CuaDriver.app.
//
// What it adds over what was there before, measured inside a VM against a
// floating window (layer != 0), which is the case that broke the others:
//
//   - Peekaboo enumerated that window and discarded it on its own because
//     of the layer: no tree, no capture.
//   - axcli read the tree, but its capture returned the DESKTOP cropped to
//     the window's dimensions, with no error, and it also activated the app.
//   - cua-driver returns a tree WITH geometry and a window capture with real
//     content in the SAME call, and the click lands in the background.
//
// Two known limits, both its own:
//
//   - `list_windows` is layer-0 only by declared design, so a floating
//     window does not show up in discovery.
//   - its elements expose no AXIdentifier: only element_token (valid within
//     the snapshot), role, label and frame.
type Cua struct {
	exec drivers.Executor

	// daemonAttempted caps the autostart at one try per process. If the
	// daemon does not come up, insisting on every command turns a clear
	// failure into a string of waits.
	daemonAttempted bool
}

var (
	_ drivers.TreeDriver       = (*Cua)(nil)
	_ drivers.ScreenshotDriver = (*Cua)(nil)
	_ drivers.TapDriver        = (*Cua)(nil)
	_ drivers.TypeDriver       = (*Cua)(nil)
	_ drivers.GestureDriver    = (*Cua)(nil)
	_ drivers.TextDriver       = (*Cua)(nil)
)

// NewCua builds the driver.
func NewCua(exec drivers.Executor) *Cua { return &Cua{exec: exec} }

func (d *Cua) ID() string { return CuaID }

func (d *Cua) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindMac {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapTreeAX,
		drivers.CapScreenshot,
		drivers.CapCoordTap,
		drivers.CapSemanticTap,
		drivers.CapType,
		drivers.CapSwipe,
		drivers.CapErase,
	)
}

// Cost declares it canonical for everything it provides: it is the only one
// covering all four capabilities with verified background delivery.
func (d *Cua) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapTreeAX, drivers.CapScreenshot, drivers.CapCoordTap, drivers.CapSemanticTap, drivers.CapType, drivers.CapSwipe, drivers.CapErase:
		return 0
	default:
		return 100
	}
}

// Probe asks the daemon about permissions, not the process running mav.
//
// That is the difference that matters: `permissions status` answers with
// CuaDriver's identity (com.trycua.driver) because it is its own responsible
// process. With no daemon it answers `unknown` instead of lying with your
// terminal's permissions, which is why that case is reported as degraded and
// not as healthy.
func (d *Cua) Probe(ctx context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("cua-driver")
	if err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "cua-driver not on PATH",
			Next:   "mav setup --install cua-driver",
		}
	}
	report := drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"cua-driver": path}}
	res := d.exec.Run(ctx, "cua-driver", "permissions", "status", "--json")
	var status struct {
		Accessibility   *bool `json:"accessibility"`
		ScreenRecording *bool `json:"screen_recording"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &status); err != nil {
		report.State = drivers.HealthDegraded
		report.Detail = "cua-driver daemon not answering"
		report.Next = "open -n -g -a CuaDriver --args serve"
		return report
	}
	var missing []string
	if status.Accessibility == nil || !*status.Accessibility {
		missing = append(missing, "Accessibility")
	}
	if status.ScreenRecording == nil || !*status.ScreenRecording {
		missing = append(missing, "Screen Recording")
	}
	if len(missing) > 0 {
		report.State = drivers.HealthDegraded
		report.Detail = "missing TCC permission: " + strings.Join(missing, ", ")
		// Its grant command launches the app through LaunchServices so the
		// dialogs are attributed to it, and registers it in the panels. It
		// is the only tool among those tested that automates this; the
		// others must be added by hand with the "+".
		report.Next = "cua-driver permissions grant"
	}
	return report
}

func (d *Cua) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// cuaCall invokes a driver tool. The shape is always
// `cua-driver call <tool> '<json>'` and the response is JSON on stdout.
func (d *Cua) cuaCall(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	raw, err := d.rawCall(ctx, tool, args)
	if err == nil || !isCuaDaemonDown(err) || d.daemonAttempted {
		return raw, err
	}
	// The daemon is down, or simply was never started in this session.
	// Bringing it up is a fixed command with no decisions, so mav does it:
	// if every agent has to learn the incantation, half will not and the
	// other half will do it wrong, starting a bare `cua-driver serve`, which
	// per its own documentation is unsupported outside the app and also
	// loses the permission attribution, which is EVERYTHING the broker
	// provides.
	d.daemonAttempted = true
	if startErr := d.startDaemon(ctx); startErr != nil {
		return nil, err
	}
	return d.rawCall(ctx, tool, args)
}

// startDaemon brings the daemon up through LaunchServices and waits for it
// to answer.
//
// `open -g` keeps the app in the background: starting the driver cannot
// steal focus from anyone, which is the property this driver was chosen for.
func (d *Cua) startDaemon(ctx context.Context) error {
	if res := d.exec.Run(ctx, "open", "-n", "-g", "-a", "CuaDriver", "--args", "serve"); res.Err != nil {
		return fmt.Errorf("cua: could not start the CuaDriver daemon: %s", firstLine(res.Stderr))
	}
	// Polling instead of a fixed wait: cold it takes a few seconds and warm
	// it answers right away, so a fixed wait would be both too short and too
	// long.
	for attempt := 0; attempt < 30; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		res := d.exec.Run(ctx, "cua-driver", "permissions", "status", "--json")
		var probe map[string]any
		if json.Unmarshal([]byte(res.Stdout), &probe) == nil {
			if _, ok := probe["accessibility"]; ok {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("cua: the CuaDriver daemon did not answer after starting")
}

// isCuaDaemonDown recognizes the only failure mav can fix on its own.
//
// It matches on text because there is nothing better: that case exits with
// code ZERO and a message for humans, not a structured error.
func isCuaDaemonDown(err error) bool {
	return err != nil && strings.Contains(err.Error(), "daemon is not running")
}

func (d *Cua) rawCall(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	res := d.exec.Run(ctx, "cua-driver", "call", tool, string(payload))
	if strings.Contains(res.Stdout, "daemon is not running") {
		return nil, fmt.Errorf("cua-driver %s: %s", tool, firstLine(res.Stdout))
	}
	if strings.TrimSpace(res.Stdout) == "" {
		if res.Err != nil {
			return nil, fmt.Errorf("cua-driver %s: %s", tool, firstLine(res.Stderr))
		}
		return nil, fmt.Errorf("cua-driver %s: empty response", tool)
	}
	// A refusal arrives with exit 0 and a `refusal` object, so checking the
	// exit code is not enough.
	var refusal struct {
		Refusal *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"refusal"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &refusal); err == nil && refusal.Refusal != nil {
		return nil, fmt.Errorf("%s: %s", refusal.Refusal.Code, firstLine(refusal.Refusal.Message))
	}
	return []byte(res.Stdout), nil
}

// cuaWindow is one row of list_windows.
type cuaWindow struct {
	WindowID int    `json:"window_id"`
	PID      int    `json:"pid"`
	AppName  string `json:"app_name"`
	Title    string `json:"title"`
	OnScreen bool   `json:"is_on_screen"`
	Bounds   struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"bounds"`
}

// resolvePID translates bundle id to pid.
//
// mav identifies a macOS app by its bundle, which is what stays stable
// across runs; cua-driver always works by pid, which changes on every
// launch. The translation happens here and not in the target layer because
// it is a property of this tool, not of the target model.
func (d *Cua) resolvePID(ctx context.Context, target drivers.Target) (int, error) {
	if target.PID > 0 {
		return target.PID, nil
	}
	if target.BundleID == "" {
		return 0, errors.New("cua: no app to target; set bundle_id")
	}
	raw, err := d.cuaCall(ctx, "list_apps", map[string]any{})
	if err != nil {
		return 0, err
	}
	apps, err := decodeCuaApps(raw)
	if err != nil {
		return 0, err
	}
	for _, a := range apps {
		if a.Running && a.PID > 0 && a.BundleID == target.BundleID {
			return a.PID, nil
		}
	}
	return 0, fmt.Errorf("cua: %s is not running", target.BundleID)
}

// cuaApp is one row of list_apps. It includes installed but not running
// apps, hence the Running filter before trusting the pid.
type cuaApp struct {
	BundleID string `json:"bundle_id"`
	Name     string `json:"name"`
	PID      int    `json:"pid"`
	Running  bool   `json:"running"`
}

func decodeCuaApps(raw []byte) ([]cuaApp, error) {
	var flat struct {
		Apps       []cuaApp `json:"apps"`
		Structured *struct {
			Apps []cuaApp `json:"apps"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, fmt.Errorf("cua: unreadable app list: %w", err)
	}
	if len(flat.Apps) > 0 {
		return flat.Apps, nil
	}
	if flat.Structured != nil {
		return flat.Structured.Apps, nil
	}
	return nil, nil
}

// resolveWindow picks the app's window.
//
// The criterion is the largest visible area and not z-order because z_index
// can come back null, the tool itself warns that no order can be inferred
// then, while the dimensions are always there.
func (d *Cua) resolveWindow(ctx context.Context, target drivers.Target) (cuaWindow, error) {
	pid, err := d.resolvePID(ctx, target)
	if err != nil {
		return cuaWindow{}, err
	}
	target.PID = pid
	// The pid goes in the request and is not filtered afterwards, and it is
	// not an optimization: without a pid the tool enumerates only layer 0,
	// to avoid flooding the caller with tooltips, popovers, menus and the
	// Dock, while naming the process admits every layer. It is the only way
	// to reach an app whose entire UI lives in an accessory window.
	raw, err := d.cuaCall(ctx, "list_windows", map[string]any{"pid": pid})
	if err != nil {
		return cuaWindow{}, err
	}
	windows, err := decodeCuaWindows(raw)
	if err != nil {
		return cuaWindow{}, err
	}
	var best cuaWindow
	var bestArea float64
	for _, w := range windows {
		if w.PID != pid || !w.OnScreen {
			continue
		}
		area := w.Bounds.Width * w.Bounds.Height
		if area > bestArea {
			best, bestArea = w, area
		}
	}
	if bestArea == 0 {
		// A real case, not a hypothetical: a floating window (panel, HUD,
		// popover, onboarding) does not show up in list_windows because the
		// tool only enumerates layer 0. The message says so, so nobody
		// reads it as "the app is not open", which is what it looks like.
		return cuaWindow{}, fmt.Errorf("cua: no on-screen window for pid %d", pid)
	}
	return best, nil
}

// decodeCuaWindows extracts the list whether it comes flat or inside
// structuredContent, which depends on the transport.
func decodeCuaWindows(raw []byte) ([]cuaWindow, error) {
	var flat struct {
		Windows    []cuaWindow `json:"windows"`
		Structured *struct {
			Windows []cuaWindow `json:"windows"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, fmt.Errorf("cua: unreadable window list: %w", err)
	}
	if len(flat.Windows) > 0 {
		return flat.Windows, nil
	}
	if flat.Structured != nil {
		return flat.Structured.Windows, nil
	}
	return nil, nil
}

// cuaState is the get_window_state response: tree AND capture at once.
type cuaState struct {
	PID            int           `json:"pid"`
	SnapshotID     string        `json:"snapshot_id"`
	Elements       []cuaElement  `json:"elements"`
	DegradedReason string        `json:"degraded_reason"`
	ScreenshotB64  string        `json:"screenshot_png_b64"`
	Structured     *cuaStateBody `json:"structuredContent"`
}

type cuaStateBody struct {
	PID            int          `json:"pid"`
	SnapshotID     string       `json:"snapshot_id"`
	Elements       []cuaElement `json:"elements"`
	DegradedReason string       `json:"degraded_reason"`
	ScreenshotB64  string       `json:"screenshot_png_b64"`
}

type cuaElement struct {
	Index int    `json:"element_index"`
	Token string `json:"element_token"`
	Role  string `json:"role"`
	Label string `json:"label"`
	Value string `json:"value"`
	Depth int    `json:"depth"`
	Frame *struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"frame"`
	Enabled *bool `json:"enabled"`
}

func (s *cuaState) normalize() {
	if s.Structured == nil {
		return
	}
	if len(s.Elements) == 0 {
		s.Elements = s.Structured.Elements
	}
	if s.SnapshotID == "" {
		s.SnapshotID = s.Structured.SnapshotID
	}
	if s.PID == 0 {
		s.PID = s.Structured.PID
	}
	if s.ScreenshotB64 == "" {
		s.ScreenshotB64 = s.Structured.ScreenshotB64
	}
	if s.DegradedReason == "" {
		s.DegradedReason = s.Structured.DegradedReason
	}
}

// windowState requests the tree and capture of the app's window.
func (d *Cua) windowState(ctx context.Context, target drivers.Target) (cuaState, error) {
	win, err := d.resolveWindow(ctx, target)
	if err != nil {
		return cuaState{}, err
	}
	raw, err := d.cuaCall(ctx, "get_window_state", map[string]any{
		"pid":       win.PID,
		"window_id": win.WindowID,
	})
	if err != nil {
		return cuaState{}, err
	}
	var state cuaState
	if err := json.Unmarshal(raw, &state); err != nil {
		return cuaState{}, fmt.Errorf("cua: unreadable window state: %w", err)
	}
	state.normalize()
	if state.PID == 0 {
		state.PID = win.PID
	}
	if state.DegradedReason != "" && len(state.Elements) == 0 {
		return state, fmt.Errorf("cua: %s", firstLine(state.DegradedReason))
	}
	return state, nil
}

// Tree returns the window's accessibility tree.
func (d *Cua) Tree(ctx context.Context, target drivers.Target, _ drivers.TreeSpec) (drivers.TreeResult, error) {
	state, err := d.windowState(ctx, target)
	if err != nil {
		return drivers.TreeResult{}, err
	}
	encoded, err := json.Marshal(cuaElementsToNodes(state.Elements))
	if err != nil {
		return drivers.TreeResult{}, err
	}
	return drivers.TreeResult{JSON: encoded}, nil
}

// cuaElementsToNodes translates into mav's vocabulary.
//
// `identifier` comes from the element_token and NOT from AXIdentifier, which
// cua-driver does not expose. It is filled anyway because `ui tap --id` has
// to be able to point at something within the same run, but that value does
// NOT survive the next snapshot: the tool itself rejects stale indexes
// instead of acting on the wrong element.
func cuaElementsToNodes(elements []cuaElement) []map[string]any {
	out := make([]map[string]any, 0, len(elements))
	for _, el := range elements {
		node := map[string]any{
			"identifier": el.Token,
			"label":      el.Label,
			"role":       el.Role,
			"value":      el.Value,
		}
		if el.Enabled != nil {
			node["enabled"] = *el.Enabled
		}
		if el.Frame != nil {
			node["frame"] = fmt.Sprintf("{{%g, %g}, {%g, %g}}", el.Frame.X, el.Frame.Y, el.Frame.W, el.Frame.H)
		}
		out = append(out, node)
	}
	return out
}

// Screenshot writes the window capture.
//
// It comes from the same get_window_state as the tree, in base64, so image
// and tree describe the SAME instant, which for evidence accompanying a tree
// is exactly what you want, and there is no second invocation that could
// catch the screen already changed.
func (d *Cua) Screenshot(ctx context.Context, target drivers.Target, spec drivers.ScreenshotSpec) error {
	if spec.OutPath == "" {
		return errors.New("cua: screenshot output path missing")
	}
	state, err := d.windowState(ctx, target)
	if err != nil {
		return err
	}
	if state.ScreenshotB64 == "" {
		// The tool omits the image on purpose when it cannot prove it
		// matches the requested dimensions, instead of delivering a guessed
		// transformation. It is propagated as is: a capture that cannot be
		// proven is not evidence.
		return errors.New("cua: no provable screenshot for this window")
	}
	png, err := base64.StdEncoding.DecodeString(state.ScreenshotB64)
	if err != nil {
		return fmt.Errorf("cua: unreadable screenshot: %w", err)
	}
	return os.WriteFile(spec.OutPath, png, 0o644)
}

// findCuaElement locates the selector's element within a snapshot.
func findCuaElement(state cuaState, selector drivers.ElementSelector) (cuaElement, bool) {
	for _, el := range state.Elements {
		if selector.ID != "" && el.Token == selector.ID {
			return el, true
		}
		if selector.Text != "" && strings.Contains(el.Label, selector.Text) {
			return el, true
		}
	}
	return cuaElement{}, false
}

// Tap clicks without bringing the app to the front.
//
// It is two calls and not one on purpose: the tool demands a fresh snapshot
// before every per-element action and invalidates the index map as soon as
// you take another. Reusing an old snapshot is exactly how you end up
// clicking the wrong thing.
func (d *Cua) Tap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	state, err := d.windowState(ctx, target)
	if err != nil {
		return drivers.TapResult{}, err
	}
	args := map[string]any{"pid": state.PID}
	switch {
	case spec.Selector.ID != "" || spec.Selector.Text != "":
		el, ok := findCuaElement(state, spec.Selector)
		if !ok {
			return drivers.TapResult{}, errors.New("cua: no element matched the selector")
		}
		args["element_token"] = el.Token
	case spec.X != 0 || spec.Y != 0:
		args["x"], args["y"] = spec.X, spec.Y
	default:
		return drivers.TapResult{}, errors.New("cua: tap requires a selector or coordinates")
	}
	if _, err := d.cuaCall(ctx, "click", args); err != nil {
		return drivers.TapResult{}, err
	}
	return drivers.TapResult{MatchedID: spec.Selector.ID, MatchedText: spec.Selector.Text}, nil
}

// Type writes into the selector's element.
func (d *Cua) Type(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	if spec.Text == "" {
		return errors.New("cua: type text missing")
	}
	state, err := d.windowState(ctx, target)
	if err != nil {
		return err
	}
	args := map[string]any{"pid": state.PID, "text": spec.Text}
	if spec.Selector.ID != "" || spec.Selector.Text != "" {
		el, ok := findCuaElement(state, spec.Selector)
		if !ok {
			return errors.New("cua: no element matched the selector")
		}
		args["element_token"] = el.Token
	}
	_, err = d.cuaCall(ctx, "type_text", args)
	return err
}

// Swipe scrolls the window's content.
//
// On a Mac a swipe IS a scroll: there is no finger, there is a wheel. The
// direction is inverted on purpose, swiping up on a phone moves the content
// up, which on desktop is requested as scrolling down, so a flow written
// once means the same on both platforms.
func (d *Cua) Swipe(ctx context.Context, target drivers.Target, spec drivers.SwipeSpec) error {
	direction := map[string]string{
		"up": "down", "down": "up", "left": "right", "right": "left",
	}[spec.Direction]
	if direction == "" {
		return errors.New("cua: swipe needs a direction; coordinate swipes are not a desktop gesture")
	}
	pid, err := d.resolvePID(ctx, target)
	if err != nil {
		return err
	}
	_, err = d.cuaCall(ctx, "scroll", map[string]any{
		"pid":           pid,
		"direction":     direction,
		"delivery_mode": "background",
	})
	return err
}

// Pinch, Rotate, TwoFingerPan and W3CActions exist because GestureDriver is
// a whole interface, and they fail saying why.
//
// It is not a gap to fill later: a trackpad sends gestures to the system and
// to the focused app, not to a PID, so reproducing them in the background
// against a specific window is not a matter of effort but of there being no
// path. A message that says so saves the search.
func (d *Cua) Pinch(context.Context, drivers.Target, drivers.PinchSpec) error {
	return errors.New("cua: multitouch gestures cannot be delivered to a pid on macOS")
}

func (d *Cua) Rotate(context.Context, drivers.Target, drivers.RotateSpec) error {
	return errors.New("cua: multitouch gestures cannot be delivered to a pid on macOS")
}

func (d *Cua) TwoFingerPan(context.Context, drivers.Target, drivers.TwoFingerPanSpec) error {
	return errors.New("cua: multitouch gestures cannot be delivered to a pid on macOS")
}

func (d *Cua) W3CActions(context.Context, drivers.Target, []byte) error {
	return errors.New("cua: W3C action chains are an iOS driver feature; use ui tap/type/swipe")
}

// Erase empties a field.
//
// It goes through set_value and not by typing deletions: writing the empty
// string leaves the field empty in one shot, while sending N Delete
// keystrokes depends on guessing how many, and on the field having focus,
// which is exactly what this driver avoids needing.
func (d *Cua) Erase(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	state, err := d.windowState(ctx, target)
	if err != nil {
		return err
	}
	args := map[string]any{"pid": state.PID, "value": ""}
	if spec.Selector.ID != "" || spec.Selector.Text != "" {
		el, ok := findCuaElement(state, spec.Selector)
		if !ok {
			return errors.New("cua: no element matched the selector")
		}
		args["element_token"] = el.Token
	}
	_, err = d.cuaCall(ctx, "set_value", args)
	return err
}

// HideKeyboard does not exist on macOS: there is no on-screen keyboard to
// hide. It is declared because TextDriver is a whole interface, and it does
// nothing instead of failing, because a flow shared between iOS and Mac will
// call it and here it is simply unnecessary.
func (d *Cua) HideKeyboard(context.Context, drivers.Target) error { return nil }
