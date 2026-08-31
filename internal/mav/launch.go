package mav

import (
	"context"
	"errors"
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

// errBuildSkippedAppMissing is what --skip-build turns "app_path failed" into.
// Without it the operator sees whatever their Makefile printed on the way out,
// which for a build that was never run is a path that does not exist and no
// hint that MAV is the one who decided not to build it.
var errBuildSkippedAppMissing = errors.New("build_skipped_app_missing")

// runLaunchRecipe runs the project's launch recipe. It returns, in order:
// the resolved .app path, the step that failed (nil if all went well), its
// result, and a non-fatal warning for the cases where continuing is right
// but staying quiet is not.
func (c CLI) runLaunchRecipe(ctx context.Context, cfg Config, run RunState, clearState bool, fixture string) (string, *launchStep, CommandResult, string) {
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
		// A failing uninstall is NOT fatal: the common case is that the app
		// was not installed yet (the project's first run, a freshly created
		// simulator), and aborting there would be worse than continuing.
		// But discarding the result, which is what this code did, also
		// mutes the case where the uninstall genuinely fails, and then
		// --clear-state lies: the user believes they start from zero and
		// drags the previous run's state along, which is exactly the
		// irreproducible bug --clear-state exists to avoid.
		//
		// No attempt is made to distinguish "there was nothing to
		// uninstall" from "genuinely failed": that would require reading
		// simctl/idb's stderr, which is theirs and they can change whenever
		// they want (the same reason isSimulatorBooted asks CoreSimulator
		// instead of guessing from the error text). It always warns and the
		// reader decides.
		if result := c.runDriverLifecycle(ctx, cfg, run, launchStep{Name: "clear_state"}, appPath); result.Err != nil {
			detail := firstLine(strings.TrimSpace(result.Stderr))
			if detail == "" {
				detail = result.Err.Error()
			}
			warn = fmt.Sprintf("clear_state_incomplete: uninstall of %s did not complete: %s (next: the app may still carry state from an earlier run; check launch.clear_state in the run's commands trail)", cfg.BundleID, detail)
		}
	}
	// The recipe splits where the machines differ. healthcheck, build and
	// app_path answer questions about the checkout and the toolchain, both
	// of which are here: a VM image carrying every project's build
	// dependencies is not an image anybody can share. install, fixture,
	// launch and cleanup act on the app, so they belong where the app runs.
	// Without a VM the two halves are the same machine and this is a no-op.
	builder := c
	if cfg.VM {
		builder = c.hostCLI()
	}
	for _, step := range steps {
		// Only the build is skipped. app_path, install and launch still run,
		// because reusing an artifact still means finding it and putting it
		// on the target.
		if step.Name == "build" && c.skipBuild {
			continue
		}
		if strings.TrimSpace(step.Command) == "" {
			continue
		}
		result := builder.runLaunchCommand(ctx, cfg, run, step, env)
		if result.Err != nil {
			if step.Name == "app_path" && c.skipBuild {
				return appPath, &step, buildSkippedAppMissing(firstLine(result.Stderr), result.Err), warn
			}
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
			// Only with --skip-build: the recipe was asked to name an
			// artifact nothing in this invocation produced, so a path that
			// is not on disk is the expected failure, and it has to read as
			// "you skipped the build" rather than as a broken install two
			// steps later.
			if c.skipBuild {
				if _, statErr := os.Stat(resolved); statErr != nil {
					return appPath, &step, buildSkippedAppMissing("app_path printed "+resolved+", which does not exist", statErr), warn
				}
			}
			appPath = resolved
			env = launchEnv(cfg, run, appPath)
		}
	}
	// Between the two halves, and only here: this is the first moment mav
	// knows which bundle the recipe produced, and the last one before
	// anything tries to install or launch it on the other machine.
	if cfg.VM {
		if err := c.vmPrepareGuest(ctx, run, appPath); err != nil {
			step := launchStep{Name: "vm_sync"}
			return appPath, &step, CommandResult{Stderr: err.Error(), Err: err}, warn
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
	// The fixture goes here, between install and launch, and it is no
	// accident: the app's container already exists (the install just
	// created it) and the app has not started yet, so nothing has its
	// database open. Before or after this window, seeding is corrupting.
	if fixture != "" {
		commandsForFixture, ok := cfg.Fixtures[fixture]
		if !ok {
			step := launchStep{Name: "fixture"}
			return appPath, &step, CommandResult{
				Stderr: fmt.Sprintf("fixture_not_found name=%s available=%s", fixture, strings.Join(fixtureNames(cfg), ",")),
				Err:    fmt.Errorf("fixture_not_found"),
			}, warn
		}
		// A live instance from a previous run would have the database open
		// while the fixture rewrites it. Killing it is best-effort: if
		// nothing was running the terminate fails and it does not matter,
		// and there is no cheap way to tell that apart from a real failure
		// without reading the driver's stderr, which is its own.
		c.terminateBeforeFixture(ctx, cfg, run)
		for i, command := range commandsForFixture {
			if strings.TrimSpace(command) == "" {
				continue
			}
			step := launchStep{Name: "fixture", Command: command}
			if result := c.runLaunchCommand(ctx, cfg, run, step, env); result.Err != nil {
				result.Stderr = fmt.Sprintf("fixture %s step %d/%d failed: %s", fixture, i+1, len(commandsForFixture), result.Stderr)
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

func buildSkippedAppMissing(detail string, cause error) CommandResult {
	if detail == "" && cause != nil {
		detail = cause.Error()
	}
	return CommandResult{
		Stderr: "build was skipped (--skip-build) and no built app was found: " + detail + "; next: rerun the same command without --skip-build to build it once",
		Err:    errBuildSkippedAppMissing,
	}
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
	// The recipe's app_path step is what resolves where the bundle ended
	// up, and for a macOS target that is its identity: the driver has no
	// UDID to talk to, it has a path to run.
	cfg.AppPath = appPath
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
	platform := "ios"
	if targetKind(cfg) == drivers.KindMac {
		platform = "macos"
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
		"MAV_PLATFORM":    platform,
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

// terminateBeforeFixture closes the app before a fixture rewrites its
// state. Best-effort on purpose: the common case is that nothing was
// running, and failing there would be absurd.
func (c CLI) terminateBeforeFixture(ctx context.Context, cfg Config, run RunState) {
	if cfg.BundleID == "" {
		return
	}
	target := targetFromConfig(cfg)
	driver, _, err := c.router().Route(ctx, drivers.CapTerminate, target, "")
	if err != nil {
		return
	}
	utility, ok := driver.(drivers.DeviceUtilityDriver)
	if !ok {
		return
	}
	if err := utility.Terminate(ctx, target, cfg.BundleID); err != nil {
		appendFile(run.LogsPath, "mav fixture: terminate before seeding did not run: "+err.Error()+"\n")
	}
}

func fixtureNames(cfg Config) []string {
	names := make([]string, 0, len(cfg.Fixtures))
	for name := range cfg.Fixtures {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
