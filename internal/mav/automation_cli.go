package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// Automation is a portable typed operation, shared by commands and flows.
// It never contains an executable or provider command line.
type Automation struct {
	Command string                `json:"command" yaml:"command"`
	Options map[string]any        `json:"options,omitempty" yaml:"options,omitempty"`
	Context *drivers.InputContext `json:"context,omitempty" yaml:"context,omitempty"`
}

func (c CLI) requireAXe2(ctx context.Context, cfg Config) error {
	if targetKind(cfg) == drivers.KindMac {
		return nil
	}
	report := c.driverRegistry().Lookup("axe").Probe(ctx, NewExecutor(c.Runner))
	if report.IsHealthy() {
		return nil
	}
	return &drivers.DriverError{Driver: "axe", Code: "axe2_required", Message: report.Detail + "; " + report.Next, Delivery: drivers.DeliveryNotSent}
}
func (c CLI) automationFailure(err error) error {
	fields := map[string]string{"error": err.Error()}
	code := "automation_failed"
	var native *drivers.DriverError
	if errors.As(err, &native) {
		code = native.Code
		fields["driver"] = native.Driver
		fields["delivery"] = string(native.Delivery)
		if native.Delivery != drivers.DeliveryNotSent {
			fields["retry"] = "unsafe"
		}
		if native.Receipt != nil {
			b, _ := json.Marshal(native.Receipt)
			fields["receipt"] = string(b)
		}
	}
	writeErr := Fail(code, fields).Write(c.Stdout)
	// The fail line already carries the code, message and receipt; the native
	// envelope is only printed when the caller asks for tool output.
	if c.rawOutput && native != nil && len(native.Details) > 0 && json.Valid(native.Details) {
		if _, err := fmt.Fprintln(c.Stdout, string(native.Details)); err != nil {
			return err
		}
	}
	return writeErr
}
func normalizedOption(s string) string {
	return strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(s))
}
func decodeOptions(options map[string]any, out any) error {
	typ := reflect.TypeOf(out).Elem()
	names := map[string]string{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		names[normalizedOption(f.Name)] = f.Name
	}
	clean := map[string]any{}
	for k, v := range options {
		name, ok := names[normalizedOption(k)]
		if !ok {
			return fmt.Errorf("unknown option %q", k)
		}
		if _, ok := clean[name]; ok {
			return fmt.Errorf("duplicate option %q", k)
		}
		clean[name] = v
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}
func automationCLI(command string, args []string) (Automation, error) {
	req := Automation{Command: command, Options: map[string]any{}}
	if raw := flagValue(args, "--spec"); raw != "" {
		if strings.HasPrefix(raw, "@") {
			b, e := os.ReadFile(strings.TrimPrefix(raw, "@"))
			if e != nil {
				return req, e
			}
			raw = string(b)
		}
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber()
		if e := dec.Decode(&req.Options); e != nil {
			return req, e
		}
		if req.Options == nil {
			return req, fmt.Errorf("spec must be a non-null object")
		}
		var extra any
		if e := dec.Decode(&extra); e != io.EOF {
			return req, fmt.Errorf("spec must contain exactly one object")
		}
	}
	bools := map[string]bool{"down": true, "up": true, "close": true, "allow-lock": true, "semantic-press": true, "continue-on-error": true, "readback": true, "focused": true, "selected": true, "follow-active-display": true}
	selectors := map[string]any{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--spec" {
			i++
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			if command == "swipe" && req.Options["Direction"] == nil {
				req.Options["Direction"] = arg
				continue
			}
			if command == "gesture" && req.Options["Name"] == nil {
				req.Options["Name"] = arg
				continue
			}
			if command == "type" && req.Options["Text"] == nil {
				req.Options["Text"] = arg
				continue
			}
			return req, fmt.Errorf("unexpected operand %q", arg)
		}
		key := strings.TrimPrefix(arg, "--")
		if key == "angle" && command == "rotate" {
			key = "degrees"
		}
		var val any
		if bools[key] {
			val = true
		} else {
			if i+1 >= len(args) {
				return req, fmt.Errorf("%s needs a value", arg)
			}
			i++
			v := args[i]
			val = v
			stringOption := map[string]bool{"id": true, "label": true, "value": true, "text": true, "focus-id": true, "screen-uuid": true, "button": true, "direction": true, "appearance": true, "text-size": true, "liquid-glass": true, "color-filter": true, "endpoint": true, "contact-token": true, "tap-style": true, "element-type": true, "until-id": true, "until-label": true, "inside-id": true, "inside-label": true, "format": true, "out": true, "out-path": true, "log-path": true, "action": true, "preset": true, "scenario": true, "name": true, "ax-cache": true, "type-submission": true}
			if !stringOption[key] {

				if n, e := strconv.ParseFloat(v, 64); e == nil {
					val = n
				}
				if v == "true" || v == "false" {
					val = v == "true"
				}
				if strings.HasPrefix(v, "[") || strings.HasPrefix(v, "{") {
					var structured any
					if e := json.Unmarshal([]byte(v), &structured); e != nil {
						return req, e
					}
					val = structured
				}
			}
		}
		switch key {
		case "selected":
			if req.Context == nil {
				req.Context = &drivers.InputContext{}
			}
			req.Context.Selected = true
			continue
		case "screen-id", "screen-uuid", "expected-pid":
			if req.Context == nil {
				req.Context = &drivers.InputContext{}
			}
			switch key {
			case "screen-id":
				n, e := strconv.ParseUint(fmt.Sprint(val), 10, 32)
				if e != nil {
					return req, e
				}
				v := uint32(n)
				req.Context.ScreenID = &v
			case "screen-uuid":
				req.Context.ScreenUUID = fmt.Sprint(val)
			case "expected-pid":
				n, e := strconv.ParseInt(fmt.Sprint(val), 10, 32)
				if e != nil || n <= 0 {
					return req, fmt.Errorf("expected-pid must be positive")
				}
				req.Context.ExpectedPID = int32(n)
			}
			continue
		case "id", "label", "value":
			field := map[string]string{"id": "ID", "label": "Text", "value": "Value"}[key]
			selectors[field] = args[i]
			continue
		case "text":
			if command == "tap" || command == "longPress" || command == "doubleTap" || command == "slider" {
				selectors["Text"] = args[i]
				continue
			}
			val = args[i]
		case "duration", "hold":
			n, e := strconv.Atoi(args[i])
			if e != nil {
				d, e := time.ParseDuration(args[i])
				if e != nil {
					return req, e
				}
				n = int(d.Milliseconds())
			}
			if n < 0 {
				return req, fmt.Errorf("duration must be non-negative")
			}
			val = n
			key = "DurationMs"
			if command == "tap" || command == "longPress" || command == "doubleTap" {
				key = "Duration"
			}
			if strings.TrimPrefix(arg, "--") == "hold" && command == "drag" {
				key = "PressMs"
			}
			if strings.TrimPrefix(arg, "--") == "hold" && command == "twoFingerPan" {
				key = "HoldMs"
			}
		}
		if _, ok := req.Options[key]; ok {
			return req, fmt.Errorf("duplicate option %s", key)
		}
		req.Options[key] = val
	}
	if command == "swipe" {
		present := 0
		for k := range req.Options {
			switch normalizedOption(k) {
			case "startx", "starty", "endx", "endy":
				present++
			}
		}
		if present != 0 && present != 4 {
			return req, fmt.Errorf("swipe requires all four start/end coordinates")
		}
		if present == 0 {
			if req.Options["direction"] == nil && req.Options["Direction"] == nil {
				return req, fmt.Errorf("swipe requires a direction or four coordinates")
			}
		}
	}
	if len(selectors) > 0 {
		req.Options["Selector"] = selectors
	}
	return req, nil
}
func automationCapability(command string) drivers.Capability {
	switch command {
	case "tap", "longPress":
		return drivers.CapCoordTap
	case "doubleTap":
		return drivers.CapDoubleTap
	case "type":
		return drivers.CapType
	case "erase":
		return drivers.CapErase
	case "swipe":
		return drivers.CapSwipe
	case "drag":
		return drivers.CapDrag
	case "dragPath":
		return drivers.CapDragPath
	case "pinch":
		return drivers.CapPinch
	case "rotate":
		return drivers.CapRotate
	case "twoFingerPan":
		return drivers.CapTwoFingerPan
	case "key", "keySequence", "keyCombo":
		return drivers.CapKey
	case "touch":
		return drivers.CapTouch
	case "draw":
		return drivers.CapDraw
	case "slider":
		return drivers.CapSlider
	case "gesture":
		return drivers.CapGesturePreset
	case "settings", "location", "statusbar", "clipboard", "turn":
		return drivers.CapSettings
	case "press":
		return drivers.CapHardwareBtn
	case "displays":
		return drivers.CapDisplays
	case "readiness":
		return drivers.CapReadiness
	case "discover":
		return drivers.CapDiscovery
	case "shutdown":
		return drivers.CapShutdown
	case "batch":
		return drivers.CapBatch
	default:
		return ""
	}
}
func (c CLI) executeAutomation(ctx context.Context, cfg Config, prefer string, req Automation) (drivers.NativeResult, string, error) {
	if err := validateAutomation(req); err != nil {
		return drivers.NativeResult{}, "", &drivers.DriverError{Code: "invalidArguments", Message: err.Error(), Delivery: drivers.DeliveryNotSent}
	}

	if err := c.requireAXe2(ctx, cfg); err != nil {
		return drivers.NativeResult{}, "axe", err
	}
	target := targetFromConfig(cfg)
	if req.Context != nil {
		target.InputContext = req.Context
	}
	if e := validateOperationContext(req.Command, target.InputContext); e != nil {
		return drivers.NativeResult{}, "", e
	}
	cap := automationCapability(req.Command)
	if cap == "" {
		return drivers.NativeResult{}, "", fmt.Errorf("unknown automation command %q", req.Command)
	}
	if req.Command == "tap" {
		if _, ok := req.Options["Selector"]; ok {
			cap = drivers.CapSemanticTap
		}
	}
	driver, _, err := c.router().Route(ctx, cap, target, routerPrefer(prefer))
	if err != nil {
		return drivers.NativeResult{}, "", err
	}
	if e := validateContextDriver(driver, target); e != nil {
		return drivers.NativeResult{}, driver.ID(), e
	}
	if ordinaryAutomation(req.Command) {
		result, e := executeOrdinaryAutomation(ctx, driver, target, req)
		return result, driver.ID(), e
	}
	d, ok := driver.(drivers.NativeAutomationDriver)
	if !ok {
		return drivers.NativeResult{}, driver.ID(), fmt.Errorf("driver %s has no typed %s operation", driver.ID(), req.Command)
	}
	var result drivers.NativeResult
	decode := func(out any) error { return decodeOptions(req.Options, out) }
	switch req.Command {
	case "key":
		var s drivers.KeySpec
		if err = decode(&s); err == nil {
			result, err = d.Key(ctx, target, s)
		}
	case "keySequence":
		var s drivers.KeySequenceSpec
		if err = decode(&s); err == nil {
			result, err = d.KeySequence(ctx, target, s)
		}
	case "keyCombo":
		var s drivers.KeyComboSpec
		if err = decode(&s); err == nil {
			result, err = d.KeyCombo(ctx, target, s)
		}
	case "touch":
		var s drivers.TouchSpec
		if err = decode(&s); err == nil {
			result, err = d.Touch(ctx, target, s)
		}
	case "draw":
		var s drivers.DrawSpec
		if err = decode(&s); err == nil {
			result, err = d.Draw(ctx, target, s)
		}
	case "slider":
		var s drivers.SliderSpec
		if err = decode(&s); err == nil {
			result, err = d.Slider(ctx, target, s)
		}
	case "gesture":
		var s drivers.GestureSpec
		if err = decode(&s); err == nil {
			result, err = d.Gesture(ctx, target, s)
		}
	case "settings":
		var s drivers.SettingsSpec
		if err = decode(&s); err == nil {
			result, err = d.Settings(ctx, target, s)
		}
	case "location":
		var s drivers.LocationSpec
		if err = decode(&s); err == nil {
			result, err = d.Location(ctx, target, s)
		}
	case "statusbar":
		var s drivers.StatusBarActionSpec
		if err = decode(&s); err == nil {
			result, err = d.StatusBar(ctx, target, s)
		}
	case "clipboard":
		var s drivers.ClipboardSpec
		if err = decode(&s); err == nil {
			result, err = d.Clipboard(ctx, target, s)
		}
	case "press":
		var s drivers.ButtonSpec
		if err = decode(&s); err == nil {
			result, err = d.Button(ctx, target, s)
		}
	case "turn":
		var s drivers.RotationSpec
		if err = decode(&s); err == nil {
			result, err = d.Turn(ctx, target, s)
		}
	case "displays", "readiness", "discover", "shutdown":
		if len(req.Options) != 0 {
			err = fmt.Errorf("%s does not accept options", req.Command)
			break
		}
		switch req.Command {
		case "displays":
			result, err = d.Displays(ctx, target)
		case "readiness":
			result, err = d.Readiness(ctx, target)
		case "discover":
			result, err = d.Discover(ctx)
		case "shutdown":
			result, err = d.Shutdown(ctx, target)
		}
	case "batch":
		var s drivers.BatchSpec
		if err = decode(&s); err == nil {
			b, ok := driver.(drivers.NativeBatchDriver)
			if !ok {
				err = fmt.Errorf("batch unsupported")
			} else {
				result, err = b.Batch(ctx, target, s)
			}
		}
	}

	return result, driver.ID(), err
}
func (c CLI) automationOutput(ctx context.Context, cfg Config, opts GlobalOptions, req Automation) error {
	result, driver, err := c.executeAutomation(ctx, cfg, routerPrefer(opts.PreferDriver), req)
	var fallbackFields map[string]string
	if err != nil && driver == "axe" && req.Command == "tap" {
		if point, fallback, ok := c.coincidentTapAutomation(ctx, cfg, opts, req, err); ok {
			req = point
			result, driver, err = c.executeAutomation(ctx, cfg, routerPrefer(opts.PreferDriver), req)
			if err == nil {
				fallbackFields = fallback
			}
		}
	}
	if err != nil {
		return c.automationFailure(err)
	}
	fields := automationFields(req, driver)
	for k, v := range fallbackFields {
		fields[k] = v
	}
	if result.Receipt != nil {
		b, _ := json.Marshal(result.Receipt)
		fields["receipt"] = string(b)
	}
	if err = c.OK("ui."+req.Command, fields).Write(c.Stdout); err != nil {
		return err
	}
	// A read returns its data here; an input's data is AXe's own completion
	// record, which the ok line already reports.
	if len(result.Data) > 0 && (automationIsReadOnly(req) || opts.Raw || c.rawOutput) {
		_, err = fmt.Fprintln(c.Stdout, string(result.Data))
	}
	return err
}
func (c CLI) automationCommand(ctx context.Context, opts GlobalOptions, command string, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return c.failConfig(err)
	}
	c.resolveConfigTools(&cfg)
	if _, err = c.resolveConfigTarget(&cfg); err != nil {
		return c.failTargetCommand(err)
	}
	req, err := c.prepareAutomation(command, args)
	if err != nil {
		return c.automationFailure(&drivers.DriverError{Code: "invalidArguments", Delivery: drivers.DeliveryNotSent, Message: err.Error()})
	}
	return c.automationOutput(ctx, cfg, opts, req)
}
func (c CLI) tryNativeUI(ctx context.Context, opts GlobalOptions, cfg Config, command string, args []string) (bool, error) {
	if contextRequested(cfg.InputContext) {
		if e := validateOperationContext(command, cfg.InputContext); e != nil {
			return true, c.automationFailure(e)
		}
		if opts.PreferDriver != "" && opts.PreferDriver != "auto" && opts.PreferDriver != "axe" {
			return true, c.automationFailure(contextUnsupported("the preferred driver does not support selected input"))
		}
		for _, flag := range []string{"--where", "--find", "--index", "--role", "--near-text", "--text-contains", "--verify", "--no-wait", "--pan-x", "--pan-y", "--hold", "--distance"} {
			if hasFlag(args, flag) {
				return true, c.automationFailure(contextUnsupported("selected input requires a plain typed operation; this composition cannot preserve its binding"))
			}
		}
	}

	if command == "dragPath" {
		return false, nil
	}
	// AXe cannot express a compound drifting pinch. The legacy composition
	// planner chooses Baguette before any input, retaining the richer operation.
	if command == "pinch" && (hasFlag(args, "--pan-x") || hasFlag(args, "--pan-y") || hasFlag(args, "--hold") || hasFlag(args, "--rotate") || hasFlag(args, "--distance")) {
		return false, nil
	}
	if (command == "rotate" || command == "twoFingerPan") && hasFlag(args, "--hold") {
		return false, nil
	}

	if targetKind(cfg) == drivers.KindMac || (opts.PreferDriver != "" && opts.PreferDriver != "axe" && opts.PreferDriver != "auto") {
		return false, nil
	}
	if hasFlag(args, "--verify") || hasFlag(args, "--no-wait") {
		return false, nil
	}
	for _, flag := range []string{"--where", "--find", "--index", "--role", "--near-text", "--text-contains", "--enabled", "--visible"} {
		if hasFlag(args, flag) {
			return false, nil
		}
	}
	req, err := c.prepareAutomation(command, args)
	if err != nil && !hasFlag(args, "--spec") {
		return false, nil
	}
	if err != nil {
		return true, c.automationFailure(&drivers.DriverError{Code: "invalidArguments", Delivery: drivers.DeliveryNotSent, Message: err.Error()})
	}
	if e := validateAutomation(req); e != nil && !hasFlag(args, "--spec") {
		return false, nil
	}
	return true, c.automationOutput(ctx, cfg, opts, req)
}

