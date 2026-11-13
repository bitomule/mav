package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bitomule/mav/internal/mav/drivers"
)

const gotoDefaultTimeout = 90 * time.Second

const goalUsage = `mav goal "<objective>" [--succeeded-when 'title:"Settings"'] [--mode navigation|action] [--input name=value] [--max-steps 12] [--timeout 90s]`

const goalHelp = `Usage: ` + goalUsage + `

Observe, plan one operation and target, act, then verify fresh evidence.
--mode action (default) offers taps, scrolling and caller-declared text for
focused inputs. --mode navigation restricts the menu to navigation operations.
The planner never executes the autonomous AXe agent loop.

--succeeded-when accepts conjunctive title:"...", text:"...", screen:"..."
and value:"accessibility-id=exact value" predicates. A title without a prefix
is a title predicate. Flows require succeededWhen. The condition must hold on
two stable observations; a model's done only sets reported_success=true.
An initially satisfied condition returns already_satisfied with zero actions.

Text comes only from quoted literals in the objective or --input name=value.
In YAML, declared flow inputs are the closed set of text choices.
Budgets: at most 12 goal actions and 90s, plus at most three explicitly named
--dismiss-permission buttons. Destructive actions are refused. Two abstentions,
two actions without progress or a repeated screen stop the run. Partial or
unknown delivery and post-input observation failure never cause replay.

AXe simulator input uses checked snapshots/ref. Other drivers expose their own
guarantees; a preflight tree check alone is not atomic with input.
Output separates succeeded=true|false|unverified, reported_success, per-action
delivery and progress. No condition means action success stays unverified.
Navigation may identify the destination among observed screens, separately
reported as criterion_source=observed. CI refuses model-driven execution.
`

func validateGoalFlowStep(index int, step FlowStep) error {
	if strings.TrimSpace(step.Params["goal"]) == "" {
		return fmt.Errorf("steps[%d].goal.goal: a goal objective is mandatory", index)
	}
	if strings.TrimSpace(step.Params["succeededWhen"]) == "" {
		return fmt.Errorf("steps[%d].goal.succeededWhen: a verifiable success condition is mandatory", index)
	}
	if step.Params["arrivedWhen"] != "" {
		return fmt.Errorf("steps[%d].goal.arrivedWhen: use succeededWhen", index)
	}
	if !step.Where.IsZero() {
		return fmt.Errorf("steps[%d].goal.where: goal chooses operation and target from observations", index)
	}
	if _, err := ParseSuccessCondition(step.Params["succeededWhen"]); err != nil {
		return fmt.Errorf("steps[%d].goal.succeededWhen: %w", index, err)
	}
	_, condition, err := parseGoalArgs(goalFlowArgs(step))
	if err != nil {
		return fmt.Errorf("steps[%d].goal: %w", index, err)
	}
	if !condition.Declared() {
		return fmt.Errorf("steps[%d].goal.succeededWhen: condition names nothing", index)
	}
	return nil
}

// goalFlowArgs supplies every declaration to the common parser before it
// derives quoted inputs, so flows preserve CLI collision checks and policy.
func goalFlowArgs(step FlowStep) []string {
	args := []string{step.Params["goal"], "--succeeded-when", step.Params["succeededWhen"]}
	for _, pair := range [][2]string{{"mode", "--mode"}, {"maxSteps", "--max-steps"}, {"timeout", "--timeout"}, {"dismissPermission", "--dismiss-permission"}} {
		if v := step.Params[pair[0]]; v != "" {
			args = append(args, pair[1], v)
		}
	}
	keys := make([]string, 0, len(step.Inputs))
	for key := range step.Inputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--input", key+"="+step.Inputs[key])
	}
	return args
}

