package mav

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type CLI struct {
	Runner Runner
	Stdout io.Writer
	Stderr io.Writer
	Root   string
}

type GlobalOptions struct {
	Verbose bool
	Raw     bool
	Help    bool
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	cli := CLI{Runner: ExecRunner{}, Stdout: stdout, Stderr: stderr, Root: root}
	return cli.Run(ctx, args)
}

func (c CLI) Run(ctx context.Context, args []string) error {
	opts, rest := parseGlobal(args)
	if len(rest) == 0 {
		return c.help(opts, "")
	}
	if rest[0] == "help" {
		topic := ""
		if len(rest) > 1 {
			topic = rest[1]
		}
		if len(rest) > 2 {
			topic = strings.Join(rest[1:], " ")
		}
		return c.help(opts, topic)
	}
	if opts.Help {
		if rest[0] == "ui" && len(rest) > 1 {
			return c.help(opts, "ui "+rest[1])
		}
		return c.help(opts, rest[0])
	}
	switch rest[0] {
	case "doctor":
		return c.doctor(ctx, opts)
	case "setup":
		return c.setup(ctx, opts, rest[1:])
	case "install-skills":
		return c.installSkills(ctx)
	case "sim":
		return c.sim(ctx, opts, rest[1:])
	case "device":
		return c.device(ctx, opts, rest[1:])
	case "open":
		return c.open(ctx, opts, rest[1:])
	case "ui":
		return c.ui(ctx, opts, rest[1:])
	case "capture":
		return c.capture(ctx, opts, rest[1:])
	case "run":
		return c.runFlow(ctx, opts, rest[1:])
	case "go":
		return c.goScreen(ctx, opts, rest[1:])
	case "logs":
		return c.logs(opts, rest[1:])
	case "stop":
		return c.stop(ctx, opts, rest[1:])
	case "crashes":
		return c.crashes(ctx, opts, rest[1:])
	case "evidence":
		return c.evidence(opts, rest[1:])
	default:
		return Fail("unknown_command", map[string]string{"command": rest[0]}).Write(c.Stdout)
	}
}

func parseGlobal(args []string) (GlobalOptions, []string) {
	opts := GlobalOptions{}
	rest := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--verbose":
			opts.Verbose = true
		case "--raw":
			opts.Raw = true
		case "--help", "-h":
			opts.Help = true
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest
}

func (c CLI) help(opts GlobalOptions, topic string) error {
	help := helpText(topic)
	_, err := fmt.Fprint(c.Stdout, help)
	return err
}

func helpText(topic string) string {
	switch topic {
	case "", "mav":
		return `mav - Mobile Agent Verifier for iOS apps

Usage:
  mav <command> [flags]
  mav help [command]

Commands:
  doctor      Check required tools.
  setup       Configure the project or install helper tools.
  install-skills
              Install the MAV agent skill globally for all supported agents.
  sim         List, select, or boot simulators.
  device      List or select real iOS devices.
  open        Build, install, launch, and start run logs.
  ui          Inspect and control the current UI.
  capture     Capture the current screen.
  run         Execute a native MAV YAML flow.
  go          Navigate from app launch to a mapped screen with evidence.
  logs        Read captured run logs.
  stop        Stop run-owned background processes.
  crashes     List crashes for the configured app.
  evidence    Start/step/stop/report evidence.

Global flags:
  --raw       Emit raw underlying tool output where supported.
  --verbose   Print extra debug details where supported.
  --help,-h   Show help.
`
	case "setup":
		return "Usage:\n  mav setup\n  mav setup --install axe idb appium\n"
	case "install-skills":
		return "Usage: mav install-skills\n"
	case "sim":
		return `Usage:
  mav sim list
  mav sim select --device "iPhone 17 Pro Max" --ios 26 [--locale es_ES] [--language es]
  mav sim select --udid <simulator-udid>
  mav sim boot
`
	case "device":
		return `Usage:
  mav device list
  mav device select --id <coredevice-id>
  mav device select --name <device-name>
`
	case "open":
		return `Usage:
  mav open [--target simulator|device] [--device NAME] [--ios VERSION] [--udid UDID] [--device-id ID] [--locale LOCALE] [--language LANG]
`
	case "ui":
		return `Usage:
  mav ui tree
  mav ui tap --id ID
  mav ui tap --x X --y Y
  mav ui tap --text TEXT
  mav ui type TEXT
  mav ui swipe [--direction up|down|left|right]
  mav ui pinch --x X --y Y --scale SCALE [--pan-x DX] [--pan-y DY] [--distance D] [--angle DEG] [--rotate DEG] [--duration 800ms] [--hold DURATION]
  mav ui rotate --x X --y Y --degrees DEG [--distance D] [--duration 800ms] [--hold DURATION]
  mav ui twoFingerPan --x X --y Y --pan-x DX --pan-y DY [--distance D] [--angle DEG] [--duration 800ms] [--hold DURATION]
  mav ui actions --file actions.json
  mav ui wait --id ID [--timeout 5s]
  mav ui scrollUntil --id ID [--direction up] [--max-swipes 5]
`
	case "capture":
		return "Usage: mav capture [--name NAME] [--run RUN_ID]\n"
	case "ui tree":
		return "Usage: mav ui tree\n\nPrints compact screen metadata followed by bounded node lines with id, label, role, value, and frame when available.\n"
	case "ui swipe":
		return "Usage: mav ui swipe [--direction up|down|left|right] [--start-x X --start-y Y --end-x X --end-y Y]\n"
	case "ui pinch":
		return "Usage: mav ui pinch --x X --y Y --scale SCALE [--pan-x DX] [--pan-y DY] [--distance D] [--angle DEG] [--rotate DEG] [--duration 800ms] [--hold DURATION]\n"
	case "ui rotate":
		return "Usage: mav ui rotate --x X --y Y --degrees DEG [--distance D] [--duration 800ms] [--hold DURATION]\n"
	case "ui twoFingerPan":
		return "Usage: mav ui twoFingerPan --x X --y Y --pan-x DX --pan-y DY [--distance D] [--angle DEG] [--duration 800ms] [--hold DURATION]\n"
	case "run":
		return "Usage: mav run flow.yaml\n"
	case "go":
		return "Usage: mav go <screen-id>\n"
	case "logs":
		return "Usage: mav logs [--run RUN_ID] [--key KEY] [--contains TEXT] [--level LEVEL] [--raw]\n"
	case "stop":
		return "Usage: mav stop [--run RUN_ID]\n"
	case "crashes":
		return "Usage: mav crashes [--raw]\n"
	case "evidence":
		return `Usage:
  mav evidence start [--run RUN_ID]
  mav evidence step --name NAME [--note NOTE] [--run RUN_ID]
  mav evidence stop [--note NOTE] [--no-capture] [--run RUN_ID]
  mav evidence report [--run RUN_ID]
`
	default:
		return "Unknown help topic. Run: mav help\n"
	}
}

func (c CLI) doctor(ctx context.Context, opts GlobalOptions) error {
	cfg, _ := LoadConfig(c.Root)
	if cfg.Root == "" {
		cfg = DefaultConfig(c.Root)
	}
	caps := c.resolveCapabilities(ctx, cfg)
	tools := caps.Tools
	fields := caps.fields()
	missing := []string{}
	nextHint := ""
	requiredTools := []string{"axe", "idb"}
	if isDeviceTarget(cfg) {
		requiredTools = []string{"idb"}
		fields["target_type"] = "device"
	}
	for _, tool := range requiredTools {
		if !tools[tool] {
			missing = append(missing, tool)
		}
	}
	if tools["appium"] {
		nodeCheck := checkAppiumNodePath(c.Runner)
		appiumReady := false
		if !nodeCheck.OK {
			fields["multitouch_issue"] = nodeCheck.Message
			fields["multitouch_next"] = nodeCheck.Next
			nextHint = nodeCheck.Next
		} else {
			driverStatus := appiumDriverStatus{}
			driverStatus = checkAppiumXCUITestDriver(ctx, c.Runner)
			if driverStatus.OK {
				appiumReady = true
			} else if driverStatus.NodeMismatch || driverStatus.HomePermission {
				fields["multitouch_issue"] = driverStatus.Message
				fields["multitouch_next"] = driverStatus.Next
				nextHint = driverStatus.Next
			} else {
				fields["multitouch_issue"] = driverStatus.Message
				missing = append(missing, "appium")
			}
		}
		if appiumReady {
			fields["multitouch"] = "ok"
			fields["multitouch_driver"] = "appium"
			delete(fields, "multitouch_next")
		}
	} else if tools["node"] && tools["npm"] {
		missing = append(missing, "appium")
	}
	if nextHint != "" {
		fields["next"] = nextHint
	} else if len(missing) > 0 {
		missing = uniqueStrings(missing)
		fields["next"] = "mav setup --install " + strings.Join(missing, " ")
	}
	return OK("doctor", fields).Write(c.Stdout)
}

func (c CLI) setup(ctx context.Context, opts GlobalOptions, args []string) error {
	install := flagValue(args, "--install")
	if install == "" {
		return c.setupProject(opts)
	}
	tools := strings.Fields(install)
	if len(tools) == 0 {
		return Fail("setup_install_missing", nil).Write(c.Stdout)
	}
	commands := map[string][]string{
		"axe": {"brew", "install", "cameroncooke/axe/axe"},
	}
	for _, tool := range tools {
		if tool == "appium" {
			ok, err := c.setupAppium(ctx, opts)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			continue
		}
		if tool == "idb" {
			ok, err := c.setupIDB(ctx, opts)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			continue
		}
		cmd, ok := commands[tool]
		if !ok {
			return Fail("setup_unknown_tool", map[string]string{"tool": tool}).Write(c.Stdout)
		}
		if opts.Verbose {
			fmt.Fprintln(c.Stderr, strings.Join(cmd, " "))
		}
		result := c.Runner.Run(ctx, cmd[0], cmd[1:]...)
		if result.Err != nil {
			return Fail("setup_failed", map[string]string{"tool": tool, "stderr": firstLine(result.Stderr)}).Write(c.Stdout)
		}
	}
	return OK("setup", map[string]string{"installed": strings.Join(tools, ",")}).Write(c.Stdout)
}

func (c CLI) setupIDB(ctx context.Context, opts GlobalOptions) (bool, error) {
	if _, err := c.Runner.LookPath("pipx"); err == nil {
		python := ""
		if _, err := c.Runner.LookPath("python3.12"); err == nil {
			python = "python3.12"
		} else if _, err := c.Runner.LookPath("python3.13"); err == nil {
			python = "python3.13"
		}
		if python == "" {
			return false, Fail("setup_failed", map[string]string{"tool": "idb", "stderr": "supported Python missing", "next": "install Python 3.12, then rerun mav setup --install idb"}).Write(c.Stdout)
		}
		cmd := []string{"pipx", "install", "--python", python, "fb-idb"}
		if opts.Verbose {
			fmt.Fprintln(c.Stderr, strings.Join(cmd, " "))
		}
		result := c.Runner.Run(ctx, cmd[0], cmd[1:]...)
		if result.Err != nil {
			return false, Fail("setup_failed", map[string]string{"tool": "idb", "stderr": firstLine(result.Stderr), "next": "pipx install --python python3.12 fb-idb"}).Write(c.Stdout)
		}
	}
	cmd := []string{"brew", "install", "idb-companion"}
	if opts.Verbose {
		fmt.Fprintln(c.Stderr, strings.Join(cmd, " "))
	}
	result := c.Runner.Run(ctx, cmd[0], cmd[1:]...)
	if result.Err != nil {
		return false, Fail("setup_failed", map[string]string{"tool": "idb", "stderr": firstLine(result.Stderr), "next": "install pipx and Python 3.12, then rerun mav setup --install idb"}).Write(c.Stdout)
	}
	return true, nil
}

