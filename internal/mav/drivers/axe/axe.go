// Package axe implements AXe 2 ordinary automation for iOS simulators and
// physical devices. Native snapshot/ref and autonomous-agent modes are separate.
package axe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/bitomule/mav/internal/mav/drivers"
)

const ID = "axe"
const MinimumVersion = "2.0.4"
const tapStyle = "physical"
const backspaceKeycode = "42"
const defaultEraseCount = 32

type Driver struct {
	exec      drivers.Executor
	path      string
	probeOnce sync.Once
	health    drivers.HealthReport
}

var _ drivers.NativeAutomationDriver = (*Driver)(nil)

func New(exec drivers.Executor) *Driver { return &Driver{exec: exec} }
func (d *Driver) ID() string            { return ID }
func (d *Driver) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind == drivers.KindMac {
		return drivers.NewSet()
	}
	set := drivers.NewSet(drivers.CapTreeAX, drivers.CapSemanticTap, drivers.CapCoordTap, drivers.CapSwipe, drivers.CapScreenshot, drivers.CapType, drivers.CapErase, drivers.CapPinch, drivers.CapRotate, drivers.CapTwoFingerPan, drivers.CapDrag, drivers.CapDoubleTap, drivers.CapHardwareBtn, drivers.CapKey, drivers.CapTouch, drivers.CapDraw, drivers.CapGesturePreset, drivers.CapSettings, drivers.CapReadiness, drivers.CapDiscovery, drivers.CapBatch, drivers.CapLocation, drivers.CapVideo, drivers.CapVideoStream)
	if target.Kind == drivers.KindSim {
		set.Add(drivers.CapSelectedInput)
		set.Add(drivers.CapHinge)
		set.Add(drivers.CapAppearance)
		set.Add(drivers.CapStatusBar)
		set.Add(drivers.CapSlider)
		set.Add(drivers.CapDisplays)
		set.Add(drivers.CapShutdown)
		set.Add(drivers.CapClipboard)
	}
	return set
}
func (d *Driver) Cost(cap drivers.Capability, _ drivers.Target) int {
	switch cap {
	case drivers.CapTreeAX, drivers.CapSemanticTap, drivers.CapCoordTap, drivers.CapScreenshot, drivers.CapSwipe, drivers.CapType, drivers.CapErase, drivers.CapPinch, drivers.CapRotate, drivers.CapTwoFingerPan, drivers.CapDrag, drivers.CapDoubleTap, drivers.CapHardwareBtn, drivers.CapHinge, drivers.CapVideo:
		return 0
	default:
		return 50
	}
}

