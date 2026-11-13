package axe

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func validCode(n int) bool  { return n >= 0 && n <= 255 }
func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func codeList(codes []int) string {
	out := make([]string, len(codes))
	for i, n := range codes {
		out[i] = strconv.Itoa(n)
	}
	return strings.Join(out, ",")
}
func keyArgs(s drivers.KeySpec) ([]string, error) {
	if !validCode(s.Code) || s.DurationMs < 0 || s.DurationMs > 10000 {
		return nil, invalid("key code must be 0...255 and duration 0...10000 ms")
	}
	return appendMS([]string{"key", strconv.Itoa(s.Code)}, "--duration", s.DurationMs), nil
}
func sequenceArgs(s drivers.KeySequenceSpec) ([]string, error) {
	if len(s.Codes) == 0 || len(s.Codes) > 100 {
		return nil, invalid("key sequence requires 1...100 keycodes")
	}
	for _, code := range s.Codes {
		if !validCode(code) {
			return nil, invalid("key code must be 0...255")
		}
	}
	args := []string{"key-sequence", "--keycodes", codeList(s.Codes)}
	if s.DelayMs != nil {
		if *s.DelayMs < 0 || *s.DelayMs > 5000 {
			return nil, invalid("key delay must be 0...5000 ms")
		}
		args = append(args, "--delay", seconds(*s.DelayMs))
	}
	return args, nil
}
func comboArgs(s drivers.KeyComboSpec) ([]string, error) {
	if !validCode(s.Key) || len(s.Modifiers) == 0 || len(s.Modifiers) > 8 {
		return nil, invalid("key combo requires a key and 1...8 modifiers")
	}
	seen := map[int]bool{}
	for _, code := range s.Modifiers {
		if code < 224 || code > 231 || seen[code] {
			return nil, invalid("modifiers must be distinct HID codes 224...231")
		}
		seen[code] = true
	}
	return []string{"key-combo", "--modifiers", codeList(s.Modifiers), "--key", strconv.Itoa(s.Key)}, nil
}
func (d *Driver) Key(ctx context.Context, t drivers.Target, s drivers.KeySpec) (drivers.NativeResult, error) {
	args, err := keyArgs(s)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	return d.input(ctx, t, args, true, s.FocusID)
}
func (d *Driver) KeySequence(ctx context.Context, t drivers.Target, s drivers.KeySequenceSpec) (drivers.NativeResult, error) {
	args, err := sequenceArgs(s)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	return d.input(ctx, t, args, true, s.FocusID)
}
func (d *Driver) KeyCombo(ctx context.Context, t drivers.Target, s drivers.KeyComboSpec) (drivers.NativeResult, error) {
	args, err := comboArgs(s)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	return d.input(ctx, t, args, true, s.FocusID)
}
func touchArgs(t drivers.Target, s drivers.TouchSpec) ([]string, error) {
	if s.X < 0 || s.Y < 0 || (!s.Down && !s.Up) {
		return nil, invalid("touch requires a nonnegative point and down or up")
	}
	if t.Kind == drivers.KindDevice && (!s.Down || !s.Up || s.ContactToken != "") {
		return nil, unsupported("physical-device touch requires down and up together")
	}
	if s.ContactToken != "" && (t.InputContext == nil || !t.InputContext.Selected) {
		return nil, invalid("contact token requires selected input")
	}
	args := []string{"touch", "-x", strconv.Itoa(s.X), "-y", strconv.Itoa(s.Y)}
	if s.Down {
		args = append(args, "--down")
	}
	if s.Up {
		args = append(args, "--up")
	}
	if s.DelayMs != nil {
		if !s.Down || !s.Up || *s.DelayMs < 0 || *s.DelayMs > 10000 {
			return nil, invalid("touch delay requires down+up and 0...10000 ms")
		}
		args = append(args, "--delay", seconds(*s.DelayMs))
	}
	if s.ContactToken != "" {
		args = append(args, "--contact-token", s.ContactToken)
	}
	return args, nil
}
func (d *Driver) Touch(ctx context.Context, t drivers.Target, s drivers.TouchSpec) (drivers.NativeResult, error) {
	args, err := touchArgs(t, s)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	return d.input(ctx, t, args, false, "")
}
func drawArgs(s drivers.DrawSpec) ([]string, error) {
	if len(s.Points) < 2 || len(s.Points) > 1000 || s.DurationMs < 0 || s.DurationMs > 30000 || s.PreDelayMs < 0 || s.PreDelayMs > 10000 || s.PostDelayMs < 0 || s.PostDelayMs > 10000 {
		return nil, invalid("draw requires 2...1000 points, duration <=30000 ms and delays <=10000 ms")
	}
	args := []string{"draw"}
	for _, p := range s.Points {
		if p.X < 0 || p.Y < 0 {
			return nil, invalid("draw points must be nonnegative")
		}
		if p.DurationMs != 0 {
			return nil, unsupported("draw has one stroke duration; per-point timing requires dragPath")
		}
		args = append(args, strconv.Itoa(p.X)+","+strconv.Itoa(p.Y))
	}
	args = appendMS(args, "--duration", s.DurationMs)
	args = appendMS(args, "--pre-delay", s.PreDelayMs)
	args = appendMS(args, "--post-delay", s.PostDelayMs)
	if s.Close {
		args = append(args, "--close")
	}
	return args, nil
}
func (d *Driver) Draw(ctx context.Context, t drivers.Target, s drivers.DrawSpec) (drivers.NativeResult, error) {
	args, err := drawArgs(s)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	return d.input(ctx, t, args, false, "")
}
func sliderArgs(t drivers.Target, s drivers.SliderSpec) ([]string, error) {
	if t.Kind == drivers.KindDevice {
		return nil, unsupported("sliders are available on simulators only")
	}
	if (s.Percent == nil) == (s.Endpoint == "") {
		return nil, invalid("slider requires exactly one percentage or endpoint")
	}
	if s.Selector.ID == "" && s.Selector.Text == "" {
		return nil, invalid("slider requires an ID or label")
	}
	args := selectorArgs([]string{"slider"}, s.Selector)
	if s.Percent != nil {
		if !finite(*s.Percent) || *s.Percent < 0 || *s.Percent > 100 {
			return nil, invalid("slider percentage must be 0...100")
		}
		args = append(args, "--value", number(*s.Percent))
	}
	if s.Endpoint != "" {
		if s.Endpoint != "minimum" && s.Endpoint != "maximum" {
			return nil, invalid("slider endpoint must be minimum or maximum")
		}
		if t.InputContext == nil || !t.InputContext.Selected {
			return nil, invalid("slider endpoint requires selected input")
		}
		args = append(args, "--endpoint", s.Endpoint)
	}
	if s.ElementType != "" {
		args = append(args, "--element-type", s.ElementType)
	}
	args = appendFloat(args, "--wait-timeout", s.WaitTimeout)
	args = appendFloat(args, "--poll-interval", s.PollInterval)
	return args, nil
}
func (d *Driver) Slider(ctx context.Context, t drivers.Target, s drivers.SliderSpec) (drivers.NativeResult, error) {
	args, err := sliderArgs(t, s)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	return d.input(ctx, t, args, false, "")
}
func gestureArgs(t drivers.Target, s drivers.GestureSpec) ([]string, error) {
	names := map[string]bool{"scroll-up": true, "scroll-down": true, "scroll-left": true, "scroll-right": true, "swipe-from-left-edge": true, "swipe-from-right-edge": true, "swipe-from-top-edge": true, "swipe-from-bottom-edge": true, "swipe-from-top-right-corner": true, "pinch": true, "rotate": true, "double-tap": true, "two-finger-tap": true, "two-finger-hold": true, "two-finger-swipe": true}
	if !names[s.Name] {
		return nil, invalid("unknown gesture preset")
	}
	if t.Kind == drivers.KindDevice && s.Name == "swipe-from-right-edge" {
		return nil, unsupported("right-edge swipe is unavailable on physical devices")
	}
	if s.DurationMs < 0 || s.DurationMs > 10000 || s.PreDelayMs < 0 || s.PreDelayMs > 10000 || s.PostDelayMs < 0 || s.PostDelayMs > 10000 {
		return nil, invalid("gesture duration and delays must be <=10000 ms")
	}
	args := []string{"gesture", s.Name}
	for _, v := range []struct {
		name  string
		value *int
	}{{"-x", s.X}, {"-y", s.Y}, {"--x2", s.X2}, {"--y2", s.Y2}, {"--end-x", s.EndX}, {"--end-y", s.EndY}, {"--end-x2", s.EndX2}, {"--end-y2", s.EndY2}, {"--edge-inset", s.EdgeInset}} {
		if v.value != nil {
			if *v.value < 0 {
				return nil, invalid("gesture coordinates must be nonnegative")
			}
			args = append(args, v.name, strconv.Itoa(*v.value))
		}
	}
	for _, v := range []struct {
		name  string
		value *float64
	}{{"--radius", s.Radius}, {"--scale", s.Scale}, {"--degrees", s.Degrees}, {"--distance", s.Distance}} {
		if v.value != nil {
			if !finite(*v.value) {
				return nil, invalid("gesture values must be finite")
			}
			args = append(args, v.name, number(*v.value))
		}
	}
	if (s.X == nil) != (s.Y == nil) {
		return nil, invalid("gesture requires both X and Y")
	}
	if s.Name == "pinch" && (s.Scale == nil || *s.Scale <= 0 || *s.Scale == 1) {
		return nil, invalid("pinch requires a positive scale other than 1")
	}
	if s.Name == "rotate" && (s.X == nil || s.Radius == nil || *s.Radius <= 0 || s.Degrees == nil || *s.Degrees == 0 || math.Abs(*s.Degrees) > 360) {
		return nil, invalid("rotation requires center, positive radius and nonzero degrees in -360...360")
	}
	if s.Radius != nil && *s.Radius <= 0 {
		return nil, invalid("radius must be positive")
	}
	if s.Distance != nil && (*s.Distance < 0.05 || *s.Distance > 0.95) {
		return nil, invalid("scroll distance must be 0.05...0.95")
	}
	if strings.HasPrefix(s.Name, "two-finger") && (s.X == nil || s.X2 == nil || s.Y2 == nil) {
		return nil, invalid("two-finger gesture requires two points")
	}
	if s.Name == "two-finger-swipe" && (s.EndX == nil || s.EndY == nil || s.EndX2 == nil || s.EndY2 == nil) {
		return nil, invalid("two-finger swipe requires both endpoints")
	}
	if s.Name == "two-finger-hold" && (s.HoldMs == nil || *s.HoldMs <= 0) {
		return nil, invalid("two-finger hold requires a duration")
	}
	for _, v := range []struct {
		name  string
		value *int
	}{{"--hold", s.HoldMs}, {"--interval", s.IntervalMs}} {
		if v.value != nil {
			if *v.value <= 0 || *v.value > 10000 {
				return nil, invalid("hold/interval must be 1...10000 ms")
			}
			args = append(args, v.name, seconds(*v.value))
		}
	}
	args = appendMS(args, "--duration", s.DurationMs)
	args = appendMS(args, "--pre-delay", s.PreDelayMs)
	args = appendMS(args, "--post-delay", s.PostDelayMs)
	args = appendInt(args, "--delta", s.Delta)
	args = appendInt(args, "--max-swipes", s.MaxSwipes)
	args = appendInt(args, "--screen-width", s.ScreenWidth)
	args = appendInt(args, "--screen-height", s.ScreenHeight)
	for _, v := range []struct{ name, value string }{{"--until-id", s.UntilID}, {"--until-label", s.UntilLabel}, {"--inside-id", s.InsideID}, {"--inside-label", s.InsideLabel}} {
		if v.value != "" {
			args = append(args, v.name, v.value)
		}
	}
	return args, nil
}
func (d *Driver) Gesture(ctx context.Context, t drivers.Target, s drivers.GestureSpec) (drivers.NativeResult, error) {
	args, err := gestureArgs(t, s)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	return d.input(ctx, t, args, false, "")
}
func ptrInt(n int) *int { return &n }
func (d *Driver) Pinch(ctx context.Context, t drivers.Target, s drivers.PinchSpec) error {
	if s.PanX != 0 || s.PanY != 0 {
		return unsupported("AXe pinch cannot drift; use a driver supporting pinch pan")
	}
	spec := drivers.GestureSpec{Name: "pinch", X: ptrInt(s.X), Y: ptrInt(s.Y), Scale: &s.Scale, DurationMs: s.DurationMs}
	if s.Radius > 0 {
		radius := float64(s.Radius)
		spec.Radius = &radius
	}
	_, err := d.Gesture(ctx, t, spec)
	return err
}
func (d *Driver) Rotate(ctx context.Context, t drivers.Target, s drivers.RotateSpec) error {
	radius := 40.0
	if s.Radius > 0 {
		radius = float64(s.Radius)
	}
	_, err := d.Gesture(ctx, t, drivers.GestureSpec{Name: "rotate", X: ptrInt(s.X), Y: ptrInt(s.Y), Radius: &radius, Degrees: &s.Degrees, DurationMs: s.DurationMs})
	return err
}
func (d *Driver) TwoFingerPan(ctx context.Context, t drivers.Target, s drivers.TwoFingerPanSpec) error {
	if s.HoldMs != 0 {
		return unsupported("AXe two-finger swipe cannot hold at its endpoint")
	}
	_, err := d.Gesture(ctx, t, drivers.GestureSpec{Name: "two-finger-swipe", X: ptrInt(s.X - 20), Y: ptrInt(s.Y), X2: ptrInt(s.X + 20), Y2: ptrInt(s.Y), EndX: ptrInt(s.X - 20 + s.PanX), EndY: ptrInt(s.Y + s.PanY), EndX2: ptrInt(s.X + 20 + s.PanX), EndY2: ptrInt(s.Y + s.PanY), DurationMs: s.DurationMs})
	return err
}
func (d *Driver) DoubleTap(ctx context.Context, t drivers.Target, s drivers.TapSpec) error {
	s.Count = 2
	_, err := d.Tap(ctx, t, s)
	return err
}
func dragArgs(s drivers.DragSpec) ([]string, error) {
	if s.StartX < 0 || s.StartY < 0 || s.EndX < 0 || s.EndY < 0 || s.DurationMs < 0 || s.DurationMs > 10000 || s.PressMs < 0 || s.PressMs > 10000 || s.HoldMs < 0 || s.Steps < 0 || s.Steps > 1000 || s.PreDelayMs < 0 || s.PreDelayMs > 10000 || s.PostDelayMs < 0 || s.PostDelayMs > 10000 {
		return nil, invalid("drag requires nonnegative points and bounded durations")
	}
	if s.HoldMs != 0 {
		return nil, unsupported("AXe drag has no configurable endpoint hold")
	}
	args := []string{"drag", "--start-x", strconv.Itoa(s.StartX), "--start-y", strconv.Itoa(s.StartY), "--end-x", strconv.Itoa(s.EndX), "--end-y", strconv.Itoa(s.EndY)}
	args = appendMS(args, "--duration", s.DurationMs)
	args = appendMS(args, "--hold", s.PressMs)
	args = appendMS(args, "--pre-delay", s.PreDelayMs)
	args = appendMS(args, "--post-delay", s.PostDelayMs)
	args = appendInt(args, "--steps", s.Steps)
	return args, nil
}
func (d *Driver) Drag(ctx context.Context, t drivers.Target, s drivers.DragSpec) error {
	args, err := dragArgs(s)
	if err != nil {
		return err
	}
	_, err = d.input(ctx, t, args, false, "")
	return err
}
func (d *Driver) DragPath(context.Context, drivers.Target, drivers.DragPathSpec) error {
	return unsupported("AXe draw does not preserve dragPath per-point timing; choose another driver")
}
func (d *Driver) W3CActions(context.Context, drivers.Target, []byte) error {
	return unsupported("AXe has no W3C Actions API")
}
func (d *Driver) HideKeyboard(context.Context, drivers.Target) error {
	return unsupported("AXe has no general hide-keyboard operation")
}
func buttonArgs(t drivers.Target, s drivers.ButtonSpec) ([]string, error) {
	value := string(s.Button)
	switch value {
	case "home", "lock", "siri", "apple-pay", "side-button":
	default:
		return nil, unsupported("AXe does not support button " + value)
	}
	if t.Kind == drivers.KindDevice {
		if value != "home" && value != "lock" && value != "siri" {
			return nil, unsupported("physical devices support home, siri and lock")
		}
		if value == "lock" && !s.AllowLock {
			return nil, invalid("locking a physical device requires allow-lock")
		}
	}
	if s.DurationMs < 0 || s.DurationMs > 10000 {
		return nil, invalid("button duration must be 0...10000 ms")
	}
	args := appendMS([]string{"button", value}, "--duration", s.DurationMs)
	if s.AllowLock {
		args = append(args, "--allow-lock")
	}
	return args, nil
}
func (d *Driver) Button(ctx context.Context, t drivers.Target, s drivers.ButtonSpec) (drivers.NativeResult, error) {
	args, err := buttonArgs(t, s)
	if err != nil {
		return drivers.NativeResult{}, err
	}
	return d.input(ctx, t, args, false, "")
}
func (d *Driver) PressButton(ctx context.Context, t drivers.Target, b drivers.HardwareButton) error {
	_, err := d.Button(ctx, t, drivers.ButtonSpec{Button: b})
	return err
}
func (d *Driver) Hinge(ctx context.Context, t drivers.Target, s drivers.HingeSpec) (drivers.HingeState, error) {
	if t.Kind == drivers.KindDevice {
		return drivers.HingeState{}, unsupported("hinge is simulator-only")
	}
	if s.Duration != 0 {
		return drivers.HingeState{}, unsupported("AXe hinge does not expose animation duration")
	}
	args := []string{"hinge", "read"}
	mutation := s.Angle != nil || s.Pose != ""
	if mutation {
		value := s.Pose
		if s.Angle != nil {
			if !finite(*s.Angle) || *s.Angle < 0 || *s.Angle > 180 {
				return drivers.HingeState{}, invalid("hinge angle must be 0...180")
			}
			value = number(*s.Angle)
		} else {
			switch value {
			case "closed":
				value = "0"
			case "open":
				value = "130"
			case "flat":
				value = "180"
			case "tabletop":
				value = "90"
			default:
				return drivers.HingeState{}, invalid("unknown hinge pose")
			}
		}
		args = []string{"hinge", "set", value}
	}
	// The normal JSON result contains readback state; verbose substitutes a
	// native receipt and does not always include the final settled reading.
	result, err := d.run(ctx, t, mutation, args...)
	if err != nil {
		return drivers.HingeState{}, err
	}
	var output struct {
		Hinge struct {
			Degrees       *float64 `json:"degrees"`
			SensedDegrees *float64 `json:"sensedDegrees"`
		} `json:"hinge"`
	}
	if json.Unmarshal(result.Data, &output) != nil {
		return drivers.HingeState{}, invalid("AXe hinge returned an invalid state")
	}
	angle := output.Hinge.Degrees
	if angle == nil {
		angle = output.Hinge.SensedDegrees
	}
	if angle == nil {
		return drivers.HingeState{}, &drivers.DriverError{Driver: ID, Code: "axe_invalid_hinge", Message: "AXe did not confirm the hinge angle", Delivery: drivers.DeliveryUncertain, Details: result.Envelope}
	}
	return drivers.HingeState{Angle: angle}, nil
}
func (d *Driver) Turn(ctx context.Context, t drivers.Target, s drivers.RotationSpec) (drivers.NativeResult, error) {
	if s.Direction != "left" && s.Direction != "right" {
		return drivers.NativeResult{}, invalid("rotation direction must be left or right")
	}
	args := appendFloat([]string{"rotate", s.Direction}, "--timeout", s.Timeout)
	if in := t.InputContext; in != nil && in.ScreenID != nil {
		args = append(args, "--screen-id", strconv.FormatUint(uint64(*in.ScreenID), 10), "--screen-uuid", in.ScreenUUID, "--verbose")
	}
	return d.run(ctx, t, true, args...)
}
func (d *Driver) Settings(ctx context.Context, t drivers.Target, s drivers.SettingsSpec) (drivers.NativeResult, error) {
	args := []string{"settings", "set"}
	for _, v := range []struct{ name, value string }{{"--appearance", s.Appearance}, {"--text-size", s.TextSize}, {"--liquid-glass", s.LiquidGlass}, {"--color-filter", s.ColorFilter}} {
		if v.value != "" {
			args = append(args, v.name, v.value)
		}
	}
	for _, v := range []struct {
		name  string
		value *bool
	}{{"--increase-contrast", s.IncreaseContrast}, {"--reduce-motion", s.ReduceMotion}, {"--reduce-transparency", s.ReduceTransparency}, {"--show-borders", s.ShowBorders}, {"--voiceover", s.VoiceOver}} {
		if v.value != nil {
			value := "off"
			if *v.value {
				value = "on"
			}
			args = append(args, v.name, value)
		}
	}
	for _, v := range []struct {
		name  string
		value *float64
	}{{"--liquid-glass-opacity", s.LiquidGlassOpacity}, {"--color-filter-intensity", s.ColorFilterIntensity}} {
		if v.value != nil {
			if !finite(*v.value) {
				return drivers.NativeResult{}, invalid("settings scalar must be finite")
			}
			args = append(args, v.name, number(*v.value))
		}
	}
	mutation := len(args) > 2
	if !mutation {
		args[1] = "show"
	}
	return d.run(ctx, t, mutation, args...)
}
func (d *Driver) SetAppearance(ctx context.Context, t drivers.Target, appearance string) error {
	_, err := d.Settings(ctx, t, drivers.SettingsSpec{Appearance: appearance})
	return err
}
func (d *Driver) Location(ctx context.Context, t drivers.Target, s drivers.LocationSpec) (drivers.NativeResult, error) {
	args := []string{"settings", "location", s.Action}
	mutation := true
	switch s.Action {
	case "set":
		if s.Latitude == nil || s.Longitude == nil || !finite(*s.Latitude) || !finite(*s.Longitude) || math.Abs(*s.Latitude) > 90 || math.Abs(*s.Longitude) > 180 {
			return drivers.NativeResult{}, invalid("location requires latitude -90...90 and longitude -180...180")
		}
		args = append(args, "--latitude", number(*s.Latitude), "--longitude", number(*s.Longitude))
	case "clear":
	case "scenarios":
		mutation = false
	case "scenario":
		if s.Scenario == "" || strings.HasPrefix(s.Scenario, "-") {
			return drivers.NativeResult{}, invalid("location scenario name required")
		}
		args = append(args, s.Scenario)
	default:
		return drivers.NativeResult{}, invalid("location action must be set, clear, scenarios or scenario")
	}
	return d.run(ctx, t, mutation, args...)
}
func (d *Driver) SetLocation(ctx context.Context, t drivers.Target, lat, lon float64) error {
	_, err := d.Location(ctx, t, drivers.LocationSpec{Action: "set", Latitude: &lat, Longitude: &lon})
	return err
}
func (d *Driver) ResetLocation(ctx context.Context, t drivers.Target) error {
	_, err := d.Location(ctx, t, drivers.LocationSpec{Action: "clear"})
	return err
}
func (d *Driver) StatusBar(ctx context.Context, t drivers.Target, s drivers.StatusBarActionSpec) (drivers.NativeResult, error) {
	if t.Kind == drivers.KindDevice {
		return drivers.NativeResult{}, unsupported("status bar overrides are simulator-only")
	}
	args := []string{"settings", "status-bar", s.Action}
	mutation := true
	switch s.Action {
	case "show":
		mutation = false
	case "clear":
	case "preset":
		if s.Preset == "" {
			return drivers.NativeResult{}, invalid("status bar preset required")
		}
		args = append(args, s.Preset)
	case "set":
		v := s.Values
		if v.WifiMode != "" || v.CellularMode != "" || v.OperatorName != "" {
			return drivers.NativeResult{}, unsupported("AXe status bar cannot set wifi-mode, cellular-mode or operator-name")
		}
		for _, option := range []struct{ name, value string }{{"--time", v.Time}, {"--data-network", v.DataNetwork}, {"--wifi-bars", v.WifiBars}, {"--cellular-bars", v.CellularBars}, {"--battery-level", v.BatteryLevel}, {"--battery-state", v.BatteryState}} {
			if option.value != "" {
				args = append(args, option.name, option.value)
			}
		}
	default:
		return drivers.NativeResult{}, invalid("status bar action must be show, set, clear or preset")
	}
	return d.run(ctx, t, mutation, args...)
}
func (d *Driver) SetStatusBar(ctx context.Context, t drivers.Target, s drivers.StatusBarSpec) error {
	_, err := d.StatusBar(ctx, t, drivers.StatusBarActionSpec{Action: "set", Values: s})
	return err
}
func (d *Driver) ClearStatusBar(ctx context.Context, t drivers.Target) error {
	_, err := d.StatusBar(ctx, t, drivers.StatusBarActionSpec{Action: "clear"})
	return err
}
func (d *Driver) Clipboard(ctx context.Context, t drivers.Target, s drivers.ClipboardSpec) (drivers.NativeResult, error) {
	if t.Kind == drivers.KindDevice {
		return drivers.NativeResult{}, unsupported("clipboard copy is simulator-only")
	}
	if len(s.Text) < 1 || len(s.Text) > 65536 {
		return drivers.NativeResult{}, invalid("clipboard requires 1...65536 UTF-8 bytes")
	}
	input, ok := d.exec.(drivers.InputExecutor)
	if !ok {
		return drivers.NativeResult{}, unsupported("executor does not support clipboard stdin")
	}
	// AXe always reads back and verifies the bytes; no optional weaker copy.
	args := append([]string{"--output", "json"}, targetArgs(t, "clipboard", "copy", "--stdin")...)
	return decodeOutput(input.RunInput(ctx, s.Text, d.binary(), args...), true)
}
func (d *Driver) ClipboardWrite(ctx context.Context, t drivers.Target, text string) error {
	_, err := d.Clipboard(ctx, t, drivers.ClipboardSpec{Text: text, Readback: true})
	return err
}
func (d *Driver) Displays(ctx context.Context, t drivers.Target) (drivers.NativeResult, error) {
	if t.Kind == drivers.KindDevice {
		return drivers.NativeResult{}, unsupported("display discovery is simulator-only")
	}
	return d.run(ctx, t, false, "displays", "--verbose")
}
func (d *Driver) Readiness(ctx context.Context, t drivers.Target) (drivers.NativeResult, error) {
	return d.run(ctx, t, false, "doctor")
}
func (d *Driver) Discover(ctx context.Context) (drivers.NativeResult, error) {
	return d.run(ctx, drivers.Target{}, false, "list-devices")
}
func (d *Driver) Shutdown(ctx context.Context, t drivers.Target) (drivers.NativeResult, error) {
	if t.Kind == drivers.KindDevice {
		return drivers.NativeResult{}, unsupported("shutdown is simulator-only")
	}
	return d.run(ctx, t, true, "shutdown")
}
