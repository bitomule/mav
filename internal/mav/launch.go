package mav

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type launchStep struct {
	Name    string
	Command string
}

func (c CLI) runLaunchRecipe(ctx context.Context, cfg Config, run RunState, clearState bool) (string, *launchStep, CommandResult) {
	commands := effectiveLaunchCommands(cfg)
	if !hasLaunchCommands(commands) && cfg.BundleID != "" {
		if isPhysicalDevice(cfg) {
			commands.Launch = `idb launch --udid "$MAV_UDID" -f "$MAV_BUNDLE_ID"`
		} else {
			commands.Launch = `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`
		}
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
		return appPath, &launchStep{Name: "launch"}, CommandResult{Stderr: "launch command missing; run mav setup or add launch.commands.launch to .mav/config.yaml", Err: fmt.Errorf("launch_command_missing")}
	}
	if clearState && strings.TrimSpace(commands.Install) == "" {
		return appPath, &launchStep{Name: "clear_state"}, CommandResult{Stderr: "clearState requires an install command in the launch recipe; add launch.commands.install to .mav/config.yaml", Err: fmt.Errorf("clear_state_install_missing")}
	}
	env := launchEnv(cfg, run, appPath)
	if clearState && cfg.BundleID != "" {
		step := launchStep{Name: "clear_state", Command: `xcrun simctl uninstall "$MAV_UDID" "$MAV_BUNDLE_ID" || true`}
		if isPhysicalDevice(cfg) {
			step.Command = `idb uninstall --udid "$MAV_UDID" "$MAV_BUNDLE_ID" || true`
		}
		_ = c.runLaunchCommand(ctx, cfg, run, step, env)
	}
	for _, step := range steps {
		if strings.TrimSpace(step.Command) == "" {
			continue
		}
		result := c.runLaunchCommand(ctx, cfg, run, step, env)
		if result.Err != nil {
			if step.Name == "install" && shouldRetryInstallFromWritableCopy(result, appPath) {
				if retryResult, retryPath := c.retryInstallFromWritableCopy(ctx, cfg, run, appPath); retryResult.Err == nil {
					appPath = retryPath
					env = launchEnv(cfg, run, appPath)
					continue
				} else {
					return appPath, &step, retryResult
				}
			}
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
	udid := targetUDID(cfg)
	if udid == "" {
		udid = "booted"
	}
	isDevice := "false"
	if isPhysicalDevice(cfg) {
		isDevice = "true"
	}
	return map[string]string{
		"MAV_ROOT":        cfg.Root,
		"MAV_RUN_DIR":     run.Dir,
		"MAV_TARGET_KIND": normalizedTargetKind(cfg),
		"MAV_IS_DEVICE":   isDevice,
		"MAV_UDID":        udid,
		"MAV_BUNDLE_ID":   cfg.BundleID,
		"MAV_APP_PATH":    appPath,
		"MAV_DEVICE_NAME": targetName(cfg),
		"MAV_RUNTIME":     targetRuntime(cfg),
		"MAV_PLATFORM":    "ios",
	}
}

func effectiveLaunchCommands(cfg Config) LaunchCommands {
	commands := cfg.Launch.Commands
	if !isPhysicalDevice(cfg) {
		return commands
	}
	if isMAVSimctlInstall(commands.Install) || commands.Install == "" && commands.AppPath != "" {
		commands.Install = `idb install --udid "$MAV_UDID" "$MAV_APP_PATH"`
	}
	if commands.Launch == "" || isMAVSimctlLaunch(commands.Launch) {
		commands.Launch = `idb launch --udid "$MAV_UDID" -f "$MAV_BUNDLE_ID"`
	}
	return commands
}

func isMAVSimctlInstall(command string) bool {
	return strings.Contains(command, "xcrun simctl install") && strings.Contains(command, "MAV_APP_PATH")
}

func isMAVSimctlLaunch(command string) bool {
	return strings.Contains(command, "xcrun simctl launch") && strings.Contains(command, "MAV_BUNDLE_ID")
}

func shouldRetryInstallFromWritableCopy(result CommandResult, appPath string) bool {
	if strings.TrimSpace(appPath) == "" || result.Err == nil {
		return false
	}
	lower := strings.ToLower(result.Stderr + "\n" + result.Stdout + "\n" + result.Err.Error())
	return strings.Contains(lower, "permission denied") &&
		strings.Contains(filepath.ToSlash(appPath), "bazel-out/") &&
		filepath.Ext(appPath) == ".app"
}

func (c CLI) retryInstallFromWritableCopy(ctx context.Context, cfg Config, run RunState, appPath string) (CommandResult, string) {
	target := filepath.Join(run.Dir, "app.tmp", filepath.Base(appPath))
	_ = os.RemoveAll(target)
	if err := copyDirWritable(appPath, target); err != nil {
		return CommandResult{Stderr: err.Error(), Err: err}, appPath
	}
	env := launchEnv(cfg, run, target)
	command := `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`
	if isPhysicalDevice(cfg) {
		command = `idb install --udid "$MAV_UDID" "$MAV_APP_PATH"`
	}
	result := c.runLaunchCommand(ctx, cfg, run, launchStep{Name: "install_retry", Command: command}, env)
	return result, target
}

func copyDirWritable(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if d.IsDir() {
			return os.MkdirAll(target, mode.Perm()|0o700)
		}
		if mode&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, mode.Perm()|0o600)
	})
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