var versionPattern = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)(?:[-+][A-Za-z0-9.-]+)?$`)

func (d *Driver) Probe(ctx context.Context, p drivers.Probe) drivers.HealthReport {
	d.probeOnce.Do(func() {
		path, err := p.LookPath("axe")
		if err != nil {
			d.health = drivers.HealthReport{State: drivers.HealthMissing, Detail: "AXe 2 is not installed", Next: "mav setup (or brew install cameroncooke/axe/axe)"}
			return
		}
		d.path = path
		res := d.exec.Run(ctx, path, "--version")
		next := "AXe >= " + MinimumVersion + " is required; run brew update && brew upgrade cameroncooke/axe/axe (or mav setup)"
		if res.Err != nil || res.Code != 0 {
			d.health = drivers.HealthReport{State: drivers.HealthBroken, Detail: failureText(res), Next: next}
			return
		}
		match := versionPattern.FindStringSubmatch(strings.TrimSpace(res.Stdout))
		if match == nil {
			d.health = drivers.HealthReport{State: drivers.HealthBroken, Detail: "Cannot read AXe version from --version", Next: next}
			return
		}
		if !drivers.VersionAtLeast(match[1], MinimumVersion) {
			d.health = drivers.HealthReport{State: drivers.HealthBroken, Detail: "AXe " + match[1] + " is unsupported; AXe >= " + MinimumVersion + " is required", Next: next}
			return
		}
		d.health = drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"axe": path, "axe.version": match[1]}}
	})
	return d.health
}
func (d *Driver) Warm(context.Context, drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

func invalid(message string) error {
	return &drivers.DriverError{Driver: ID, Code: "invalidArguments", Message: message, Delivery: drivers.DeliveryNotSent}
}
func unsupported(message string) error {
	return &drivers.DriverError{Driver: ID, Code: "unsupportedCapability", Message: message, Delivery: drivers.DeliveryNotSent}
}
func seconds(ms int) string       { return strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64) }
func number(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
func appendInt(args []string, name string, value int) []string {
	if value != 0 {
		return append(args, name, strconv.Itoa(value))
	}
	return args
}
func appendMS(args []string, name string, value int) []string {
	if value != 0 {
		return append(args, name, seconds(value))
	}
	return args
}
func appendFloat(args []string, name string, value float64) []string {
	if value != 0 {
		return append(args, name, number(value))
	}
	return args
}
func (d *Driver) binary() string {
	if d.path != "" {
		return d.path
	}
	return "axe"
}
func (d *Driver) run(ctx context.Context, target drivers.Target, mutation bool, args ...string) (drivers.NativeResult, error) {
	if target.Kind == drivers.KindMac {
		return drivers.NativeResult{}, unsupported("AXe does not automate macOS targets")
	}
	return decodeOutput(d.exec.Run(ctx, d.binary(), append([]string{"--output", "json"}, targetArgs(target, args...)...)...), mutation)
}
func targetArgs(target drivers.Target, args ...string) []string {
	out := append([]string{}, args...)
	if target.UDID != "" {
		out = append(out, "--udid", target.UDID)
	}
	return out
}
func inputArgs(target drivers.Target, args []string, keyboard bool, focusID string) ([]string, error) {
	in := target.InputContext
	if in == nil {
		return args, nil
	}
	if in.ExpectedPID < 0 {
		return nil, invalid("expected PID must be positive")
	}
	if (in.ScreenID == nil) != (in.ScreenUUID == "") {
		return nil, invalid("screen ID and UUID must be supplied together")
	}
	selected := in.Selected || in.ScreenID != nil || in.ExpectedPID > 0
	if selected && target.Kind == drivers.KindDevice {
		return nil, unsupported("selected input context is available on simulators only")
	}
	if selected {
		args = append(args, "--input-mode", "selected")
	}
	if in.ScreenID != nil {
		args = append(args, "--screen-id", strconv.FormatUint(uint64(*in.ScreenID), 10), "--screen-uuid", in.ScreenUUID)
	}
	if in.ExpectedPID > 0 {
		switch args[0] {
		case "tap", "describe-ui", "type", "key", "key-sequence", "key-combo", "slider":
			args = append(args, "--expected-pid", strconv.FormatInt(int64(in.ExpectedPID), 10))
		default:
			return nil, unsupported("this AXe operation does not support a selected PID assertion")
		}
	}
	if keyboard && selected {
		if focusID == "" {
			return nil, invalid("selected keyboard input requires a focused field ID")
		}
		args = append(args, "--focus-id", focusID)
	}
	return args, nil
}
func (d *Driver) input(ctx context.Context, target drivers.Target, args []string, keyboard bool, focusID string) (drivers.NativeResult, error) {
	args, err := inputArgs(target, args, keyboard, focusID)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	return d.run(ctx, target, true, append(args, "--verbose")...)
}
func selectorArgs(args []string, s drivers.ElementSelector) []string {
	switch {
	case s.ID != "":
		return append(args, "--id", s.ID)
	case s.Text != "":
		return append(args, "--label", s.Text)
	case s.Value != "":
		return append(args, "--value", s.Value)
	default:
		return args
	}
}
func (d *Driver) Tap(ctx context.Context, target drivers.Target, s drivers.TapSpec) (drivers.TapResult, error) {
	args, err := tapArgs(s)
	if err != nil {
		return drivers.TapResult{}, err
	}
	if s.SemanticPress && target.Kind != drivers.KindDevice {
		copy := drivers.InputContext{Selected: true}
		if target.InputContext != nil {
			copy = *target.InputContext
			copy.Selected = true
		}
		target.InputContext = &copy
	}
	native, err := d.input(ctx, target, args, false, "")
	if err != nil {
		return drivers.TapResult{Native: &native}, err
	}
	return drivers.TapResult{Native: &native, MatchedID: s.Selector.ID, MatchedText: s.Selector.Text, MatchedValue: s.Selector.Value, X: s.X, Y: s.Y}, nil
}
func treeData(result drivers.NativeResult) ([]byte, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(result.Data, &object) == nil {
		// Selected description includes its complete roots in a broker receipt.
		if ax := object["ax"]; ax != nil {
			var selected struct {
				Roots json.RawMessage `json:"roots"`
			}
			if json.Unmarshal(ax, &selected) == nil && selected.Roots != nil {
				return selected.Roots, nil
			}
		}
	}
	if len(result.Data) == 0 {
		return nil, &drivers.DriverError{Driver: ID, Code: "axe_invalid_tree", Message: "AXe returned no accessibility tree", Delivery: drivers.DeliveryNotSent}
	}
	return result.Data, nil
}
func (d *Driver) Tree(ctx context.Context, target drivers.Target, _ drivers.TreeSpec) (drivers.TreeResult, error) {
	return d.tree(ctx, target, nil)
}
func (d *Driver) TreeAtPoint(ctx context.Context, target drivers.Target, x, y int) (drivers.TreeResult, error) {
	return d.tree(ctx, target, []string{"--point", strconv.Itoa(x) + "," + strconv.Itoa(y)})
}
func (d *Driver) tree(ctx context.Context, target drivers.Target, extra []string) (drivers.TreeResult, error) {
	args, err := inputArgs(target, append([]string{"describe-ui"}, extra...), false, "")
	if err != nil {
		return drivers.TreeResult{}, err
	}
	if target.InputContext != nil {
		args = append(args, "--verbose")
	}
	result, err := d.run(ctx, target, false, args...)
	if err != nil {
		return drivers.TreeResult{}, err
	}
	data, err := treeData(result)
	return drivers.TreeResult{JSON: data, Native: &result}, err
}
func swipeArgs(s drivers.SwipeSpec) ([]string, error) {
	if s.StartX < 0 || s.StartY < 0 || s.EndX < 0 || s.EndY < 0 || s.DurationMs < 0 || s.DurationMs > 10000 || s.PreDelayMs < 0 || s.PreDelayMs > 10000 || s.PostDelayMs < 0 || s.PostDelayMs > 10000 || s.Delta < 0 || s.MaxSwipes < 0 || s.MaxSwipes > 50 {
		return nil, invalid("invalid swipe geometry, duration or scroll bound")
	}
	if s.UntilID != "" && s.UntilLabel != "" {
		return nil, invalid("choose one swipe-until selector")
	}
	args := []string{"swipe", "--start-x", strconv.Itoa(s.StartX), "--start-y", strconv.Itoa(s.StartY), "--end-x", strconv.Itoa(s.EndX), "--end-y", strconv.Itoa(s.EndY)}
	args = appendMS(args, "--duration", s.DurationMs)
	args = appendMS(args, "--pre-delay", s.PreDelayMs)
	args = appendMS(args, "--post-delay", s.PostDelayMs)
	args = appendInt(args, "--delta", s.Delta)
	args = appendInt(args, "--max-swipes", s.MaxSwipes)
	if s.UntilID != "" {
		args = append(args, "--until-id", s.UntilID)
	}
	if s.UntilLabel != "" {
		args = append(args, "--until-label", s.UntilLabel)
	}
	return args, nil
}
func (d *Driver) Swipe(ctx context.Context, target drivers.Target, s drivers.SwipeSpec) error {
	if s.Direction != "" {
		_, err := d.Gesture(ctx, target, drivers.GestureSpec{Name: "scroll-" + s.Direction, DurationMs: s.DurationMs, Delta: s.Delta, PreDelayMs: s.PreDelayMs, PostDelayMs: s.PostDelayMs, UntilID: s.UntilID, UntilLabel: s.UntilLabel, MaxSwipes: s.MaxSwipes})
		return err
	}
	args, err := swipeArgs(s)
	if err != nil {
		return err
	}
	_, err = d.input(ctx, target, args, false, "")
	return err
}

func (d *Driver) Screenshot(ctx context.Context, target drivers.Target, s drivers.ScreenshotSpec) error {
	if s.OutPath == "" {
		return invalid("screenshot output path missing")
	}
	args := []string{"screenshot", "--output", s.OutPath}
	if in := target.InputContext; in != nil {
		if in.ExpectedPID != 0 || in.Selected {
			return unsupported("screenshot supports a screen pair, not selected input or expected PID")
		}
		if (in.ScreenID == nil) != (in.ScreenUUID == "") {
			return invalid("screen ID and UUID must be supplied together")
		}
		if in.ScreenID != nil {
			args = append(args, "--screen-id", strconv.FormatUint(uint64(*in.ScreenID), 10), "--screen-uuid", in.ScreenUUID, "--verbose")
		}
	}
	_, err := d.run(ctx, target, false, args...)
	return err
}
func (d *Driver) Type(ctx context.Context, target drivers.Target, s drivers.TextSpec) error {
	if s.Text == "" {
		return invalid("type text missing")
	}
	selected := target.InputContext != nil && (target.InputContext.Selected || target.InputContext.ScreenID != nil || target.InputContext.ExpectedPID > 0)
	if (target.Kind == drivers.KindDevice || selected) && len(s.Text) > 256 {
		return invalid("physical and selected typing accepts at most 256 UTF-8 bytes")
	}
	for _, r := range s.Text {
		if r > 126 || (r < 32 && r != '\n' && r != '\t' && r != '\r') {
			return invalid("AXe type supports the US keyboard layout only")
		}
	}
	// A positional operand beginning with '-' is otherwise parsed as an option.
	args := []string{"type"}
	args, err := inputArgs(target, args, true, s.Selector.ID)
	if err != nil {
		return err
	}
	args = append(args, "--verbose")
	if input, ok := d.exec.(drivers.InputExecutor); ok {
		args = append([]string{"--output", "json"}, targetArgs(target, append(args, "--stdin")...)...)
		_, err = decodeOutput(input.RunInput(ctx, s.Text, d.binary(), args...), true)
		return err
	}
	args = targetArgs(target, args...)
	args = append(args, "--", s.Text)
	_, err = decodeOutput(d.exec.Run(ctx, d.binary(), append([]string{"--output", "json"}, args...)...), true)
	return err
}
func (d *Driver) Erase(ctx context.Context, target drivers.Target, s drivers.TextSpec) error {
	count := s.Deletions
	if count <= 0 {
		count = len([]rune(s.Text))
	}
	if count <= 0 {
		count = defaultEraseCount
	}
	// One prevalidated ordered session, rather than separate commands that can
	// interleave. Respect AXe's maximum of 100 keys without replaying a chunk.
	completed := 0
	var previous []json.RawMessage
	for count > 0 {
		n := count
		if n > 100 {
			n = 100
		}
		codes := make([]int, n)
		for i := range codes {
			codes[i] = 42
		}
		zero := 0
		result, err := d.KeySequence(ctx, target, drivers.KeySequenceSpec{Codes: codes, DelayMs: &zero, FocusID: s.Selector.ID})
		if err != nil {
			if completed > 0 {
				var typed *drivers.DriverError
				if errors.As(err, &typed) {
					copy := *typed
					if copy.Delivery == drivers.DeliveryNotSent {
						copy.Delivery = drivers.DeliverySent
					}
					copy.Message += "; earlier deletion chunks completed, so do not repeat erase"
					copy.Details, _ = json.Marshal(struct {
						CompletedDeletions int               `json:"completedDeletions"`
						CompletedReceipts  []json.RawMessage `json:"completedReceipts"`
						LastResult         json.RawMessage   `json:"lastResult"`
					}{completed, previous, copy.Details})
					return &copy
				}
				return &drivers.DriverError{Driver: ID, Code: "erase_incomplete", Message: err.Error() + "; earlier deletion chunks completed, so do not repeat erase", Delivery: drivers.DeliveryUncertain}
			}
			return err
		}
		if len(result.Envelope) > 0 {
			previous = append(previous, result.Envelope)
		}
		count -= n
		completed += n
	}
	return nil
}
func failureText(res drivers.ExecResult) string {
	if line := firstLine(res.Stderr); res.Stderr != "" && line != "" {
		return line
	}
	ended := fmt.Sprintf("exit code %d", res.Code)
	if res.Err != nil {
		ended = res.Err.Error()
	}
	text := "axe printed nothing and ended with " + ended
	if strings.Contains(ended, "signal: killed") {
		text += "; macOS kills an unsigned or damaged binary at launch, reinstall axe (mav doctor checks it)"
	}
	return text
}
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if s == "" {
		return "command failed"
	}
	return s
}
