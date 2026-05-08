package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const appiumBasePath = "/wd/hub"

type appiumError struct {
	Code    string
	Message string
	Next    string
}

func (e appiumError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

type appiumSessionState struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	BaseURL   string `json:"base_url"`
	SessionID string `json:"session_id"`
	UDID      string `json:"udid"`
	BundleID  string `json:"bundle_id"`
}

type appiumSourceResult struct {
	Raw          string
	ActiveBundle string
	SystemSource bool
}

type gestureParams struct {
	Kind     string
	X        string
	Y        string
	Scale    string
	PanX     string
	PanY     string
	Distance string
	Angle    string
	Rotate   string
	Degrees  string
	Duration string
	Hold     string
}

type appiumDriverStatus struct {
	OK             bool
	Message        string
	Version        string
	PredicateOK    bool
	NodeMismatch   bool
	HomePermission bool
	Next           string
}

func appiumHasXCUITestDriver(ctx context.Context, runner Runner) bool {
	return checkAppiumXCUITestDriver(ctx, runner).OK
}

func checkAppiumXCUITestDriver(ctx context.Context, runner Runner) appiumDriverStatus {
	result := runner.Run(ctx, "appium", "driver", "list", "--installed")
	output := result.Stdout + "\n" + result.Stderr
	if result.Err == nil && strings.Contains(strings.ToLower(output), "xcuitest") {
		version := appiumXCUITestVersion(output)
		return appiumDriverStatus{
			OK:          true,
			Version:     version,
			PredicateOK: appiumXCUITestPredicateSupported(version),
		}
	}
	if looksLikeAppiumHomePermissionFailure(output) {
		if retry := retryAppiumDriverListWithWritableHome(ctx, runner); retry.OK {
			return retry
		}
		return appiumDriverStatus{
			Message:        "appium_home_not_writable",
			HomePermission: true,
			Next:           "run outside the sandbox or set APPIUM_HOME to a writable directory",
		}
	}
	if looksLikeAppiumNodeRuntimeFailure(output) {
		return appiumDriverStatus{
			Message:      "appium_node_runtime_failed",
			NodeMismatch: true,
			Next:         "put the Node.js used to install Appium first in PATH",
		}
	}
	if result.Err != nil && strings.TrimSpace(output) != "" {
		return appiumDriverStatus{Message: firstLine(output), Next: "mav setup --install appium"}
	}
	return appiumDriverStatus{Message: "xcuitest driver missing", Next: "mav setup --install appium"}
}

func appiumXCUITestVersion(output string) string {
	for _, field := range strings.Fields(output) {
		lower := strings.ToLower(strings.Trim(field, ",;()[]{}"))
		if strings.HasPrefix(lower, "xcuitest@") {
			return strings.TrimPrefix(lower, "xcuitest@")
		}
	}
	return ""
}

func appiumXCUITestPredicateSupported(version string) bool {
	major, ok := majorVersion(version)
	if !ok {
		return true
	}
	return major >= 9
}

func majorVersion(version string) (int, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return 0, false
	}
	head := version
	if idx := strings.IndexAny(head, ".-+"); idx >= 0 {
		head = head[:idx]
	}
	major, err := strconv.Atoi(head)
	return major, err == nil
}

func looksLikeAppiumNodeRuntimeFailure(output string) bool {
	output = strings.ToLower(output)
	return strings.Contains(output, "err_require_esm") || strings.Contains(output, "node.js v")
}

func looksLikeAppiumHomePermissionFailure(output string) bool {
	output = strings.ToLower(output)
	return strings.Contains(output, "appium home") && (strings.Contains(output, "writeable") || strings.Contains(output, "writable"))
}

func retryAppiumDriverListWithWritableHome(ctx context.Context, runner Runner) appiumDriverStatus {
	cleanup := withWritableAppiumHome(runner)
	defer cleanup()
	result := runner.Run(ctx, "appium", "driver", "list", "--installed")
	output := result.Stdout + "\n" + result.Stderr
	if result.Err == nil && strings.Contains(strings.ToLower(output), "xcuitest") {
		return appiumDriverStatus{OK: true}
	}
	return appiumDriverStatus{}
}

