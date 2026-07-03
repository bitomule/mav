package mav

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Flow struct {
	Name   string               `yaml:"name"`
	Params map[string]FlowParam `yaml:"params,omitempty"`
	Steps  []FlowStep           `yaml:"steps"`
}

type FlowStep struct {
	Action    string
	Params    map[string]string
	Where     Selector
	After     *FlowAfter
	OnFailure FailurePolicy
	Any       []FlowCondition
	All       []FlowCondition
	Not       *FlowCondition
	Points    []FlowPathPoint
	Do        []FlowStep
	Env       map[string]string
}

type FlowCondition struct {
	ID             string          `yaml:"id,omitempty"`
	Text           string          `yaml:"text,omitempty"`
	TextContains   string          `yaml:"textContains,omitempty"`
	TextStartsWith string          `yaml:"textStartsWith,omitempty"`
	TextRegex      string          `yaml:"textRegex,omitempty"`
	Value          string          `yaml:"value,omitempty"`
	ValueContains  string          `yaml:"valueContains,omitempty"`
	Role           string          `yaml:"role,omitempty"`
	Enabled        *bool           `yaml:"enabled,omitempty"`
	Selected       *bool           `yaml:"selected,omitempty"`
	Focused        *bool           `yaml:"focused,omitempty"`
	Visible        *bool           `yaml:"visible,omitempty"`
	Index          *int            `yaml:"index,omitempty"`
	Bounds         string          `yaml:"bounds,omitempty"`
	Near           *NearSelector   `yaml:"near,omitempty"`
	ParentOf       *Selector       `yaml:"parentOf,omitempty"`
	ChangedFrom    string          `yaml:"changedFrom,omitempty"`
	Stable         bool            `yaml:"stable,omitempty"`
	Any            []FlowCondition `yaml:"any,omitempty"`
	All            []FlowCondition `yaml:"all,omitempty"`
	Not            *FlowCondition  `yaml:"not,omitempty"`
}

type FlowParam struct {
	Required bool   `yaml:"required,omitempty"`
	Default  string `yaml:"default,omitempty"`
}

type FailurePolicy struct {
	Strategy    string   `yaml:"strategy,omitempty"`
	MaxAttempts int      `yaml:"maxAttempts,omitempty"`
	Delay       string   `yaml:"delay,omitempty"`
	Backoff     float64  `yaml:"backoff,omitempty"`
	RetryOn     []string `yaml:"retryOn,omitempty"`
}

type FlowAfter struct {
	Wait    *FlowWait `yaml:"wait,omitempty"`
	Observe string    `yaml:"observe,omitempty"`
}

type FlowWait struct {
	ID           string          `yaml:"id,omitempty"`
	Text         string          `yaml:"text,omitempty"`
	TextContains string          `yaml:"textContains,omitempty"`
	Value        string          `yaml:"value,omitempty"`
	ChangedFrom  string          `yaml:"changedFrom,omitempty"`
	Stable       bool            `yaml:"stable,omitempty"`
	Any          []FlowCondition `yaml:"any,omitempty"`
	All          []FlowCondition `yaml:"all,omitempty"`
	Not          *FlowCondition  `yaml:"not,omitempty"`
	Timeout      string          `yaml:"timeout,omitempty"`
}

type FlowPathPoint struct {
	X          int    `yaml:"x"`
	Y          int    `yaml:"y"`
	Duration   string `yaml:"duration,omitempty"`
	DurationMs int    `yaml:"durationMs,omitempty"`
}

type FlowCoordinate struct {
	X string `yaml:"x"`
	Y string `yaml:"y"`
}