func validateAutomation(req Automation) error {
	if req.Command == "batch" {
		if e := validateBatchPresence(req.Options); e != nil {
			return e
		}
	}

	present := map[string]bool{}
	for k := range req.Options {
		present[normalizedOption(k)] = true
	}
	switch req.Command {
	case "touch", "pinch", "rotate", "twoFingerPan":
		if !present["x"] || !present["y"] {
			return fmt.Errorf("%s requires x and y", req.Command)
		}
	case "key":
		if !present["code"] {
			return fmt.Errorf("key requires code")
		}
	case "keyCombo":
		if !present["key"] || !present["modifiers"] {
			return fmt.Errorf("keyCombo requires key and modifiers")
		}
	}

	if req.Command == "swipe" || req.Command == "drag" {
		n := 0
		for _, key := range []string{"startx", "starty", "endx", "endy"} {
			if present[key] {
				n++
			}
		}
		if n != 0 && n != 4 {
			return fmt.Errorf("%s requires all four start/end coordinates", req.Command)
		}
		if req.Command == "drag" && n == 0 {
			return fmt.Errorf("drag requires start/end coordinates")
		}
		if req.Command == "swipe" && n == 0 && !present["direction"] {
			return fmt.Errorf("swipe requires a direction or start/end coordinates")
		}
	}
	if req.Command == "tap" || req.Command == "longPress" || req.Command == "doubleTap" {
		if !present["selector"] && (!present["x"] || !present["y"]) {
			return fmt.Errorf("tap requires a selector or both x and y")
		}
	}

	var spec any
	switch req.Command {
	case "key":
		spec = &drivers.KeySpec{}
	case "keySequence":
		spec = &drivers.KeySequenceSpec{}
	case "keyCombo":
		spec = &drivers.KeyComboSpec{}
	case "touch":
		spec = &drivers.TouchSpec{}
	case "draw":
		spec = &drivers.DrawSpec{}
	case "slider":
		spec = &drivers.SliderSpec{}
	case "gesture":
		spec = &drivers.GestureSpec{}
	case "settings":
		spec = &drivers.SettingsSpec{}
	case "location":
		spec = &drivers.LocationSpec{}
	case "statusbar":
		spec = &drivers.StatusBarActionSpec{}
	case "clipboard":
		spec = &drivers.ClipboardSpec{}
	case "press":
		spec = &drivers.ButtonSpec{}
	case "turn":
		spec = &drivers.RotationSpec{}
	case "batch":
		spec = &drivers.BatchSpec{}
	case "tap", "longPress", "doubleTap":
		spec = &drivers.TapSpec{}
	case "type", "erase":
		spec = &drivers.TextSpec{}
	case "swipe":
		spec = &drivers.SwipeSpec{}
	case "drag":
		spec = &drivers.DragSpec{}
	case "dragPath":
		spec = &drivers.DragPathSpec{}
	case "pinch":
		spec = &drivers.PinchSpec{}
	case "rotate":
		spec = &drivers.RotateSpec{}
	case "twoFingerPan":
		spec = &drivers.TwoFingerPanSpec{}
	case "displays", "readiness", "discover", "shutdown":
		if len(req.Options) > 0 {
			return fmt.Errorf("%s accepts no options", req.Command)
		}
		return nil
	default:
		return fmt.Errorf("unknown automation command %q", req.Command)
	}
	if err := decodeOptions(req.Options, spec); err != nil {
		return err
	}
	if tap, ok := spec.(*drivers.TapSpec); ok && tap.Selector.IsZero() && (!present["x"] || !present["y"]) {
		return fmt.Errorf("tap requires a nonempty selector or x and y")
	}
	return nil
}