func withWritableAppiumHome(runner Runner) func() {
	_ = runner
	if strings.TrimSpace(os.Getenv("APPIUM_HOME")) != "" {
		return func() {}
	}
	dir, err := os.MkdirTemp("", "mav-appium-home-")
	if err != nil {
		return func() {}
	}
	oldValue, hadValue := os.LookupEnv("APPIUM_HOME")
	_ = os.Setenv("APPIUM_HOME", dir)
	return func() {
		if hadValue {
			_ = os.Setenv("APPIUM_HOME", oldValue)
		} else {
			_ = os.Unsetenv("APPIUM_HOME")
		}
		_ = os.RemoveAll(dir)
	}
}

type appiumNodeCheck struct {
	OK      bool
	Message string
	Next    string
}

func checkAppiumNodePath(runner Runner) appiumNodeCheck {
	appiumPath, err := runner.LookPath("appium")
	if err != nil {
		return appiumNodeCheck{OK: false, Message: "appium missing", Next: "mav setup --install appium"}
	}
	nodePath, err := runner.LookPath("node")
	if err != nil {
		return appiumNodeCheck{OK: false, Message: "node missing", Next: "put the Node.js used to install Appium first in PATH"}
	}
	data, err := os.ReadFile(appiumPath)
	if err != nil {
		if _, ok := runner.(ExecRunner); ok {
			return appiumNodeCheck{OK: false, Message: "appium_node_unreadable", Next: "verify appium is installed with the active Node.js on PATH"}
		}
		return appiumNodeCheck{OK: true}
	}
	first := firstLine(string(data))
	if !strings.HasPrefix(first, "#!") || !strings.Contains(first, "node") {
		return appiumNodeCheck{OK: true}
	}
	shebangNode := strings.TrimSpace(strings.TrimPrefix(first, "#!"))
	fields := strings.Fields(shebangNode)
	if len(fields) > 0 {
		shebangNode = fields[0]
	}
	if filepath.Base(shebangNode) == "env" && len(fields) > 1 && fields[1] == "node" {
		return appiumNodeCheck{OK: true}
	}
	if filepath.Base(shebangNode) != "node" || !filepath.IsAbs(shebangNode) {
		return appiumNodeCheck{OK: true}
	}
	if filepath.Clean(shebangNode) == filepath.Clean(nodePath) {
		return appiumNodeCheck{OK: true}
	}
	return appiumNodeCheck{
		OK:      false,
		Message: "appium_node_mismatch",
		Next:    "put " + filepath.Dir(shebangNode) + " before " + filepath.Dir(nodePath) + " in PATH",
	}
}

func gestureParamsFromArgs(args []string) gestureParams {
	return gestureParams{
		X:        flagValue(args, "--x"),
		Y:        flagValue(args, "--y"),
		Scale:    flagValue(args, "--scale"),
		PanX:     flagValue(args, "--pan-x"),
		PanY:     flagValue(args, "--pan-y"),
		Distance: flagValue(args, "--distance"),
		Angle:    flagValue(args, "--angle"),
		Rotate:   flagValue(args, "--rotate"),
		Degrees:  flagValue(args, "--degrees"),
		Duration: flagValue(args, "--duration"),
		Hold:     flagValue(args, "--hold"),
	}
}

