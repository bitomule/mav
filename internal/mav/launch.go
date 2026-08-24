package mav

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type launchStep struct {
	Name    string
	Command string
}

// runLaunchRecipe ejecuta la receta de lanzamiento del proyecto. Devuelve, por
// orden: la ruta del .app resuelta, el paso que falló (nil si todo fue bien),
// su resultado, y un aviso no fatal para los casos en los que seguir es
// correcto pero callarse no.
func (c CLI) runLaunchRecipe(ctx context.Context, cfg Config, run RunState, clearState bool) (string, *launchStep, CommandResult, string) {
	commands := effectiveLaunchCommands(cfg)
	steps := []launchStep{
		{Name: "healthcheck", Command: commands.Healthcheck},
		{Name: "build", Command: commands.Build},
		{Name: "app_path", Command: commands.AppPath},
	}
	appPath := ""
	driverLaunch := shouldUseDriverLaunch(cfg, commands)
	if strings.TrimSpace(commands.Launch) == "" && !driverLaunch {
		return appPath, &launchStep{Name: "launch"}, CommandResult{Stderr: "launch command missing; run mav setup or add launch.commands.launch to .mav/config.yaml", Err: fmt.Errorf("launch_command_missing")}, ""
	}
	if clearState && strings.TrimSpace(commands.Install) == "" && strings.TrimSpace(commands.AppPath) == "" {
		return appPath, &launchStep{Name: "clear_state"}, CommandResult{Stderr: "clearState requires an install command in the launch recipe; add launch.commands.install to .mav/config.yaml", Err: fmt.Errorf("clear_state_install_missing")}, ""
	}
	env := launchEnv(cfg, run, appPath)
	warn := ""
	if clearState && cfg.BundleID != "" {
		// Un uninstall que falla NO es fatal: el caso corriente es que la app
		// todavía no estuviera instalada (primer run del proyecto, simulador
		// recién creado), y ahí abortar sería peor que seguir. Pero descartar
		// el resultado -- que es lo que este código hacía -- deja mudo también
		// el caso en el que el uninstall falla de verdad, y entonces
		// --clear-state miente: el usuario cree que parte de cero y arrastra
		// el estado del run anterior, que es exactamente el bug irreproducible
		// que --clear-state existe para evitar.
		//
		// No se intenta distinguir "no había nada que desinstalar" de "falló
		// de verdad": eso obligaría a leer el stderr de simctl/idb, que es
		// suyo y pueden cambiarlo cuando quieran (la misma razón por la que
		// isSimulatorBooted pregunta a CoreSimulator en vez de adivinar por el
		// texto del error). Se avisa siempre y decide quien lee.
		if result := c.runDriverLifecycle(ctx, cfg, run, launchStep{Name: "clear_state"}, appPath); result.Err != nil {
			detail := firstLine(strings.TrimSpace(result.Stderr))
			if detail == "" {
				detail = result.Err.Error()
			}
			warn = fmt.Sprintf("clear_state_incomplete: uninstall of %s did not complete: %s (next: the app may still carry state from an earlier run; check launch.clear_state in the run's commands trail)", cfg.BundleID, detail)
		}
	}
	for _, step := range steps {
		if step.Name == "build" && os.Getenv("MAV_SKIP_BUILD") == "1" {
			continue
		}
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
					return appPath, &step, retryResult, warn
				}
			}
			return appPath, &step, result, warn
		}
		if step.Name == "app_path" {
			resolved, err := parseAppPath(result.Stdout)
			if err != nil {
				result.Err = err
				result.Stderr = err.Error()
				return appPath, &step, result, warn
			}
			appPath = resolved
			env = launchEnv(cfg, run, appPath)
		}
	}
	if strings.TrimSpace(commands.Install) != "" || appPath != "" {
		step := launchStep{Name: "install", Command: commands.Install}
		var result CommandResult
		if shouldUseDriverInstall(cfg, commands) {
			result = c.runDriverLifecycle(ctx, cfg, run, step, appPath)
		} else {
			result = c.runLaunchCommand(ctx, cfg, run, step, env)
		}
		if result.Err != nil {
			if shouldRetryInstallFromWritableCopy(result, appPath) {
				if retryResult, retryPath := c.retryInstallFromWritableCopy(ctx, cfg, run, appPath); retryResult.Err == nil {
					appPath = retryPath
					env = launchEnv(cfg, run, appPath)
				} else {
					return appPath, &step, retryResult, warn
				}
			} else {
				return appPath, &step, result, warn
			}
		}
	}
	if strings.TrimSpace(commands.Launch) != "" || driverLaunch {
		step := launchStep{Name: "launch", Command: commands.Launch}
		var result CommandResult
		if driverLaunch {
			result = c.runDriverLifecycle(ctx, cfg, run, step, appPath)
		} else {
			result = c.runLaunchCommand(ctx, cfg, run, step, env)
		}
		if result.Err != nil {
			return appPath, &step, result, warn
		}
	}
	if strings.TrimSpace(commands.Cleanup) != "" {
		step := launchStep{Name: "cleanup", Command: commands.Cleanup}
		result := c.runLaunchCommand(ctx, cfg, run, step, env)
		if result.Err != nil {
			return appPath, &step, result, warn
		}
	}
	return appPath, nil, CommandResult{}, warn
}