func (c CLI) setupAppium(ctx context.Context, opts GlobalOptions) (bool, error) {
	if _, err := c.Runner.LookPath("npm"); err != nil {
		return false, Fail("setup_failed", map[string]string{"tool": "appium", "stderr": "npm missing", "next": "install Node.js/npm, then rerun mav setup --install appium"}).Write(c.Stdout)
	}
	commands := [][]string{
		{"npm", "install", "-g", "appium"},
		{"appium", "driver", "install", "xcuitest"},
	}
	for _, cmd := range commands {
		if opts.Verbose {
			fmt.Fprintln(c.Stderr, strings.Join(cmd, " "))
		}
		result := c.Runner.Run(ctx, cmd[0], cmd[1:]...)
		if result.Err != nil {
			return false, Fail("setup_failed", map[string]string{"tool": "appium", "stderr": firstLine(result.Stderr)}).Write(c.Stdout)
		}
	}
	nodeCheck := checkAppiumNodePath(c.Runner)
	if !nodeCheck.OK {
		return false, Fail("setup_failed", map[string]string{"tool": "appium", "stderr": nodeCheck.Message, "next": nodeCheck.Next}).Write(c.Stdout)
	}
	driverStatus := checkAppiumXCUITestDriver(ctx, c.Runner)
	if !driverStatus.OK {
		fields := map[string]string{"tool": "appium", "stderr": driverStatus.Message}
		if driverStatus.Next != "" {
			fields["next"] = driverStatus.Next
		}
		return false, Fail("setup_failed", fields).Write(c.Stdout)
	}
	if cfg, err := LoadConfig(c.Root); err == nil {
		if cfg.Tools == nil {
			cfg.Tools = map[string]bool{}
		}
		cfg.Tools["appium"] = true
		_ = SaveConfig(c.Root, cfg)
	}
	return true, nil
}

func (c CLI) installSkills(ctx context.Context) error {
	if _, err := c.Runner.LookPath("npx"); err != nil {
		return Fail("install_skills_unavailable", map[string]string{
			"tool": "npx",
			"next": "install Node.js/npm so npx is available, then rerun mav install-skills",
		}).Write(c.Stdout)
	}
	args := []string{"skills", "add", "bitomule/mav", "--skill", "mav", "--global", "--yes"}
	result := c.Runner.Run(ctx, "npx", args...)
	if result.Err != nil {
		fields := map[string]string{
			"code": strconv.Itoa(result.Code),
			"next": "rerun npx skills add bitomule/mav --skill mav --global --yes",
		}
		if stderr := firstLine(result.Stderr); stderr != "" {
			fields["stderr"] = stderr
		}
		if stdout := firstLine(result.Stdout); stdout != "" {
			fields["stdout"] = stdout
		}
		return Fail("install_skills_failed", fields).Write(c.Stdout)
	}
	return OK("install-skills", map[string]string{"skill": "mav", "scope": "global"}).Write(c.Stdout)
}

func (c CLI) setupProject(opts GlobalOptions) error {
	cfg, err := SetupConfig(c.Root, c.Runner)
	if existing, loadErr := LoadConfig(c.Root); loadErr == nil {
		existingHadLaunch := existing.Launch.Mode != "" || hasLaunchCommands(existing.Launch.Commands)
		cfg = mergeSetupConfig(existing, cfg)
		if isDeviceTarget(cfg) && !existingHadLaunch {
			if candidate, ok := selectLaunchCandidate(c.Root, cfg); ok {
				cfg.Launch = candidate.Launch
			} else {
				cfg.Launch = defaultLaunchConfig(cfg)
			}
		}
	}
	if saveErr := SaveConfig(c.Root, cfg); saveErr != nil {
		return saveErr
	}
	if !exists(filepath.Join(c.Root, MapIndexFile)) && !exists(filepath.Join(c.Root, AppMapFile)) {
		_ = SaveAppMap(c.Root, DefaultAppMap(cfg.BundleID))
	}
	fields := map[string]string{
		"config":      filepath.Join(c.Root, ConfigFile),
		"target":      cfg.AppTarget,
		"bundle":      cfg.BundleID,
		"target_type": normalizeTargetType(cfg.TargetType),
	}
	if hasLaunchCommands(cfg.Launch.Commands) {
		fields["launch_recipe"] = "ok"
		if cfg.Launch.Mode != "" {
			fields["launch_mode"] = cfg.Launch.Mode
		}
	} else {
		fields["launch_recipe"] = "missing"
	}
	if err != nil {
		fields["warning"] = err.Error()
	}
	return OK("setup", fields).Write(c.Stdout)
}

func (c CLI) sim(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("sim_command_missing", map[string]string{"usage": "mav sim list|select|boot"}).Write(c.Stdout)
	}
	switch args[0] {
	case "list":
		sims, err := ListSimulators(c.Runner)
		if err != nil {
			fields := map[string]string{"error": err.Error()}
			addSandboxNext(fields, err.Error())
			return Fail("sim_list_failed", fields).Write(c.Stdout)
		}
		if err := OK("sim.list", map[string]string{"count": strconv.Itoa(len(sims))}).Write(c.Stdout); err != nil {
			return err
		}
		for _, sim := range sims {
			fmt.Fprintf(c.Stdout, "sim udid=%s name=%q runtime=%s state=%s\n", sim.UDID, sim.Name, sim.Runtime, sim.State)
		}
		return nil
	case "select":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
		}
		sims, err := ListSimulators(c.Runner)
		if err != nil {
			return Fail("sim_list_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
		}
		sim, ok := SelectSimulator(sims, flagValue(args[1:], "--device"), flagValue(args[1:], "--ios"), flagValue(args[1:], "--udid"))
		if !ok {
			return Fail("sim_not_found", map[string]string{"device": flagValue(args[1:], "--device"), "ios": flagValue(args[1:], "--ios"), "udid": flagValue(args[1:], "--udid")}).Write(c.Stdout)
		}
		cfg.SimulatorUDID = sim.UDID
		cfg.SimulatorName = sim.Name
		cfg.SimulatorRuntime = sim.Runtime
		if locale := flagValue(args[1:], "--locale"); locale != "" {
			cfg.Locale = locale
		}
		if language := flagValue(args[1:], "--language"); language != "" {
			cfg.Language = language
		}
		if err := SaveConfig(c.Root, cfg); err != nil {
			return err
		}
		return OK("sim.select", map[string]string{"udid": sim.UDID, "name": sim.Name, "runtime": sim.Runtime}).Write(c.Stdout)
	case "boot":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
		}
		if cfg.SimulatorUDID == "" {
			return Fail("sim_not_selected", map[string]string{"next": "mav sim select --device 'iPhone' --ios 26"}).Write(c.Stdout)
		}
		boot := c.Runner.Run(ctx, "xcrun", "simctl", "boot", cfg.SimulatorUDID)
		if boot.Err != nil && !strings.Contains(boot.Stderr, "Unable to boot device in current state") {
			return Fail("sim_boot_failed", map[string]string{"stderr": firstLine(boot.Stderr)}).Write(c.Stdout)
		}
		status := c.Runner.Run(ctx, "xcrun", "simctl", "bootstatus", cfg.SimulatorUDID, "-b")
		if status.Err != nil {
			return Fail("sim_bootstatus_failed", map[string]string{"stderr": firstLine(status.Stderr)}).Write(c.Stdout)
		}
		return OK("sim.boot", map[string]string{"udid": cfg.SimulatorUDID, "name": cfg.SimulatorName}).Write(c.Stdout)
	default:
		return Fail("sim_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
}

