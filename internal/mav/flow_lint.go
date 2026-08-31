package mav

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bitomule/mav/internal/mav/codes"
)

type flowLintIssue struct {
	Severity string
	Step     int
	Code     string
	Message  string
}

func (c CLI) flow(ctx context.Context, opts GlobalOptions, args []string) error {
	_ = ctx
	if len(args) == 0 {
		return Fail("flow_command_missing", map[string]string{"usage": "mav flow lint flow.yaml"}).Write(c.Stdout)
	}
	switch args[0] {
	case "lint":
		return c.flowLint(opts, args[1:])
	default:
		return Fail("flow_unknown_command", map[string]string{"command": args[0], "usage": "mav flow lint flow.yaml"}).Write(c.Stdout)
	}
}

func (c CLI) flowLint(opts GlobalOptions, args []string) error {
	path := firstNonFlagArg(args)
	if path == "" {
		return Fail("flow_missing", map[string]string{"usage": "mav flow lint flow.yaml"}).Write(c.Stdout)
	}
	flow, err := LoadFlow(path)
	if err != nil {
		return FailCode(codes.FlowLintFailed, map[string]string{"file": path, "errors": "1", "warnings": "0", "error": err.Error()}).Write(c.Stdout)
	}
	cfg, _ := LoadConfig(c.Root)
	issues := lintFlow(flow, cfg)
	errors, warnings := countLintIssues(issues)
	if opts.Raw || hasFlag(args, "--raw") {
		for _, issue := range issues {
			fmt.Fprintf(c.Stdout, "%s step=%d code=%s message=%q\n", issue.Severity, issue.Step, issue.Code, issue.Message)
		}
	}
	fields := map[string]string{
		"file":     filepath.Clean(path),
		"errors":   strconv.Itoa(errors),
		"warnings": strconv.Itoa(warnings),
	}
	if errors > 0 {
		return FailCode(codes.FlowLintFailed, fields).Write(c.Stdout)
	}
	return c.OK("flow.lint", fields).Write(c.Stdout)
}

func lintFlow(flow Flow, cfg Config) []flowLintIssue {
	var issues []flowLintIssue
	var walk func([]FlowStep)
	stepIndex := 0
	walk = func(steps []FlowStep) {
		for _, step := range steps {
			stepIndex++
			issues = append(issues, lintFlowStep(stepIndex, step, cfg)...)
			if len(step.Do) > 0 {
				walk(step.Do)
			}
		}
	}
	walk(flow.Steps)
	return issues
}

func lintFlowStep(index int, step FlowStep, cfg Config) []flowLintIssue {
	var issues []flowLintIssue
	add := func(severity, code, message string) {
		issues = append(issues, flowLintIssue{Severity: severity, Step: index, Code: code, Message: message})
	}
	switch step.Action {
	case "delay", "sleep":
		if _, err := time.ParseDuration(step.Params["duration"]); err != nil {
			add("error", "duration_invalid", "delay/sleep requires a valid duration")
		}
	case "wait", "waitUntil", "whileNotVisible":
		if timeout := step.Params["timeout"]; timeout != "" {
			if _, err := time.ParseDuration(timeout); err != nil {
				add("error", "timeout_invalid", "timeout must be a valid duration")
			}
		}
	case "tap":
		if step.Params["text"] != "" && step.Params["id"] == "" {
			add("warning", "tap_text_fragile", "tap by text is localization/copy fragile; prefer accessibility id")
		}
	case "evidence.step":
		if strings.TrimSpace(step.Params["note"]) == "" {
			add("warning", "evidence_note_missing", "evidence.step should explain the assertion")
		}
	case "sim.appearance":
		// The step reaches simctl through `mav sim appearance`, which only
		// answers to light|dark. Linting it here means a screenshot flow
		// fails at lint time instead of halfway through a capture matrix.
		if appearance := step.Params["appearance"]; appearance != "light" && appearance != "dark" {
			add("error", "appearance_invalid", fmt.Sprintf("sim.appearance requires appearance: light|dark, got %q", appearance))
		}
	case "sim.statusbar.set":
		// Validated by the parser the executor itself uses, so the linter
		// cannot drift from what a run would accept.
		if _, _, failure := statusBarSpecFromArgs(statusBarFlowArgs(step.Params)); failure != nil {
			add("error", failure.Code, statusBarLintMessage(*failure))
		}
	case "sim.statusbar.clear":
		if keys := statusBarParamsIn(step.Params); len(keys) > 0 {
			add("warning", "status_bar_clear_params_ignored", fmt.Sprintf("sim.statusbar.clear resets the whole status bar and ignores %s", strings.Join(keys, ", ")))
		}
	case "exec":
		if !cfg.AllowShell {
			add("error", "exec_requires_allow_shell", "exec steps require allow_shell: true in .mav/config.yaml")
		}
	}
	if (step.Action == "wait" || step.Action == "when" || step.Action == "whileNotVisible") &&
		step.Params["id"] == "" && step.Params["text"] == "" && step.Params["value"] == "" && len(step.Any) == 0 {
		add("error", "wait_target_missing", "wait-like steps require id, text, value, or any")
	}
	return issues
}

func countLintIssues(issues []flowLintIssue) (int, int) {
	errors, warnings := 0, 0
	for _, issue := range issues {
		if issue.Severity == "error" {
			errors++
		} else if issue.Severity == "warning" {
			warnings++
		}
	}
	return errors, warnings
}

func firstNonFlagArg(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return ""
}

// statusBarLintMessage turns a status bar parse failure into lint prose that
// names the YAML key, not the CLI flag the step was translated into.
func statusBarLintMessage(failure Output) string {
	key := statusBarParamForFlag(failure.Fields["flag"])
	switch failure.Code {
	case "status_bar_preset_invalid":
		return fmt.Sprintf("preset %q is not a known preset (allowed: %s)", failure.Fields["preset"], failure.Fields["allowed"])
	case "status_bar_value_invalid":
		return fmt.Sprintf("%s: %q is not allowed (allowed: %s)", key, failure.Fields["value"], failure.Fields["allowed"])
	case "status_bar_value_missing":
		return fmt.Sprintf("%s needs a value", key)
	case "status_bar_fields_missing":
		return "sim.statusbar.set needs preset or at least one status bar field"
	default:
		return failure.Code
	}
}

func statusBarParamForFlag(flag string) string {
	pairs := statusBarFlowFlags()
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i] == flag {
			return pairs[i+1]
		}
	}
	return strings.TrimPrefix(flag, "--")
}

// statusBarParamsIn lists the status bar keys a step actually carries, sorted
// so the lint message is stable across runs.
func statusBarParamsIn(params map[string]string) []string {
	pairs := statusBarFlowFlags()
	var keys []string
	for i := 1; i < len(pairs); i += 2 {
		if params[pairs[i]] != "" {
			keys = append(keys, pairs[i])
		}
	}
	sort.Strings(keys)
	return keys
}