func buildGestureActions(params gestureParams) ([]map[string]any, map[string]string, error) {
	x, err := parseRequiredFloat(params.X, "x")
	if err != nil {
		return nil, nil, err
	}
	y, err := parseRequiredFloat(params.Y, "y")
	if err != nil {
		return nil, nil, err
	}
	distance, err := parseOptionalFloat(params.Distance, 140, "distance")
	if err != nil {
		return nil, nil, err
	}
	if distance <= 0 {
		return nil, nil, fmt.Errorf("distance_must_be_positive")
	}
	rawAngle, err := parseOptionalFloat(params.Angle, 0, "angle")
	if err != nil {
		return nil, nil, err
	}
	angle := degreesToRadians(rawAngle)
	rotate, err := parseOptionalFloat(params.Rotate, 0, "rotate")
	if err != nil {
		return nil, nil, err
	}
	scale, err := parseOptionalFloat(params.Scale, 1, "scale")
	if err != nil {
		return nil, nil, err
	}
	panX, err := parseOptionalFloat(params.PanX, 0, "panX")
	if err != nil {
		return nil, nil, err
	}
	panY, err := parseOptionalFloat(params.PanY, 0, "panY")
	if err != nil {
		return nil, nil, err
	}

	switch params.Kind {
	case "pinch":
		if strings.TrimSpace(params.Scale) == "" {
			return nil, nil, fmt.Errorf("scale_missing")
		}
		if scale <= 0 {
			return nil, nil, fmt.Errorf("scale_must_be_positive")
		}
	case "rotate":
		if strings.TrimSpace(params.Degrees) == "" {
			return nil, nil, fmt.Errorf("degrees_missing")
		}
		rotate, err = parseOptionalFloat(params.Degrees, 0, "degrees")
		if err != nil {
			return nil, nil, err
		}
		scale = 1
		panX = 0
		panY = 0
	case "twoFingerPan":
		scale = 1
		rotate = 0
		if panX == 0 && panY == 0 {
			return nil, nil, fmt.Errorf("pan_delta_missing")
		}
	default:
		return nil, nil, fmt.Errorf("gesture_kind_unknown")
	}

	duration := parseFlowDuration(params.Duration, 800*time.Millisecond)
	if duration <= 0 {
		return nil, nil, fmt.Errorf("duration_invalid")
	}
	durationMS := int(duration / time.Millisecond)
	if durationMS <= 0 {
		durationMS = 1
	}
	hold := parseFlowDuration(params.Hold, 0)
	if hold < 0 {
		return nil, nil, fmt.Errorf("hold_invalid")
	}
	holdMS := int(hold / time.Millisecond)
	if params.Hold != "" && holdMS <= 0 {
		return nil, nil, fmt.Errorf("hold_invalid")
	}

	startA, startB := twoFingerPoints(x, y, distance, angle)
	endA, endB := twoFingerPoints(x+panX, y+panY, distance*scale, angle+degreesToRadians(rotate))
	actions := []map[string]any{
		touchPointerActions("finger1", startA, endA, durationMS, holdMS),
		touchPointerActions("finger2", startB, endB, durationMS, holdMS),
	}
	fields := map[string]string{
		"x":        formatNumber(x),
		"y":        formatNumber(y),
		"duration": strconv.Itoa(durationMS) + "ms",
	}
	if params.Kind == "pinch" {
		fields["scale"] = formatNumber(scale)
	}
	if panX != 0 || panY != 0 {
		fields["pan_x"] = formatNumber(panX)
		fields["pan_y"] = formatNumber(panY)
	}
	if rotate != 0 {
		fields["rotate"] = formatNumber(rotate)
	}
	if params.Kind == "rotate" {
		fields["degrees"] = formatNumber(rotate)
	}
	if holdMS > 0 {
		fields["hold"] = strconv.Itoa(holdMS) + "ms"
	}
	return actions, fields, nil
}

type point struct {
	X float64
	Y float64
}

func twoFingerPoints(centerX, centerY, distance, angle float64) (point, point) {
	half := distance / 2
	dx := math.Cos(angle) * half
	dy := math.Sin(angle) * half
	return point{X: centerX - dx, Y: centerY - dy}, point{X: centerX + dx, Y: centerY + dy}
}

func touchPointerActions(id string, start, end point, durationMS, holdMS int) map[string]any {
	steps := []map[string]any{
		{"type": "pointerMove", "duration": 0, "origin": "viewport", "x": rounded(start.X), "y": rounded(start.Y)},
		{"type": "pointerDown", "button": 0},
		{"type": "pointerMove", "duration": durationMS, "origin": "viewport", "x": rounded(end.X), "y": rounded(end.Y)},
	}
	if holdMS > 0 {
		steps = append(steps, map[string]any{"type": "pause", "duration": holdMS})
	}
	steps = append(steps, map[string]any{"type": "pointerUp", "button": 0})
	return map[string]any{
		"type":       "pointer",
		"id":         id,
		"parameters": map[string]any{"pointerType": "touch"},
		"actions":    steps,
	}
}

