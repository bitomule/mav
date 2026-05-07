package mav

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type launchStep struct {
	Name    string
	Command string
}

func (c CLI) runLaunchRecipe(ctx context.Context, cfg Config, run RunState) (string, *launchStep, CommandResult) {
	commands := cfg.Launch.Commands
	if !hasLaunchCommands(commands) && cfg.BundleID != "" {
		commands.Launch = `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`
	}
	steps := []launchStep{
		{Name: "healthcheck", Command: commands.Healthcheck},
		{Name: "build", Command: commands.Build},
		{Name: "app_path", Command: commands.AppPath},
		{Name: "install", Command: commands.Install},
		{Name: "launch", Command: commands.Launch},
	}
	appPath := ""
	if strings.TrimSpace(commands.Launch) == "" {
		return appPath, &launchStep{Name: "launch"}, CommandResult{Stderr: "launch command missing", Err: fmt.Errorf("launch command missing")}
	}
	env := launchEnv(cfg, run, appPath)
	for _, step := range steps {
		if strings.TrimSpace(step.Command) == "" {
			continue
		}
		result := c.runLaunchCommand(ctx, cfg, run, step, env)
		if result.Err != nil {
			return appPath, &step, result
		}
		if step.Name == "app_path" {
			resolved, err := parseAppPath(result.Stdout)
			if err != nil {
				result.Err = err
				result.Stderr = err.Error()
				return appPath, &step, result
			}
			appPath = resolved
			env = launchEnv(cfg, run, appPath)
		}
	}
	if strings.TrimSpace(commands.Cleanup) != "" {
		step := launchStep{Name: "cleanup", Command: commands.Cleanup}
		result := c.runLaunchCommand(ctx, cfg, run, step, env)
		if result.Err != nil {
			return appPath, &step, result
		}
	}
	return appPath, nil, CommandResult{}
}

func (c CLI) runLaunchCommand(ctx context.Context, cfg Config, run RunState, step launchStep, env map[string]string) CommandResult {
	command := shellEnvPrefix(env) + " " + step.Command
	result := c.Runner.Run(ctx, "/bin/sh", "-lc", command)
	appendCommand(run, "launch."+step.Name+" "+step.Command, result)
	return result
}

func launchEnv(cfg Config, run RunState, appPath string) map[string]string {
	udid := cfg.SimulatorUDID
	if udid == "" {
		udid = "booted"
	}
	return map[string]string{
		"MAV_ROOT":        cfg.Root,
		"MAV_RUN_DIR":     run.Dir,
		"MAV_UDID":        udid,
		"MAV_BUNDLE_ID":   cfg.BundleID,
		"MAV_APP_PATH":    appPath,
		"MAV_DEVICE_NAME": cfg.SimulatorName,
		"MAV_RUNTIME":     cfg.SimulatorRuntime,
		"MAV_PLATFORM":    "ios",
	}
}

func shellEnvPrefix(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{"cd", shellQuote(env["MAV_ROOT"]), "&&", "export"}
	for _, key := range keys {
		parts = append(parts, key+"="+shellQuote(env[key]))
	}
	parts = append(parts, "&&")
	return strings.Join(parts, " ")
}

func parseAppPath(stdout string) (string, error) {
	lines := []string{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("app_path command printed no .app path")
	}
	if len(lines) > 1 {
		return "", fmt.Errorf("app_path command printed multiple lines")
	}
	if filepath.Ext(lines[0]) != ".app" {
		return "", fmt.Errorf("app_path is not a .app bundle: %s", lines[0])
	}
	return lines[0], nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