func (c CLI) device(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("device_command_missing", map[string]string{"usage": "mav device list|select"}).Write(c.Stdout)
	}
	switch args[0] {
	case "list":
		devices, err := ListPhysicalDevices(c.Runner)
		if err != nil {
			fields := map[string]string{"error": err.Error()}
			addSandboxNext(fields, err.Error())
			return Fail("device_list_failed", fields).Write(c.Stdout)
		}
		if err := OK("device.list", map[string]string{"count": strconv.Itoa(len(devices))}).Write(c.Stdout); err != nil {
			return err
		}
		for _, device := range devices {
			fmt.Fprintf(c.Stdout, "device id=%s udid=%s name=%q model=%q os=%q state=%s\n", device.Identifier, device.UDID, device.Name, device.Model, device.OS, device.State)
		}
		return nil
	case "select":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
		}
		devices, err := ListPhysicalDevices(c.Runner)
		if err != nil {
			return Fail("device_list_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
		}
		device, ok := SelectPhysicalDevice(devices, flagValue(args[1:], "--id"), flagValue(args[1:], "--name"))
		if !ok {
			return Fail("device_not_found", map[string]string{"id": flagValue(args[1:], "--id"), "name": flagValue(args[1:], "--name")}).Write(c.Stdout)
		}
		applyPhysicalDevice(&cfg, device)
		if locale := flagValue(args[1:], "--locale"); locale != "" {
			cfg.Locale = locale
		}
		if language := flagValue(args[1:], "--language"); language != "" {
			cfg.Language = language
		}
		if err := SaveConfig(c.Root, cfg); err != nil {
			return err
		}
		return OK("device.select", map[string]string{"id": device.Identifier, "udid": device.UDID, "name": device.Name, "model": device.Model, "os": device.OS}).Write(c.Stdout)
	default:
		return Fail("device_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
}

func applyPhysicalDevice(cfg *Config, device PhysicalDevice) {
	cfg.TargetType = "device"
	cfg.DeviceIdentifier = device.Identifier
	cfg.DeviceUDID = device.UDID
	cfg.DeviceName = device.Name
	cfg.DeviceModel = device.Model
	cfg.DeviceOS = device.OS
}

func (c CLI) open(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	if err := c.applyOpenTargetOverrides(ctx, &cfg, args); err != nil {
		return Fail("sim_select_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if existing, err := LoadRun(c.Root, ""); err == nil && existing.ID != "" {
		_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", existing.ID})
	}
	run, err := NewRunState()
	if err != nil {
		return err
	}
	if err := SaveCurrentRun(c.Root, run); err != nil {
		return err
	}
	probeLogPID, probeLogErr := c.startProbeLogs(ctx, cfg, run)
	if probeLogErr != nil {
		appendFile(run.LogsPath, "mav probe log capture failed: "+probeLogErr.Error()+"\n")
	}
	appPath, failedStep, failedResult := c.runLaunchRecipe(ctx, cfg, run)
	if failedStep != nil {
		fields := map[string]string{"run": run.ID, "logs": run.LogsPath, "step": failedStep.Name, "stderr": firstLine(failedResult.Stderr)}
		if fields["stderr"] == "" {
			fields["stderr"] = failedResult.Err.Error()
		}
		return Fail("launch_step_failed", fields).Write(c.Stdout)
	}
	fields := map[string]string{"run": run.ID, "logs": run.LogsPath, "dir": run.Dir}
	if appPath != "" {
		fields["app"] = appPath
	}
	if probeLogPID > 0 {
		fields["probe_log_pid"] = strconv.Itoa(probeLogPID)
		fields["log_subsystem"] = probeLogSubsystem(cfg)
		fields["log_category"] = probeLogCategory(cfg)
	}
	fields["target"] = targetDisplayName(cfg)
	fields["target_type"] = normalizeTargetType(cfg.TargetType)
	if isDeviceTarget(cfg) {
		fields["device_id"] = cfg.DeviceIdentifier
		fields["device_udid"] = cfg.DeviceUDID
	}
	if m, err := EnsureAppMap(c.Root, cfg); err == nil {
		SetCurrentScreen(c.Root, m.Start, run.ID)
		ClearPendingMapAction(c.Root)
	}
	return OK("open", fields).Write(c.Stdout)
}

func (c CLI) applyOpenTargetOverrides(ctx context.Context, cfg *Config, args []string) error {
	targetType := flagValue(args, "--target")
	device := flagValue(args, "--device")
	ios := flagValue(args, "--ios")
	udid := flagValue(args, "--udid")
	deviceID := flagValue(args, "--device-id")
	if locale := flagValue(args, "--locale"); locale != "" {
		cfg.Locale = locale
	}
	if language := flagValue(args, "--language"); language != "" {
		cfg.Language = language
	}
	if targetType != "" {
		normalizedTargetType := normalizeTargetType(targetType)
		switch normalizedTargetType {
		case "device":
			cfg.TargetType = "device"
		case "simulator":
			cfg.TargetType = "simulator"
		}
		if normalizedTargetType != strings.ToLower(strings.TrimSpace(targetType)) {
			return fmt.Errorf("target_invalid")
		}
	}
	if deviceID != "" || normalizeTargetType(targetType) == "device" {
		devices, err := ListPhysicalDevices(c.Runner)
		if err != nil {
			return err
		}
		selected, ok := SelectPhysicalDevice(devices, deviceID, device)
		if !ok && deviceID == "" && cfg.DeviceIdentifier != "" {
			selected, ok = SelectPhysicalDevice(devices, cfg.DeviceIdentifier, "")
		}
		if !ok {
			return fmt.Errorf("device_not_found")
		}
		applyPhysicalDevice(cfg, selected)
		return SaveConfig(c.Root, *cfg)
	}
	if device == "" && ios == "" && udid == "" && targetType == "" {
		return nil
	}
	sims, err := ListSimulators(c.Runner)
	if err != nil {
		return err
	}
	sim, ok := SelectSimulator(sims, device, ios, udid)
	if !ok {
		return fmt.Errorf("sim_not_found")
	}
	cfg.SimulatorUDID = sim.UDID
	cfg.SimulatorName = sim.Name
	cfg.SimulatorRuntime = sim.Runtime
	cfg.TargetType = "simulator"
	boot := c.Runner.Run(ctx, "xcrun", "simctl", "boot", sim.UDID)
	if boot.Err != nil && !strings.Contains(boot.Stderr, "Unable to boot device in current state") {
		return fmt.Errorf("%s", firstLine(boot.Stderr))
	}
	_ = c.Runner.Run(ctx, "xcrun", "simctl", "bootstatus", sim.UDID, "-b")
	return SaveConfig(c.Root, *cfg)
}

func simctlLaunchLanguageArgs(cfg Config) []string {
	args := []string{}
	if cfg.Language != "" {
		args = append(args, "-AppleLanguages", "("+cfg.Language+")")
		if cfg.Locale != "" {
			args = append(args, "-AppleLocale", cfg.Locale)
		}
	} else if cfg.Locale != "" {
		args = append(args, "-AppleLocale", cfg.Locale)
	}
	return args
}

func (c CLI) startProbeLogs(ctx context.Context, cfg Config, run RunState) (int, error) {
	subsystem := probeLogSubsystem(cfg)
	category := probeLogCategory(cfg)
	predicate := fmt.Sprintf(`subsystem == "%s" AND category == "%s"`, subsystem, category)
	if !isDeviceTarget(cfg) && hasTool(cfg, "xcrun") && cfg.SimulatorUDID != "" {
		args := []string{"simctl", "spawn", cfg.SimulatorUDID, "log", "stream", "--style", "compact", "--level", "debug", "--predicate", predicate}
		pid, err := c.Runner.Start(ctx, run.LogsPath, "xcrun", args...)
		if err == nil {
			appendProcess(run, "probe-logs", pid, "xcrun "+strings.Join(args, " "))
		}
		return pid, err
	}
	if hasTool(cfg, "idb") {
		args := []string{"log"}
		if cfg.SimulatorUDID != "" {
			args = append(args, "--udid", cfg.SimulatorUDID)
		}
		args = append(args, "--", "--style", "compact", "--level", "debug", "--predicate", predicate)
		pid, err := c.Runner.Start(ctx, run.LogsPath, "idb", args...)
		if err == nil {
			appendProcess(run, "probe-logs", pid, "idb "+strings.Join(args, " "))
		}
		return pid, err
	}
	if isDeviceTarget(cfg) {
		return 0, fmt.Errorf("log_tool_missing")
	}
	if !hasTool(cfg, "xcrun") {
		return 0, fmt.Errorf("log_tool_missing")
	}
	args := []string{"simctl", "spawn", "booted", "log", "stream", "--style", "compact", "--level", "debug", "--predicate", predicate}
	pid, err := c.Runner.Start(ctx, run.LogsPath, "xcrun", args...)
	if err == nil {
		appendProcess(run, "probe-logs", pid, "xcrun "+strings.Join(args, " "))
	}
	return pid, err
}

func (c CLI) ui(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("ui_command_missing", map[string]string{"usage": "mav ui tree|tap|type|swipe|pinch|rotate|twoFingerPan|actions|wait|scrollUntil"}).Write(c.Stdout)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	switch args[0] {
	case "tree":
		return c.uiTree(ctx, opts, cfg)
	case "tap":
		return c.uiTap(ctx, opts, cfg, args[1:])
	case "type":
		return c.uiType(ctx, opts, cfg, args[1:])
	case "swipe":
		return c.uiSwipe(ctx, opts, cfg, args[1:])
	case "pinch":
		return c.uiPinch(ctx, opts, cfg, args[1:])
	case "rotate":
		return c.uiRotate(ctx, opts, cfg, args[1:])
	case "twoFingerPan":
		return c.uiTwoFingerPan(ctx, opts, cfg, args[1:])
	case "actions":
		return c.uiActions(ctx, opts, cfg, args[1:])
	case "wait":
		return c.uiWait(ctx, opts, cfg, args[1:])
	case "scrollUntil":
		return c.uiScrollUntil(ctx, opts, args[1:])
	default:
		return Fail("ui_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
}

func (c CLI) uiTree(ctx context.Context, opts GlobalOptions, cfg Config) error {
	driver, result, err := c.describeUITree(ctx, cfg)
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "axe|idb", "next": "mav setup --install axe idb"}).Write(c.Stdout)
	}
	recovered := false
	if result.Err == nil && isEmptyAXTree(result.Stdout) {
		if err := c.recoverEmptyAXTree(ctx, cfg); err == nil {
			if retryDriver, retryResult, retryErr := c.describeUITree(ctx, cfg); retryErr == nil {
				driver = retryDriver
				result = retryResult
				recovered = true
			}
		}
	}
	if opts.Raw {
		fmt.Fprint(c.Stdout, result.Stdout)
		return nil
	}
	if result.Err != nil {
		fields := map[string]string{"stderr": firstLine(result.Stderr)}
		addSandboxNext(fields, result.Stderr)
		return Fail("ui_tree_failed", fields).Write(c.Stdout)
	}
	if isEmptyAXTree(result.Stdout) {
		return Fail("ui_tree_empty", map[string]string{"driver": driver, "reason": "simulator_accessibility_unavailable", "recovered": strconv.FormatBool(recovered)}).Write(c.Stdout)
	}
	screen := "unknown"
	screenSource := ""
	previousScreen := ""
	elements := ExtractElements(result.Stdout)
	if run, err := LoadRun(c.Root, ""); err == nil {
		if observed, err := ObserveScreenDetailed(c.Root, cfg, run, result.Stdout); err == nil && observed.Screen != "" {
			screen = observed.Screen
			screenSource = observed.Source
			previousScreen = observed.PreviousScreen
			elements = observed.Elements
		}
	}
	nodes := countTreeNodes(result.Stdout)
	if nodes == 0 {
		nodes = strings.Count(result.Stdout, "\n")
	}
	fields := map[string]string{"driver": driver, "nodes": strconv.Itoa(nodes), "screen": screen}
	if screenSource != "" {
		fields["screen_source"] = screenSource
	}
	if screenSource == "recognized" || screenSource == "inferred" || screenSource == "start" {
		fields["recognized_screen"] = screen
		fields["screen_confidence"] = screenConfidence(screenSource)
		fields["screen"] = "unknown"
	}
	if previousScreen != "" && previousScreen != screen {
		fields["previous_screen"] = previousScreen
	}
	if screen == "unknown" {
		if pending, ok := peekPendingMapAction(c.Root); ok {
			fields["map_pending"] = "true"
			if pending.From != "" {
				fields["previous_screen"] = pending.From
			}
			fields["next"] = "add accessibility ids or capture/inspect before mapping"
		}
	}
	if recovered {
		fields["recovered"] = "true"
	}
	if err := OK("ui.tree", fields).Write(c.Stdout); err != nil {
		return err
	}
	return writeElementLines(c.Stdout, elements)
}

func screenConfidence(source string) string {
	switch source {
	case "recognized":
		return "0.80"
	case "inferred":
		return "0.55"
	case "start":
		return "0.40"
	default:
		return "1.00"
	}
}

func (c CLI) describeUITree(ctx context.Context, cfg Config) (string, CommandResult, error) {
	if !isDeviceTarget(cfg) && hasTool(cfg, "axe") {
		return "axe", c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "describe-ui")...), nil
	}
	if hasTool(cfg, "idb") {
		return "idb", c.Runner.Run(ctx, "idb", idbTargetArgs(cfg, "ui", "describe-all", "--json", "--nested")...), nil
	}
	return "", CommandResult{}, fmt.Errorf("tree_tool_missing")
}

func (c CLI) recoverEmptyAXTree(ctx context.Context, cfg Config) error {
	if isDeviceTarget(cfg) {
		return fmt.Errorf("device_recovery_unavailable")
	}
	if !hasTool(cfg, "xcrun") || cfg.SimulatorUDID == "" {
		return fmt.Errorf("simulator_recovery_unavailable")
	}
	run, _ := LoadRun(c.Root, "")
	shutdown := c.Runner.Run(ctx, "xcrun", "simctl", "shutdown", cfg.SimulatorUDID)
	if run.ID != "" {
		appendCommand(run, "xcrun simctl shutdown "+cfg.SimulatorUDID, shutdown)
	}
	boot := c.Runner.Run(ctx, "xcrun", "simctl", "boot", cfg.SimulatorUDID)
	if run.ID != "" {
		appendCommand(run, "xcrun simctl boot "+cfg.SimulatorUDID, boot)
	}
	if boot.Err != nil && !strings.Contains(boot.Stderr, "Unable to boot device in current state") {
		return fmt.Errorf("sim_boot_failed")
	}
	status := c.Runner.Run(ctx, "xcrun", "simctl", "bootstatus", cfg.SimulatorUDID, "-b")
	if run.ID != "" {
		appendCommand(run, "xcrun simctl bootstatus "+cfg.SimulatorUDID+" -b", status)
	}
	if status.Err != nil {
		return fmt.Errorf("sim_bootstatus_failed")
	}
	if run.ID != "" {
		if pid, err := c.startProbeLogs(ctx, cfg, run); err == nil && pid > 0 {
			appendFile(run.LogsPath, fmt.Sprintf("mav restarted probe log capture pid=%d after accessibility recovery\n", pid))
		}
	}
	if cfg.BundleID != "" {
		args := []string{"simctl", "launch", cfg.SimulatorUDID, cfg.BundleID}
		args = append(args, simctlLaunchLanguageArgs(cfg)...)
		launch := c.Runner.Run(ctx, "xcrun", args...)
		if run.ID != "" {
			appendCommand(run, "xcrun "+strings.Join(args, " "), launch)
		}
		if launch.Err != nil {
			return fmt.Errorf("launch_failed")
		}
		time.Sleep(1200 * time.Millisecond)
	}
	return nil
}