func gestureCompletionDelay(fields map[string]string) time.Duration {
	delay := parseFlowDuration(fields["duration"], 0)
	if delay < 0 {
		delay = 0
	}
	hold := parseFlowDuration(fields["hold"], 0)
	if hold > 0 {
		delay += hold
	}
	return delay
}

func waitForGestureCompletion(fields map[string]string) {
	if delay := gestureCompletionDelay(fields); delay > 0 {
		time.Sleep(delay)
	}
}

func (c CLI) performAppiumActions(ctx context.Context, cfg Config, actions any) error {
	if err := c.ensureAppiumAvailable(ctx); err != nil {
		return err
	}
	run, runErr := LoadRun(c.Root, "")
	transient := false
	if runErr != nil || run.ID == "" {
		var err error
		run, err = NewRunState()
		if err != nil {
			return err
		}
		transient = true
	}
	session, err := c.ensureAppiumSession(ctx, cfg, run)
	if err != nil {
		if transient && session.PID > 0 {
			_ = stopProcess(session.PID)
		}
		return err
	}
	if transient {
		defer stopProcess(session.PID)
	}
	if err := appiumPostJSON(ctx, session.BaseURL+"/session/"+session.SessionID+"/actions", map[string]any{"actions": actions}, nil); err != nil {
		return appiumError{Code: "ui_gesture_failed", Message: err.Error()}
	}
	_ = appiumDelete(ctx, session.BaseURL+"/session/"+session.SessionID+"/actions")
	return nil
}

func (c CLI) ensureAppiumAvailable(ctx context.Context) error {
	if _, err := c.Runner.LookPath("appium"); err != nil {
		return appiumError{Code: "tool_missing", Message: "appium missing", Next: "mav setup --install appium"}
	}
	if nodeCheck := checkAppiumNodePath(c.Runner); !nodeCheck.OK {
		return appiumError{Code: "tool_missing", Message: nodeCheck.Message, Next: nodeCheck.Next}
	}
	driverStatus := checkAppiumXCUITestDriver(ctx, c.Runner)
	if !driverStatus.OK {
		next := driverStatus.Next
		if next == "" {
			next = "mav setup --install appium"
		}
		return appiumError{Code: "tool_missing", Message: driverStatus.Message, Next: next}
	}
	return nil
}

func (c CLI) appiumSessionForCommand(ctx context.Context, cfg Config) (appiumSessionState, func(), error) {
	if err := c.ensureAppiumAvailable(ctx); err != nil {
		return appiumSessionState{}, func() {}, err
	}
	run, runErr := LoadRun(c.Root, "")
	transient := false
	if runErr != nil || run.ID == "" {
		var err error
		run, err = NewRunState()
		if err != nil {
			return appiumSessionState{}, func() {}, err
		}
		transient = true
	}
	session, err := c.ensureAppiumSession(ctx, cfg, run)
	cleanup := func() {}
	if transient {
		cleanup = func() {
			if session.PID > 0 {
				_ = stopProcess(session.PID)
			}
		}
	}
	if err != nil {
		cleanup()
		return session, func() {}, err
	}
	return session, cleanup, nil
}

func (c CLI) appiumSourceTree(ctx context.Context, cfg Config, includeSystem bool) (appiumSourceResult, error) {
	session, cleanup, err := c.appiumSessionForCommand(ctx, cfg)
	if err != nil {
		return appiumSourceResult{}, err
	}
	defer cleanup()
	activeBundle := ""
	if includeSystem {
		active, activeErr := appiumActiveAppInfo(ctx, session)
		if activeErr == nil {
			activeBundle = active.BundleID
		}
	}
	if activeBundle != "" && cfg.BundleID != "" && activeBundle != cfg.BundleID {
		raw, sourceErr := appiumSourceForActiveBundle(ctx, session, activeBundle)
		if sourceErr == nil && !isEmptyAXTree(raw) {
			return appiumSourceResult{Raw: raw, ActiveBundle: activeBundle, SystemSource: true}, nil
		}
	}
	raw, err := appiumDefaultSourceTree(ctx, session)
	if err != nil {
		return appiumSourceResult{}, err
	}
	return appiumSourceResult{Raw: raw, ActiveBundle: activeBundle}, nil
}