func inputRetrySafe(err error) bool {
	var native *drivers.DriverError
	if errors.As(err, &native) {
		return native.Delivery == drivers.DeliveryNotSent
	}
	return err == nil || (!strings.Contains(err.Error(), "retry=unsafe") && !strings.Contains(err.Error(), "delivery=uncertain") && !strings.Contains(err.Error(), "delivery=sent"))
}

func splitInputContext(args []string) (*drivers.InputContext, []string, error) {
	var binding, rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--selected":
			binding = append(binding, args[i])
		case "--screen-id", "--screen-uuid", "--expected-pid":
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("%s needs a value", args[i])
			}
			binding = append(binding, args[i], args[i+1])
			i++
		default:
			rest = append(rest, args[i])
		}
	}
	req, err := automationCLI("readiness", binding)
	return req.Context, rest, err
}

func automationFields(req Automation, driver string) map[string]string {
	fields := map[string]string{"driver": driver}
	for key, value := range req.Options {
		switch normalizedOption(key) {
		case "x", "y", "direction", "button":
			fields[strings.ToLower(key)] = fmt.Sprint(value)
		case "startx", "starty", "endx", "endy":
			if req.Command == "swipe" {
				fields["direction"] = "custom"
				name := map[string]string{"startx": "start_x", "starty": "start_y", "endx": "end_x", "endy": "end_y"}[normalizedOption(key)]
				fields[name] = fmt.Sprint(value)
			}
		case "selector":
			data, _ := json.Marshal(value)
			var selector drivers.ElementSelector
			_ = json.Unmarshal(data, &selector)
			if selector.ID != "" {
				fields["id"] = selector.ID
			}
			if selector.Text != "" {
				fields["text"] = selector.Text
			}
			if selector.Value != "" {
				fields["value"] = selector.Value
			}
		}
	}
	return fields
}