type flowStepPayload struct {
	Screen         string          `yaml:"screen"`
	Text           string          `yaml:"text"`
	ID             string          `yaml:"id"`
	Value          string          `yaml:"value"`
	X              string          `yaml:"x"`
	Y              string          `yaml:"y"`
	Cmd            string          `yaml:"cmd"`
	Out            string          `yaml:"out"`
	Name           string          `yaml:"name"`
	Note           string          `yaml:"note"`
	Timeout        string          `yaml:"timeout"`
	Duration       string          `yaml:"duration"`
	Hold           string          `yaml:"hold"`
	Direction      string          `yaml:"direction"`
	Contains       string          `yaml:"contains"`
	Key            string          `yaml:"key"`
	Level          string          `yaml:"level"`
	Device         string          `yaml:"device"`
	IOS            string          `yaml:"ios"`
	UDID           string          `yaml:"udid"`
	Locale         string          `yaml:"locale"`
	Language       string          `yaml:"language"`
	ChangedFrom    string          `yaml:"changedFrom"`
	MaxSwipes      string          `yaml:"maxSwipes"`
	Scale          string          `yaml:"scale"`
	PanX           string          `yaml:"panX"`
	PanY           string          `yaml:"panY"`
	Distance       string          `yaml:"distance"`
	Angle          string          `yaml:"angle"`
	Rotate         string          `yaml:"rotate"`
	Degrees        string          `yaml:"degrees"`
	File           string          `yaml:"file"`
	HAR            string          `yaml:"har"`
	Port           string          `yaml:"port"`
	PreferDriver   string          `yaml:"prefer-driver"`
	ClearState     bool            `yaml:"clearState"`
	ClearStateDash bool            `yaml:"clear-state"`
	Network        bool            `yaml:"network"`
	Focused        string          `yaml:"focused"`
	Optional       bool            `yaml:"optional"`
	Any            []FlowCondition `yaml:"any"`
	All            []FlowCondition `yaml:"all"`
	Not            *FlowCondition  `yaml:"not"`
	Where          Selector        `yaml:"where"`
	After          *FlowAfter      `yaml:"after"`
	OnFailure      FailurePolicy   `yaml:"onFailure"`
	Expected       string          `yaml:"expected"`
	Field          string          `yaml:"field"`
	Regex          string          `yaml:"regex"`
	At             string          `yaml:"at"`
	By             string          `yaml:"by"`
	Factor         string          `yaml:"factor"`
	Preserve       bool            `yaml:"preserve"`
	TimeControl    bool            `yaml:"timeControl"`
	URL            string          `yaml:"url"`
	Latitude       string          `yaml:"latitude"`
	Longitude      string          `yaml:"longitude"`
	Button         string          `yaml:"button"`
	State          string          `yaml:"state"`
	StartX         string          `yaml:"startX"`
	StartY         string          `yaml:"startY"`
	EndX           string          `yaml:"endX"`
	EndY           string          `yaml:"endY"`
	ToX            string          `yaml:"toX"`
	ToY            string          `yaml:"toY"`
	Interval       string          `yaml:"interval"`
	Count          string          `yaml:"count"`
	Max            string          `yaml:"max"`
	Agent          bool            `yaml:"agent"`
	WithFrame      bool            `yaml:"withFrame"`
	Since          string          `yaml:"since"`
	Bundle         string          `yaml:"bundle"`
	Breakpoint     string          `yaml:"breakpoint"`
	Expression     string          `yaml:"expression"`
	DirectionDebug string          `yaml:"debugDirection"`
	Kind           string          `yaml:"kind"`
	Kill           bool            `yaml:"kill"`
	Points         []FlowPathPoint `yaml:"points"`
	From           *FlowCoordinate `yaml:"from"`
	To             *FlowCoordinate `yaml:"to"`
}

func LoadFlow(path string) (Flow, error) {
	return loadFlow(path, nil, nil, false)
}

func loadFlow(path string, env map[string]string, stack []string, nestedInWhen bool) (Flow, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Flow{}, err
	}
	for _, active := range stack {
		if active == absPath {
			return Flow{}, fmt.Errorf("include_cycle file=%s", path)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Flow{}, err
	}
	flow, err := ParseFlow(data)
	if err != nil {
		return Flow{}, err
	}
	steps, err := expandFlowSteps(flow.Steps, filepath.Dir(absPath), env, append(stack, absPath), nestedInWhen)
	if err != nil {
		return Flow{}, err
	}
	if err := validateFlowSteps(steps); err != nil {
		return Flow{}, err
	}
	flow.Steps = steps
	return flow, nil
}