type appiumActiveApp struct {
	BundleID string
	Name     string
	PID      string
}

func appiumActiveAppInfo(ctx context.Context, session appiumSessionState) (appiumActiveApp, error) {
	var response map[string]any
	if err := appiumExecuteScript(ctx, session, "mobile: activeAppInfo", []any{}, &response); err != nil {
		return appiumActiveApp{}, err
	}
	value, _ := response["value"].(map[string]any)
	return appiumActiveApp{
		BundleID: stringField(value, "bundleId", "bundleID"),
		Name:     stringField(value, "name"),
		PID:      stringField(value, "pid"),
	}, nil
}

func appiumSourceForActiveBundle(ctx context.Context, session appiumSessionState, bundleID string) (string, error) {
	if bundleID == "" {
		return "", fmt.Errorf("active_bundle_missing")
	}
	oldSettings, err := appiumGetSettings(ctx, session)
	if err != nil {
		return "", err
	}
	restore := map[string]any{"defaultActiveApplication": "auto"}
	if oldValue, ok := oldSettings["defaultActiveApplication"]; ok {
		restore["defaultActiveApplication"] = oldValue
	}
	if err := appiumUpdateSettings(ctx, session, map[string]any{"defaultActiveApplication": bundleID}); err != nil {
		return "", err
	}
	defer func() {
		_ = appiumUpdateSettings(context.Background(), session, restore)
	}()
	return appiumDefaultSourceTree(ctx, session)
}

func appiumDefaultSourceTree(ctx context.Context, session appiumSessionState) (string, error) {
	var response map[string]any
	if err := appiumGetJSON(ctx, session.BaseURL+"/session/"+session.SessionID+"/source", &response); err != nil {
		return "", appiumError{Code: "ui_tree_failed", Message: err.Error()}
	}
	source, ok := response["value"].(string)
	if !ok {
		return "", appiumError{Code: "ui_tree_failed", Message: "appium source missing"}
	}
	raw, err := appiumSourceToAXJSON(source)
	if err != nil {
		return "", appiumError{Code: "ui_tree_failed", Message: err.Error()}
	}
	return raw, nil
}

func appiumGetSettings(ctx context.Context, session appiumSessionState) (map[string]any, error) {
	var response map[string]any
	if err := appiumGetJSON(ctx, session.BaseURL+"/session/"+session.SessionID+"/appium/settings", &response); err != nil {
		return nil, err
	}
	if value, ok := response["value"].(map[string]any); ok {
		return value, nil
	}
	return map[string]any{}, nil
}

func appiumUpdateSettings(ctx context.Context, session appiumSessionState, settings map[string]any) error {
	return appiumPostJSON(ctx, session.BaseURL+"/session/"+session.SessionID+"/appium/settings", map[string]any{"settings": settings}, nil)
}

func (c CLI) appiumClickByAccessibilityID(ctx context.Context, cfg Config, id string) error {
	return c.appiumFindElementAndClick(ctx, cfg, "accessibility id", id)
}

func (c CLI) appiumClickByPredicate(ctx context.Context, cfg Config, predicate string) error {
	err := c.appiumFindElementAndClick(ctx, cfg, "predicate string", predicate)
	if err == nil || !appiumPredicateUnsupported(err) {
		return err
	}
	return c.appiumFindElementAndClick(ctx, cfg, "-ios class chain", appiumPredicateClassChain(predicate))
}