// coincidentTapAutomation turns a selector tap AXe refused as ambiguous into a
// tap at the innermost match's centre, when the matches' frames are nested.
func (c CLI) coincidentTapAutomation(ctx context.Context, cfg Config, opts GlobalOptions, req Automation, tapErr error) (Automation, map[string]string, bool) {
	var spec drivers.TapSpec
	// Ordinary tree coordinates cannot retain a selected display/process binding.
	if contextRequested(req.Context) || contextRequested(cfg.InputContext) || decodeOptions(req.Options, &spec) != nil || spec.SemanticPress || spec.Selector.IsZero() {
		return req, nil, false
	}
	selector := Selector{ID: spec.Selector.ID, Text: spec.Selector.Text, Value: spec.Selector.Value, Role: spec.ElementType}
	fallback, x, y, ok := c.coincidentTapPoint(ctx, cfg, opts.PreferDriver, selector, tapErr)
	if !ok {
		return req, nil, false
	}
	options := map[string]any{}
	for k, v := range req.Options {
		switch normalizedOption(k) {
		case "selector", "elementtype", "waittimeout", "pollinterval":
			continue
		}
		options[k] = v
	}
	options["X"], options["Y"] = x, y
	return Automation{Command: "tap", Options: options, Context: req.Context}, fallback, true
}