func parseGoalArgs(args []string) (GoalRequest, SuccessCondition, error) {
	req := GoalRequest{Mode: "action", MaxSteps: gotoMaxSteps, Timeout: gotoDefaultTimeout, Inputs: map[string]string{}}
	var objective []string
	var spec string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			objective = append(objective, a)
			continue
		}
		name, value, inline := strings.Cut(a, "=")
		switch name {
		case "--succeeded-when", "--mode", "--input", "--max-steps", "--timeout", "--dismiss-permission":
		default:
			return req, SuccessCondition{}, fmt.Errorf("unknown goal option %s; use --succeeded-when instead of --arrived-when", name)
		}
		if !inline {
			i++
			if i >= len(args) {
				return req, SuccessCondition{}, fmt.Errorf("%s needs a value", name)
			}
			value = args[i]
		}
		switch name {
		case "--succeeded-when":
			spec = value
		case "--mode":
			req.Mode = value
		case "--input":
			key, text, ok := strings.Cut(value, "=")
			if !ok || strings.TrimSpace(key) == "" {
				return req, SuccessCondition{}, fmt.Errorf("--input requires name=value")
			}
			if _, ok := req.Inputs[key]; ok {
				return req, SuccessCondition{}, fmt.Errorf("duplicate input %q", key)
			}
			req.Inputs[key] = text
		case "--max-steps":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > gotoMaxSteps {
				return req, SuccessCondition{}, fmt.Errorf("--max-steps must be 1..12")
			}
			req.MaxSteps = n
		case "--timeout":
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 || d > gotoDefaultTimeout {
				return req, SuccessCondition{}, fmt.Errorf("--timeout must be positive and at most 90s")
			}
			req.Timeout = d
		case "--dismiss-permission":
			req.DismissPermission = value
		}
	}
	req.Goal = strings.TrimSpace(strings.Join(objective, " "))
	if req.Goal == "" {
		return req, SuccessCondition{}, fmt.Errorf("goal is required")
	}
	if req.Mode != "action" && req.Mode != "navigation" {
		return req, SuccessCondition{}, fmt.Errorf("--mode must be navigation or action")
	}
	// Only explicitly quoted literals are eligible for typing. An unmatched
	// quote contributes nothing; a model cannot create a text source.
	for i, text := range goalLiteralInputs(req.Goal) {
		key := fmt.Sprintf("literal_%d", i+1)
		if _, exists := req.Inputs[key]; exists {
			return req, SuccessCondition{}, fmt.Errorf("input %s collides with a quoted literal", key)
		}
		req.Inputs[key] = text
	}
	condition, err := ParseSuccessCondition(spec)
	return req, condition, err
}

func goalLiteralInputs(goal string) []string {
	var values []string
	var quote rune
	var text strings.Builder
	runes := []rune(goal)
	for i, r := range runes {
		if quote == 0 {
			if (r == '\'' || r == '"') && (i == 0 || !unicode.IsLetter(runes[i-1]) && !unicode.IsDigit(runes[i-1])) {
				quote = r
				text.Reset()
			}
			continue
		}
		if r == quote && (i+1 == len(runes) || !unicode.IsLetter(runes[i+1]) && !unicode.IsDigit(runes[i+1])) {
			values = append(values, text.String())
			quote = 0
		} else {
			text.WriteRune(r)
		}
	}
	return values
}

func (c CLI) goalCommand(ctx context.Context, opts GlobalOptions, args []string) error {
	req, condition, err := parseGoalArgs(args)
	if err != nil {
		return Fail("goal_invalid", map[string]string{"error": err.Error(), "usage": goalUsage}).Write(c.Stdout)
	}
	if reason := findIsRefusedHere(); reason != "" {
		return c.writeGoalResult(GoalResult{Goal: req.Goal, Mode: req.Mode, Outcome: "ci_refused", Succeeded: "unverified", CriterionSource: CriterionNone})
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return c.failConfig(err)
	}
	c.resolveConfigTools(&cfg)
	if _, err = c.resolveConfigTarget(&cfg); err != nil {
		return c.failTargetCommand(err)
	}
	result := c.runGoal(ctx, opts, cfg, req, condition)
	return c.writeGoalResult(result)
}

