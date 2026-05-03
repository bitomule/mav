package mav

import (
	"fmt"
	"os"
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
	Name        string          `yaml:"name"`
	Note        string          `yaml:"note"`
	Timeout     string          `yaml:"timeout"`
	Duration    string          `yaml:"duration"`
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
	data, err := os.ReadFile(path)
	if err != nil {
		return Flow{}, err
	}
	return ParseFlow(data)
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
	if node.Kind != yaml.MappingNode || len(node.Content) != 2 {
		return FlowStep{}, fmt.Errorf("step_action_missing")
	}
	action := node.Content[0].Value
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
	put("name", payload.Name)
	put("note", payload.Note)
	put("timeout", payload.Timeout)
	put("duration", payload.Duration)
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