func automationIsReadOnly(req Automation) bool {
	switch req.Command {
	case "displays", "readiness", "discover":
		return true
	case "settings":
		return len(req.Options) == 0
	case "location":
		return req.Options["Action"] == "scenarios" || req.Options["action"] == "scenarios"
	case "statusbar":
		return req.Options["Action"] == "show" || req.Options["action"] == "show"
	}
	return false
}

func ordinaryAutomation(command string) bool {
	switch command {
	case "tap", "longPress", "doubleTap", "type", "erase", "swipe", "drag", "dragPath", "pinch", "rotate", "twoFingerPan", "press":
		return true
	}
	return false
}

func executeOrdinaryAutomation(ctx context.Context, d drivers.Driver, target drivers.Target, req Automation) (drivers.NativeResult, error) {
	var result drivers.NativeResult
	unsupported := func() error {
		return &drivers.DriverError{Driver: d.ID(), Code: "unsupportedCapability", Delivery: drivers.DeliveryNotSent, Message: "driver does not implement " + req.Command}
	}
	decode := func(spec any) error { return decodeOptions(req.Options, spec) }
	switch req.Command {
	case "tap", "longPress", "doubleTap":
		var s drivers.TapSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		if req.Command == "longPress" && s.Duration == 0 {
			s.Duration = 800
		}
		if req.Command == "doubleTap" && d.ID() != "axe" {
			v, ok := d.(drivers.AdvancedGestureDriver)
			if !ok {
				return result, unsupported()
			}
			return result, v.DoubleTap(ctx, target, s)
		}
		v, ok := d.(drivers.TapDriver)
		if !ok {
			return result, unsupported()
		}
		if req.Command == "doubleTap" {
			s.Count = 2
		}
		tapped, e := v.Tap(ctx, target, s)
		if tapped.Native != nil {
			result = *tapped.Native
		}
		return result, e
	case "type":
		var s drivers.TextSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		v, ok := d.(drivers.TypeDriver)
		if !ok {
			return result, unsupported()
		}
		return result, v.Type(ctx, target, s)
	case "erase":
		var s drivers.TextSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		v, ok := d.(drivers.EraseDriver)
		if !ok {
			return result, unsupported()
		}
		return result, v.Erase(ctx, target, s)
	case "swipe":
		var s drivers.SwipeSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		v, ok := d.(drivers.GestureDriver)
		if !ok {
			return result, unsupported()
		}
		return result, v.Swipe(ctx, target, s)
	case "drag":
		var s drivers.DragSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		v, ok := d.(drivers.AdvancedGestureDriver)
		if !ok {
			return result, unsupported()
		}
		return result, v.Drag(ctx, target, s)
	case "dragPath":
		var s drivers.DragPathSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		v, ok := d.(drivers.AdvancedGestureDriver)
		if !ok {
			return result, unsupported()
		}
		return result, v.DragPath(ctx, target, s)
	case "pinch":
		var s drivers.PinchSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		v, ok := d.(drivers.GestureDriver)
		if !ok {
			return result, unsupported()
		}
		return result, v.Pinch(ctx, target, s)
	case "rotate":
		var s drivers.RotateSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		v, ok := d.(drivers.GestureDriver)
		if !ok {
			return result, unsupported()
		}
		return result, v.Rotate(ctx, target, s)
	case "twoFingerPan":
		var s drivers.TwoFingerPanSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		v, ok := d.(drivers.GestureDriver)
		if !ok {
			return result, unsupported()
		}
		return result, v.TwoFingerPan(ctx, target, s)
	case "press":
		var s drivers.ButtonSpec
		if e := decode(&s); e != nil {
			return result, e
		}
		if v, ok := d.(drivers.NativeAutomationDriver); ok {
			return v.Button(ctx, target, s)
		}
		if s.DurationMs != 0 {
			return result, unsupported()
		}
		v, ok := d.(drivers.HardwareButtonDriver)
		if !ok {
			return result, unsupported()
		}
		return result, v.PressButton(ctx, target, s.Button)
	}
	return result, unsupported()
}