func (c CLI) appiumFindElementAndClick(ctx context.Context, cfg Config, using, value string) error {
	session, cleanup, err := c.appiumSessionForCommand(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	var response map[string]any
	body := map[string]any{"using": using, "value": value}
	if err := appiumPostJSON(ctx, session.BaseURL+"/session/"+session.SessionID+"/element", body, &response); err != nil {
		return appiumError{Code: "ui_tap_failed", Message: err.Error()}
	}
	elementID := appiumElementID(response)
	if elementID == "" {
		return appiumError{Code: "ui_tap_failed", Message: "appium element id missing"}
	}
	clickURL := session.BaseURL + "/session/" + session.SessionID + "/element/" + url.PathEscape(elementID) + "/click"
	if err := appiumPostJSON(ctx, clickURL, map[string]any{}, nil); err != nil {
		return appiumError{Code: "ui_tap_failed", Message: err.Error()}
	}
	return nil
}

func appiumElementID(response map[string]any) string {
	value, _ := response["value"].(map[string]any)
	for _, key := range []string{"element-6066-11e4-a52e-4f735466cecf", "ELEMENT"} {
		if id, ok := value[key].(string); ok && id != "" {
			return id
		}
	}
	return ""
}

func appiumPredicateUnsupported(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "predicate string") &&
		(strings.Contains(message, "not supported") || strings.Contains(message, "invalid selector"))
}

func appiumPredicateClassChain(predicate string) string {
	return "**/XCUIElementTypeAny[`" + strings.ReplaceAll(predicate, "`", "\\`") + "`]"
}

func appiumGetJSON(ctx context.Context, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(firstLine(string(respData)))
	}
	if out != nil && len(respData) > 0 {
		return json.Unmarshal(respData, out)
	}
	return nil
}

func appiumExecuteScript(ctx context.Context, session appiumSessionState, script string, args []any, out any) error {
	return appiumPostJSON(ctx, session.BaseURL+"/session/"+session.SessionID+"/execute/sync", map[string]any{"script": script, "args": args}, out)
}

type appiumXMLNode struct {
	Data     map[string]any
	Children []*appiumXMLNode
}

