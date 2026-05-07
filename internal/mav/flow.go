package mav

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Flow struct {
	Version int        `yaml:"version"`
	Name    string     `yaml:"name"`
	Steps   []FlowStep `yaml:"steps"`
}

type FlowStep struct {
	Action string
	Params map[string]string
	Any    []FlowCondition
	Do     []FlowStep
	Env    map[string]string
}

type FlowCondition struct {
	Text        string `yaml:"text"`
	ID          string `yaml:"id"`
	Value       string `yaml:"value"`
	ChangedFrom string `yaml:"changedFrom"`
}

type flowStepPayload struct {
	Screen      string          `yaml:"screen"`
	Text        string          `yaml:"text"`
	ID          string          `yaml:"id"`
	Value       string          `yaml:"value"`
	X           string          `yaml:"x"`
	Y           string          `yaml:"y"`
	Cmd         string          `yaml:"cmd"`
	Out         string          `yaml:"out"`
	Name        string          `yaml:"name"`
	Note        string          `yaml:"note"`
	Timeout     string          `yaml:"timeout"`
	Duration    string          `yaml:"duration"`
	Hold        string          `yaml:"hold"`
	Direction   string          `yaml:"direction"`
	Contains    string          `yaml:"contains"`
	Key         string          `yaml:"key"`
	Level       string          `yaml:"level"`
	Device      string          `yaml:"device"`
	IOS         string          `yaml:"ios"`
	UDID        string          `yaml:"udid"`
	Locale      string          `yaml:"locale"`
	Language    string          `yaml:"language"`
	ChangedFrom string          `yaml:"changedFrom"`
	MaxSwipes   string          `yaml:"maxSwipes"`
	Scale       string          `yaml:"scale"`
	PanX        string          `yaml:"panX"`
	PanY        string          `yaml:"panY"`
	Distance    string          `yaml:"distance"`
	Angle       string          `yaml:"angle"`
	Rotate      string          `yaml:"rotate"`
	Degrees     string          `yaml:"degrees"`
	File        string          `yaml:"file"`
	Any         []FlowCondition `yaml:"any"`
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
		Version int         `yaml:"version"`
		Name    string      `yaml:"name"`
		Steps   []yaml.Node `yaml:"steps"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Flow{}, err
	}
	flow := Flow{Version: raw.Version, Name: raw.Name}
	if flow.Version == 0 {
		flow.Version = 1
	}
	if flow.Version != 1 {
		return Flow{}, fmt.Errorf("unsupported_flow_version version=%d", flow.Version)
	}
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
	if err := node.Content[1].Decode(&payload); err != nil {
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
	return FlowStep{Action: action, Params: params, Any: payload.Any}, nil
}

type flowWhenPayload struct {
	Visible FlowCondition   `yaml:"visible"`
	Text    string          `yaml:"text"`
	ID      string          `yaml:"id"`
	Value   string          `yaml:"value"`
	Any     []FlowCondition `yaml:"any"`
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
	for _, step := range steps {
		if !isSupportedFlowAction(step.Action) {
			return fmt.Errorf("unknown_step action=%s", step.Action)
		}
		if len(step.Do) > 0 {
			if err := validateFlowSteps(step.Do); err != nil {
				return err
			}
		}
	}
	return nil
}

func isSupportedFlowAction(action string) bool {
	switch action {
	case "open", "when", "go", "tree", "tap", "type", "swipe", "pinch", "rotate", "twoFingerPan", "actions",
		"delay", "sleep", "wait", "assert", "waitUntil", "scrollUntil", "capture",
		"evidence.start", "video.start", "evidence.step", "evidence.stop", "video.stop",
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
	for i := range step.Any {
		condition, err := expandFlowConditionEnv(step.Any[i], env)
		if err != nil {
			return FlowStep{}, err
		}
		step.Any[i] = condition
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
	if condition.ID, err = expandEnvString(condition.ID, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.Value, err = expandEnvString(condition.Value, env); err != nil {
		return FlowCondition{}, err
	}
	if condition.ChangedFrom, err = expandEnvString(condition.ChangedFrom, env); err != nil {
		return FlowCondition{}, err
	}
	return condition, nil
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