func (c CLI) waitForTreeReady(ctx context.Context, cfg Config, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	recovered := false
	for time.Now().Before(deadline) {
		_, result, err := c.describeUITree(ctx, cfg)
		if err != nil {
			return "", err
		}
		if result.Err == nil && isEmptyAXTree(result.Stdout) && !recovered {
			_ = c.recoverEmptyAXTree(ctx, cfg)
			recovered = true
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if result.Err == nil && !isEmptyAXTree(result.Stdout) && countTreeNodes(result.Stdout) > 1 && len(ExtractElements(result.Stdout)) > 0 {
			return result.Stdout, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("tree_not_ready")
}

func (c CLI) uiTap(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	id := flagValue(args, "--id")
	text := flagValue(args, "--text")
	x := flagValue(args, "--x")
	y := flagValue(args, "--y")
	caps := c.resolveCapabilities(ctx, cfg)
	if id != "" || text != "" {
		if isDeviceTarget(cfg) || !caps.Tools["axe"] {
			return Fail("tool_missing", map[string]string{"tool": "axe", "next": "use idb coordinates or simulator AXe"}).Write(c.Stdout)
		}
		axeArgs := axeTargetArgs(cfg, "tap")
		fields := map[string]string{}
		command := "mav ui tap"
		if id != "" {
			axeArgs = append(axeArgs, "--id", id)
			fields["id"] = id
			command += " --id " + id
		} else {
			axeArgs = append(axeArgs, "--label", text)
			fields["text"] = text
			command += " --text " + text
		}
		result := c.Runner.Run(ctx, "axe", axeArgs...)
		if result.Err != nil {
			return Fail("ui_tap_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
		}
		c.appendCurrentCommand(command, result)
		c.recordPendingTap(id, text, "", "")
		return OK("ui.tap", fields).Write(c.Stdout)
	}
	if x != "" && y != "" {
		if !caps.CoordinateTap {
			fields := map[string]string{"tool": "idb"}
			if caps.IDBNext != "" {
				fields["next"] = caps.IDBNext
			}
			return Fail("tool_missing", fields).Write(c.Stdout)
		}
		result := c.Runner.Run(ctx, "idb", idbTargetArgs(cfg, "ui", "tap", x, y)...)
		if result.Err != nil {
			return Fail("ui_tap_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
		}
		c.appendCurrentCommand("mav ui tap --x "+x+" --y "+y, result)
		c.recordPendingTap("", "", x, y)
		return OK("ui.tap", map[string]string{"x": x, "y": y}).Write(c.Stdout)
	}
	return Fail("tap_target_missing", map[string]string{"usage": "mav ui tap --id ID | --x X --y Y | --text TEXT"}).Write(c.Stdout)
}

func (c CLI) recordPendingTap(id, text, x, y string) {
	from := CurrentScreen(c.Root)
	if from == "" {
		return
	}
	SetPendingMapAction(c.Root, pendingMapAction{From: from, ID: id, Text: text, X: x, Y: y})
}

func (c CLI) appendCurrentCommand(command string, result CommandResult) {
	run, err := LoadRun(c.Root, "")
	if err == nil && run.ID != "" {
		appendCommand(run, command, result)
	}
}

func (c CLI) uiType(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	if len(args) == 0 {
		return Fail("type_text_missing", nil).Write(c.Stdout)
	}
	if isDeviceTarget(cfg) || !hasTool(cfg, "axe") {
		return Fail("tool_missing", map[string]string{"tool": "axe", "next": "use idb coordinates or simulator AXe"}).Write(c.Stdout)
	}
	axeArgs := axeTargetArgs(cfg, "type")
	axeArgs = append(axeArgs, strings.Join(args, " "))
	result := c.Runner.Run(ctx, "axe", axeArgs...)
	if result.Err != nil {
		return Fail("ui_type_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
	}
	return OK("ui.type", map[string]string{"chars": strconv.Itoa(len(strings.Join(args, " ")))}).Write(c.Stdout)
}

func (c CLI) uiSwipe(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	direction := flagValue(args, "--direction")
	if direction == "" && len(args) > 0 && isSwipeDirection(args[0]) {
		direction = args[0]
	}
	if direction == "" {
		direction = "up"
	}
	if !isSwipeDirection(direction) {
		return Fail("swipe_direction_invalid", map[string]string{"direction": direction, "usage": "mav ui swipe [--direction up|down|left|right] [--start-x X --start-y Y --end-x X --end-y Y]"}).Write(c.Stdout)
	}
	startX, startY, endX, endY := swipeCoordinates(direction)
	customCoordinates := false
	if value := flagValue(args, "--start-x"); value != "" {
		startX = value
		customCoordinates = true
	}
	if value := flagValue(args, "--start-y"); value != "" {
		startY = value
		customCoordinates = true
	}
	if value := flagValue(args, "--end-x"); value != "" {
		endX = value
		customCoordinates = true
	}
	if value := flagValue(args, "--end-y"); value != "" {
		endY = value
		customCoordinates = true
	}
	driver := "axe"
	var result CommandResult
	if !isDeviceTarget(cfg) && hasTool(cfg, "axe") {
		axeArgs := axeTargetArgs(cfg, "swipe", "--start-x", startX, "--start-y", startY, "--end-x", endX, "--end-y", endY)
		result = c.Runner.Run(ctx, "axe", axeArgs...)
	} else if hasTool(cfg, "idb") {
		driver = "idb"
		result = c.Runner.Run(ctx, "idb", idbTargetArgs(cfg, "ui", "swipe", startX, startY, endX, endY)...)
	} else {
		return Fail("tool_missing", map[string]string{"tool": "axe|idb"}).Write(c.Stdout)
	}
	if result.Err != nil {
		return Fail("ui_swipe_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
	}
	fields := map[string]string{"direction": direction, "driver": driver}
	if customCoordinates {
		fields["direction"] = "custom"
		fields["start_x"] = startX
		fields["start_y"] = startY
		fields["end_x"] = endX
		fields["end_y"] = endY
	}
	return OK("ui.swipe", fields).Write(c.Stdout)
}

func (c CLI) uiPinch(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	params := gestureParamsFromArgs(args)
	params.Kind = "pinch"
	actions, fields, err := buildGestureActions(params)
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if err := c.performAppiumActions(ctx, cfg, actions); err != nil {
		return c.writeAppiumGestureError(err)
	}
	waitForGestureCompletion(fields)
	fields["driver"] = "appium"
	c.appendCurrentCommand("mav ui pinch "+strings.Join(args, " "), CommandResult{})
	return OK("ui.pinch", fields).Write(c.Stdout)
}

func (c CLI) uiRotate(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	params := gestureParamsFromArgs(args)
	params.Kind = "rotate"
	actions, fields, err := buildGestureActions(params)
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if err := c.performAppiumActions(ctx, cfg, actions); err != nil {
		return c.writeAppiumGestureError(err)
	}
	waitForGestureCompletion(fields)
	fields["driver"] = "appium"
	c.appendCurrentCommand("mav ui rotate "+strings.Join(args, " "), CommandResult{})
	return OK("ui.rotate", fields).Write(c.Stdout)
}

func (c CLI) uiTwoFingerPan(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	params := gestureParamsFromArgs(args)
	params.Kind = "twoFingerPan"
	actions, fields, err := buildGestureActions(params)
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if err := c.performAppiumActions(ctx, cfg, actions); err != nil {
		return c.writeAppiumGestureError(err)
	}
	waitForGestureCompletion(fields)
	fields["driver"] = "appium"
	c.appendCurrentCommand("mav ui twoFingerPan "+strings.Join(args, " "), CommandResult{})
	return OK("ui.twoFingerPan", fields).Write(c.Stdout)
}

func (c CLI) uiActions(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	path := flagValue(args, "--file")
	if path == "" {
		return Fail("gesture_invalid", map[string]string{"error": "actions_file_missing"}).Write(c.Stdout)
	}
	actions, err := loadW3CActionsFile(c.Root, path)
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if err := c.performAppiumActions(ctx, cfg, actions); err != nil {
		return c.writeAppiumGestureError(err)
	}
	c.appendCurrentCommand("mav ui actions --file "+path, CommandResult{})
	return OK("ui.actions", map[string]string{"driver": "appium", "file": path}).Write(c.Stdout)
}

func (c CLI) writeAppiumGestureError(err error) error {
	if appErr, ok := err.(appiumError); ok {
		fields := map[string]string{"tool": "appium"}
		if appErr.Message != "" {
			fields["stderr"] = appErr.Message
		}
		if appErr.Next != "" {
			fields["next"] = appErr.Next
		}
		return Fail(appErr.Code, fields).Write(c.Stdout)
	}
	return Fail("ui_gesture_failed", map[string]string{"tool": "appium", "stderr": err.Error()}).Write(c.Stdout)
}

func (c CLI) uiWait(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	id := flagValue(args, "--id")
	if id == "" {
		return Fail("wait_target_missing", map[string]string{"usage": "mav ui wait --id ID"}).Write(c.Stdout)
	}
	timeout := 5 * time.Second
	if raw := flagValue(args, "--timeout"); raw != "" {
		if parsed := parseFlowDuration(raw, timeout); parsed > 0 {
			timeout = parsed
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		if isDeviceTarget(cfg) || !hasTool(cfg, "axe") {
			return Fail("tool_missing", map[string]string{"tool": "axe", "next": "use idb coordinates or simulator AXe"}).Write(c.Stdout)
		}
		result := c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "describe-ui")...)
		if result.Err == nil && strings.Contains(result.Stdout, id) {
			return OK("ui.wait", map[string]string{"id": id}).Write(c.Stdout)
		}
		if time.Now().After(deadline) {
			return Fail("ui_wait_timeout", map[string]string{"id": id}).Write(c.Stdout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (c CLI) uiScrollUntil(ctx context.Context, opts GlobalOptions, args []string) error {
	params := map[string]string{
		"id":        flagValue(args, "--id"),
		"text":      flagValue(args, "--text"),
		"value":     flagValue(args, "--value"),
		"direction": flagValue(args, "--direction"),
		"maxSwipes": flagValue(args, "--max-swipes"),
	}
	fields, err := c.scrollUntilFlowCondition(ctx, params)
	if err != nil {
		if fields == nil {
			fields = map[string]string{}
		}
		for key, value := range params {
			if value != "" {
				fields[key] = value
			}
		}
		return Fail(err.Error(), fields).Write(c.Stdout)
	}
	for key, value := range params {
		if value != "" {
			fields[key] = value
		}
	}
	return OK("ui.scrollUntil", fields).Write(c.Stdout)
}

func (c CLI) capture(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		run, err = NewRunState()
		if err != nil {
			return err
		}
		_ = SaveCurrentRun(c.Root, run)
	}
	path := uniqueCapturePath(run, flagValue(args, "--name"))
	result, err := c.captureScreenshot(ctx, cfg, path)
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "axe|idb|xcrun"}).Write(c.Stdout)
	}
	if result.Err != nil {
		fields := map[string]string{"stderr": firstLine(result.Stderr)}
		addSandboxNext(fields, result.Stderr)
		return Fail("capture_failed", fields).Write(c.Stdout)
	}
	_ = os.WriteFile(filepath.Join(run.Dir, "latest_capture.txt"), []byte(path+"\n"), 0o644)
	return OK("capture", map[string]string{"file": path, "run": run.ID}).Write(c.Stdout)
}

func uniqueCapturePath(run RunState, name string) string {
	dir := filepath.Join(run.Dir, "captures")
	base := safeFileName(name)
	if base == "step" && strings.TrimSpace(name) == "" {
		base = time.Now().UTC().Format("20060102T150405.000")
	}
	path := filepath.Join(dir, base+".png")
	if !exists(path) {
		return path
	}
	for i := 2; ; i++ {
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.png", base, i))
		if !exists(path) {
			return path
		}
	}
}

func (c CLI) captureScreenshot(ctx context.Context, cfg Config, path string) (CommandResult, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CommandResult{}, err
	}
	if !isDeviceTarget(cfg) && hasTool(cfg, "axe") {
		return c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "screenshot", "--output", path)...), nil
	}
	if hasTool(cfg, "idb") {
		return c.Runner.Run(ctx, "idb", idbTargetArgs(cfg, "screenshot", path)...), nil
	}
	if !isDeviceTarget(cfg) && hasTool(cfg, "xcrun") {
		target := cfg.SimulatorUDID
		if target == "" {
			target = "booted"
		}
		return c.Runner.Run(ctx, "xcrun", "simctl", "io", target, "screenshot", path), nil
	}
	return CommandResult{}, fmt.Errorf("capture_tool_missing")
}

func (c CLI) runFlow(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("flow_missing", map[string]string{"usage": "mav run flow.yaml"}).Write(c.Stdout)
	}
	flow, err := LoadFlow(args[0])
	if err != nil {
		return Fail("flow_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	run, err := c.currentOrNewRun()
	if err != nil {
		return err
	}
	start := time.Now()
	for index, step := range flow.Steps {
		stepStart := time.Now()
		fields, err := c.executeFlowStep(ctx, run, index+1, step)
		elapsed := time.Since(stepStart)
		if err != nil {
			failFields := map[string]string{
				"step":    strconv.Itoa(index + 1),
				"action":  step.Action,
				"code":    err.Error(),
				"elapsed": elapsed.String(),
				"run":     run.ID,
			}
			for key, value := range fields {
				failFields[key] = value
			}
			c.cleanupFailedFlow(ctx, run, failFields)
			return Fail(err.Error(), failFields).Write(c.Stdout)
		}
		if step.Action == "open" {
			if openedRun, err := LoadRun(c.Root, ""); err == nil && openedRun.ID != "" {
				run = openedRun
				fields["run"] = run.ID
			}
		}
		appendFlowStep(run, index+1, step.Action, elapsed, "ok", fields)
	}
	_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
	return OK("run", map[string]string{"name": flow.Name, "run": run.ID, "steps": strconv.Itoa(len(flow.Steps)), "elapsed": time.Since(start).String()}).Write(c.Stdout)
}

func (c CLI) executeFlowStep(ctx context.Context, run RunState, index int, step FlowStep) (map[string]string, error) {
	switch step.Action {
	case "open":
		err := c.withStdout(io.Discard).open(ctx, GlobalOptions{}, flowArgs(step.Params, "--target", "target", "--device", "device", "--ios", "ios", "--udid", "udid", "--device-id", "deviceId", "--locale", "locale", "--language", "language"))
		return map[string]string{"run": run.ID}, outputErr(err, "open_failed")
	case "go":
		screen := step.Params["screen"]
		if screen == "" {
			return nil, fmt.Errorf("screen_missing")
		}
		fields, err := c.navigateToScreen(ctx, screen)
		return fields, err
	case "tree":
		err := c.withStdout(io.Discard).ui(ctx, GlobalOptions{}, []string{"tree"})
		return map[string]string{"driver": "axe"}, outputErr(err, "tree_failed")
	case "tap":
		args := flowArgs(step.Params, "--id", "id", "--text", "text", "--value", "value", "--x", "x", "--y", "y")
		err := c.withStdout(io.Discard).uiTap(ctx, GlobalOptions{}, mustLoadConfig(c.Root), args)
		return copyParams(step.Params), outputErr(err, "tap_failed")
	case "type":
		text := step.Params["text"]
		err := c.withStdout(io.Discard).uiType(ctx, GlobalOptions{}, mustLoadConfig(c.Root), []string{text})
		return map[string]string{"chars": strconv.Itoa(len(text))}, outputErr(err, "type_failed")
	case "swipe":
		args := flowArgs(step.Params, "--direction", "direction")
		err := c.withStdout(io.Discard).uiSwipe(ctx, GlobalOptions{}, mustLoadConfig(c.Root), args)
		return copyParams(step.Params), outputErr(err, "swipe_failed")
	case "pinch":
		args := gestureFlowArgs(step.Params)
		err := c.withStdout(io.Discard).uiPinch(ctx, GlobalOptions{}, mustLoadConfig(c.Root), args)
		return copyParams(step.Params), outputErr(err, "pinch_failed")
	case "rotate":
		args := gestureFlowArgs(step.Params)
		err := c.withStdout(io.Discard).uiRotate(ctx, GlobalOptions{}, mustLoadConfig(c.Root), args)
		return copyParams(step.Params), outputErr(err, "rotate_failed")
	case "twoFingerPan":
		args := gestureFlowArgs(step.Params)
		err := c.withStdout(io.Discard).uiTwoFingerPan(ctx, GlobalOptions{}, mustLoadConfig(c.Root), args)
		return copyParams(step.Params), outputErr(err, "two_finger_pan_failed")
	case "actions":
		args := flowArgs(step.Params, "--file", "file")
		err := c.withStdout(io.Discard).uiActions(ctx, GlobalOptions{}, mustLoadConfig(c.Root), args)
		return copyParams(step.Params), outputErr(err, "actions_failed")
	case "delay", "sleep":
		duration := parseFlowDuration(step.Params["duration"], 1*time.Second)
		if duration <= 0 {
			return nil, fmt.Errorf("delay_invalid")
		}
		time.Sleep(duration)
		return map[string]string{"duration": duration.String()}, nil
	case "wait", "assert":
		err := c.waitForFlowCondition(ctx, step.Params, nil)
		return copyParams(step.Params), err
	case "waitUntil":
		err := c.waitForFlowCondition(ctx, step.Params, step.Any)
		return map[string]string{"conditions": strconv.Itoa(len(step.Any))}, err
	case "scrollUntil":
		return c.scrollUntilFlowCondition(ctx, step.Params)
	case "capture":
		name := step.Params["name"]
		if name != "" {
			return c.captureEvidenceStep(ctx, run, name, step.Params["note"])
		}
		err := c.withStdout(io.Discard).capture(ctx, GlobalOptions{}, []string{"--run", run.ID})
		return map[string]string{"run": run.ID}, outputErr(err, "capture_failed")
	case "evidence.start":
		err := c.withStdout(io.Discard).evidenceStart(ctx, GlobalOptions{}, []string{"--run", run.ID})
		return map[string]string{"run": run.ID}, outputErr(err, "evidence_start_failed")
	case "video.start":
		err := c.withStdout(io.Discard).evidenceStart(ctx, GlobalOptions{}, []string{"--run", run.ID})
		return map[string]string{"run": run.ID}, outputErr(err, "video_start_failed")
	case "evidence.step":
		name := step.Params["name"]
		if name == "" {
			return nil, fmt.Errorf("evidence_step_name_missing")
		}
		return c.captureEvidenceStep(ctx, run, name, step.Params["note"])
	case "evidence.stop":
		args := []string{"--run", run.ID}
		if note := step.Params["note"]; note != "" {
			args = append(args, "--note", note)
		}
		err := c.withStdout(io.Discard).evidenceStop(ctx, GlobalOptions{}, args)
		return map[string]string{"run": run.ID}, outputErr(err, "evidence_stop_failed")
	case "video.stop":
		args := []string{"--run", run.ID}
		if note := step.Params["note"]; note != "" {
			args = append(args, "--note", note)
		}
		err := c.withStdout(io.Discard).evidenceStop(ctx, GlobalOptions{}, args)
		return map[string]string{"run": run.ID}, outputErr(err, "video_stop_failed")
	case "logs":
		args := flowArgs(step.Params, "--contains", "contains", "--key", "key", "--level", "level")
		args = append(args, "--run", run.ID)
		err := c.withStdout(io.Discard).logs(GlobalOptions{}, args)
		return copyParams(step.Params), outputErr(err, "logs_failed")
	case "exec":
		return c.execFlowShell(ctx, run, index, step.Params)
	case "crashes":
		err := c.withStdout(io.Discard).crashes(ctx, GlobalOptions{}, []string{"--run", run.ID})
		return map[string]string{"run": run.ID}, outputErr(err, "crashes_failed")
	case "report":
		err := c.withStdout(io.Discard).evidenceReport(GlobalOptions{}, []string{"--run", run.ID})
		return map[string]string{"run": run.ID, "file": filepath.Join(run.Dir, "report.html")}, outputErr(err, "report_failed")
	default:
		return nil, fmt.Errorf("unknown_step")
	}
}

func (c CLI) goScreen(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("screen_missing", map[string]string{"usage": "mav go SCREEN_ID"}).Write(c.Stdout)
	}
	screenID := args[0]
	m, mapErr := LoadAppMap(c.Root)
	if mapErr != nil {
		cfg, cfgErr := LoadConfig(c.Root)
		if cfgErr != nil {
			return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
		}
		m, mapErr = EnsureAppMap(c.Root, cfg)
		if mapErr != nil {
			return Fail("app_map_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
		}
	}
	if err := ValidateAppMap(m); err != nil {
		return Fail("app_map_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	route, routeErr := Route(m, screenID)
	if routeErr != nil {
		fields := map[string]string{"screen": screenID}
		code := routeErr.Error()
		if code == "screen_not_found" || code == "route_not_found" {
			fields["next"] = "explore with mav ui tree/tap; map updates when the next screen is observed"
		}
		return Fail(code, fields).Write(c.Stdout)
	}
	if _, cfgErr := LoadConfig(c.Root); cfgErr != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	if err := c.withStdout(io.Discard).open(ctx, GlobalOptions{}, nil); err != nil {
		return Fail("open_failed", map[string]string{"screen": screenID}).Write(c.Stdout)
	}
	run, runErr := LoadRun(c.Root, "")
	if runErr != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	evidenceStarted := time.Now()
	if err := c.withStdout(io.Discard).evidenceStart(ctx, GlobalOptions{}, []string{"--run", run.ID}); err != nil {
		return Fail("evidence_start_failed", map[string]string{"run": run.ID, "screen": screenID}).Write(c.Stdout)
	}
	cfg, _ := LoadConfig(c.Root)
	if raw, err := c.waitForTreeReady(ctx, cfg, 8*time.Second); err == nil {
		_ = ObserveExpectedScreen(c.Root, cfg, run, raw, m.Start)
	} else {
		_, _ = c.captureEvidenceStep(ctx, run, "launch-not-ready", "App did not expose a ready accessibility tree after launch")
		_ = c.withStdout(io.Discard).evidenceStop(ctx, GlobalOptions{}, []string{"--run", run.ID, "--note", "Launch tree was not ready"})
		_ = c.withStdout(io.Discard).evidenceReport(GlobalOptions{}, []string{"--run", run.ID})
		_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
		return Fail("launch_tree_not_ready", map[string]string{"run": run.ID, "screen": screenID, "report": filepath.Join(run.Dir, "report.html")}).Write(c.Stdout)
	}
	_, _ = c.captureEvidenceStep(ctx, run, "start", "Start screen before navigation")
	fields, err := c.navigateToScreen(ctx, screenID, route)
	if err != nil {
		_, _ = c.captureEvidenceStep(ctx, run, "failure", "Failure while navigating to "+screenID)
		_ = c.withStdout(io.Discard).evidenceStop(ctx, GlobalOptions{}, []string{"--run", run.ID, "--note", "Failure while navigating to " + screenID})
		_ = c.withStdout(io.Discard).evidenceReport(GlobalOptions{}, []string{"--run", run.ID})
		_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
		fields["run"] = run.ID
		fields["report"] = filepath.Join(run.Dir, "report.html")
		return Fail(err.Error(), fields).Write(c.Stdout)
	}
	if time.Since(evidenceStarted) < 1200*time.Millisecond {
		time.Sleep(1200*time.Millisecond - time.Since(evidenceStarted))
	}
	_, _ = c.captureEvidenceStep(ctx, run, screenID, "Arrived at "+screenID)
	if err := c.withStdout(io.Discard).evidenceStop(ctx, GlobalOptions{}, []string{"--run", run.ID, "--note", "Arrived at " + screenID}); err != nil {
		return Fail("evidence_stop_failed", map[string]string{"run": run.ID, "screen": screenID}).Write(c.Stdout)
	}
	if err := c.withStdout(io.Discard).evidenceReport(GlobalOptions{}, []string{"--run", run.ID}); err != nil {
		return Fail("report_failed", map[string]string{"run": run.ID, "screen": screenID}).Write(c.Stdout)
	}
	_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
	fields["run"] = run.ID
	fields["report"] = filepath.Join(run.Dir, "report.html")
	fields["video"] = filepath.Join(run.Dir, "video.mov")
	return OK("go", fields).Write(c.Stdout)
}

func (c CLI) navigateToScreen(ctx context.Context, screenID string, routeOverride ...[]Edge) (map[string]string, error) {
	m, err := LoadAppMap(c.Root)
	if err != nil {
		return map[string]string{"next": "mav setup"}, fmt.Errorf("app_map_not_found")
	}
	if err := ValidateAppMap(m); err != nil {
		return map[string]string{"error": err.Error()}, fmt.Errorf("app_map_invalid")
	}
	route := []Edge{}
	if len(routeOverride) > 0 {
		route = routeOverride[0]
	} else {
		route, err = Route(m, screenID)
		if err != nil {
			return map[string]string{"screen": screenID}, err
		}
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return map[string]string{"next": "mav setup"}, fmt.Errorf("config_not_found")
	}
	run, _ := LoadRun(c.Root, "")
	SetCurrentScreen(c.Root, m.Start, run.ID)
	ClearPendingMapAction(c.Root)
	for _, edge := range route {
		beforeTree, beforeErr := c.waitForTreeReady(ctx, cfg, 5*time.Second)
		if beforeErr != nil {
			return map[string]string{"screen": screenID, "target": edge.To}, fmt.Errorf("tree_not_ready")
		}
		args := []string{}
		if edge.ID != "" {
			args = append(args, "--id", edge.ID)
		}
		if edge.Text != "" {
			args = append(args, "--text", edge.Text)
		}
		if edge.X != "" && edge.Y != "" {
			args = append(args, "--x", edge.X, "--y", edge.Y)
		}
		if err := c.withStdout(io.Discard).uiTap(ctx, GlobalOptions{}, cfg, args); err != nil {
			return map[string]string{"screen": screenID, "target": edge.To}, fmt.Errorf("tap_failed")
		}
		if edge.Wait != "" {
			time.Sleep(parseFlowDuration(edge.Wait, 0))
		}
		afterTree, afterErr := c.waitForTreeReady(ctx, cfg, 5*time.Second)
		if afterErr != nil {
			return map[string]string{"screen": screenID, "target": edge.To}, fmt.Errorf("tree_not_ready")
		}
		if sameTreeForRoute(beforeTree, afterTree) {
			return map[string]string{"screen": screenID, "target": edge.To}, fmt.Errorf("route_no_screen_change")
		}
		_ = ObserveExpectedScreen(c.Root, cfg, run, afterTree, edge.To)
		SetCurrentScreen(c.Root, edge.To, run.ID)
		ClearPendingMapAction(c.Root)
	}
	screen := m.Screens[screenID]
	params := map[string]string{}
	if screen.AssertID != "" {
		params["id"] = screen.AssertID
	}
	if screen.AssertText != "" {
		params["text"] = screen.AssertText
	}
	if len(params) > 0 {
		params["timeout"] = "5s"
		if err := c.waitForFlowCondition(ctx, params, nil); err != nil {
			return map[string]string{"screen": screenID}, err
		}
	}
	return map[string]string{"screen": screenID, "steps": strconv.Itoa(len(route))}, nil
}

func (c CLI) currentOrNewRun() (RunState, error) {
	run, err := LoadRun(c.Root, "")
	if err == nil && run.ID != "" {
		return run, nil
	}
	run, err = NewRunState()
	if err != nil {
		return RunState{}, err
	}
	return run, SaveCurrentRun(c.Root, run)
}

func (c CLI) withStdout(stdout io.Writer) CLI {
	c.Stdout = stdout
	return c
}

func outputErr(err error, code string) error {
	if err != nil {
		return fmt.Errorf("%s", code)
	}
	return nil
}

func writeElementLines(w io.Writer, elements []Element) error {
	const maxNodes = 80
	for i, el := range elements {
		if i >= maxNodes {
			_, err := fmt.Fprintf(w, "node_more remaining=%d\n", len(elements)-maxNodes)
			return err
		}
		fields := map[string]string{
			"index": strconv.Itoa(i + 1),
			"id":    el.ID,
			"label": el.Label,
			"role":  el.Role,
			"value": el.Value,
			"frame": el.Frame,
		}
		parts := []string{"node"}
		keys := []string{"index", "id", "label", "role", "value", "frame"}
		for _, key := range keys {
			if fields[key] != "" {
				parts = append(parts, key+"="+quoteIfNeeded(fields[key]))
			}
		}
		if _, err := fmt.Fprintln(w, strings.Join(parts, " ")); err != nil {
			return err
		}
	}
	return nil
}

func addSandboxNext(fields map[string]string, stderr string) {
	if sandboxAccessHint(stderr) != "" {
		fields["next"] = sandboxAccessHint(stderr)
	}
}

func sandboxAccessHint(text string) string {
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"core simulator",
		"coresimulator",
		"operation not permitted",
		"permission denied",
		"not authorized",
		"unable to boot device",
		"idb",
	} {
		if strings.Contains(lower, needle) {
			return "requires simulator/idb access; rerun outside sandbox"
		}
	}
	return ""
}

func mustLoadConfig(root string) Config {
	cfg, _ := LoadConfig(root)
	return cfg
}

func flowArgs(params map[string]string, pairs ...string) []string {
	args := []string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		flag := pairs[i]
		key := pairs[i+1]
		if value := params[key]; value != "" {
			args = append(args, flag, value)
		}
	}
	return args
}

func gestureFlowArgs(params map[string]string) []string {
	return flowArgs(params,
		"--x", "x",
		"--y", "y",
		"--scale", "scale",
		"--pan-x", "panX",
		"--pan-y", "panY",
		"--distance", "distance",
		"--angle", "angle",
		"--rotate", "rotate",
		"--degrees", "degrees",
		"--duration", "duration",
		"--hold", "hold",
	)
}

func copyParams(params map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range params {
		out[key] = value
	}
	return out
}

func appendFlowStep(run RunState, index int, action string, elapsed time.Duration, status string, fields map[string]string) {
	record := map[string]any{
		"time":    time.Now().Format(time.RFC3339),
		"step":    index,
		"action":  action,
		"status":  status,
		"elapsed": elapsed.String(),
	}
	for key, value := range fields {
		record[key] = value
	}
	data, _ := json.Marshal(record)
	appendFile(run.Commands, string(data)+"\n")
}

func (c CLI) cleanupFailedFlow(ctx context.Context, run RunState, fields map[string]string) {
	_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
	if pid, err := readPID(filepath.Join(run.Dir, "video.pid")); err == nil {
		_ = stopProcess(pid)
		_ = os.Remove(filepath.Join(run.Dir, "video.pid"))
	}
	if cfg, err := LoadConfig(c.Root); err == nil {
		path := filepath.Join(run.Dir, "failure.png")
		if result, err := c.captureScreenshot(ctx, cfg, path); err == nil && result.Err == nil {
			fields["screenshot"] = path
		}
	}
	_, _ = GenerateReport(run)
}

func (c CLI) captureEvidenceStep(ctx context.Context, run RunState, name, note string) (map[string]string, error) {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return nil, fmt.Errorf("config_not_found")
	}
	name = safeFileName(name)
	file := filepath.Join(run.Dir, "steps", fmt.Sprintf("%02d_%s.png", len(LoadEvidenceSteps(run))+1, name))
	result, err := c.captureScreenshot(ctx, cfg, file)
	if err != nil {
		return nil, fmt.Errorf("capture_tool_missing")
	}
	if result.Err != nil {
		return map[string]string{"stderr": firstLine(result.Stderr)}, fmt.Errorf("capture_failed")
	}
	if err := AppendEvidenceStep(run, EvidenceStep{Name: name, Note: note, File: file, Kind: "screenshot"}); err != nil {
		return nil, err
	}
	return map[string]string{"name": name, "file": file}, nil
}

func (c CLI) waitForFlowCondition(ctx context.Context, params map[string]string, any []FlowCondition) error {
	timeout := parseFlowDuration(params["timeout"], 5*time.Second)
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		ok, err := c.evaluateFlowCondition(ctx, params, any)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait_timeout")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (c CLI) scrollUntilFlowCondition(ctx context.Context, params map[string]string) (map[string]string, error) {
	maxSwipes := 5
	if raw := params["maxSwipes"]; raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxSwipes = parsed
		}
	}
	direction := params["direction"]
	if direction == "" {
		direction = "up"
	}
	for i := 0; i <= maxSwipes; i++ {
		ok, err := c.evaluateSingleCondition(ctx, FlowCondition{Text: params["text"], ID: params["id"], Value: params["value"]})
		if err != nil {
			return nil, err
		}
		if ok {
			return map[string]string{"swipes": strconv.Itoa(i), "direction": direction}, nil
		}
		if i == maxSwipes {
			break
		}
		if err := c.withStdout(io.Discard).uiSwipe(ctx, GlobalOptions{}, mustLoadConfig(c.Root), []string{"--direction", direction}); err != nil {
			return nil, fmt.Errorf("swipe_failed")
		}
		time.Sleep(500 * time.Millisecond)
	}
	return map[string]string{"swipes": strconv.Itoa(maxSwipes), "direction": direction}, fmt.Errorf("scroll_until_timeout")
}

func (c CLI) execFlowShell(ctx context.Context, run RunState, index int, params map[string]string) (map[string]string, error) {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return nil, fmt.Errorf("config_not_found")
	}
	if !cfg.AllowShell {
		return map[string]string{"next": "set allow_shell: true in .mav/config.yaml for trusted project-local flows"}, fmt.Errorf("shell_not_allowed")
	}
	command := params["cmd"]
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("exec_cmd_missing")
	}
	timeout := parseFlowDuration(params["timeout"], 30*time.Second)
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(stepCtx, "/bin/bash", "-lc", command)
	cmd.Dir = c.Root
	cmd.Env = append(os.Environ(),
		"MAV_ROOT="+c.Root,
		"MAV_RUN_ID="+run.ID,
		"MAV_RUN_DIR="+run.Dir,
		"MAV_LOGS="+run.LogsPath,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		code = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
	}
	stdoutPath := filepath.Join(run.Dir, fmt.Sprintf("exec_%02d.out", index))
	stderrPath := filepath.Join(run.Dir, fmt.Sprintf("exec_%02d.err", index))
	_ = os.WriteFile(stdoutPath, stdout.Bytes(), 0o644)
	_ = os.WriteFile(stderrPath, stderr.Bytes(), 0o644)
	appendCommand(run, "mav exec "+command, CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), Code: code, Err: err})

	fields := map[string]string{
		"exit_code": strconv.Itoa(code),
		"stdout":    stdoutPath,
		"stderr":    stderrPath,
	}
	if stepCtx.Err() == context.DeadlineExceeded {
		fields["timeout"] = timeout.String()
		return fields, fmt.Errorf("exec_timeout")
	}
	if contains := params["contains"]; contains != "" && !strings.Contains(stdout.String()+stderr.String(), contains) {
		fields["contains"] = contains
		return fields, fmt.Errorf("exec_assert_failed")
	}
	if err != nil {
		return fields, fmt.Errorf("exec_failed")
	}
	return fields, nil
}