func (c CLI) runGoal(ctx context.Context, opts GlobalOptions, cfg Config, req GoalRequest, condition SuccessCondition) GoalResult {
	prefer, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return GoalResult{Goal: req.Goal, Mode: req.Mode, Outcome: "blocked", Succeeded: "false", Next: err.Error()}
	}
	if prefer == "auto" {
		prefer = ""
	}
	target := targetFromConfig(cfg)
	driver, _, err := c.router().Route(ctx, drivers.CapTreeAX, target, prefer)
	if err != nil {
		return GoalResult{Goal: req.Goal, Mode: req.Mode, Outcome: "blocked", Succeeded: "false", Next: err.Error()}
	}
	adapter := goalDriverAdapter{driver: driver, target: target}
	if driver.ID() != "axe" && target.Kind == drivers.KindSim {
		adapter.transform = func(ctx context.Context, els []Element, x, y int) (int, int, error) {
			hid := c.hidPoint(ctx, cfg, x, y)
			if treeShapeContradictsRotation(els, hid.Detected) {
				return x, y, nil
			}
			if hid.Detected != 0 && hid.Rotation == 0 {
				return 0, 0, fmt.Errorf("rotation_unavailable: cannot validate the touch surface")
			}
			if hid.Detected == 0 && treeIsLandscape(els) {
				return 0, 0, fmt.Errorf("rotation_unavailable: cannot infer landscape direction")
			}
			return hid.X, hid.Y, nil
		}
	}
	engine := GoalEngine{Observer: adapter, Executor: adapter, Planner: goalJevPlanner{}, Verifier: condition, Capabilities: adapter.capabilities()}
	result := engine.Run(ctx, req)
	if condition.Declared() {
		result.Criterion = condition.String()
	}

	return result
}

func (c CLI) writeGoalResult(result GoalResult) error {
	fields := map[string]string{"succeeded": result.Succeeded, "outcome": result.Outcome, "reported_success": strconv.FormatBool(result.ReportedSuccess), "steps": strconv.Itoa(len(result.Steps)), "criterion_source": result.CriterionSource}
	if result.Next != "" {
		fields["next"] = result.Next
	}
	if !result.Final.IsZero() {
		fields["route"] = result.Final.String()
	}
	if err := c.OK("goal", fields).Write(c.Stdout); err != nil {
		return err
	}
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.Stdout, "goal json=%s\n", quoteIfNeeded(string(b)))
	return err
}

func (c CLI) executeGoalFlowStep(ctx context.Context, opts GlobalOptions, step FlowStep) (map[string]string, error) {
	req, condition, err := parseGoalArgs(goalFlowArgs(step))
	if err != nil {
		return nil, err
	}
	cfg, err := c.mustLoadConfig()
	if err != nil {
		return flowStepTargetFailure(step, err)
	}
	result := c.runGoal(ctx, opts, cfg, req, condition)
	fields := map[string]string{"goal": req.Goal, "succeeded": result.Succeeded, "outcome": result.Outcome, "reported_success": strconv.FormatBool(result.ReportedSuccess), "steps": strconv.Itoa(len(result.Steps)), "criterion": condition.String()}
	if result.Next != "" {
		fields["next"] = result.Next
	}
	if result.Succeeded != "true" {
		return fields, goalFlowFailure(result, fields)
	}
	return fields, nil
}

// A failed goal may have delivered several earlier inputs. Retrying the whole
// flow step would replay those effects, even when the final action was refused.
func goalFlowFailure(result GoalResult, fields map[string]string) error {
	err := fmt.Errorf("goal_%s", result.Outcome)
	for _, step := range result.Steps {
		if step.Receipt.Delivery != "not_sent" {
			fields["retry"] = "unsafe"
			fields["delivery"] = step.Receipt.Delivery
			return afterInputError(err)
		}
	}
	return err
}
