package axe

import (
	"context"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

var _ drivers.NativeBatchDriver = (*Driver)(nil)

func tapArgs(s drivers.TapSpec) ([]string, error) {
	if s.Duration < 0 || s.Duration > 10000 || s.PreDelayMs < 0 || s.PreDelayMs > 10000 || s.PostDelayMs < 0 || s.PostDelayMs > 10000 || s.Count < 0 || s.Count > 2 || s.Touches < 0 || s.Touches > 2 || !finite(s.WaitTimeout) || s.WaitTimeout < 0 || !finite(s.PollInterval) || s.PollInterval < 0 {
		return nil, invalid("invalid tap count, touches or timing")
	}
	style := s.TapStyle
	if style == "" {
		style = tapStyle
	}
	if style != "automatic" && style != "physical" && style != "simulator" {
		return nil, invalid("unknown tap style")
	}
	args := []string{"tap"}
	if !s.SemanticPress {
		args = append(args, "--tap-style", style)
	} else {
		if s.TapStyle != "" || s.ElementType != "" || s.Duration != 0 || s.PreDelayMs != 0 || s.PostDelayMs != 0 || s.WaitTimeout != 0 || s.PollInterval != 0 || s.Count > 1 || s.Touches > 1 || s.Selector.Text != "" || s.Selector.Value != "" {
			return nil, invalid("semantic press accepts one exact ID without style, timing or other selectors")
		}
	}
	if s.Selector.IsZero() {
		if s.X < 0 || s.Y < 0 {
			return nil, invalid("tap coordinates must be nonnegative")
		}
		args = append(args, "-x", strconv.Itoa(s.X), "-y", strconv.Itoa(s.Y))
	} else {
		args = selectorArgs(args, s.Selector)
	}
	if s.ElementType != "" {
		args = append(args, "--element-type", s.ElementType)
	}
	if s.SemanticPress {
		if s.Selector.ID == "" {
			return nil, invalid("semantic press requires an accessibility ID")
		}
		args = append(args, "--semantic-press")
	}
	args = appendMS(args, "--hold", s.Duration)
	args = appendInt(args, "--count", s.Count)
	args = appendInt(args, "--touches", s.Touches)
	args = appendMS(args, "--pre-delay", s.PreDelayMs)
	args = appendMS(args, "--post-delay", s.PostDelayMs)
	args = appendFloat(args, "--wait-timeout", s.WaitTimeout)
	args = appendFloat(args, "--poll-interval", s.PollInterval)
	return args, nil
}

// AXe tokenizes its own step string; quoting here targets that grammar, not a
// host shell. Neither a caller nor an app can inject a second step or options.
func quoteStep(args []string) string {
	out := make([]string, len(args))
	for i, s := range args {
		out[i] = "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
	}
	return strings.Join(out, " ")
}
func batchStepArgs(t drivers.Target, s drivers.BatchStep) ([]string, error) {
	count := 0
	for _, present := range []bool{s.Tap != nil, s.Type != nil, s.Key != nil, s.KeySequence != nil, s.KeyCombo != nil, s.Touch != nil, s.Draw != nil, s.Gesture != nil, s.Swipe != nil, s.Drag != nil, s.Button != nil, s.Slider != nil, s.SleepMs != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return nil, invalid("each batch step requires exactly one operation")
	}
	selected := t.InputContext != nil && (t.InputContext.Selected || t.InputContext.ScreenID != nil)
	var args []string
	var err error
	focusID := ""
	keyboard := false
	switch {
	case s.Tap != nil:
		if s.Tap.SemanticPress && (!selected || t.Kind == drivers.KindDevice) {
			return nil, unsupported("batch semantic press requires a selected simulator batch")
		}
		args, err = tapArgs(*s.Tap)
	case s.Type != nil:
		if s.Type.Text == "" || len(s.Type.Text) > 256 {
			return nil, invalid("batch type requires 1...256 UTF-8 bytes")
		}
		for _, r := range s.Type.Text {
			if r > 126 || r < 32 && r != '\n' && r != '\t' && r != '\r' {
				return nil, invalid("AXe type requires characters in the US keyboard layout")
			}
		}
		if strings.HasPrefix(s.Type.Text, "-") {
			return nil, unsupported("AXe batch type cannot safely parse text beginning with a dash; use a separate type step")
		}
		keyboard = true
		focusID = s.Type.Selector.ID
		args = []string{"type", s.Type.Text}
	case s.Key != nil:
		args, err = keyArgs(*s.Key)
		keyboard = true
		focusID = s.Key.FocusID
	case s.KeySequence != nil:
		args, err = sequenceArgs(*s.KeySequence)
		keyboard = true
		focusID = s.KeySequence.FocusID
	case s.KeyCombo != nil:
		args, err = comboArgs(*s.KeyCombo)
		keyboard = true
		focusID = s.KeyCombo.FocusID
	case s.Touch != nil:
		args, err = touchArgs(t, *s.Touch)
		if s.Touch.ContactToken != "" || !s.Touch.Down || !s.Touch.Up {
			return nil, invalid("batch touch must complete down and up in one step")
		}
	case s.Draw != nil:
		args, err = drawArgs(*s.Draw)
	case s.Gesture != nil:
		args, err = gestureArgs(t, *s.Gesture)
	case s.Swipe != nil:
		sw := s.Swipe
		if sw.Direction != "" {
			args, err = gestureArgs(t, drivers.GestureSpec{Name: "scroll-" + sw.Direction, DurationMs: sw.DurationMs, Delta: sw.Delta, PreDelayMs: sw.PreDelayMs, PostDelayMs: sw.PostDelayMs, UntilID: sw.UntilID, UntilLabel: sw.UntilLabel, MaxSwipes: sw.MaxSwipes})
		} else {
			args, err = swipeArgs(*sw)
		}

	case s.Drag != nil:
		if !selected {
			return nil, unsupported("batch drag requires selected input")
		}
		args, err = dragArgs(*s.Drag)
	case s.Button != nil:
		args, err = buttonArgs(t, *s.Button)
	case s.Slider != nil:
		if !selected {
			return nil, unsupported("batch slider requires selected input")
		}
		args, err = sliderArgs(t, *s.Slider)
	case s.SleepMs != nil:
		if *s.SleepMs < 0 || *s.SleepMs > 10000 {
			return nil, invalid("batch sleep must be 0...10000 ms")
		}
		args = []string{"sleep", seconds(*s.SleepMs)}
	}
	if err != nil {
		return nil, err
	}
	// Context belongs to the batch. Selected per-step keyboard focus/PID is
	// valid; repeated per-step input-mode/display options are not.
	if selected && keyboard {
		if focusID == "" {
			return nil, invalid("selected batch keyboard step requires focus ID")
		}
		args = append(args, "--focus-id", focusID)
	}
	if selected && t.InputContext.ExpectedPID > 0 && args[0] != "sleep" {
		switch args[0] {
		case "tap", "type", "key", "key-sequence", "key-combo", "slider":
			args = append(args, "--expected-pid", strconv.FormatInt(int64(t.InputContext.ExpectedPID), 10))
		default:
			return nil, unsupported("this AXe batch operation cannot assert an expected PID")
		}
	}
	return args, nil
}
func (d *Driver) Batch(ctx context.Context, t drivers.Target, s drivers.BatchSpec) (drivers.NativeResult, error) {
	if len(s.Steps) == 0 || len(s.Steps) > 1000 {
		return drivers.NativeResult{}, invalid("batch requires 1...1000 steps")
	}
	selected := t.InputContext != nil && (t.InputContext.Selected || t.InputContext.ScreenID != nil)
	if s.TypeChunkSize < 0 || !finite(s.WaitTimeout) || s.WaitTimeout < 0 || !finite(s.PollInterval) || s.PollInterval < 0 {
		return drivers.NativeResult{}, invalid("invalid batch timing or type chunk size")
	}
	if s.AXCache != "" && s.AXCache != "perBatch" && s.AXCache != "perStep" && s.AXCache != "none" {
		return drivers.NativeResult{}, invalid("unknown AX cache policy")
	}
	if s.TypeSubmission != "" && s.TypeSubmission != "chunked" && s.TypeSubmission != "composite" {
		return drivers.NativeResult{}, invalid("unknown type submission mode")
	}
	if s.TapStyle != "" && s.TapStyle != "automatic" && s.TapStyle != "physical" && s.TapStyle != "simulator" {
		return drivers.NativeResult{}, invalid("unknown batch tap style")
	}
	special := s.ContinueOnError || (s.AXCache != "" && s.AXCache != "perBatch") || (s.TypeSubmission != "" && s.TypeSubmission != "chunked") || s.TypeChunkSize != 0 || s.WaitTimeout != 0 || s.PollInterval != 0
	if (selected || t.Kind == drivers.KindDevice) && special {
		return drivers.NativeResult{}, unsupported("selected and physical batches require default settings and stop on failure")
	}
	if selected && s.TapStyle != "" && s.TapStyle != "automatic" {
		return drivers.NativeResult{}, invalid("selected batch accepts only automatic tap style")
	}
	if t.Kind == drivers.KindDevice && s.TapStyle == "simulator" {
		return drivers.NativeResult{}, unsupported("simulator tap style is unavailable on hardware")
	}
	args := []string{"batch"}
	// Validate every step before launching AXe, because legacy AXe batches
	// otherwise parse later steps after earlier input has already been sent.
	for _, step := range s.Steps {
		parts, err := batchStepArgs(t, step)
		if err != nil {
			return drivers.NativeResult{}, err
		}
		args = append(args, "--step", quoteStep(parts))
	}
	if s.ContinueOnError {
		args = append(args, "--continue-on-error")
	}
	if s.AXCache != "" {
		args = append(args, "--ax-cache", s.AXCache)
	}
	if s.TypeSubmission != "" {
		args = append(args, "--type-submission", s.TypeSubmission)
	}
	if s.TapStyle != "" {
		args = append(args, "--tap-style", s.TapStyle)
	}
	args = appendInt(args, "--type-chunk-size", s.TypeChunkSize)
	args = appendFloat(args, "--wait-timeout", s.WaitTimeout)
	args = appendFloat(args, "--poll-interval", s.PollInterval)
	// Batch-level expected-pid is reserved for AXe agent mode. Carry the
	// assertion on the typed steps instead and never enable that mode here.
	scoped := t
	if t.InputContext != nil {
		copy := *t.InputContext
		copy.ExpectedPID = 0
		scoped.InputContext = &copy
	}
	if selected {
		var err error
		args, err = inputArgs(scoped, args, false, "")
		if err != nil {
			return drivers.NativeResult{}, err
		}
		args = append(args, "--verbose")
	}
	return d.run(ctx, t, true, args...)
}