func appiumSourceToAXJSON(source string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(source))
	var roots []*appiumXMLNode
	var stack []*appiumXMLNode
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("appium_source_xml_invalid")
		}
		switch value := token.(type) {
		case xml.StartElement:
			node := appiumNodeFromStart(value)
			if len(stack) == 0 {
				roots = append(roots, node)
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	out := make([]any, 0, len(roots))
	for _, root := range roots {
		out = append(out, root.toMap())
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func appiumNodeFromStart(start xml.StartElement) *appiumXMLNode {
	attrs := map[string]string{}
	for _, attr := range start.Attr {
		attrs[attr.Name.Local] = strings.TrimSpace(attr.Value)
	}
	data := map[string]any{
		"type": start.Name.Local,
	}
	put := func(key, value string) {
		if value != "" {
			data[key] = value
		}
	}
	put("identifier", attrs["name"])
	put("label", attrs["label"])
	put("value", attrs["value"])
	put("enabled", attrs["enabled"])
	put("subrole", attrs["subrole"])
	put("title", attrs["title"])
	put("pid", attrs["pid"])
	put("focused", attrs["focused"])
	put("frame", appiumFrame(attrs))
	return &appiumXMLNode{Data: data}
}

func (n *appiumXMLNode) toMap() map[string]any {
	out := map[string]any{}
	for key, value := range n.Data {
		out[key] = value
	}
	if len(n.Children) > 0 {
		children := make([]any, 0, len(n.Children))
		for _, child := range n.Children {
			children = append(children, child.toMap())
		}
		out["children"] = children
	}
	return out
}

func appiumFrame(attrs map[string]string) string {
	x, y, width, height := attrs["x"], attrs["y"], attrs["width"], attrs["height"]
	if x == "" && y == "" && width == "" && height == "" {
		return ""
	}
	return "{{" + x + ", " + y + "}, {" + width + ", " + height + "}}"
}

func (c CLI) ensureAppiumSession(ctx context.Context, cfg Config, run RunState) (appiumSessionState, error) {
	if existing, err := readAppiumSession(run); err == nil && existing.SessionID != "" && existing.UDID == cfg.SimulatorUDID && existing.BundleID == cfg.BundleID {
		return existing, nil
	}
	port, err := freeLocalPort()
	if err != nil {
		return appiumSessionState{}, err
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d%s", port, appiumBasePath)
	logPath := filepath.Join(run.Dir, "appium.log")
	args := []string{"--port", strconv.Itoa(port), "--base-path", appiumBasePath}
	pid, err := c.Runner.Start(ctx, logPath, "appium", args...)
	if err != nil {
		return appiumSessionState{}, appiumError{Code: "ui_gesture_failed", Message: firstLine(err.Error())}
	}
	appendProcess(run, "appium", pid, "appium "+strings.Join(args, " "))
	state := appiumSessionState{PID: pid, Port: port, BaseURL: baseURL, UDID: cfg.SimulatorUDID, BundleID: cfg.BundleID}
	if err := waitForAppiumStatus(ctx, baseURL, 10*time.Second); err != nil {
		return state, appiumError{Code: "appium_status_failed", Message: err.Error()}
	}
	sessionID, err := createAppiumSession(ctx, baseURL, cfg)
	if err != nil {
		return state, err
	}
	state.SessionID = sessionID
	_ = writeAppiumSession(run, state)
	return state, nil
}

func createAppiumSession(ctx context.Context, baseURL string, cfg Config) (string, error) {
	caps := map[string]any{
		"platformName":             "iOS",
		"appium:automationName":    "XCUITest",
		"appium:noReset":           true,
		"appium:newCommandTimeout": 120,
	}
	if cfg.SimulatorUDID != "" {
		caps["appium:udid"] = cfg.SimulatorUDID
	}
	if cfg.BundleID != "" {
		caps["appium:bundleId"] = cfg.BundleID
	}
	body := map[string]any{"capabilities": map[string]any{"alwaysMatch": caps}}
	var response map[string]any
	if err := appiumPostJSON(ctx, baseURL+"/session", body, &response); err != nil {
		return "", appiumError{Code: "session_create_failed", Message: err.Error()}
	}
	if value, ok := response["value"].(map[string]any); ok {
		if id, ok := value["sessionId"].(string); ok && id != "" {
			return id, nil
		}
	}
	if id, ok := response["sessionId"].(string); ok && id != "" {
		return id, nil
	}
	return "", appiumError{Code: "session_create_failed", Message: "appium session id missing"}
}

func waitForAppiumStatus(ctx context.Context, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/status", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("appium_status_timeout")
}

func appiumPostJSON(ctx context.Context, url string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(firstLine(string(respData)))
	}
	if out != nil && len(respData) > 0 {
		return json.Unmarshal(respData, out)
	}
	return nil
}

func appiumDelete(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

func readAppiumSession(run RunState) (appiumSessionState, error) {
	data, err := os.ReadFile(filepath.Join(run.Dir, "appium-session.json"))
	if err != nil {
		return appiumSessionState{}, err
	}
	var state appiumSessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return appiumSessionState{}, err
	}
	return state, nil
}

func writeAppiumSession(run RunState, state appiumSessionState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(run.Dir, "appium-session.json"), data, 0o644)
}

func freeLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func loadW3CActionsFile(root, path string) (any, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("actions_file_read_failed")
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("actions_json_invalid")
	}
	if object, ok := payload.(map[string]any); ok {
		if actions, ok := object["actions"]; ok {
			return actions, nil
		}
	}
	if actions, ok := payload.([]any); ok {
		return actions, nil
	}
	return nil, fmt.Errorf("actions_payload_invalid")
}

func parseRequiredFloat(value, name string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s_missing", name)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s_invalid", name)
	}
	return parsed, nil
}

func parseOptionalFloat(value string, fallback float64, name string) (float64, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s_invalid", name)
	}
	return parsed, nil
}

func degreesToRadians(value float64) float64 {
	return value * math.Pi / 180
}

func rounded(value float64) int {
	return int(math.Round(value))
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