func (c CLI) prepareAutomation(command string, args []string) (Automation, error) {
	if !hasFlag(args, "--stdin") && !hasFlag(args, "--file") {
		return automationCLI(command, args)
	}
	if command != "type" && command != "clipboard" {
		return Automation{}, fmt.Errorf("stdin/file input only applies to type and clipboard")
	}
	if hasFlag(args, "--stdin") && hasFlag(args, "--file") {
		return Automation{}, fmt.Errorf("choose stdin or file input")
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--stdin":
		case "--file":
			if i+1 >= len(args) {
				return Automation{}, fmt.Errorf("file path missing")
			}
			i++
		default:
			rest = append(rest, args[i])
		}
	}
	req, e := automationCLI(command, rest)
	if e != nil {
		return req, e
	}
	for key := range req.Options {
		if normalizedOption(key) == "text" {
			return req, fmt.Errorf("choose one text source")
		}
	}
	var data []byte
	if path := flagValue(args, "--file"); path != "" {
		data, e = os.ReadFile(path)
	} else {
		if c.Stdin == nil {
			return req, fmt.Errorf("stdin unavailable")
		}
		data, e = io.ReadAll(c.Stdin)
	}
	if e != nil {
		return req, e
	}
	req.Options["Text"] = string(data)
	return req, nil
}

