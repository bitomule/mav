package mav

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoalFlowLoadRejectsDeclaredInputQuotedLiteralCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow.yaml")
	writeTestFlow(t, path, `
name: declared_input_collision
inputs:
  literal_1: Other
steps:
  - goal:
      goal: 'type "Sample" into Note'
      succeededWhen: 'value:"note=Sample"'
`)
	_, err := LoadFlow(path)
	if err == nil || !strings.Contains(err.Error(), "literal_1 collides with a quoted literal") {
		t.Fatalf("LoadFlow accepted an input that replaces quoted objective text: %v", err)
	}
}

type goalFlowInputProbeRunner struct {
	Runner
	lookups, commands int
}

func (r *goalFlowInputProbeRunner) LookPath(string) (string, error) {
	r.lookups++
	return "", os.ErrNotExist
}

func (r *goalFlowInputProbeRunner) Run(context.Context, string, ...string) CommandResult {
	r.commands++
	return CommandResult{Err: os.ErrNotExist}
}

func TestGoalFlowExecutionRejectsInputCollisionBeforeResolvingConfigOrTools(t *testing.T) {
	t.Setenv("CI", "1")
	runner := &goalFlowInputProbeRunner{}
	c := CLI{Root: t.TempDir(), Runner: runner}
	step := FlowStep{Action: "goal", Params: map[string]string{
		"goal": `type "Sample" into Note`, "succeededWhen": `value:"note=Sample"`,
	}, Inputs: map[string]string{"literal_1": "Other"}}
	_, err := c.executeGoalFlowStep(t.Context(), GlobalOptions{}, step)
	if err == nil || !strings.Contains(err.Error(), "literal_1 collides with a quoted literal") {
		t.Fatalf("execution did not reject input collision first: err=%v lookups=%d commands=%d", err, runner.lookups, runner.commands)
	}
	if runner.lookups != 0 || runner.commands != 0 {
		t.Fatalf("invalid input reached config/tool resolution: lookups=%d commands=%d", runner.lookups, runner.commands)
	}
}

func TestGoalFlowValidationIdentifiesObjectiveAndSuccessConditionFields(t *testing.T) {
	for _, tc := range []struct {
		name, goal, condition, field string
	}{
		{"missing objective", "", `title:"Saved"`, "steps[2].goal.goal"},
		{"invalid success condition", "save note", `unknown:"Saved"`, "steps[2].goal.succeededWhen"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := FlowStep{Action: "goal", Params: map[string]string{"goal": tc.goal, "succeededWhen": tc.condition}}
			if err := validateGoalFlowStep(2, step); err == nil || !strings.Contains(err.Error(), tc.field+":") {
				t.Fatalf("expected diagnostic naming %s, got %v", tc.field, err)
			}
		})
	}
}