func (c CLI) evaluateFlowCondition(ctx context.Context, params map[string]string, any []FlowCondition) (bool, error) {
	if len(any) > 0 {
		for _, condition := range any {
			ok, err := c.evaluateSingleCondition(ctx, condition)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	return c.evaluateSingleCondition(ctx, FlowCondition{Text: params["text"], ID: params["id"], Value: params["value"], ChangedFrom: params["changedFrom"]})
}

func (c CLI) evaluateSingleCondition(ctx context.Context, condition FlowCondition) (bool, error) {
	if condition.ChangedFrom != "" {
		return c.screenshotChangedFrom(ctx, condition.ChangedFrom)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return false, fmt.Errorf("config_not_found")
	}
	result := c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "describe-ui")...)
	if result.Err != nil {
		return false, fmt.Errorf("tree_failed")
	}
	raw := result.Stdout
	if condition.Text != "" && !strings.Contains(raw, condition.Text) {
		return false, nil
	}
	if condition.ID != "" && !strings.Contains(raw, condition.ID) {
		return false, nil
	}
	if condition.Value != "" && !strings.Contains(raw, condition.Value) {
		return false, nil
	}
	return condition.Text != "" || condition.ID != "" || condition.Value != "", nil
}

func (c CLI) screenshotChangedFrom(ctx context.Context, name string) (bool, error) {
	run, err := LoadRun(c.Root, "")
	if err != nil {
		return false, fmt.Errorf("run_not_found")
	}
	var baseline string
	for _, step := range LoadEvidenceSteps(run) {
		if step.Name == safeFileName(name) || step.Name == name {
			baseline = step.File
		}
	}
	if baseline == "" {
		return false, fmt.Errorf("baseline_not_found")
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return false, fmt.Errorf("config_not_found")
	}
	current := filepath.Join(run.Dir, "wait-current.png")
	result, err := c.captureScreenshot(ctx, cfg, current)
	if err != nil || result.Err != nil {
		return false, fmt.Errorf("capture_failed")
	}
	a, err := os.ReadFile(baseline)
	if err != nil {
		return false, fmt.Errorf("baseline_not_found")
	}
	b, err := os.ReadFile(current)
	if err != nil {
		return false, fmt.Errorf("capture_failed")
	}
	return string(a) != string(b), nil
}