func afterInputError(err error) error {
	var native *drivers.DriverError
	if errors.As(err, &native) {
		copy := *native
		if copy.Delivery == drivers.DeliveryNotSent {
			copy.Delivery = drivers.DeliverySent
		}
		return &copy
	}
	return &drivers.DriverError{Code: "post_input_failed", Message: err.Error(), Delivery: drivers.DeliverySent}
}

func contextRequested(in *drivers.InputContext) bool {
	return in != nil && (in.Selected || in.ScreenID != nil || in.ScreenUUID != "" || in.ExpectedPID != 0)
}
func contextUnsupported(message string) error {
	return &drivers.DriverError{Code: "unsupportedInputContext", Message: message, Delivery: drivers.DeliveryNotSent}
}
func validateContextDriver(d drivers.Driver, target drivers.Target) error {
	if contextRequested(target.InputContext) && !d.Provides(target).Has(drivers.CapSelectedInput) {
		return contextUnsupported("driver " + d.ID() + " cannot enforce the requested input/display binding")
	}
	return nil
}
func validateOperationContext(command string, in *drivers.InputContext) error {
	if !contextRequested(in) {
		return nil
	}
	if (in.ScreenID == nil) != (in.ScreenUUID == "") {
		return &drivers.DriverError{Code: "invalidArguments", Message: "screen ID and UUID must be supplied together", Delivery: drivers.DeliveryNotSent}
	}
	if in.ExpectedPID != 0 {
		switch command {
		case "tap", "doubleTap", "longPress", "tree", "type", "erase", "key", "keySequence", "keyCombo", "slider":
		default:
			return contextUnsupported("this operation cannot enforce an expected PID")
		}
	}
	switch command {
	case "screenshot", "video":
		if in.Selected {
			return contextUnsupported("capture supports a screen ID/UUID pair, without selected input mode")
		}
		return nil
	case "tap", "doubleTap", "longPress", "tree", "type", "erase", "key", "keySequence", "keyCombo", "slider", "touch", "draw", "swipe", "drag", "pinch", "rotate", "twoFingerPan", "gesture", "press", "batch":
		return nil
	default:
		return contextUnsupported("this operation does not support selected input or display binding")
	}
}

func validateBatchPresence(options map[string]any) error {
	for key, raw := range options {
		if normalizedOption(key) != "steps" {
			continue
		}
		steps, ok := raw.([]any)
		if !ok {
			return nil
		} // typed Go specs retain their own explicit zero values.
		for index, raw := range steps {
			step, ok := raw.(map[string]any)
			if !ok {
				continue
			} // decoding reports the wrong type.
			for name, rawOptions := range step {
				if normalizedOption(name) == "sleepms" {
					continue
				}
				nested, ok := rawOptions.(map[string]any)
				if !ok {
					continue
				}
				command := map[string]string{"tap": "tap", "type": "type", "key": "key", "keysequence": "keySequence", "keycombo": "keyCombo", "touch": "touch", "draw": "draw", "gesture": "gesture", "swipe": "swipe", "drag": "drag", "button": "press", "slider": "slider"}[normalizedOption(name)]
				if command != "" {
					if e := validateAutomation(Automation{Command: command, Options: nested}); e != nil {
						return fmt.Errorf("batch step %d: %w", index+1, e)
					}
				}
			}
		}
	}
	return nil
}