func ParseFlow(data []byte) (Flow, error) {
	var raw struct {
		Version any                  `yaml:"version,omitempty"`
		Name    string               `yaml:"name"`
		Params  map[string]FlowParam `yaml:"params,omitempty"`
		Steps   []yaml.Node          `yaml:"steps"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return Flow{}, err
	}
	flow := Flow{Name: raw.Name, Params: raw.Params}
	if len(raw.Steps) == 0 {
		return Flow{}, fmt.Errorf("flow_steps_missing")
	}
	for _, node := range raw.Steps {
		step, err := parseFlowStepNode(node)
		if err != nil {
			return Flow{}, err
		}
		flow.Steps = append(flow.Steps, step)
	}
	return flow, nil
}

func parseFlowStepNode(node yaml.Node) (FlowStep, error) {
	if node.Kind != yaml.MappingNode {
		return FlowStep{}, fmt.Errorf("step_action_missing")
	}
	if whenNode, ok := mappingValue(node, "when"); ok {
		return parseWhenFlowStepNode(node, whenNode)
	}
	if loopNode, ok := mappingValue(node, "whileNotVisible"); ok {
		return parseLoopFlowStepNode(node, loopNode)
	}
	if len(node.Content) != 2 {
		return FlowStep{}, fmt.Errorf("step_action_missing")
	}
	action := node.Content[0].Value
	if action == "include" {
		return parseIncludeFlowStepNode(node.Content[1])
	}
	if node.Content[1].Kind == yaml.ScalarNode {
		if step, ok := parseScalarFlowStep(action, node.Content[1].Value); ok {
			return step, nil
		}
	}
	var payload flowStepPayload
	if err := decodeKnownNode(node.Content[1], &payload); err != nil {
		return FlowStep{}, err
	}
	params := map[string]string{}
	put := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			params[key] = value
		}
	}
	put("screen", payload.Screen)
	put("text", payload.Text)
	put("id", payload.ID)
	put("value", payload.Value)
	put("x", payload.X)
	put("y", payload.Y)
	put("cmd", payload.Cmd)
	put("out", payload.Out)
	put("name", payload.Name)
	put("note", payload.Note)
	put("timeout", payload.Timeout)
	put("duration", payload.Duration)
	put("hold", payload.Hold)
	put("direction", payload.Direction)
	put("contains", payload.Contains)
	put("key", payload.Key)
	put("level", payload.Level)
	put("device", payload.Device)
	put("ios", payload.IOS)
	put("udid", payload.UDID)
	put("locale", payload.Locale)
	put("language", payload.Language)
	put("changedFrom", payload.ChangedFrom)
	put("maxSwipes", payload.MaxSwipes)
	put("scale", payload.Scale)
	put("panX", payload.PanX)
	put("panY", payload.PanY)
	put("distance", payload.Distance)
	put("angle", payload.Angle)
	put("rotate", payload.Rotate)
	put("degrees", payload.Degrees)
	put("file", payload.File)
	put("har", payload.HAR)
	put("port", payload.Port)
	put("prefer-driver", payload.PreferDriver)
	put("expected", payload.Expected)
	put("field", payload.Field)
	put("regex", payload.Regex)
	put("at", payload.At)
	put("by", payload.By)
	put("factor", payload.Factor)
	put("url", payload.URL)
	put("latitude", payload.Latitude)
	put("longitude", payload.Longitude)
	put("button", payload.Button)
	put("state", payload.State)
	put("startX", payload.StartX)
	put("startY", payload.StartY)
	put("endX", payload.EndX)
	put("endY", payload.EndY)
	put("toX", payload.ToX)
	put("toY", payload.ToY)
	put("interval", payload.Interval)
	put("count", payload.Count)
	put("max", payload.Max)
	put("since", payload.Since)
	put("bundle", payload.Bundle)
	put("breakpoint", payload.Breakpoint)
	put("expression", payload.Expression)
	put("debugDirection", payload.DirectionDebug)
	put("kind", payload.Kind)
	if payload.From != nil {
		put("startX", payload.From.X)
		put("startY", payload.From.Y)
	}
	if payload.To != nil {
		put("endX", payload.To.X)
		put("endY", payload.To.Y)
	}
	if payload.ClearState || payload.ClearStateDash {
		params["clearState"] = "true"
	}
	if payload.Optional {
		params["optional"] = "true"
	}
	if payload.Network {
		params["network"] = "true"
	}
	if payload.Preserve {
		params["preserve"] = "true"
	}
	if payload.TimeControl {
		params["timeControl"] = "true"
	}
	if payload.Agent {
		params["agent"] = "true"
	}
	if payload.WithFrame {
		params["withFrame"] = "true"
	}
	if payload.Kill {
		params["kill"] = "true"
	}
	put("focused", payload.Focused)
	where := payload.Where
	if where.IsZero() {
		if action == "type" {
			// For type steps, "text" is the content to type, not a tap
			// target selector, so it must not feed the legacy fallback.
			where = selectorFromLegacy(map[string]string{"id": params["id"], "value": params["value"]})
		} else {
			where = selectorFromLegacy(params)
		}
	}
	if !where.IsZero() {
		if err := where.Validate(); err != nil {
			return FlowStep{}, err
		}
	}
	if err := validateFailurePolicy(payload.OnFailure); err != nil {
		return FlowStep{}, err
	}
	return FlowStep{
		Action: action, Params: params, Where: where, After: payload.After,
		OnFailure: payload.OnFailure, Any: payload.Any, All: payload.All, Not: payload.Not,
		Points: payload.Points,
	}, nil
}

func decodeKnownNode(node *yaml.Node, out any) error {
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	return decoder.Decode(out)
}

func validateFailurePolicy(policy FailurePolicy) error {
	switch policy.Strategy {
	case "", "abort", "skip", "retry":
	default:
		return fmt.Errorf("on_failure_strategy_invalid")
	}
	if policy.MaxAttempts < 0 {
		return fmt.Errorf("on_failure_attempts_invalid")
	}
	if policy.Backoff < 0 {
		return fmt.Errorf("on_failure_backoff_invalid")
	}
	return nil
}

func mappingBoolValue(node *yaml.Node, key string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	value, ok := mappingValue(*node, key)
	if !ok {
		return false
	}
	switch value.Kind {
	case yaml.ScalarNode:
		parsed, err := strconv.ParseBool(value.Value)
		return err == nil && parsed
	default:
		return false
	}
}

type flowWhenPayload struct {
	Visible      FlowCondition   `yaml:"visible"`
	Text         string          `yaml:"text"`
	ID           string          `yaml:"id"`
	Value        string          `yaml:"value"`
	Timeout      string          `yaml:"timeout"`
	PreferDriver string          `yaml:"prefer-driver"`
	Any          []FlowCondition `yaml:"any"`
}

func parseWhenFlowStepNode(node yaml.Node, whenNode *yaml.Node) (FlowStep, error) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if key != "when" && key != "do" {
			return FlowStep{}, fmt.Errorf("when_field_unknown field=%s", key)
		}
	}
	doNode, ok := mappingValue(node, "do")
	if !ok {
		return FlowStep{}, fmt.Errorf("when_do_missing")
	}
	var payload flowWhenPayload
	if err := whenNode.Decode(&payload); err != nil {
		return FlowStep{}, err
	}
	params := map[string]string{}
	condition := payload.Visible
	if condition.Text == "" && condition.ID == "" && condition.Value == "" {
		condition = FlowCondition{Text: payload.Text, ID: payload.ID, Value: payload.Value}
	}
	putParam(params, "text", condition.Text)
	putParam(params, "id", condition.ID)
	putParam(params, "value", condition.Value)
	if len(params) == 0 && len(payload.Any) == 0 {
		return FlowStep{}, fmt.Errorf("when_condition_missing")
	}
	putParam(params, "prefer-driver", payload.PreferDriver)
	if doNode.Kind != yaml.SequenceNode || len(doNode.Content) == 0 {
		return FlowStep{}, fmt.Errorf("when_do_missing")
	}
	steps := make([]FlowStep, 0, len(doNode.Content))
	for _, child := range doNode.Content {
		step, err := parseFlowStepNode(*child)
		if err != nil {
			return FlowStep{}, err
		}
		if step.Action == "open" || step.Action == "exec" {
			return FlowStep{}, fmt.Errorf("when_child_unsupported action=%s", step.Action)
		}
		steps = append(steps, step)
	}
	return FlowStep{Action: "when", Params: params, Any: payload.Any, Do: steps}, nil
}

func parseLoopFlowStepNode(node yaml.Node, loopNode *yaml.Node) (FlowStep, error) {
	var doNode *yaml.Node
	if nestedDo, ok := mappingValue(*loopNode, "do"); ok {
		doNode = nestedDo
	} else if topLevelDo, ok := mappingValue(node, "do"); ok {
		doNode = topLevelDo
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if key != "whileNotVisible" && key != "do" {
			return FlowStep{}, fmt.Errorf("while_field_unknown field=%s", key)
		}
	}
	if doNode == nil {
		return FlowStep{}, fmt.Errorf("while_do_missing")
	}
	var payload flowWhenPayload
	if err := loopNode.Decode(&payload); err != nil {
		return FlowStep{}, err
	}
	params := map[string]string{}
	condition := payload.Visible
	if condition.Text == "" && condition.ID == "" && condition.Value == "" {
		condition = FlowCondition{Text: payload.Text, ID: payload.ID, Value: payload.Value}
	}
	putParam(params, "text", condition.Text)
	putParam(params, "id", condition.ID)
	putParam(params, "value", condition.Value)
	if len(params) == 0 && len(payload.Any) == 0 {
		return FlowStep{}, fmt.Errorf("while_condition_missing")
	}
	putParam(params, "timeout", payload.Timeout)
	putParam(params, "prefer-driver", payload.PreferDriver)
	if doNode.Kind != yaml.SequenceNode || len(doNode.Content) == 0 {
		return FlowStep{}, fmt.Errorf("while_do_missing")
	}
	steps := make([]FlowStep, 0, len(doNode.Content))
	for _, child := range doNode.Content {
		step, err := parseFlowStepNode(*child)
		if err != nil {
			return FlowStep{}, err
		}
		if step.Action == "open" || step.Action == "exec" {
			return FlowStep{}, fmt.Errorf("while_child_unsupported action=%s", step.Action)
		}
		steps = append(steps, step)
	}
	return FlowStep{Action: "whileNotVisible", Params: params, Any: payload.Any, Do: steps}, nil
}

type flowIncludePayload struct {
	File string         `yaml:"file"`
	Env  map[string]any `yaml:"env"`
}

func parseIncludeFlowStepNode(node *yaml.Node) (FlowStep, error) {
	var payload flowIncludePayload
	if err := node.Decode(&payload); err != nil {
		return FlowStep{}, err
	}
	if strings.TrimSpace(payload.File) == "" {
		return FlowStep{}, fmt.Errorf("include_file_missing")
	}
	env := map[string]string{}
	for key, value := range payload.Env {
		env[key] = fmt.Sprint(value)
	}
	return FlowStep{Action: "include", Params: map[string]string{"file": payload.File}, Env: env}, nil
}

func mappingValue(node yaml.Node, key string) (*yaml.Node, bool) {
	if node.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], true
		}
	}
	return nil, false
}

func putParam(params map[string]string, key, value string) {
	if strings.TrimSpace(value) != "" {
		params[key] = value
	}
}

func parseScalarFlowStep(action, value string) (FlowStep, bool) {
	if strings.TrimSpace(value) == "" {
		return FlowStep{}, false
	}
	switch action {
	case "type":
		return FlowStep{Action: action, Params: map[string]string{"text": value}}, true
	case "delay", "sleep":
		return FlowStep{Action: action, Params: map[string]string{"duration": strings.TrimSpace(value)}}, true
	default:
		return FlowStep{}, false
	}
}

func expandFlowSteps(steps []FlowStep, baseDir string, env map[string]string, stack []string, nestedInWhen bool) ([]FlowStep, error) {
	var expanded []FlowStep
	for _, step := range steps {
		if step.Action == "include" {
			includeEnv, err := mergeIncludeEnv(env, step.Env)
			if err != nil {
				return nil, err
			}
			file, err := expandEnvString(step.Params["file"], includeEnv)
			if err != nil {
				return nil, err
			}
			if !filepath.IsAbs(file) {
				file = filepath.Join(baseDir, file)
			}
			included, err := loadFlow(file, includeEnv, stack, nestedInWhen)
			if err != nil {
				return nil, err
			}
			expanded = append(expanded, included.Steps...)
			continue
		}
		updated, err := expandFlowStepEnv(step, baseDir, env, stack, nestedInWhen)
		if err != nil {
			return nil, err
		}
		expanded = append(expanded, updated)
	}
	return expanded, nil
}

func validateFlowSteps(steps []FlowStep) error {
	for index, step := range steps {
		if !isSupportedFlowAction(step.Action) {
			return fmt.Errorf("steps[%d]: unknown_step action=%s", index, step.Action)
		}
		if !step.Where.IsZero() {
			if err := step.Where.Validate(); err != nil {
				return fmt.Errorf("steps[%d].%s.where: %w", index, step.Action, err)
			}
		}
		switch step.OnFailure.Strategy {
		case "", "abort", "skip", "retry":
		default:
			return fmt.Errorf("steps[%d].%s.onFailure.strategy: expected abort|skip|retry", index, step.Action)
		}
		if step.OnFailure.MaxAttempts < 0 || step.OnFailure.Backoff < 0 {
			return fmt.Errorf("steps[%d].%s.onFailure: retry values must be non-negative", index, step.Action)
		}
		if step.Params["regex"] != "" {
			if _, err := regexp.Compile(step.Params["regex"]); err != nil {
				return fmt.Errorf("steps[%d].%s.regex: %w", index, step.Action, err)
			}
		}
		for conditionIndex, condition := range append(append([]FlowCondition{}, step.Any...), step.All...) {
			if err := validateFlowCondition(condition); err != nil {
				return fmt.Errorf("steps[%d].%s.conditions[%d]: %w", index, step.Action, conditionIndex, err)
			}
		}
		if step.Not != nil {
			if err := validateFlowCondition(*step.Not); err != nil {
				return fmt.Errorf("steps[%d].%s.not: %w", index, step.Action, err)
			}
		}
		if step.After != nil && step.After.Wait != nil {
			for conditionIndex, condition := range append(append([]FlowCondition{}, step.After.Wait.Any...), step.After.Wait.All...) {
				if err := validateFlowCondition(condition); err != nil {
					return fmt.Errorf("steps[%d].%s.after.wait.conditions[%d]: %w", index, step.Action, conditionIndex, err)
				}
			}
		}
		if len(step.Do) > 0 {
			if err := validateFlowSteps(step.Do); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateFlowCondition(condition FlowCondition) error {
	selector := condition.Selector()
	if !selector.IsZero() {
		if err := selector.Validate(); err != nil {
			return err
		}
	}
	for _, nested := range append(append([]FlowCondition{}, condition.Any...), condition.All...) {
		if err := validateFlowCondition(nested); err != nil {
			return err
		}
	}
	if condition.Not != nil {
		return validateFlowCondition(*condition.Not)
	}
	return nil
}

func isSupportedFlowAction(action string) bool {
	switch action {
	case "open", "when", "whileNotVisible", "go", "tree", "tap", "type", "erase", "hideKeyboard", "swipe", "longPress", "pinch", "rotate", "twoFingerPan", "actions",
		"doubleTap", "drag", "dragPath", "toggle", "press",
		"app.list", "app.kill", "openURL", "location.set", "location.reset", "clipboard.copy", "clipboard.read",
		"time.freeze", "time.travel", "time.scale", "time.status", "time.reset",
		"debug.attach", "debug.wait", "debug.break", "debug.eval", "debug.step", "debug.detach",
		"delay", "sleep", "wait", "assert", "assertCount", "waitUntil", "scrollUntil", "capture", "extract",
		"evidence.start", "video.start", "evidence.step", "evidence.stop", "video.stop",
		"network.start", "network.stop", "network.status",
		"logs", "exec", "crashes", "report":
		return true
	default:
		return false
	}
}

func expandFlowStepEnv(step FlowStep, baseDir string, env map[string]string, stack []string, nestedInWhen bool) (FlowStep, error) {
	if nestedInWhen && (step.Action == "open" || step.Action == "exec") {
		return FlowStep{}, fmt.Errorf("when_child_unsupported action=%s", step.Action)
	}
	for key, value := range step.Params {
		expanded, err := expandEnvString(value, env)
		if err != nil {
			return FlowStep{}, err
		}
		step.Params[key] = expanded
	}
	where, err := expandSelectorEnv(step.Where, env)
	if err != nil {
		return FlowStep{}, err
	}
	step.Where = where
	for i := range step.Any {
		condition, err := expandFlowConditionEnv(step.Any[i], env)
		if err != nil {
			return FlowStep{}, err
		}
		step.Any[i] = condition
	}
	for i := range step.All {
		condition, err := expandFlowConditionEnv(step.All[i], env)
		if err != nil {
			return FlowStep{}, err
		}
		step.All[i] = condition
	}
	if step.Not != nil {
		condition, err := expandFlowConditionEnv(*step.Not, env)
		if err != nil {
			return FlowStep{}, err
		}
		step.Not = &condition
	}
	if step.After != nil && step.After.Wait != nil {
		wait, err := expandFlowWaitEnv(*step.After.Wait, env)
		if err != nil {
			return FlowStep{}, err
		}
		after := *step.After
		after.Wait = &wait
		step.After = &after
	}
	if len(step.Do) > 0 {
		do, err := expandFlowSteps(step.Do, baseDir, env, stack, true)
		if err != nil {
			return FlowStep{}, err
		}
		step.Do = do
	}
	return step, nil
}

func expandFlowConditionEnv(condition FlowCondition, env map[string]string) (FlowCondition, error) {
	var err error
	if condition.Text, err = expandEnvString(condition.Text, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.TextContains, err = expandEnvString(condition.TextContains, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.TextStartsWith, err = expandEnvString(condition.TextStartsWith, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.TextRegex, err = expandEnvString(condition.TextRegex, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.ID, err = expandEnvString(condition.ID, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.Value, err = expandEnvString(condition.Value, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.ValueContains, err = expandEnvString(condition.ValueContains, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.Bounds, err = expandEnvString(condition.Bounds, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.ChangedFrom, err = expandEnvString(condition.ChangedFrom, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.Near != nil {
		near := *condition.Near
		where, err := expandSelectorEnv(near.Where, env)
		if err != nil {
			return FlowCondition{}, err
		}
		near.Where = where
		condition.Near = &near
	}
	if condition.ParentOf != nil {
		parent, err := expandSelectorEnv(*condition.ParentOf, env)
		if err != nil {
			return FlowCondition{}, err
		}
		condition.ParentOf = &parent
	}
	for i := range condition.Any {
		child, err := expandFlowConditionEnv(condition.Any[i], env)
		if err != nil {
			return FlowCondition{}, err
		}
		condition.Any[i] = child
	}
	for i := range condition.All {
		child, err := expandFlowConditionEnv(condition.All[i], env)
		if err != nil {
			return FlowCondition{}, err
		}
		condition.All[i] = child
	}
	if condition.Not != nil {
		child, err := expandFlowConditionEnv(*condition.Not, env)
		if err != nil {
			return FlowCondition{}, err
		}
		condition.Not = &child
	}
	return condition, nil
}

func expandSelectorEnv(selector Selector, env map[string]string) (Selector, error) {
	var err error
	if selector.ID, err = expandEnvString(selector.ID, env); err != nil {
		return Selector{}, err
	}
	if selector.Text, err = expandEnvString(selector.Text, env); err != nil {
		return Selector{}, err
	}
	if selector.TextContains, err = expandEnvString(selector.TextContains, env); err != nil {
		return Selector{}, err
	}
	if selector.TextStartsWith, err = expandEnvString(selector.TextStartsWith, env); err != nil {
		return Selector{}, err
	}
	if selector.TextRegex, err = expandEnvString(selector.TextRegex, env); err != nil {
		return Selector{}, err
	}
	if selector.Value, err = expandEnvString(selector.Value, env); err != nil {
		return Selector{}, err
	}
	if selector.ValueContains, err = expandEnvString(selector.ValueContains, env); err != nil {
		return Selector{}, err
	}
	if selector.Bounds, err = expandEnvString(selector.Bounds, env); err != nil {
		return Selector{}, err
	}
	if selector.Near != nil {
		near := *selector.Near
		where, err := expandSelectorEnv(near.Where, env)
		if err != nil {
			return Selector{}, err
		}
		near.Where = where
		selector.Near = &near
	}
	if selector.ParentOf != nil {
		parent, err := expandSelectorEnv(*selector.ParentOf, env)
		if err != nil {
			return Selector{}, err
		}
		selector.ParentOf = &parent
	}
	return selector, nil
}

func expandFlowWaitEnv(wait FlowWait, env map[string]string) (FlowWait, error) {
	var err error
	if wait.ID, err = expandEnvString(wait.ID, env); err != nil {
		return FlowWait{}, err
	}
	if wait.Text, err = expandEnvString(wait.Text, env); err != nil {
		return FlowWait{}, err
	}
	if wait.TextContains, err = expandEnvString(wait.TextContains, env); err != nil {
		return FlowWait{}, err
	}
	if wait.Value, err = expandEnvString(wait.Value, env); err != nil {
		return FlowWait{}, err
	}
	if wait.ChangedFrom, err = expandEnvString(wait.ChangedFrom, env); err != nil {
		return FlowWait{}, err
	}
	for i := range wait.Any {
		condition, err := expandFlowConditionEnv(wait.Any[i], env)
		if err != nil {
			return FlowWait{}, err
		}
		wait.Any[i] = condition
	}
	for i := range wait.All {
		condition, err := expandFlowConditionEnv(wait.All[i], env)
		if err != nil {
			return FlowWait{}, err
		}
		wait.All[i] = condition
	}
	if wait.Not != nil {
		condition, err := expandFlowConditionEnv(*wait.Not, env)
		if err != nil {
			return FlowWait{}, err
		}
		wait.Not = &condition
	}
	return wait, nil
}

func mergeIncludeEnv(parent map[string]string, include map[string]string) (map[string]string, error) {
	merged := map[string]string{}
	for key, value := range parent {
		merged[key] = value
	}
	for key, value := range include {
		expanded, err := expandEnvString(value, parent)
		if err != nil {
			return nil, err
		}
		merged[key] = expanded
	}
	return merged, nil
}

func expandEnvString(value string, env map[string]string) (string, error) {
	out := value
	for {
		start := strings.Index(out, "${env.")
		if start < 0 {
			return out, nil
		}
		end := strings.Index(out[start:], "}")
		if end < 0 {
			return "", fmt.Errorf("env_binding_invalid")
		}
		end += start
		name := out[start+len("${env.") : end]
		replacement, ok := env[name]
		if !ok {
			return "", fmt.Errorf("env_missing name=%s", name)
		}
		out = out[:start] + replacement + out[end+1:]
	}
}

func parseFlowDuration(value string, fallback time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if d, err := time.ParseDuration(value); err == nil {
		return d
	}
	if ms, err := strconv.Atoi(value); err == nil {
		return time.Duration(ms) * time.Millisecond
	}
	return fallback
}

func flowBoolParam(params map[string]string, key string) bool {
	value, ok := params[key]
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}