func (c CLI) logs(opts GlobalOptions, args []string) error {
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	data, err := os.ReadFile(run.LogsPath)
	if err != nil {
		return Fail("logs_not_found", map[string]string{"run": run.ID, "file": run.LogsPath}).Write(c.Stdout)
	}
	contains := flagValue(args, "--contains")
	key := flagValue(args, "--key")
	level := flagValue(args, "--level")
	lines := filterLines(strings.Split(string(data), "\n"), contains, level)
	if key != "" {
		lines = filterLogKey(lines, key)
	}
	if opts.Raw {
		for _, line := range lines {
			fmt.Fprintln(c.Stdout, line)
		}
		return nil
	}
	fields := map[string]string{"run": run.ID, "file": run.LogsPath, "matches": strconv.Itoa(len(lines))}
	if key != "" {
		fields["key"] = key
	}
	return OK("logs", fields).Write(c.Stdout)
}

func (c CLI) stop(ctx context.Context, opts GlobalOptions, args []string) error {
	_ = ctx
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	records := loadProcessRecords(run)
	stopped := 0
	failed := 0
	for _, record := range records {
		if record.PID <= 0 {
			continue
		}
		if record.Kind == "video" && fileExists(filepath.Join(run.Dir, "video.pid")) {
			continue
		}
		if err := stopProcess(record.PID); err != nil {
			failed++
			appendCommand(run, "mav stop "+strconv.Itoa(record.PID), CommandResult{Stderr: err.Error(), Code: 1, Err: err})
			continue
		}
		stopped++
		appendCommand(run, "mav stop "+strconv.Itoa(record.PID), CommandResult{})
	}
	fields := map[string]string{"run": run.ID, "stopped": strconv.Itoa(stopped), "failed": strconv.Itoa(failed)}
	if failed > 0 {
		return Fail("stop_failed", fields).Write(c.Stdout)
	}
	return OK("stop", fields).Write(c.Stdout)
}