func shouldUseDriverInstall(cfg Config, commands LaunchCommands) bool {
	command := strings.TrimSpace(commands.Install)
	return command == "" ||
		isMAVSimctlInstall(command) ||
		strings.Contains(command, "idb install") && strings.Contains(command, "MAV_APP_PATH") ||
		strings.Contains(command, "idb install") && strings.Contains(command, "$MAV_APP_PATH") ||
		(targetKind(cfg) == drivers.KindDevice && strings.Contains(command, "simctl install"))
}

func shouldUseDriverLaunch(cfg Config, commands LaunchCommands) bool {
	command := strings.TrimSpace(commands.Launch)
	return command == "" && cfg.BundleID != "" ||
		isMAVSimctlLaunch(command) ||
		strings.Contains(command, "idb launch") && strings.Contains(command, "MAV_BUNDLE_ID") ||
		(targetKind(cfg) == drivers.KindDevice && strings.Contains(command, "simctl launch"))
}

func (c CLI) runDriverLifecycle(ctx context.Context, cfg Config, run RunState, step launchStep, appPath string) CommandResult {
	target := targetFromConfig(cfg)
	capability := drivers.CapLaunch
	if step.Name == "install" || step.Name == "install_retry" {
		capability = drivers.CapInstall
	}
	if step.Name == "clear_state" {
		capability = drivers.CapUninstall
	}
	driver, _, err := c.router().Route(ctx, capability, target, "")
	if err != nil {
		result := CommandResult{Stderr: err.Error(), Err: err}
		appendCommand(run, "launch."+step.Name+" capability="+string(capability), result)
		return result
	}
	lifecycle, ok := driver.(drivers.LifecycleDriver)
	if !ok {
		err := fmt.Errorf("driver %s does not implement lifecycle", driver.ID())
		result := CommandResult{Stderr: err.Error(), Err: err}
		appendCommand(run, "launch."+step.Name+" driver="+driver.ID(), result)
		return result
	}
	switch step.Name {
	case "install", "install_retry":
		err = lifecycle.Install(ctx, target, drivers.InstallSpec{Path: appPath})
	case "launch":
		_, err = lifecycle.Launch(ctx, target, drivers.LaunchSpec{BundleID: cfg.BundleID})
	case "clear_state":
		err = lifecycle.Uninstall(ctx, target, cfg.BundleID)
	}
	result := CommandResult{}
	if err != nil {
		result = CommandResult{Stderr: err.Error(), Err: err}
	}
	appendCommand(run, "launch."+step.Name+" driver="+driver.ID(), result)
	return result
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
	if targetKind(cfg) == drivers.KindDevice {
		isDevice = "true"
	}
	return map[string]string{
		"MAV_ROOT":        cfg.Root,
		"MAV_RUN_DIR":     run.Dir,
		"MAV_TARGET_KIND": targetKindLabel(targetKind(cfg)),
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
	if targetKind(cfg) != drivers.KindDevice {
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
	result := c.runDriverLifecycle(ctx, cfg, run, launchStep{Name: "install_retry"}, target)
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