func filterLogKey(lines []string, key string) []string {
	if key == "" {
		return lines
	}
	needle := "key=" + key
	filtered := []string{}
	for _, line := range lines {
		if strings.Contains(line, "MAV_LOG") && strings.Contains(line, needle) {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

type processRecord struct {
	Kind    string `json:"kind"`
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

func appendProcess(run RunState, kind string, pid int, command string) {
	if pid <= 0 {
		return
	}
	data, err := json.Marshal(processRecord{Kind: kind, PID: pid, Command: command})
	if err != nil {
		return
	}
	appendFile(run.Processes, string(data)+"\n")
}

func loadProcessRecords(run RunState) []processRecord {
	data, err := os.ReadFile(run.Processes)
	if err != nil {
		return nil
	}
	records := []processRecord{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record processRecord
		if err := json.Unmarshal([]byte(line), &record); err == nil && record.PID > 0 {
			records = append(records, record)
		}
	}
	return records
}

func removeProcess(run RunState, pid int) {
	records := loadProcessRecords(run)
	var b strings.Builder
	for _, record := range records {
		if record.PID == pid {
			continue
		}
		data, err := json.Marshal(record)
		if err == nil {
			b.Write(data)
			b.WriteByte('\n')
		}
	}
	_ = os.WriteFile(run.Processes, []byte(b.String()), 0o644)
}

func (c CLI) crashes(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	if !hasTool(cfg, "idb") {
		return Fail("tool_missing", map[string]string{"tool": "idb"}).Write(c.Stdout)
	}
	idbArgs := idbTargetArgs(cfg, "crash", "list")
	if cfg.BundleID != "" {
		idbArgs = append(idbArgs, "--bundle-id", cfg.BundleID)
	}
	result := c.Runner.Run(ctx, "idb", idbArgs...)
	if opts.Raw {
		fmt.Fprint(c.Stdout, result.Stdout)
		return nil
	}
	if result.Err != nil {
		fields := map[string]string{"stderr": firstLine(result.Stderr)}
		addSandboxNext(fields, result.Stderr)
		return Fail("crashes_failed", fields).Write(c.Stdout)
	}
	count := 0
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return OK("crashes", map[string]string{"count": strconv.Itoa(count)}).Write(c.Stdout)
}

func (c CLI) evidence(opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("evidence_command_missing", map[string]string{"usage": "mav evidence start|step|stop|report"}).Write(c.Stdout)
	}
	switch args[0] {
	case "start":
		return c.evidenceStart(context.Background(), opts, args[1:])
	case "step":
		return c.evidenceStep(context.Background(), opts, args[1:])
	case "stop":
		return c.evidenceStop(context.Background(), opts, args[1:])
	case "report":
		return c.evidenceReport(opts, args[1:])
	default:
		return Fail("evidence_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
}

func (c CLI) evidenceReport(opts GlobalOptions, args []string) error {
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	path, err := GenerateReport(run)
	if err != nil {
		return Fail("report_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	fields := map[string]string{"run": run.ID, "file": path, "video": "missing"}
	if video, validation := reportVideo(run); video != "" {
		if validation.OK {
			fields["video"] = video
			fields["video_duration"] = validation.Duration.String()
		} else {
			fields["video"] = "invalid"
			fields["video_file"] = video
			fields["video_issue"] = validation.Issue
		}
	}
	return OK("evidence.report", fields).Write(c.Stdout)
}

func (c CLI) evidenceStart(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	if issue := existingEvidenceIssue(run); issue != "" {
		return Fail("evidence_run_not_clean", map[string]string{"run": run.ID, "issue": issue, "next": "start a new run with mav open, or remove old evidence from the run directory"}).Write(c.Stdout)
	}
	path, pid, err := c.startVideoRecording(ctx, cfg, run)
	if err != nil {
		if strings.Contains(err.Error(), "video_unsupported") {
			return Fail("video_unsupported", map[string]string{"run": run.ID, "target": "device"}).Write(c.Stdout)
		}
		return Fail("evidence_start_failed", map[string]string{"run": run.ID, "error": err.Error()}).Write(c.Stdout)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, "video.pid"), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return err
	}
	appendCommand(run, "mav evidence start", CommandResult{})
	return OK("evidence.start", map[string]string{"run": run.ID, "file": path, "pid": strconv.Itoa(pid)}).Write(c.Stdout)
}

func (c CLI) evidenceStep(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	name := flagValue(args, "--name")
	if name == "" {
		name = "step"
	}
	name = safeFileName(name)
	steps := LoadEvidenceSteps(run)
	file := filepath.Join(run.Dir, "steps", fmt.Sprintf("%02d_%s.png", len(steps)+1, name))
	result, err := c.captureScreenshot(ctx, cfg, file)
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "axe|idb|xcrun"}).Write(c.Stdout)
	}
	if result.Err != nil {
		return Fail("evidence_step_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
	}
	step := EvidenceStep{Name: name, Note: flagValue(args, "--note"), File: file, Kind: "screenshot"}
	if err := AppendEvidenceStep(run, step); err != nil {
		return err
	}
	appendCommand(run, "mav evidence step --name "+name, result)
	return OK("evidence.step", map[string]string{"run": run.ID, "name": name, "file": file}).Write(c.Stdout)
}

func (c CLI) evidenceStop(ctx context.Context, opts GlobalOptions, args []string) error {
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	pid, err := readPID(filepath.Join(run.Dir, "video.pid"))
	if err != nil {
		return Fail("video_not_running", map[string]string{"run": run.ID}).Write(c.Stdout)
	}
	_ = stopProcess(pid)
	_ = os.Remove(filepath.Join(run.Dir, "video.pid"))
	removeProcess(run, pid)
	fields := map[string]string{"run": run.ID, "file": filepath.Join(run.Dir, "video.mov")}
	if issue := videoLogIssue(filepath.Join(run.Dir, "video.log")); issue != "" {
		return Fail("video_recording_failed", map[string]string{"run": run.ID, "file": fields["file"], "log": filepath.Join(run.Dir, "video.log"), "error": issue}).Write(c.Stdout)
	}
	if !waitForFile(fields["file"], 6*time.Second) {
		return Fail("video_not_written", map[string]string{"run": run.ID, "file": fields["file"], "log": filepath.Join(run.Dir, "video.log")}).Write(c.Stdout)
	}
	validation, err := waitForEvidenceVideo(fields["file"], 6*time.Second)
	if err != nil || !validation.OK {
		if validation.Issue == "" {
			validation.Issue = "video_invalid"
		}
		duration := "0s"
		if validation.Duration > 0 {
			duration = validation.Duration.String()
		}
		return Fail("video_invalid", map[string]string{"run": run.ID, "file": fields["file"], "duration": duration, "issue": validation.Issue, "log": filepath.Join(run.Dir, "video.log")}).Write(c.Stdout)
	}
	fields["duration"] = validation.Duration.String()
	if !hasFlag(args, "--no-capture") {
		cfg, cfgErr := LoadConfig(c.Root)
		if cfgErr == nil {
			file := filepath.Join(run.Dir, "steps", fmt.Sprintf("%02d_final.png", len(LoadEvidenceSteps(run))+1))
			if result, err := c.captureScreenshot(ctx, cfg, file); err == nil && result.Err == nil {
				_ = AppendEvidenceStep(run, EvidenceStep{Name: "final", Note: flagValue(args, "--note"), File: file, Kind: "screenshot"})
				fields["screenshot"] = file
			}
		}
	}
	appendCommand(run, "mav evidence stop", CommandResult{})
	return OK("evidence.stop", fields).Write(c.Stdout)
}

func videoDuration(path string) (time.Duration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return mp4Duration(data)
}

type VideoValidation struct {
	OK       bool
	Duration time.Duration
	Frames   int
	Issue    string
}

const minEvidenceVideoDuration = 500 * time.Millisecond
const minEvidenceVideoFrames = 2

func ValidateEvidenceVideo(path string) VideoValidation {
	data, err := os.ReadFile(path)
	if err != nil {
		return VideoValidation{Issue: err.Error()}
	}
	duration, err := mp4Duration(data)
	if err != nil || duration <= 0 {
		return VideoValidation{Issue: "duration_missing"}
	}
	frames := mp4VideoFrameCount(data)
	if duration < minEvidenceVideoDuration {
		return VideoValidation{Duration: duration, Frames: frames, Issue: "duration_too_short"}
	}
	if frames > 0 && frames < minEvidenceVideoFrames {
		return VideoValidation{Duration: duration, Frames: frames, Issue: "frame_count_too_low"}
	}
	return VideoValidation{OK: true, Duration: duration, Frames: frames}
}

func waitForEvidenceVideo(path string, timeout time.Duration) (VideoValidation, error) {
	deadline := time.Now().Add(timeout)
	var last VideoValidation
	for {
		validation := ValidateEvidenceVideo(path)
		if validation.OK {
			return validation, nil
		}
		last = validation
		if time.Now().After(deadline) {
			if last.Issue == "" {
				last.Issue = "video_invalid"
			}
			return last, fmt.Errorf("%s", last.Issue)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForVideoDuration(path string, timeout time.Duration) (time.Duration, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		duration, err := videoDuration(path)
		if err == nil && duration > 0 {
			return duration, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			if lastErr == nil {
				lastErr = fmt.Errorf("video_duration_missing")
			}
			return 0, lastErr
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func mp4Duration(data []byte) (time.Duration, error) {
	timescale, duration, ok := findMVHDDuration(data)
	if !ok || timescale == 0 || duration == 0 {
		return 0, fmt.Errorf("video_duration_missing")
	}
	return time.Duration(duration) * time.Second / time.Duration(timescale), nil
}

func findMVHDDuration(data []byte) (uint64, uint64, bool) {
	for offset := 0; offset+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		kind := string(data[offset+4 : offset+8])
		header := 8
		if size == 1 {
			if offset+16 > len(data) {
				return 0, 0, false
			}
			wide := binary.BigEndian.Uint64(data[offset+8 : offset+16])
			if wide > uint64(len(data)-offset) {
				return 0, 0, false
			}
			size = int(wide)
			header = 16
		} else if size == 0 {
			size = len(data) - offset
		}
		if size < header || offset+size > len(data) {
			return 0, 0, false
		}
		payload := data[offset+header : offset+size]
		if kind == "mvhd" {
			return parseMVHD(payload)
		}
		if isContainerAtom(kind) {
			if scale, dur, ok := findMVHDDuration(payload); ok {
				return scale, dur, true
			}
		}
		offset += size
	}
	return 0, 0, false
}

func mp4VideoFrameCount(data []byte) int {
	frames, _ := findSTSZSampleCount(data)
	return frames
}

func findSTSZSampleCount(data []byte) (int, bool) {
	for offset := 0; offset+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		kind := string(data[offset+4 : offset+8])
		header := 8
		if size == 1 {
			if offset+16 > len(data) {
				return 0, false
			}
			wide := binary.BigEndian.Uint64(data[offset+8 : offset+16])
			if wide > uint64(len(data)-offset) {
				return 0, false
			}
			size = int(wide)
			header = 16
		} else if size == 0 {
			size = len(data) - offset
		}
		if size < header || offset+size > len(data) {
			return 0, false
		}
		payload := data[offset+header : offset+size]
		if kind == "stsz" {
			if len(payload) < 12 {
				return 0, false
			}
			return int(binary.BigEndian.Uint32(payload[8:12])), true
		}
		if isContainerAtom(kind) {
			if frames, ok := findSTSZSampleCount(payload); ok {
				return frames, true
			}
		}
		offset += size
	}
	return 0, false
}

func parseMVHD(payload []byte) (uint64, uint64, bool) {
	if len(payload) < 20 {
		return 0, 0, false
	}
	version := payload[0]
	switch version {
	case 0:
		if len(payload) < 20 {
			return 0, 0, false
		}
		return uint64(binary.BigEndian.Uint32(payload[12:16])), uint64(binary.BigEndian.Uint32(payload[16:20])), true
	case 1:
		if len(payload) < 32 {
			return 0, 0, false
		}
		return uint64(binary.BigEndian.Uint32(payload[20:24])), binary.BigEndian.Uint64(payload[24:32]), true
	default:
		return 0, 0, false
	}
}

func isContainerAtom(kind string) bool {
	switch kind {
	case "moov", "trak", "mdia", "minf", "stbl", "edts", "udta", "meta":
		return true
	default:
		return false
	}
}

func axeTargetArgs(cfg Config, args ...string) []string {
	out := append([]string{}, args...)
	if !isDeviceTarget(cfg) && cfg.SimulatorUDID != "" {
		out = append(out, "--udid", cfg.SimulatorUDID)
	}
	return out
}

func idbTargetArgs(cfg Config, args ...string) []string {
	out := append([]string{}, args...)
	if udid := targetUDID(cfg); udid != "" {
		out = append(out, "--udid", udid)
	}
	return out
}

func swipeCoordinates(direction string) (string, string, string, string) {
	switch strings.ToLower(direction) {
	case "down":
		return "220", "260", "220", "760"
	case "left":
		return "360", "500", "80", "500"
	case "right":
		return "80", "500", "360", "500"
	default:
		return "220", "760", "220", "260"
	}
}

func isSwipeDirection(direction string) bool {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "up", "down", "left", "right":
		return true
	default:
		return false
	}
}

func (c CLI) startVideoRecording(ctx context.Context, cfg Config, run RunState) (string, int, error) {
	if isDeviceTarget(cfg) {
		return "", 0, fmt.Errorf("video_unsupported target=device")
	}
	if !hasTool(cfg, "xcrun") {
		return "", 0, fmt.Errorf("xcrun_missing")
	}
	target := cfg.SimulatorUDID
	if target == "" {
		target = "booted"
	}
	videoPath := filepath.Join(run.Dir, "video.mov")
	args := []string{"simctl", "io", target, "recordVideo", "--codec=h264", videoPath}
	pid, err := c.Runner.Start(ctx, filepath.Join(run.Dir, "video.log"), "xcrun", args...)
	if err == nil {
		appendProcess(run, "video", pid, "xcrun "+strings.Join(args, " "))
	}
	return videoPath, pid, err
}

func existingEvidenceIssue(run RunState) string {
	if fileExists(filepath.Join(run.Dir, "video.pid")) {
		return "video_already_running"
	}
	if fileExists(filepath.Join(run.Dir, "video.mov")) || fileExists(filepath.Join(run.Dir, "video.mp4")) {
		return "video_exists"
	}
	if fileExists(filepath.Join(run.Dir, EvidenceStepsFile)) {
		return "evidence_steps_exist"
	}
	stepsDir := filepath.Join(run.Dir, "steps")
	if entries, err := os.ReadDir(stepsDir); err == nil && len(entries) > 0 {
		return "steps_exist"
	}
	return ""
}

func videoLogIssue(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lower := strings.ToLower(string(data))
	switch {
	case strings.Contains(lower, "cannot save recorded video output into a file that already exists"):
		return "video_file_exists"
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		return firstLine(string(data))
	default:
		return ""
	}
}

func readPID(path string) (int, error) {
	pidData, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(pidData)))
}

func stopProcess(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGINT); err == nil {
		time.Sleep(1500 * time.Millisecond)
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = proc.Signal(syscall.SIGINT)
	time.Sleep(1500 * time.Millisecond)
	return nil
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func safeFileName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "step"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "step"
	}
	return out
}

func flagValue(args []string, name string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			if name == "--install" {
				return strings.Join(args[i+1:], " ")
			}
			return args[i+1]
		}
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimPrefix(arg, name+"=")
		}
	}
	return ""
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func hasTool(cfg Config, tool string) bool {
	if cfg.Tools == nil {
		return false
	}
	return cfg.Tools[tool]
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func appendFile(path, text string) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(text)
}

func appendCommand(run RunState, command string, result CommandResult) {
	record := map[string]any{
		"time":    time.Now().Format(time.RFC3339),
		"command": command,
		"code":    result.Code,
	}
	data, _ := json.Marshal(record)
	appendFile(run.Commands, string(data)+"\n")
}

func filterLines(lines []string, contains, level string) []string {
	out := []string{}
	for _, line := range lines {
		if line == "" {
			continue
		}
		if contains != "" && !strings.Contains(line, contains) {
			continue
		}
		if level != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(level)) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func countTreeNodes(raw string) int {
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return 0
	}
	var walk func(any) int
	walk = func(v any) int {
		switch t := v.(type) {
		case []any:
			total := 0
			for _, item := range t {
				total += walk(item)
			}
			return total
		case map[string]any:
			total := 1
			if children, ok := t["children"]; ok {
				total += walk(children)
			}
			return total
		default:
			return 0
		}
	}
	return walk(parsed)
}

func isEmptyAXTree(raw string) bool {
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return false
	}
	roots := []any{}
	switch value := parsed.(type) {
	case []any:
		roots = value
	case map[string]any:
		roots = []any{value}
	default:
		return false
	}
	if len(roots) != 1 {
		return false
	}
	root, ok := roots[0].(map[string]any)
	if !ok {
		return false
	}
	role := stringField(root, "role", "type")
	if role != "AXApplication" && !strings.EqualFold(role, "application") {
		return false
	}
	if children, ok := root["children"].([]any); ok && len(children) > 0 {
		return false
	}
	frame := stringField(root, "AXFrame")
	if strings.Contains(frame, "{0, 0}, {0, 0}") || strings.Contains(frame, "0,0,0,0") {
		return true
	}
	if frameMap, ok := root["frame"].(map[string]any); ok {
		width := fmt.Sprint(frameMap["width"])
		height := fmt.Sprint(frameMap["height"])
		return (width == "0" || width == "0.0") && (height == "0" || height == "0.0")
	}
	return countTreeNodes(raw) <= 1 && len(ExtractElements(raw)) == 0
}

func sameTreeForRoute(a, b string) bool {
	left := ExtractElements(a)
	right := ExtractElements(b)
	if len(left) == 0 || len(right) == 0 || len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
