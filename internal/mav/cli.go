package mav

import (
	"bufio"
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

	"github.com/bitomule/mav/internal/mav/drivers"
)

type CLI struct {
	Runner Runner
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Root   string
}

type GlobalOptions struct {
	Verbose      bool
	Raw          bool
	Help         bool
	PreferDriver string
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	cli := CLI{Runner: ExecRunner{}, Stdin: os.Stdin, Stdout: stdout, Stderr: stderr, Root: root}
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
	case "flow":
		return c.flow(ctx, opts, rest[1:])
	case "logs":
		return c.logs(opts, rest[1:])
	case "stop":
		return c.stop(ctx, opts, rest[1:])
	case "crashes":
		return c.crashes(ctx, opts, rest[1:])
	case "network":
		return c.network(ctx, opts, rest[1:])
	case "evidence":
		return c.evidence(opts, rest[1:])
	default:
		return Fail("unknown_command", map[string]string{"command": rest[0]}).Write(c.Stdout)
	}
}

func parseGlobal(args []string) (GlobalOptions, []string) {
	opts := GlobalOptions{}
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--verbose":
			opts.Verbose = true
		case "--raw":
			opts.Raw = true
		case "--help", "-h":
			opts.Help = true
		case "--prefer-driver":
			if i+1 < len(args) {
				opts.PreferDriver = args[i+1]
				i++
			} else {
				rest = append(rest, arg)
			}
		default:
			if strings.HasPrefix(arg, "--prefer-driver=") {
				opts.PreferDriver = strings.TrimPrefix(arg, "--prefer-driver=")
			} else {
				rest = append(rest, arg)
			}
		}
	}
	return opts, rest
}

func normalizePreferDriver(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "auto", nil
	}
	switch value {
	case "auto", "axe":
		return value, nil
	default:
		return "", fmt.Errorf("prefer_driver_invalid")
	}
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
  device      List or select physical iOS devices.
  open        Build, install, launch, and start run logs.
  ui          Inspect and control the current UI.
  capture     Capture the current screen.
  run         Execute a native MAV YAML flow.
  flow        Lint native MAV YAML flows.
  logs        Read captured run logs.
  stop        Stop run-owned background processes.
  crashes     List crashes for the configured app.
  evidence    Start/step/stop/report evidence.
  network     Start/stop a HAR network capture (sim only).

Global flags:
  --raw       Emit raw underlying tool output where supported.
  --verbose   Print extra debug details where supported.
  --prefer-driver auto|axe
              Prefer a UI driver for semantic tree/tap commands.
  --help,-h   Show help.
`
	case "setup":
		return "Usage:\n  mav setup [--non-interactive]\n  mav setup --install axe idb baguette\n"
	case "install-skills":
		return "Usage: mav install-skills\n"
	case "sim":
		return `Usage:
  mav sim list
  mav sim select --device "iPhone 17 Pro Max" --ios 26 [--locale es_ES] [--language es] [--force]
  mav sim select --udid <simulator-udid> [--force]
  mav sim boot
`
	case "device":
		return `Usage:
  mav device list
  mav device select --udid <device-udid>
  mav device select --name <device-name>
`
	case "open":
		return `Usage:
  mav open [--device NAME] [--ios VERSION] [--udid UDID] [--locale LOCALE] [--language LANG] [--clear-state] [--no-relaunch] [--force]

--no-relaunch reuses the app already running on the selected target. It starts or reuses a MAV run without executing the launch recipe.
--force ignores a fresh MAV simulator lock when you know the run is yours.
`
	case "ui":
		return `Usage:
  mav ui tree [--prefer-driver auto|axe] [--include-system]
  mav ui tap --id ID [--prefer-driver auto|axe]
  mav ui tap --x X --y Y
  mav ui tap --text TEXT [--prefer-driver auto|axe]
  mav ui tap --value VALUE
  mav ui type TEXT [--prefer-driver auto|axe]
  mav ui erase [--id ID | --text TEXT | --value VALUE | --focused true]
  mav ui hideKeyboard
  mav ui swipe [--direction up|down|left|right]
  mav ui longPress --x X --y Y [--duration 800ms]
  mav ui pinch --x X --y Y --scale SCALE [--pan-x DX] [--pan-y DY] [--distance D] [--angle DEG] [--rotate DEG] [--duration 800ms] [--hold DURATION]
  mav ui rotate --x X --y Y --degrees DEG [--distance D] [--duration 800ms] [--hold DURATION]
  mav ui twoFingerPan --x X --y Y --pan-x DX --pan-y DY [--distance D] [--angle DEG] [--duration 800ms] [--hold DURATION]
  mav ui actions --file actions.json
  mav ui wait --id ID [--timeout 5s]
  mav ui wait --text TEXT [--timeout 5s]
  mav ui wait --value VALUE [--timeout 5s]
  mav ui scrollUntil --id ID [--direction up] [--max-swipes 5]
`
	case "capture":
		return "Usage: mav capture [--name NAME] [--run RUN_ID]\n"
	case "ui tree":
		return "Usage: mav ui tree [--prefer-driver auto|axe] [--include-system] [--agent] [--with-frame]\n\nPrints compact screen metadata followed by bounded node lines with id, label, role, value, enabled, subrole, title, pid, focused, and frame when available. --include-system asks the system/SpringBoard tree via baguette when a system service, permission prompt, or cross-app view is in front. Simulator only.\n\n--agent emits a ranked 40-element view that puts focused + actionable elements first and drops frame to save tokens. Combine with --with-frame to keep coordinates.\n"
	case "ui swipe":
		return "Usage: mav ui swipe [--direction up|down|left|right] [--start-x X --start-y Y --end-x X --end-y Y]\n"
	case "ui pinch":
		return "Usage: mav ui pinch --x X --y Y --scale SCALE [--pan-x DX] [--pan-y DY] [--distance D] [--angle DEG] [--rotate DEG] [--duration 800ms] [--hold DURATION]\n"
	case "ui rotate":
		return "Usage: mav ui rotate --x X --y Y --degrees DEG [--distance D] [--duration 800ms] [--hold DURATION]\n"
	case "ui twoFingerPan":
		return "Usage: mav ui twoFingerPan --x X --y Y --pan-x DX --pan-y DY [--distance D] [--angle DEG] [--duration 800ms] [--hold DURATION]\n"
	case "ui longPress":
		return "Usage: mav ui longPress --x X --y Y [--duration 800ms]\n"
	case "run":
		return "Usage: mav run flow.yaml\n"
	case "flow":
		return "Usage:\n  mav flow lint flow.yaml [--raw]\n"
	case "logs":
		return "Usage: mav logs [--run RUN_ID] [--key KEY] [--contains TEXT] [--level LEVEL] [--raw]\n"
	case "stop":
		return "Usage: mav stop [--run RUN_ID]\n"
	case "crashes":
		return "Usage: mav crashes [--raw]\n"
	case "evidence":
		return `Usage:
  mav evidence start [--run RUN_ID]
  mav evidence start --network [--port PORT] [--run RUN_ID]
  mav evidence step --name NAME [--note NOTE] [--run RUN_ID]
  mav evidence stop [--note NOTE] [--no-capture] [--run RUN_ID]
  mav evidence report [--run RUN_ID]
`
	case "network":
		return `Usage:
  mav network start [--har PATH] [--port PORT] [--run RUN_ID]
  mav network stop  [--run RUN_ID]
  mav network status [--har PATH] [--run RUN_ID] [--raw]

Starts a HAR network capture via mitmproxy on the current simulator.
The HAR file lands at <runDir>/network.har by default. The PID of the
mitmdump process is recorded in processes.jsonl so other commands
(mav stop, the evidence report) can find it.

Simulator only. On physical devices, point the device at an externally-
running proxy manually; mav does not bundle that flow.
`
	default:
		return "Unknown help topic. Run: mav help\n"
	}
}

func (c CLI) doctor(ctx context.Context, opts GlobalOptions) error {
	_ = opts
	cfg, _ := LoadConfig(c.Root)
	if cfg.Root == "" {
		cfg = DefaultConfig(c.Root)
	}
	c.resolveConfigTools(&cfg)
	caps := c.resolveCapabilities(ctx, cfg)
	tools := caps.Tools
	fields := caps.fields()
	commands := effectiveLaunchCommands(cfg)
	if hasLaunchCommands(commands) {
		fields["launch_recipe"] = "ok"
		if cfg.Launch.Mode != "" {
			fields["launch_mode"] = cfg.Launch.Mode
		}
	} else {
		fields["launch_recipe"] = "missing"
		fields["launch_next"] = "mav setup, or add launch.commands build/app_path/install/launch to .mav/config.yaml"
	}
	if hasLaunchCommands(commands) && strings.TrimSpace(commands.Launch) == "" {
		fields["launch_recipe"] = "incomplete"
		fields["launch_missing"] = "launch"
		fields["launch_next"] = "add launch.commands.launch to .mav/config.yaml"
	}
	missing := []string{}
	for _, tool := range []string{"axe", "idb"} {
		if !tools[tool] {
			missing = append(missing, tool)
		}
	}
	if !tools["baguette"] {
		missing = append(missing, "baguette")
	}
	if len(missing) > 0 {
		fields["next"] = "mav setup --install " + strings.Join(missing, " ")
	}
	addDoctorMatrixFields(fields, caps)
	if normalizedTargetKind(cfg) != "device" && cfg.SimulatorUDID != "" {
		if lock, locked := simulatorLockedByOther(cfg.SimulatorUDID, c.Root); locked {
			fields["sim_contention"] = "locked"
			fields["sim_lock_run"] = lock.RunID
			fields["sim_lock_project"] = lock.Project
			fields["sim_lock_next"] = "select a different simulator with mav sim select, or pass --force if you own this run"
		}
	}
	if tools["xcrun"] {
		if sims, err := ListSimulators(c.Runner); err == nil {
			booted, owned := 0, 0
			for _, sim := range sims {
				if sim.State != "Booted" {
					continue
				}
				booted++
				if simulatorOwner(sim) != "" {
					owned++
				}
			}
			if booted > 1 {
				fields["booted_sims"] = strconv.Itoa(booted)
				fields["owned_booted_sims"] = strconv.Itoa(owned)
			}
		}
	}
	return OK("doctor", fields).Write(c.Stdout)
}

func addDoctorMatrixFields(fields map[string]string, caps Capabilities) {
	if caps.AccessibilityDriver != "" {
		fields["sim_tree_driver"] = caps.AccessibilityDriver
		fields["device_tree_driver"] = caps.AccessibilityDriver
	}
	if caps.SemanticActions {
		fields["sim_semantic_tap_driver"] = caps.AccessibilityDriver
		fields["device_semantic_tap_driver"] = caps.AccessibilityDriver
	}
	if caps.CoordinateTapDriver != "" {
		fields["sim_coord_tap_driver"] = caps.CoordinateTapDriver
		fields["device_coord_tap_driver"] = caps.CoordinateTapDriver
	}
	if caps.MultitouchDriver != "" {
		fields["sim_multitouch_driver"] = caps.MultitouchDriver
		fields["device_multitouch"] = "unsupported"
	}
	if caps.NetworkCaptureDriver != "" {
		fields["sim_network_driver"] = caps.NetworkCaptureDriver
		fields["device_network"] = "manual_proxy"
	}
}

func (c CLI) setup(ctx context.Context, opts GlobalOptions, args []string) error {
	install := flagValue(args, "--install")
	if install == "" {
		return c.setupProject(opts, args)
	}
	tools := strings.Fields(install)
	if len(tools) == 0 {
		return Fail("setup_install_missing", nil).Write(c.Stdout)
	}
	commands := map[string][]string{
		"axe":       {"brew", "install", "cameroncooke/axe/axe"},
		"baguette":  {"brew", "install", "tddworks/tap/baguette"},
		"mitmproxy": {"brew", "install", "mitmproxy"},
	}
	for _, tool := range tools {
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

func (c CLI) setupProject(opts GlobalOptions, args []string) error {
	cfg, err := SetupConfig(c.Root, c.Runner)
	if existing, loadErr := LoadConfig(c.Root); loadErr == nil {
		cfg = mergeSetupConfig(existing, cfg)
	}
	interactive := !hasFlag(args, "--non-interactive")
	if interactive {
		prompted, promptErr := c.promptSetupConfig(cfg)
		if promptErr != nil {
			return Fail("setup_interrupted", map[string]string{"error": promptErr.Error()}).Write(c.Stdout)
		}
		cfg = prompted
	}
	if saveErr := SaveConfig(c.Root, cfg); saveErr != nil {
		return saveErr
	}
	fields := map[string]string{
		"config": filepath.Join(c.Root, ConfigFile),
		"target": cfg.AppTarget,
		"bundle": cfg.BundleID,
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
	if interactive {
		fields["interactive"] = "true"
	}
	if _, baguetteErr := c.Runner.LookPath("baguette"); baguetteErr != nil {
		fields["multitouch"] = "missing"
		fields["multitouch_next"] = "mav setup --install baguette"
	}
	return OK("setup", fields).Write(c.Stdout)
}

func (c CLI) promptSetupConfig(cfg Config) (Config, error) {
	reader := bufio.NewReader(c.setupInput())
	prompts := c.setupPromptWriter()
	fmt.Fprintln(prompts, "MAV setup interactive. Press Enter to accept the detected/current value, type a custom value, or type '-' to clear an optional value.")
	var err error
	if cfg.ProjectName, err = c.promptString(prompts, reader, "project_name", cfg.ProjectName); err != nil {
		return cfg, err
	}
	if cfg.AppTarget, err = c.promptString(prompts, reader, "app_target", cfg.AppTarget); err != nil {
		return cfg, err
	}
	if cfg.DeviceTarget, err = c.promptString(prompts, reader, "device_target", cfg.DeviceTarget); err != nil {
		return cfg, err
	}
	if cfg.BundleID, err = c.promptString(prompts, reader, "bundle_id", cfg.BundleID); err != nil {
		return cfg, err
	}
	if cfg.ProcessName, err = c.promptString(prompts, reader, "process_name", cfg.ProcessName); err != nil {
		return cfg, err
	}
	if cfg.SimulatorUDID, err = c.promptString(prompts, reader, "simulator_udid", cfg.SimulatorUDID); err != nil {
		return cfg, err
	}
	if cfg.Locale, err = c.promptString(prompts, reader, "locale", cfg.Locale); err != nil {
		return cfg, err
	}
	if cfg.Language, err = c.promptString(prompts, reader, "language", cfg.Language); err != nil {
		return cfg, err
	}
	if cfg.LogSubsystem, err = c.promptString(prompts, reader, "log_subsystem", cfg.LogSubsystem); err != nil {
		return cfg, err
	}
	if cfg.LogCategory, err = c.promptString(prompts, reader, "log_category", cfg.LogCategory); err != nil {
		return cfg, err
	}
	if cfg.PreferredUIDriver, err = c.promptString(prompts, reader, "preferred_ui_driver", cfg.PreferredUIDriver); err != nil {
		return cfg, err
	}
	cfg.AllowShell, err = c.promptBool(prompts, reader, "allow_shell", cfg.AllowShell)
	if err != nil {
		return cfg, err
	}
	if cfg.Launch.Mode, err = c.promptString(prompts, reader, "launch.mode", cfg.Launch.Mode); err != nil {
		return cfg, err
	}
	if cfg.Launch.Commands.Healthcheck, err = c.promptString(prompts, reader, "launch.commands.healthcheck", cfg.Launch.Commands.Healthcheck); err != nil {
		return cfg, err
	}
	if cfg.Launch.Commands.Build, err = c.promptString(prompts, reader, "launch.commands.build", cfg.Launch.Commands.Build); err != nil {
		return cfg, err
	}
	if cfg.Launch.Commands.AppPath, err = c.promptString(prompts, reader, "launch.commands.app_path", cfg.Launch.Commands.AppPath); err != nil {
		return cfg, err
	}
	if cfg.Launch.Commands.Install, err = c.promptString(prompts, reader, "launch.commands.install", cfg.Launch.Commands.Install); err != nil {
		return cfg, err
	}
	if cfg.Launch.Commands.Launch, err = c.promptString(prompts, reader, "launch.commands.launch", cfg.Launch.Commands.Launch); err != nil {
		return cfg, err
	}
	if cfg.Launch.Commands.Cleanup, err = c.promptString(prompts, reader, "launch.commands.cleanup", cfg.Launch.Commands.Cleanup); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c CLI) setupInput() io.Reader {
	if c.Stdin != nil {
		return c.Stdin
	}
	return strings.NewReader("")
}

func (c CLI) setupPromptWriter() io.Writer {
	if c.Stderr != nil {
		return c.Stderr
	}
	return io.Discard
}

func (c CLI) promptString(prompts io.Writer, reader *bufio.Reader, label, current string) (string, error) {
	fmt.Fprintf(prompts, "%s [%s]: ", label, displayPromptDefault(current))
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		if err == io.EOF {
			fmt.Fprintln(prompts)
			return current, nil
		}
		return "", err
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		return current, nil
	}
	if answer == "-" {
		return "", nil
	}
	return answer, nil
}

func (c CLI) promptBool(prompts io.Writer, reader *bufio.Reader, label string, current bool) (bool, error) {
	defaultValue := "n"
	if current {
		defaultValue = "y"
	}
	fmt.Fprintf(prompts, "%s [y/N, current=%s]: ", label, defaultValue)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		if err == io.EOF {
			fmt.Fprintln(prompts)
			return current, nil
		}
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	switch answer {
	case "":
		return current, nil
	case "y", "yes", "true", "1":
		return true, nil
	case "n", "no", "false", "0", "-":
		return false, nil
	default:
		return false, fmt.Errorf("invalid_bool field=%s", label)
	}
}

func displayPromptDefault(value string) string {
	if value == "" {
		return "empty"
	}
	return value
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
			owner := simulatorOwner(sim)
			if owner == "" {
				fmt.Fprintf(c.Stdout, "sim udid=%s name=%q runtime=%s state=%s\n", sim.UDID, sim.Name, sim.Runtime, sim.State)
			} else {
				fmt.Fprintf(c.Stdout, "sim udid=%s name=%q runtime=%s state=%s owner=%q\n", sim.UDID, sim.Name, sim.Runtime, sim.State, owner)
			}
		}
		return nil
	case "select":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
		}
		c.resolveConfigTools(&cfg)
		sims, err := ListSimulators(c.Runner)
		if err != nil {
			return Fail("sim_list_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
		}
		sim, ok := SelectSimulator(sims, flagValue(args[1:], "--device"), flagValue(args[1:], "--ios"), flagValue(args[1:], "--udid"))
		if !ok {
			return Fail("sim_not_found", map[string]string{"device": flagValue(args[1:], "--device"), "ios": flagValue(args[1:], "--ios"), "udid": flagValue(args[1:], "--udid")}).Write(c.Stdout)
		}
		if lock, locked := simulatorLockedByOther(sim.UDID, c.Root); locked && !hasFlag(args[1:], "--force") {
			return Fail("sim_locked", map[string]string{"udid": sim.UDID, "run": lock.RunID, "project": lock.Project, "next": "choose another simulator or rerun with --force"}).Write(c.Stdout)
		}
		cfg.TargetKind = "simulator"
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
		c.resolveConfigTools(&cfg)
		if isPhysicalDevice(cfg) {
			return Fail("sim_not_applicable", map[string]string{"target": "device", "next": "select a simulator with mav sim select"}).Write(c.Stdout)
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
	_ = opts
	if len(args) == 0 {
		return Fail("device_command_missing", map[string]string{"usage": "mav device list|select"}).Write(c.Stdout)
	}
	switch args[0] {
	case "list":
		devices, err := ListPhysicalDevices(ctx, c.Runner)
		if err != nil {
			fields := map[string]string{"error": err.Error()}
			addSandboxNext(fields, err.Error())
			return Fail("device_list_failed", fields).Write(c.Stdout)
		}
		if err := OK("device.list", map[string]string{"count": strconv.Itoa(len(devices))}).Write(c.Stdout); err != nil {
			return err
		}
		for _, device := range devices {
			fmt.Fprintf(c.Stdout, "device udid=%s name=%q\n", device.UDID, device.Name)
		}
		return nil
	case "select":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
		}
		udid := flagValue(args[1:], "--udid")
		name := flagValue(args[1:], "--name")
		if udid == "" && name == "" {
			return Fail("device_selector_missing", map[string]string{"usage": "mav device select --udid <device-udid> | --name <device-name>"}).Write(c.Stdout)
		}
		devices, err := ListPhysicalDevices(ctx, c.Runner)
		if err != nil {
			return Fail("device_list_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
		}
		device, ok := selectPhysicalDevice(devices, udid, name)
		if !ok {
			return Fail("device_not_found", map[string]string{"udid": udid, "name": name}).Write(c.Stdout)
		}
		cfg.TargetKind = "device"
		cfg.DeviceUDID = device.UDID
		cfg.DeviceName = device.Name
		if err := SaveConfig(c.Root, cfg); err != nil {
			return err
		}
		return OK("device.select", map[string]string{"udid": device.UDID, "name": device.Name}).Write(c.Stdout)
	default:
		return Fail("device_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
}

func (c CLI) open(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTools(&cfg)
	if err := c.applyOpenTargetOverrides(ctx, &cfg, args); err != nil {
		return Fail("sim_select_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if !isPhysicalDevice(cfg) && cfg.SimulatorUDID != "" && !hasFlag(args, "--force") {
		if lock, locked := simulatorLockedByOther(cfg.SimulatorUDID, c.Root); locked {
			return Fail("sim_locked", map[string]string{"udid": cfg.SimulatorUDID, "run": lock.RunID, "project": lock.Project, "next": "select another simulator or rerun with --force"}).Write(c.Stdout)
		}
	}
	noRelaunch := hasFlag(args, "--no-relaunch")
	if noRelaunch && hasFlag(args, "--clear-state") {
		return Fail("open_flags_invalid", map[string]string{"usage": "--no-relaunch cannot be combined with --clear-state"}).Write(c.Stdout)
	}
	var previousRunID string
	var run RunState
	if noRelaunch {
		run, err = c.currentOrNewRun()
	} else {
		if existing, err := LoadRun(c.Root, ""); err == nil && existing.ID != "" {
			previousRunID = existing.ID
		}
		run, err = NewProjectRunState(c.Root)
	}
	if err != nil {
		return err
	}
	if previousRunID != "" {
		_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", previousRunID})
	}
	if err := SaveCurrentRun(c.Root, run); err != nil {
		return err
	}
	if isPhysicalDevice(cfg) && !hasTool(cfg, "idb") {
		return Fail("tool_missing", map[string]string{"tool": "idb", "target": "device", "next": "mav setup --install idb"}).Write(c.Stdout)
	}
	probeLogPID, probeLogErr := c.startProbeLogs(ctx, cfg, run)
	if probeLogErr != nil {
		appendFile(run.LogsPath, "mav probe log capture failed: "+probeLogErr.Error()+"\n")
	}
	appPath := ""
	if !noRelaunch {
		var failedStep *launchStep
		var failedResult CommandResult
		appPath, failedStep, failedResult = c.runLaunchRecipe(ctx, cfg, run, hasFlag(args, "--clear-state"))
		if failedStep != nil {
			fields := map[string]string{"run": run.ID, "logs": run.LogsPath, "step": failedStep.Name, "stderr": firstLine(failedResult.Stderr)}
			if fields["stderr"] == "" && failedResult.Err != nil {
				fields["stderr"] = failedResult.Err.Error()
			}
			return Fail("launch_step_failed", fields).Write(c.Stdout)
		}
	}
	fields := map[string]string{"run": run.ID, "logs": run.LogsPath, "dir": run.Dir}
	if noRelaunch {
		fields["relaunch"] = "false"
	}
	if appPath != "" {
		fields["app"] = appPath
	}
	if probeLogPID > 0 {
		fields["probe_log_pid"] = strconv.Itoa(probeLogPID)
		fields["log_subsystem"] = probeLogSubsystem(cfg)
		fields["log_category"] = probeLogCategory(cfg)
	}
	if !isPhysicalDevice(cfg) && cfg.SimulatorUDID != "" && probeLogPID > 0 {
		if err := writeSimulatorLock(cfg.SimulatorUDID, run, c.Root, probeLogPID); err == nil {
			fields["sim_lock"] = simLockPath(cfg.SimulatorUDID)
		}
	}
	fields["target_kind"] = normalizedTargetKind(cfg)
	fields["target"] = targetName(cfg)
	if fields["target"] == "" {
		fields["target"] = targetUDID(cfg)
	}
	if fields["target"] == "" {
		fields["target"] = "booted"
	}
	return OK("open", fields).Write(c.Stdout)
}

func (c CLI) applyOpenTargetOverrides(ctx context.Context, cfg *Config, args []string) error {
	device := flagValue(args, "--device")
	ios := flagValue(args, "--ios")
	udid := flagValue(args, "--udid")
	if locale := flagValue(args, "--locale"); locale != "" {
		cfg.Locale = locale
	}
	if language := flagValue(args, "--language"); language != "" {
		cfg.Language = language
	}
	if device == "" && ios == "" && udid == "" {
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
	if lock, locked := simulatorLockedByOther(sim.UDID, c.Root); locked && !hasFlag(args, "--force") {
		return fmt.Errorf("sim_locked run=%s project=%s", lock.RunID, lock.Project)
	}
	cfg.SimulatorUDID = sim.UDID
	cfg.SimulatorName = sim.Name
	cfg.SimulatorRuntime = sim.Runtime
	cfg.TargetKind = "simulator"
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
	predicate := probeLogPredicate(cfg)
	if isPhysicalDevice(cfg) {
		if !hasTool(cfg, "idb") {
			return 0, fmt.Errorf("idb_missing")
		}
		args := []string{"log"}
		if cfg.DeviceUDID != "" {
			args = append(args, "--udid", cfg.DeviceUDID)
		}
		args = append(args, "--", "--style", "compact", "--level", "debug", "--predicate", predicate)
		pid, err := c.Runner.Start(ctx, run.LogsPath, "idb", args...)
		if err == nil {
			appendProcess(run, "probe-logs", pid, "idb "+strings.Join(args, " "))
		}
		return pid, err
	}
	if hasTool(cfg, "xcrun") && cfg.SimulatorUDID != "" {
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

func probeLogPredicate(cfg Config) string {
	parts := []string{fmt.Sprintf(`(subsystem == "%s" AND category == "%s")`, probeLogSubsystem(cfg), probeLogCategory(cfg))}
	if cfg.ProcessName != "" {
		parts = append(parts, fmt.Sprintf(`process == "%s"`, cfg.ProcessName))
	}
	if cfg.BundleID != "" {
		parts = append(parts, fmt.Sprintf(`subsystem == "%s"`, cfg.BundleID))
	}
	parts = append(parts, `eventMessage CONTAINS "MAV_LOG"`)
	return strings.Join(parts, " OR ")
}

func (c CLI) ui(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("ui_command_missing", map[string]string{"usage": "mav ui tree|tap|type|erase|hideKeyboard|swipe|longPress|pinch|rotate|twoFingerPan|actions|wait|scrollUntil"}).Write(c.Stdout)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTools(&cfg)
	switch args[0] {
	case "tree":
		return c.uiTree(ctx, opts, cfg, args[1:])
	case "tap":
		return c.uiTap(ctx, opts, cfg, args[1:])
	case "type":
		return c.uiType(ctx, opts, cfg, args[1:])
	case "erase":
		return c.uiErase(ctx, opts, cfg, args[1:])
	case "hideKeyboard":
		return c.uiHideKeyboard(ctx, opts, cfg, args[1:])
	case "swipe":
		return c.uiSwipe(ctx, opts, cfg, args[1:])
	case "longPress":
		return c.uiLongPress(ctx, opts, cfg, args[1:])
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

func (c CLI) uiTree(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	prefer, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe"}).Write(c.Stdout)
	}
	includeSystem := hasFlag(args, "--include-system")
	if includeSystem && isPhysicalDevice(cfg) {
		return Fail("tree_system_unsupported_on_device", map[string]string{"next": "run mav ui tree on a simulator for system/SpringBoard inspection"}).Write(c.Stdout)
	}
	described, err := c.describeUITree(ctx, cfg, prefer, includeSystem)
	if err != nil {
		return c.writeUITreeToolError(err)
	}
	driver := described.Driver
	result := described.Result
	recovered := false
	if driver == "axe" && result.Err == nil && isEmptyAXTree(result.Stdout) {
		if err := c.recoverEmptyAXTree(ctx, cfg); err == nil {
			if retry, retryErr := c.describeUITree(ctx, cfg, prefer, includeSystem); retryErr == nil {
				described = retry
				driver = retry.Driver
				result = retry.Result
				recovered = true
			}
		}
	}
	if result.Err != nil {
		fields := map[string]string{"stderr": firstLine(result.Stderr)}
		addSandboxNext(fields, result.Stderr)
		return Fail("ui_tree_failed", fields).Write(c.Stdout)
	}
	if opts.Raw {
		fmt.Fprint(c.Stdout, result.Stdout)
		return nil
	}
	if isEmptyAXTree(result.Stdout) {
		return Fail("ui_tree_empty", map[string]string{"driver": driver, "reason": "simulator_accessibility_unavailable", "recovered": strconv.FormatBool(recovered)}).Write(c.Stdout)
	}
	state := c.observeUITree(cfg, result.Stdout, driver, true)
	fields := map[string]string{"driver": driver, "nodes": strconv.Itoa(state.Nodes), "screen": state.Screen}
	if state.ScreenSource != "" {
		fields["screen_source"] = state.ScreenSource
	}
	if state.ScreenSource == "identity_missing" {
		addIdentityMissingNext(fields, described)
	}
	if recovered {
		fields["recovered"] = "true"
	}
	if described.ActiveBundle != "" && described.ActiveBundle != cfg.BundleID {
		fields["active_bundle"] = described.ActiveBundle
		fields["system_source"] = strconv.FormatBool(described.SystemSource)
	}
	agentMode := hasFlag(args, "--agent")
	withFrame := hasFlag(args, "--with-frame")
	if agentMode {
		fields["agent"] = "true"
	}
	if err := OK("ui.tree", fields).Write(c.Stdout); err != nil {
		return err
	}
	if agentMode {
		// Agent mode: rank, cap to 40, drop noisy fields. Stays in the
		// same line-oriented shape as the legacy output so existing
		// line-parser agents keep working; only the field set differs.
		agents := AgentTree(state.Elements, AgentTreeOptions{WithFrame: withFrame})
		return writeAgentElementLines(c.Stdout, agents)
	}
	return writeElementLines(c.Stdout, state.Elements)
}

type uiTreeState struct {
	Screen       string
	ScreenSource string
	Driver       string
	Elements     []Element
	Nodes        int
}

// observeUITree extracts the framework-neutral view of `raw` and
// derives a natural screen id from the elements alone. No
// persisted map, no recogniser pipeline — `state.Screen` is either
// the kebab-case form of the shallowest View-suffix root id
// (e.g. `ItemDetailView` → `item-detail-view`) or `"unknown"`. The `cfg`
// argument is kept on the signature so callers don't need to
// thread it conditionally; it's reserved for any future driver-
// specific observation tweaks.
func (c CLI) observeUITree(cfg Config, raw, treeDriver string, _persist bool) uiTreeState {
	state := uiTreeState{
		Driver:   treeDriver,
		Screen:   "unknown",
		Elements: ExtractElements(raw),
		Nodes:    countTreeNodes(raw),
	}
	if id, _, ok := explicitScreenIdentity(state.Elements); ok {
		state.Screen = id
		state.ScreenSource = "identity"
	} else {
		state.ScreenSource = "identity_missing"
	}
	if state.Nodes == 0 {
		state.Nodes = strings.Count(raw, "\n")
	}
	return state
}

func addIdentityMissingNext(fields map[string]string, described describedUITree) {
	if described.SystemSource {
		fields["system_overlay"] = "true"
		fields["next"] = "this looks like a system overlay. Cannot add accessibility id from app code. Use --include-system or coordinate taps."
		return
	}
	fields["next"] = "add a stable screen accessibility identifier to the screen root before mapping"
}

func (c CLI) writeUITreeToolError(err error) error {
	_ = err
	return Fail("tool_missing", map[string]string{"tool": "axe|idb", "next": "mav setup --install axe idb"}).Write(c.Stdout)
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

type describedUITree struct {
	Driver       string
	Result       CommandResult
	ActiveBundle string
	SystemSource bool
}

type readyUITree struct {
	Raw    string
	Driver string
}

func (c CLI) describeUITree(ctx context.Context, cfg Config, prefer string, includeSystem bool) (describedUITree, error) {
	if includeSystem {
		if isPhysicalDevice(cfg) {
			return describedUITree{}, fmt.Errorf("tree_system_unsupported_on_device")
		}
		target := targetFromConfig(cfg)
		baguette, err := baguetteTree(ctx, c.router(), target, true)
		if err == nil {
			return describedUITree{Driver: "baguette", Result: CommandResult{Stdout: baguette}, SystemSource: true}, nil
		}
		if prefer != "auto" {
			return describedUITree{}, err
		}
	}
	target := targetFromConfig(cfg)
	if hasTool(cfg, "axe") {
		driver, _, err := c.router().Route(ctx, drivers.CapTreeAX, target, "axe")
		if err != nil {
			return describedUITree{}, err
		}
		treeDriver, ok := driver.(drivers.TreeDriver)
		if !ok {
			return describedUITree{}, fmt.Errorf("tree_tool_missing")
		}
		tree, err := treeDriver.Tree(ctx, target, drivers.TreeSpec{})
		if err != nil {
			return describedUITree{Driver: driver.ID(), Result: CommandResult{Stderr: err.Error(), Err: err}}, nil
		}
		return describedUITree{Driver: driver.ID(), Result: CommandResult{Stdout: string(tree.JSON)}}, nil
	}
	if prefer == "axe" {
		return describedUITree{}, fmt.Errorf("tree_tool_missing")
	}
	if hasTool(cfg, "idb") {
		result := c.Runner.Run(ctx, "idb", idbTargetArgs(cfg, "ui", "describe-all", "--json", "--nested")...)
		return describedUITree{Driver: "idb", Result: result}, nil
	}
	return describedUITree{}, fmt.Errorf("tree_tool_missing")
}

func (c CLI) recoverEmptyAXTree(ctx context.Context, cfg Config) error {
	if isPhysicalDevice(cfg) {
		return fmt.Errorf("device_accessibility_recovery_unavailable")
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

func (c CLI) waitForTreeReady(ctx context.Context, cfg Config, timeout time.Duration) (readyUITree, error) {
	deadline := time.Now().Add(timeout)
	recovered := false
	for time.Now().Before(deadline) {
		described, err := c.describeUITree(ctx, cfg, "auto", false)
		if err != nil {
			return readyUITree{}, err
		}
		driver := described.Driver
		result := described.Result
		if result.Err == nil && isEmptyAXTree(result.Stdout) && !recovered {
			_ = c.recoverEmptyAXTree(ctx, cfg)
			recovered = true
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if result.Err == nil && !isEmptyAXTree(result.Stdout) && countTreeNodes(result.Stdout) > 1 && len(ExtractElements(result.Stdout)) > 0 {
			state := c.observeUITree(cfg, result.Stdout, driver, false)
			if state.ScreenSource == "identity_missing" {
				time.Sleep(300 * time.Millisecond)
				continue
			}
			return readyUITree{Raw: result.Stdout, Driver: driver}, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return readyUITree{}, fmt.Errorf("tree_not_ready")
}

func (c CLI) uiTap(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	id := flagValue(args, "--id")
	text := flagValue(args, "--text")
	value := flagValue(args, "--value")
	x := flagValue(args, "--x")
	y := flagValue(args, "--y")
	prefer, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe"}).Write(c.Stdout)
	}
	caps := c.resolveCapabilities(ctx, cfg)
	if id != "" || text != "" || value != "" {
		fields := map[string]string{}
		command := "mav ui tap"
		if id != "" {
			fields["id"] = id
			command += " --id " + id
		} else if text != "" {
			fields["text"] = text
			command += " --text " + text
		} else {
			fields["value"] = value
			command += " --value " + value
		}
		if !caps.Tools["axe"] {
			return Fail("tool_missing", map[string]string{"tool": "axe", "next": "use mav ui tap --x X --y Y when AXe is unavailable"}).Write(c.Stdout)
		}
		if value != "" {
			return Fail("tap_target_missing", map[string]string{"usage": "mav ui tap --value VALUE not supported; use --id or --text"}).Write(c.Stdout)
		}
		_ = prefer
		target := targetFromConfig(cfg)
		driver, _, err := c.router().Route(ctx, drivers.CapSemanticTap, target, "axe")
		if err != nil {
			return Fail("tool_missing", map[string]string{"tool": "axe", "next": "use mav ui tap --x X --y Y when AXe is unavailable"}).Write(c.Stdout)
		}
		td, ok := driver.(drivers.TapDriver)
		if !ok {
			return Fail("tool_missing", map[string]string{"tool": "axe"}).Write(c.Stdout)
		}
		_, tapErr := td.Tap(ctx, target, drivers.TapSpec{Selector: drivers.ElementSelector{ID: id, Text: text}})
		result := CommandResult{}
		if tapErr != nil {
			result = CommandResult{Stderr: tapErr.Error(), Err: tapErr}
			diagnosticFields, hasTextDiagnostic := c.diagnoseTextTapFailure(ctx, cfg, text, result.Stderr)
			if hasTextDiagnostic {
				return Fail("ui_tap_text_no_label_match", diagnosticFields).Write(c.Stdout)
			}
			return Fail("ui_tap_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
		}
		fields["driver"] = driver.ID()
		c.appendCurrentCommand(command, result)
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
		xi, _ := strconv.Atoi(x)
		yi, _ := strconv.Atoi(y)
		target := targetFromConfig(cfg)
		driver, _, err := c.router().Route(ctx, drivers.CapCoordTap, target, "idb")
		if err != nil {
			return Fail("tool_missing", map[string]string{"tool": "idb"}).Write(c.Stdout)
		}
		td, ok := driver.(drivers.TapDriver)
		if !ok {
			return Fail("tool_missing", map[string]string{"tool": "idb"}).Write(c.Stdout)
		}
		tapErr := error(nil)
		_, tapErr = td.Tap(ctx, target, drivers.TapSpec{X: xi, Y: yi})
		result := CommandResult{}
		if tapErr != nil {
			result = CommandResult{Stderr: tapErr.Error(), Err: tapErr}
			return Fail("ui_tap_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
		}
		c.appendCurrentCommand("mav ui tap --x "+x+" --y "+y, result)
		return OK("ui.tap", map[string]string{"x": x, "y": y, "driver": driver.ID(), "route_recorded": "false"}).Write(c.Stdout)
	}
	return Fail("tap_target_missing", map[string]string{"usage": "mav ui tap --id ID | --x X --y Y | --text TEXT"}).Write(c.Stdout)
}

func (c CLI) diagnoseTextTapFailure(ctx context.Context, cfg Config, text, stderr string) (map[string]string, bool) {
	if text == "" {
		return nil, false
	}
	result := c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "describe-ui")...)
	if result.Err != nil {
		return nil, false
	}
	labelMatches := 0
	valueMatches := 0
	for _, el := range ExtractElements(result.Stdout) {
		if el.Label == text {
			labelMatches++
		}
		if el.Value == text {
			valueMatches++
		}
	}
	if valueMatches == 0 || labelMatches > 0 {
		return nil, false
	}
	fields := map[string]string{
		"text":          text,
		"matched_value": strconv.Itoa(valueMatches),
		"matched_label": strconv.Itoa(labelMatches),
		"next":          "--text matched AXValue but not AXLabel; prefer --id or tap coordinates from a capture",
	}
	if line := firstLine(stderr); line != "" {
		fields["stderr"] = line
	}
	return fields, true
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
	_, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe"}).Write(c.Stdout)
	}
	text := strings.Join(args, " ")
	target := targetFromConfig(cfg)
	driver, _, err := c.router().Route(ctx, drivers.CapType, target, "axe")
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "axe", "next": "mav setup --install axe"}).Write(c.Stdout)
	}
	td, ok := driver.(drivers.TextDriver)
	if !ok {
		return Fail("tool_missing", map[string]string{"tool": "axe"}).Write(c.Stdout)
	}
	if err := td.Type(ctx, target, drivers.TextSpec{Text: text}); err != nil {
		return Fail("ui_type_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
	}
	fields := map[string]string{
		"chars":      strconv.Itoa(len(text)),
		"chars_sent": strconv.Itoa(len(text)),
		"driver":     driver.ID(),
	}
	return OK("ui.type", fields).Write(c.Stdout)
}

func (c CLI) uiErase(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if isPhysicalDevice(cfg) {
		return Fail("erase_unsupported_on_device", map[string]string{"next": "device erase is not supported; tap and retype the field"}).Write(c.Stdout)
	}
	id := flagValue(args, "--id")
	text := flagValue(args, "--text")
	value := flagValue(args, "--value")
	focused := flagValue(args, "--focused") == "true" || hasFlag(args, "--focused")
	target := targetFromConfig(cfg)
	if err := baguetteErase(ctx, c.router(), target, drivers.TextSpec{Text: text, Selector: drivers.ElementSelector{ID: id, Value: value}, Focused: focused}); err != nil {
		return Fail("ui_erase_failed", map[string]string{"driver": "baguette", "stderr": err.Error()}).Write(c.Stdout)
	}
	fields := map[string]string{"driver": "baguette"}
	if id != "" {
		fields["id"] = id
	}
	if text != "" {
		fields["text"] = text
	}
	if value != "" {
		fields["value"] = value
	}
	if focused {
		fields["focused"] = "true"
	}
	return OK("ui.erase", fields).Write(c.Stdout)
}

func (c CLI) uiHideKeyboard(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	_ = args
	if isPhysicalDevice(cfg) {
		return Fail("hide_keyboard_unsupported_on_device", map[string]string{"next": "device hide-keyboard is not supported; tap outside the field"}).Write(c.Stdout)
	}
	target := targetFromConfig(cfg)
	if err := baguetteHideKeyboard(ctx, c.router(), target); err != nil {
		return Fail("ui_hide_keyboard_failed", map[string]string{"driver": "baguette", "stderr": err.Error()}).Write(c.Stdout)
	}
	return OK("ui.hideKeyboard", map[string]string{"driver": "baguette"}).Write(c.Stdout)
}

func (c CLI) uiSwipe(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	prefer, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe"}).Write(c.Stdout)
	}
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
	target := targetFromConfig(cfg)
	preferred := ""
	if prefer == "axe" || hasTool(cfg, "axe") {
		preferred = "axe"
	}
	driver, _, err := c.router().Route(ctx, drivers.CapSwipe, target, preferred)
	if err != nil {
		if prefer == "axe" {
			return Fail("tool_missing", map[string]string{"tool": "axe", "next": "install AXe or use --prefer-driver auto"}).Write(c.Stdout)
		}
		return Fail("tool_missing", map[string]string{"tool": "axe|idb"}).Write(c.Stdout)
	}
	gd, ok := driver.(interface {
		Swipe(context.Context, drivers.Target, drivers.SwipeSpec) error
	})
	if !ok {
		return Fail("tool_missing", map[string]string{"tool": "axe|idb"}).Write(c.Stdout)
	}
	sx, _ := strconv.Atoi(startX)
	sy, _ := strconv.Atoi(startY)
	ex, _ := strconv.Atoi(endX)
	ey, _ := strconv.Atoi(endY)
	if err := gd.Swipe(ctx, target, drivers.SwipeSpec{Direction: direction, StartX: sx, StartY: sy, EndX: ex, EndY: ey}); err != nil {
		return Fail("ui_swipe_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
	}
	fields := map[string]string{"direction": direction, "driver": driver.ID()}
	if customCoordinates {
		fields["direction"] = "custom"
		fields["start_x"] = startX
		fields["start_y"] = startY
		fields["end_x"] = endX
		fields["end_y"] = endY
	}
	return OK("ui.swipe", fields).Write(c.Stdout)
}

func (c CLI) uiLongPress(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if isPhysicalDevice(cfg) {
		return Fail("gesture_unsupported_on_device", map[string]string{"gesture": "longPress", "next": "use sim for multitouch"}).Write(c.Stdout)
	}
	x := flagValue(args, "--x")
	y := flagValue(args, "--y")
	durationText := flagValue(args, "--duration")
	xv, err := parseRequiredFloat(x, "x")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error(), "usage": "mav ui longPress --x X --y Y [--duration 800ms]"}).Write(c.Stdout)
	}
	yv, err := parseRequiredFloat(y, "y")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error(), "usage": "mav ui longPress --x X --y Y [--duration 800ms]"}).Write(c.Stdout)
	}
	duration := parseFlowDuration(durationText, 800*time.Millisecond)
	if duration <= 0 {
		return Fail("gesture_invalid", map[string]string{"error": "duration_invalid"}).Write(c.Stdout)
	}
	durationMs := int(duration / time.Millisecond)
	target := targetFromConfig(cfg)
	spec := drivers.TapSpec{X: int(xv), Y: int(yv), Duration: durationMs}
	if _, err := baguetteTap(ctx, c.router(), target, spec); err != nil {
		return c.writeGestureError(err)
	}
	fields := map[string]string{
		"x":        formatNumber(xv),
		"y":        formatNumber(yv),
		"duration": strconv.Itoa(durationMs) + "ms",
		"driver":   "baguette",
	}
	c.appendCurrentCommand("mav ui longPress "+strings.Join(args, " "), CommandResult{})
	return OK("ui.longPress", fields).Write(c.Stdout)
}

func (c CLI) uiPinch(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if isPhysicalDevice(cfg) {
		return Fail("gesture_unsupported_on_device", map[string]string{"gesture": "pinch", "next": "use sim for multitouch"}).Write(c.Stdout)
	}
	params := gestureParamsFromArgs(args)
	x, err := parseRequiredFloat(params.X, "x")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	y, err := parseRequiredFloat(params.Y, "y")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if strings.TrimSpace(params.Scale) == "" {
		return Fail("gesture_invalid", map[string]string{"error": "scale_missing"}).Write(c.Stdout)
	}
	scale, err := parseRequiredFloat(params.Scale, "scale")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if scale <= 0 {
		return Fail("gesture_invalid", map[string]string{"error": "scale_must_be_positive"}).Write(c.Stdout)
	}
	panX, err := parseOptionalFloat(params.PanX, 0, "panX")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	panY, err := parseOptionalFloat(params.PanY, 0, "panY")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	duration := parseFlowDuration(params.Duration, 800*time.Millisecond)
	if duration <= 0 {
		return Fail("gesture_invalid", map[string]string{"error": "duration_invalid"}).Write(c.Stdout)
	}
	durationMs := int(duration / time.Millisecond)
	target := targetFromConfig(cfg)
	spec := drivers.PinchSpec{X: int(x), Y: int(y), Scale: scale, PanX: int(panX), PanY: int(panY), DurationMs: durationMs}
	if err := baguettePinch(ctx, c.router(), target, spec); err != nil {
		return c.writeGestureError(err)
	}
	fields := map[string]string{
		"x":        formatNumber(x),
		"y":        formatNumber(y),
		"scale":    formatNumber(scale),
		"duration": strconv.Itoa(durationMs) + "ms",
		"driver":   "baguette",
	}
	if panX != 0 || panY != 0 {
		fields["pan_x"] = formatNumber(panX)
		fields["pan_y"] = formatNumber(panY)
	}
	c.appendCurrentCommand("mav ui pinch "+strings.Join(args, " "), CommandResult{})
	return OK("ui.pinch", fields).Write(c.Stdout)
}

func (c CLI) uiRotate(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if isPhysicalDevice(cfg) {
		return Fail("gesture_unsupported_on_device", map[string]string{"gesture": "rotate", "next": "use sim for multitouch"}).Write(c.Stdout)
	}
	params := gestureParamsFromArgs(args)
	x, err := parseRequiredFloat(params.X, "x")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	y, err := parseRequiredFloat(params.Y, "y")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if strings.TrimSpace(params.Degrees) == "" {
		return Fail("gesture_invalid", map[string]string{"error": "degrees_missing"}).Write(c.Stdout)
	}
	degrees, err := parseRequiredFloat(params.Degrees, "degrees")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	duration := parseFlowDuration(params.Duration, 800*time.Millisecond)
	if duration <= 0 {
		return Fail("gesture_invalid", map[string]string{"error": "duration_invalid"}).Write(c.Stdout)
	}
	durationMs := int(duration / time.Millisecond)
	target := targetFromConfig(cfg)
	spec := drivers.RotateSpec{X: int(x), Y: int(y), Degrees: degrees, DurationMs: durationMs}
	if err := baguetteRotate(ctx, c.router(), target, spec); err != nil {
		return c.writeGestureError(err)
	}
	fields := map[string]string{
		"x":        formatNumber(x),
		"y":        formatNumber(y),
		"degrees":  formatNumber(degrees),
		"duration": strconv.Itoa(durationMs) + "ms",
		"driver":   "baguette",
	}
	c.appendCurrentCommand("mav ui rotate "+strings.Join(args, " "), CommandResult{})
	return OK("ui.rotate", fields).Write(c.Stdout)
}

func (c CLI) uiTwoFingerPan(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if isPhysicalDevice(cfg) {
		return Fail("gesture_unsupported_on_device", map[string]string{"gesture": "twoFingerPan", "next": "use sim for multitouch"}).Write(c.Stdout)
	}
	params := gestureParamsFromArgs(args)
	x, err := parseRequiredFloat(params.X, "x")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	y, err := parseRequiredFloat(params.Y, "y")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	panX, err := parseOptionalFloat(params.PanX, 0, "panX")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	panY, err := parseOptionalFloat(params.PanY, 0, "panY")
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if panX == 0 && panY == 0 {
		return Fail("gesture_invalid", map[string]string{"error": "pan_delta_missing"}).Write(c.Stdout)
	}
	duration := parseFlowDuration(params.Duration, 800*time.Millisecond)
	if duration <= 0 {
		return Fail("gesture_invalid", map[string]string{"error": "duration_invalid"}).Write(c.Stdout)
	}
	durationMs := int(duration / time.Millisecond)
	hold := parseFlowDuration(params.Hold, 0)
	holdMs := int(hold / time.Millisecond)
	target := targetFromConfig(cfg)
	spec := drivers.TwoFingerPanSpec{X: int(x), Y: int(y), PanX: int(panX), PanY: int(panY), DurationMs: durationMs, HoldMs: holdMs}
	if err := baguetteTwoFingerPan(ctx, c.router(), target, spec); err != nil {
		return c.writeGestureError(err)
	}
	fields := map[string]string{
		"x":        formatNumber(x),
		"y":        formatNumber(y),
		"pan_x":    formatNumber(panX),
		"pan_y":    formatNumber(panY),
		"duration": strconv.Itoa(durationMs) + "ms",
		"driver":   "baguette",
	}
	if holdMs > 0 {
		fields["hold"] = strconv.Itoa(holdMs) + "ms"
	}
	c.appendCurrentCommand("mav ui twoFingerPan "+strings.Join(args, " "), CommandResult{})
	return OK("ui.twoFingerPan", fields).Write(c.Stdout)
}

func (c CLI) uiActions(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if isPhysicalDevice(cfg) {
		return Fail("gesture_unsupported_on_device", map[string]string{"gesture": "w3c_actions", "next": "use sim for multitouch"}).Write(c.Stdout)
	}
	path := flagValue(args, "--file")
	if path == "" {
		return Fail("gesture_invalid", map[string]string{"error": "actions_file_missing"}).Write(c.Stdout)
	}
	body, err := loadW3CActionsBody(c.Root, path)
	if err != nil {
		return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	target := targetFromConfig(cfg)
	if err := baguetteW3CActions(ctx, c.router(), target, body); err != nil {
		return c.writeGestureError(err)
	}
	c.appendCurrentCommand("mav ui actions --file "+path, CommandResult{})
	return OK("ui.actions", map[string]string{"driver": "baguette", "file": path}).Write(c.Stdout)
}

func (c CLI) writeGestureError(err error) error {
	return Fail("ui_gesture_failed", map[string]string{"driver": "baguette", "stderr": err.Error()}).Write(c.Stdout)
}

func (c CLI) uiWait(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = cfg
	prefer, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe"}).Write(c.Stdout)
	}
	id := flagValue(args, "--id")
	text := flagValue(args, "--text")
	value := flagValue(args, "--value")
	if id == "" && text == "" && value == "" {
		return Fail("wait_target_missing", map[string]string{"usage": "mav ui wait --id ID | --text TEXT | --value VALUE"}).Write(c.Stdout)
	}
	timeout := 5 * time.Second
	if raw := flagValue(args, "--timeout"); raw != "" {
		if parsed := parseFlowDuration(raw, timeout); parsed > 0 {
			timeout = parsed
		}
	}
	params := map[string]string{"id": id, "text": text, "value": value, "timeout": timeout.String()}
	if err := c.waitForFlowConditionWithPrefer(ctx, params, nil, prefer); err != nil {
		fields := map[string]string{}
		for key, raw := range params {
			if raw != "" {
				fields[key] = raw
			}
		}
		return Fail("ui_wait_timeout", fields).Write(c.Stdout)
	}
	fields := map[string]string{}
	for key, raw := range params {
		if raw != "" && key != "timeout" {
			fields[key] = raw
		}
	}
	return OK("ui.wait", fields).Write(c.Stdout)
}

func (c CLI) uiScrollUntil(ctx context.Context, opts GlobalOptions, args []string) error {
	prefer, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe"}).Write(c.Stdout)
	}
	params := map[string]string{
		"id":        flagValue(args, "--id"),
		"text":      flagValue(args, "--text"),
		"value":     flagValue(args, "--value"),
		"direction": flagValue(args, "--direction"),
		"maxSwipes": flagValue(args, "--max-swipes"),
	}
	fields, err := c.scrollUntilFlowConditionWithPrefer(ctx, params, prefer)
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
	c.resolveConfigTools(&cfg)
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		run, err = NewProjectRunState(c.Root)
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
	target := targetFromConfig(cfg)
	prefer := ""
	switch {
	case isPhysicalDevice(cfg):
		prefer = "idb"
	case hasTool(cfg, "axe"):
		prefer = "axe"
	case hasTool(cfg, "idb"):
		prefer = "idb"
	case hasTool(cfg, "xcrun"):
		prefer = "simctl"
	}
	driver, _, err := c.router().Route(ctx, drivers.CapScreenshot, target, prefer)
	if err != nil {
		return CommandResult{}, fmt.Errorf("capture_tool_missing")
	}
	sd, ok := driver.(drivers.ScreenshotDriver)
	if !ok {
		return CommandResult{}, fmt.Errorf("capture_tool_missing")
	}
	if err := sd.Screenshot(ctx, target, drivers.ScreenshotSpec{OutPath: path}); err != nil {
		return CommandResult{Stderr: err.Error(), Err: err}, nil
	}
	return CommandResult{}, nil
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
	bindings := flowExecBindings{}
	start := time.Now()
	for index, step := range flow.Steps {
		stepStart := time.Now()
		fields, err := c.executeFlowStepBoundWithOptions(ctx, opts, run, index+1, step, bindings)
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

type flowExecBinding struct {
	Raw     string
	JSON    any
	HasJSON bool
}

type flowExecBindings map[string]flowExecBinding

func newFlowExecBinding(raw string) flowExecBinding {
	trimmed := strings.TrimSpace(raw)
	binding := flowExecBinding{Raw: trimmed}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		binding.JSON = decoded
		binding.HasJSON = true
	}
	return binding
}

func substituteExecBindingsInStep(step FlowStep, bindings flowExecBindings) (FlowStep, error) {
	return substituteExecBindingsInStepFields(step, bindings, true)
}

func substituteExecBindingsInStepHeader(step FlowStep, bindings flowExecBindings) (FlowStep, error) {
	return substituteExecBindingsInStepFields(step, bindings, false)
}

func substituteExecBindingsInStepFields(step FlowStep, bindings flowExecBindings, includeDo bool) (FlowStep, error) {
	prepared := step
	var err error
	if step.Params != nil {
		prepared.Params = make(map[string]string, len(step.Params))
	}
	for key, value := range step.Params {
		prepared.Params[key], err = substituteExecBindings(value, bindings)
		if err != nil {
			return FlowStep{}, err
		}
	}
	if step.Any != nil {
		prepared.Any = make([]FlowCondition, len(step.Any))
	}
	for i := range step.Any {
		prepared.Any[i], err = substituteExecBindingsInCondition(step.Any[i], bindings)
		if err != nil {
			return FlowStep{}, err
		}
	}
	if includeDo {
		if step.Do != nil {
			prepared.Do = make([]FlowStep, len(step.Do))
		}
		for i := range step.Do {
			prepared.Do[i], err = substituteExecBindingsInStep(step.Do[i], bindings)
			if err != nil {
				return FlowStep{}, err
			}
		}
	}
	return prepared, nil
}

func substituteExecBindingsInCondition(condition FlowCondition, bindings flowExecBindings) (FlowCondition, error) {
	var err error
	if condition.Text, err = substituteExecBindings(condition.Text, bindings); err != nil {
		return FlowCondition{}, err
	}
	if condition.ID, err = substituteExecBindings(condition.ID, bindings); err != nil {
		return FlowCondition{}, err
	}
	if condition.Value, err = substituteExecBindings(condition.Value, bindings); err != nil {
		return FlowCondition{}, err
	}
	if condition.ChangedFrom, err = substituteExecBindings(condition.ChangedFrom, bindings); err != nil {
		return FlowCondition{}, err
	}
	return condition, nil
}

func substituteExecBindings(value string, bindings flowExecBindings) (string, error) {
	out := value
	searchFrom := 0
	for {
		if searchFrom >= len(out) {
			return out, nil
		}
		start := strings.Index(out[searchFrom:], "${exec.")
		if start < 0 {
			return out, nil
		}
		start += searchFrom
		end := strings.Index(out[start:], "}")
		if end < 0 {
			return "", fmt.Errorf("exec_binding_invalid")
		}
		end += start
		expr := out[start+len("${exec.") : end]
		replacement, err := resolveExecBinding(expr, bindings)
		if err != nil {
			return "", err
		}
		out = out[:start] + replacement + out[end+1:]
		searchFrom = start + len(replacement)
	}
}

func resolveExecBinding(expr string, bindings flowExecBindings) (string, error) {
	name := expr
	path := ""
	if dot := strings.Index(expr, "."); dot >= 0 {
		name = expr[:dot]
		path = expr[dot+1:]
	}
	if name == "" {
		return "", fmt.Errorf("exec_binding_invalid")
	}
	binding, ok := bindings[name]
	if !ok {
		return "", fmt.Errorf("exec_binding_missing name=%s", name)
	}
	if path == "" {
		return binding.Raw, nil
	}
	if !binding.HasJSON {
		return "", fmt.Errorf("exec_json_path_missing name=%s path=%s", name, path)
	}
	value, ok := lookupExecJSONPath(binding.JSON, strings.Split(path, "."))
	if !ok {
		return "", fmt.Errorf("exec_json_path_missing name=%s path=%s", name, path)
	}
	return execBindingValueString(value), nil
}

func lookupExecJSONPath(value any, parts []string) (any, bool) {
	current := value
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[part]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func execBindingValueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(data)
	}
}

func validExecBindingName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		alpha := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		digit := r >= '0' && r <= '9'
		if alpha || r == '_' || i > 0 && (digit || r == '-') {
			continue
		}
		return false
	}
	return true
}

func (c CLI) executeFlowStepBound(ctx context.Context, run RunState, index int, step FlowStep, bindings flowExecBindings) (map[string]string, error) {
	return c.executeFlowStepBoundWithOptions(ctx, GlobalOptions{}, run, index, step, bindings)
}

func flowStepPreferDriver(opts GlobalOptions, step FlowStep) (string, error) {
	prefer := opts.PreferDriver
	if step.Params != nil && step.Params["prefer-driver"] != "" {
		prefer = step.Params["prefer-driver"]
	}
	return normalizePreferDriver(prefer)
}

func (c CLI) executeFlowStepBoundWithOptions(ctx context.Context, opts GlobalOptions, run RunState, index int, step FlowStep, bindings flowExecBindings) (map[string]string, error) {
	if step.Action == "when" {
		prepared, err := substituteExecBindingsInStepHeader(step, bindings)
		if err != nil {
			return nil, err
		}
		return c.executeWhenFlowStepBoundWithOptions(ctx, opts, run, index, prepared, bindings)
	}
	if step.Action == "whileNotVisible" {
		prepared, err := substituteExecBindingsInStepHeader(step, bindings)
		if err != nil {
			return nil, err
		}
		return c.executeWhileNotVisibleFlowStepBoundWithOptions(ctx, opts, run, index, prepared, bindings)
	}
	if step.Action == "evidence.start" && flowBoolParam(step.Params, "network") {
		args := []string{"--run", run.ID, "--network"}
		if port := step.Params["port"]; port != "" {
			args = append(args, "--port", port)
		}
		err := c.withStdout(io.Discard).evidenceStart(ctx, GlobalOptions{}, args)
		return map[string]string{"run": run.ID, "network": "true"}, outputErr(err, "evidence_start_failed")
	}
	prepared, err := substituteExecBindingsInStep(step, bindings)
	if err != nil {
		return nil, err
	}
	switch prepared.Action {
	case "exec":
		if out := prepared.Params["out"]; out != "" && !validExecBindingName(out) {
			return map[string]string{"out": out}, fmt.Errorf("exec_out_invalid")
		}
		fields, raw, err := c.execFlowShellOutput(ctx, run, index, prepared.Params)
		if err != nil {
			return fields, err
		}
		if out := prepared.Params["out"]; out != "" {
			if strings.TrimSpace(raw) == "" {
				fields["out"] = out
				return fields, fmt.Errorf("exec_output_missing")
			}
			bindings[out] = newFlowExecBinding(raw)
			fields["out"] = out
		}
		return fields, nil
	default:
		return c.executeFlowStepWithOptions(ctx, opts, run, index, prepared)
	}
}

func (c CLI) executeFlowStep(ctx context.Context, run RunState, index int, step FlowStep) (map[string]string, error) {
	return c.executeFlowStepWithOptions(ctx, GlobalOptions{}, run, index, step)
}

func (c CLI) executeFlowStepWithOptions(ctx context.Context, opts GlobalOptions, run RunState, index int, step FlowStep) (map[string]string, error) {
	prefer, preferErr := flowStepPreferDriver(opts, step)
	if preferErr != nil {
		return copyParams(step.Params), preferErr
	}
	switch step.Action {
	case "open":
		args := flowArgs(step.Params, "--device", "device", "--ios", "ios", "--udid", "udid", "--locale", "locale", "--language", "language")
		if step.Params["clearState"] == "true" {
			args = append(args, "--clear-state")
		}
		var out bytes.Buffer
		err := c.withStdout(&out).open(ctx, GlobalOptions{}, args)
		return map[string]string{"run": run.ID}, commandOutputErr(err, out.String(), "open_failed")
	case "when":
		return c.executeWhenFlowStepWithOptions(ctx, opts, run, index, step)
	case "whileNotVisible":
		return c.executeWhileNotVisibleFlowStepBoundWithOptions(ctx, opts, run, index, step, nil)
	case "tree":
		err := c.withStdout(io.Discard).ui(ctx, GlobalOptions{PreferDriver: prefer}, []string{"tree"})
		return map[string]string{"driver": prefer}, outputErr(err, "tree_failed")
	case "tap":
		args := flowArgs(step.Params, "--id", "id", "--text", "text", "--value", "value", "--x", "x", "--y", "y")
		var out bytes.Buffer
		err := c.withStdout(&out).uiTap(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), args)
		cmdErr := commandOutputErr(err, out.String(), "tap_failed")
		if cmdErr != nil && step.Params["optional"] == "true" {
			fields := copyParams(step.Params)
			fields["skipped"] = "true"
			return fields, nil
		}
		return copyParams(step.Params), cmdErr
	case "type":
		text := step.Params["text"]
		var out bytes.Buffer
		err := c.withStdout(&out).uiType(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), []string{text})
		fields := map[string]string{"chars": strconv.Itoa(len(text))}
		if prefer != "" {
			fields["driver"] = prefer
		}
		return fields, commandOutputErr(err, out.String(), "type_failed")
	case "erase":
		args := flowArgs(step.Params, "--id", "id", "--text", "text", "--value", "value", "--focused", "focused")
		var out bytes.Buffer
		err := c.withStdout(&out).uiErase(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), args)
		return copyParams(step.Params), commandOutputErr(err, out.String(), "erase_failed")
	case "hideKeyboard":
		var out bytes.Buffer
		err := c.withStdout(&out).uiHideKeyboard(ctx, GlobalOptions{}, c.mustLoadConfig(), nil)
		return map[string]string{"driver": "baguette"}, commandOutputErr(err, out.String(), "hide_keyboard_failed")
	case "swipe":
		args := flowArgs(step.Params, "--direction", "direction", "--start-x", "start-x", "--start-y", "start-y", "--end-x", "end-x", "--end-y", "end-y")
		err := c.withStdout(io.Discard).uiSwipe(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "swipe_failed")
	case "longPress":
		args := flowArgs(step.Params, "--x", "x", "--y", "y", "--duration", "duration")
		err := c.withStdout(io.Discard).uiLongPress(ctx, GlobalOptions{}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "long_press_failed")
	case "pinch":
		args := gestureFlowArgs(step.Params)
		err := c.withStdout(io.Discard).uiPinch(ctx, GlobalOptions{}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "pinch_failed")
	case "rotate":
		args := gestureFlowArgs(step.Params)
		err := c.withStdout(io.Discard).uiRotate(ctx, GlobalOptions{}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "rotate_failed")
	case "twoFingerPan":
		args := gestureFlowArgs(step.Params)
		err := c.withStdout(io.Discard).uiTwoFingerPan(ctx, GlobalOptions{}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "two_finger_pan_failed")
	case "actions":
		args := flowArgs(step.Params, "--file", "file")
		err := c.withStdout(io.Discard).uiActions(ctx, GlobalOptions{}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "actions_failed")
	case "delay", "sleep":
		duration := parseFlowDuration(step.Params["duration"], 1*time.Second)
		if duration <= 0 {
			return nil, fmt.Errorf("delay_invalid")
		}
		time.Sleep(duration)
		return map[string]string{"duration": duration.String()}, nil
	case "wait", "assert":
		err := c.waitForFlowConditionWithPrefer(ctx, step.Params, nil, prefer)
		return copyParams(step.Params), err
	case "waitUntil":
		err := c.waitForFlowConditionWithPrefer(ctx, step.Params, step.Any, prefer)
		return map[string]string{"conditions": strconv.Itoa(len(step.Any))}, err
	case "scrollUntil":
		return c.scrollUntilFlowConditionWithPrefer(ctx, step.Params, prefer)
	case "capture":
		name := step.Params["name"]
		if name != "" {
			return c.captureEvidenceStep(ctx, run, name, step.Params["note"])
		}
		err := c.withStdout(io.Discard).capture(ctx, GlobalOptions{}, []string{"--run", run.ID})
		return map[string]string{"run": run.ID}, outputErr(err, "capture_failed")
	case "evidence.start":
		args := []string{"--run", run.ID}
		if flowBoolParam(step.Params, "network") {
			args = append(args, "--network")
		}
		if port := step.Params["port"]; port != "" {
			args = append(args, "--port", port)
		}
		err := c.withStdout(io.Discard).evidenceStart(ctx, GlobalOptions{}, args)
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
	case "network.start":
		args := flowArgs(step.Params, "--har", "har", "--port", "port")
		args = append(args, "--run", run.ID)
		err := c.withStdout(io.Discard).networkStart(ctx, GlobalOptions{}, args)
		return copyParams(step.Params), outputErr(err, "network_start_failed")
	case "network.stop":
		err := c.withStdout(io.Discard).networkStop(ctx, GlobalOptions{}, []string{"--run", run.ID})
		return map[string]string{"run": run.ID}, outputErr(err, "network_stop_failed")
	case "network.status":
		args := flowArgs(step.Params, "--har", "har")
		args = append(args, "--run", run.ID)
		err := c.withStdout(io.Discard).networkStatus(GlobalOptions{}, args)
		return copyParams(step.Params), outputErr(err, "network_status_failed")
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
		return map[string]string{"run": run.ID, "data": filepath.Join(run.Dir, "report.json"), "file": filepath.Join(run.Dir, "report.json")}, outputErr(err, "report_failed")
	default:
		return nil, fmt.Errorf("unknown_step")
	}
}

func (c CLI) executeWhenFlowStep(ctx context.Context, run RunState, index int, step FlowStep) (map[string]string, error) {
	return c.executeWhenFlowStepWithOptions(ctx, GlobalOptions{}, run, index, step)
}

func (c CLI) executeWhenFlowStepWithOptions(ctx context.Context, opts GlobalOptions, run RunState, index int, step FlowStep) (map[string]string, error) {
	if len(step.Do) == 0 {
		return nil, fmt.Errorf("when_do_missing")
	}
	prefer, preferErr := flowStepPreferDriver(opts, step)
	if preferErr != nil {
		return copyParams(step.Params), preferErr
	}
	matched, err := c.evaluateFlowConditionWithPrefer(ctx, step.Params, step.Any, prefer)
	if err != nil {
		return copyParams(step.Params), err
	}
	fields := copyParams(step.Params)
	fields["matched"] = strconv.FormatBool(matched)
	fields["steps"] = strconv.Itoa(len(step.Do))
	if !matched {
		fields["skipped"] = strconv.Itoa(len(step.Do))
		return fields, nil
	}
	for childIndex, child := range step.Do {
		childStart := time.Now()
		childFields, err := c.executeFlowStepWithOptions(ctx, opts, run, childIndex+1, child)
		elapsed := time.Since(childStart)
		if err != nil {
			fields["child_step"] = strconv.Itoa(childIndex + 1)
			fields["child_action"] = child.Action
			fields["child_code"] = err.Error()
			for key, value := range childFields {
				fields["child_"+key] = value
			}
			return fields, err
		}
		appendFlowStep(run, index, "when."+child.Action, elapsed, "ok", childFields)
	}
	fields["executed"] = strconv.Itoa(len(step.Do))
	return fields, nil
}

func (c CLI) executeWhenFlowStepBound(ctx context.Context, run RunState, index int, step FlowStep, bindings flowExecBindings) (map[string]string, error) {
	return c.executeWhenFlowStepBoundWithOptions(ctx, GlobalOptions{}, run, index, step, bindings)
}

func (c CLI) executeWhenFlowStepBoundWithOptions(ctx context.Context, opts GlobalOptions, run RunState, index int, step FlowStep, bindings flowExecBindings) (map[string]string, error) {
	if len(step.Do) == 0 {
		return nil, fmt.Errorf("when_do_missing")
	}
	prefer, preferErr := flowStepPreferDriver(opts, step)
	if preferErr != nil {
		return copyParams(step.Params), preferErr
	}
	matched, err := c.evaluateFlowConditionWithPrefer(ctx, step.Params, step.Any, prefer)
	if err != nil {
		return copyParams(step.Params), err
	}
	fields := copyParams(step.Params)
	fields["matched"] = strconv.FormatBool(matched)
	fields["steps"] = strconv.Itoa(len(step.Do))
	if !matched {
		fields["skipped"] = strconv.Itoa(len(step.Do))
		return fields, nil
	}
	for childIndex, child := range step.Do {
		childStart := time.Now()
		childFields, err := c.executeFlowStepBoundWithOptions(ctx, opts, run, childIndex+1, child, bindings)
		elapsed := time.Since(childStart)
		if err != nil {
			fields["child_step"] = strconv.Itoa(childIndex + 1)
			fields["child_action"] = child.Action
			fields["child_code"] = err.Error()
			for key, value := range childFields {
				fields["child_"+key] = value
			}
			return fields, err
		}
		appendFlowStep(run, index, "when."+child.Action, elapsed, "ok", childFields)
	}
	fields["executed"] = strconv.Itoa(len(step.Do))
	return fields, nil
}

func (c CLI) executeWhileNotVisibleFlowStepBoundWithOptions(ctx context.Context, opts GlobalOptions, run RunState, index int, step FlowStep, bindings flowExecBindings) (map[string]string, error) {
	if len(step.Do) == 0 {
		return nil, fmt.Errorf("while_do_missing")
	}
	prefer, preferErr := flowStepPreferDriver(opts, step)
	if preferErr != nil {
		return copyParams(step.Params), preferErr
	}
	timeout := parseFlowDuration(step.Params["timeout"], 30*time.Second)
	if timeout <= 0 {
		return copyParams(step.Params), fmt.Errorf("while_timeout_invalid")
	}
	start := time.Now()
	fields := copyParams(step.Params)
	iterations := 0
	executed := 0
	for {
		matched, err := c.evaluateFlowConditionWithPrefer(ctx, step.Params, step.Any, prefer)
		if err != nil {
			return fields, err
		}
		if matched {
			fields["matched"] = "true"
			fields["iterations"] = strconv.Itoa(iterations)
			fields["executed"] = strconv.Itoa(executed)
			return fields, nil
		}
		if time.Since(start) >= timeout {
			fields["matched"] = "false"
			fields["iterations"] = strconv.Itoa(iterations)
			fields["executed"] = strconv.Itoa(executed)
			return fields, fmt.Errorf("while_timeout")
		}
		iterations++
		for childIndex, child := range step.Do {
			childStart := time.Now()
			var childFields map[string]string
			var err error
			if bindings != nil {
				childFields, err = c.executeFlowStepBoundWithOptions(ctx, opts, run, childIndex+1, child, bindings)
			} else {
				childFields, err = c.executeFlowStepWithOptions(ctx, opts, run, childIndex+1, child)
			}
			elapsed := time.Since(childStart)
			if err != nil {
				fields["child_step"] = strconv.Itoa(childIndex + 1)
				fields["child_action"] = child.Action
				fields["child_code"] = err.Error()
				for key, value := range childFields {
					fields["child_"+key] = value
				}
				return fields, err
			}
			executed++
			appendFlowStep(run, index, "whileNotVisible."+child.Action, elapsed, "ok", childFields)
			if time.Since(start) >= timeout {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (c CLI) currentOrNewRun() (RunState, error) {
	run, err := LoadRun(c.Root, "")
	if err == nil && run.ID != "" {
		return run, nil
	}
	run, err = NewProjectRunState(c.Root)
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

func commandOutputErr(err error, out, code string) error {
	if err != nil {
		return fmt.Errorf("%s", code)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "fail code=") {
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
			"index":   strconv.Itoa(i + 1),
			"id":      el.ID,
			"label":   el.Label,
			"role":    el.Role,
			"value":   el.Value,
			"enabled": el.Enabled,
			"subrole": el.Subrole,
			"title":   el.Title,
			"pid":     el.PID,
			"focused": el.Focused,
			"frame":   el.Frame,
		}
		parts := []string{"node"}
		keys := []string{"index", "id", "label", "role", "value", "enabled", "subrole", "title", "pid", "focused", "frame"}
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

// writeAgentElementLines emits the compact agent-mode form: one `node` line
// per element, fewer fields, ranked-and-capped by AgentTree. The output
// is still kv-pair shaped (not JSON) so existing line-oriented agents can
// parse it the same way as the legacy mode; only the field set differs.
//
// Each line carries `actionable=true|false` so an agent can pick a target
// without re-deriving the role-list heuristic in its own code.
func writeAgentElementLines(w io.Writer, agents []AgentElement) error {
	for i, ae := range agents {
		fields := map[string]string{
			"index":      strconv.Itoa(i + 1),
			"id":         ae.ID,
			"label":      ae.Label,
			"role":       ae.Role,
			"value":      ae.Value,
			"title":      ae.Title,
			"subrole":    ae.Subrole,
			"focused":    ae.Focused,
			"enabled":    ae.Enabled,
			"frame":      ae.Frame,
			"actionable": strconv.FormatBool(ae.Actionable),
		}
		parts := []string{"node"}
		// Order: index first; actionable second so it's prominent; then
		// identity (id, label), descriptive (role, value, title, subrole),
		// then state (focused, enabled), then frame (only present with
		// --with-frame).
		keys := []string{"index", "actionable", "id", "label", "role", "value", "title", "subrole", "focused", "enabled", "frame"}
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

func (c CLI) mustLoadConfig() Config {
	cfg, _ := LoadConfig(c.Root)
	c.resolveConfigTools(&cfg)
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
		c.resolveConfigTools(&cfg)
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
	c.resolveConfigTools(&cfg)
	name = safeFileName(name)
	idx := len(LoadEvidenceSteps(run)) + 1
	file := filepath.Join(run.Dir, "steps", fmt.Sprintf("%02d_%s.png", idx, name))
	result, err := c.captureScreenshot(ctx, cfg, file)
	if err != nil {
		return nil, fmt.Errorf("capture_tool_missing")
	}
	if result.Err != nil {
		return map[string]string{"stderr": firstLine(result.Stderr)}, fmt.Errorf("capture_failed")
	}

	step := EvidenceStep{Name: name, Note: note, File: file, Kind: "screenshot"}
	attachStepTimings(run, &step)
	attachStepTree(ctx, c, cfg, run, &step, idx, name)

	if err := AppendEvidenceStep(run, step); err != nil {
		return nil, err
	}

	fields := map[string]string{"name": name, "file": file}
	if step.TreePath != "" {
		fields["tree"] = step.TreePath
	}
	if step.DeltaPath != "" {
		fields["tree_delta"] = step.DeltaPath
	}
	return fields, nil
}

// attachStepTimings populates MonotonicMs and (when a video recording is
// active) VideoOffsetMs on the step. The offset is computed relative to the
// recording's start, persisted in <runDir>/video.start.ms when video.start
// runs. Best-effort: missing file -> VideoOffsetMs stays zero.
func attachStepTimings(run RunState, step *EvidenceStep) {
	now := time.Now().UnixMilli()
	step.MonotonicMs = now
	if data, err := os.ReadFile(filepath.Join(run.Dir, "video.start.ms")); err == nil {
		if start, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); parseErr == nil && start > 0 {
			step.VideoOffsetMs = now - start
		}
	}
}

// attachStepTree extracts the current screen's accessibility tree via axe,
// persists the compact + full + delta JSON under <runDir>/trees/, and
// populates the tree fields on step. Best-effort: a failure to extract the
// tree leaves the step's screenshot untouched. The previous step's tree (if
// any) feeds the delta.
func attachStepTree(ctx context.Context, c CLI, cfg Config, run RunState, step *EvidenceStep, idx int, name string) {
	described, err := c.describeUITree(ctx, cfg, "auto", false)
	if err != nil || described.Result.Err != nil || described.Result.Stdout == "" {
		return
	}
	raw := ExtractElementsRaw(described.Result.Stdout)
	if len(raw) == 0 {
		return
	}
	previous := loadPreviousTree(run)
	persisted, err := PersistTree(run.Dir, idx, name, raw, previous)
	if err != nil {
		return
	}
	step.TreePath = persisted.CompactPath
	step.FullPath = persisted.FullPath
	step.DeltaPath = persisted.DeltaPath
	step.TreeHash = persisted.Hash
}

// loadPreviousTree returns the elements of the most recent persisted tree
// snapshot for the run, used as the baseline for the next step's delta.
// Returns nil (signalling "no previous") when there is no prior snapshot.
func loadPreviousTree(run RunState) []Element {
	steps := LoadEvidenceSteps(run)
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].TreePath == "" {
			continue
		}
		elements, err := LoadPersistedTree(steps[i].TreePath)
		if err == nil {
			return elements
		}
	}
	return nil
}

func (c CLI) waitForFlowCondition(ctx context.Context, params map[string]string, any []FlowCondition) error {
	return c.waitForFlowConditionWithPrefer(ctx, params, any, "auto")
}

func (c CLI) waitForFlowConditionWithPrefer(ctx context.Context, params map[string]string, any []FlowCondition, prefer string) error {
	timeout := parseFlowDuration(params["timeout"], 5*time.Second)
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		ok, err := c.evaluateFlowConditionWithPrefer(ctx, params, any, prefer)
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
	return c.scrollUntilFlowConditionWithPrefer(ctx, params, "auto")
}

func (c CLI) scrollUntilFlowConditionWithPrefer(ctx context.Context, params map[string]string, prefer string) (map[string]string, error) {
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
		ok, err := c.evaluateSingleConditionWithPrefer(ctx, FlowCondition{Text: params["text"], ID: params["id"], Value: params["value"]}, prefer)
		if err != nil {
			return nil, err
		}
		if ok {
			return map[string]string{"swipes": strconv.Itoa(i), "direction": direction}, nil
		}
		if i == maxSwipes {
			break
		}
		if err := c.withStdout(io.Discard).uiSwipe(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), []string{"--direction", direction}); err != nil {
			return nil, fmt.Errorf("swipe_failed")
		}
		time.Sleep(500 * time.Millisecond)
	}
	return map[string]string{"swipes": strconv.Itoa(maxSwipes), "direction": direction}, fmt.Errorf("scroll_until_timeout")
}

func (c CLI) execFlowShell(ctx context.Context, run RunState, index int, params map[string]string) (map[string]string, error) {
	fields, _, err := c.execFlowShellOutput(ctx, run, index, params)
	return fields, err
}

func (c CLI) execFlowShellOutput(ctx context.Context, run RunState, index int, params map[string]string) (map[string]string, string, error) {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return nil, "", fmt.Errorf("config_not_found")
	}
	if !cfg.AllowShell {
		return map[string]string{"next": "set allow_shell: true in .mav/config.yaml for trusted project-local flows"}, "", fmt.Errorf("shell_not_allowed")
	}
	command := params["cmd"]
	if strings.TrimSpace(command) == "" {
		return nil, "", fmt.Errorf("exec_cmd_missing")
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
		return fields, stdout.String(), fmt.Errorf("exec_timeout")
	}
	if contains := params["contains"]; contains != "" && !strings.Contains(stdout.String()+stderr.String(), contains) {
		fields["contains"] = contains
		return fields, stdout.String(), fmt.Errorf("exec_assert_failed")
	}
	if err != nil {
		return fields, stdout.String(), fmt.Errorf("exec_failed")
	}
	return fields, stdout.String(), nil
}

func (c CLI) evaluateFlowCondition(ctx context.Context, params map[string]string, any []FlowCondition) (bool, error) {
	return c.evaluateFlowConditionWithPrefer(ctx, params, any, "auto")
}

func (c CLI) evaluateFlowConditionWithPrefer(ctx context.Context, params map[string]string, any []FlowCondition, prefer string) (bool, error) {
	if len(any) > 0 {
		for _, condition := range any {
			ok, err := c.evaluateSingleConditionWithPrefer(ctx, condition, prefer)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	return c.evaluateSingleConditionWithPrefer(ctx, FlowCondition{Text: params["text"], ID: params["id"], Value: params["value"], ChangedFrom: params["changedFrom"]}, prefer)
}

func (c CLI) evaluateSingleCondition(ctx context.Context, condition FlowCondition) (bool, error) {
	return c.evaluateSingleConditionWithPrefer(ctx, condition, "auto")
}

func (c CLI) evaluateSingleConditionWithPrefer(ctx context.Context, condition FlowCondition, prefer string) (bool, error) {
	if condition.ChangedFrom != "" {
		return c.screenshotChangedFrom(ctx, condition.ChangedFrom)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return false, fmt.Errorf("config_not_found")
	}
	c.resolveConfigTools(&cfg)
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil {
		return false, fmt.Errorf("tree_failed")
	}
	result := described.Result
	if result.Err != nil {
		return false, fmt.Errorf("tree_failed")
	}
	return flowConditionMatchesElements(ExtractElements(result.Stdout), condition), nil
}

func flowConditionMatchesElements(elements []Element, condition FlowCondition) bool {
	if condition.Text == "" && condition.ID == "" && condition.Value == "" {
		return false
	}
	for _, el := range elements {
		if condition.ID != "" && el.ID != condition.ID {
			continue
		}
		if condition.Text != "" && el.Label != condition.Text && el.Title != condition.Text {
			continue
		}
		if condition.Value != "" && el.Value != condition.Value {
			continue
		}
		return true
	}
	return false
}

func parseElementFrame(frame string) (float64, float64, float64, float64, bool) {
	frame = strings.TrimSpace(frame)
	var x, y, width, height float64
	if _, err := fmt.Sscanf(frame, "{{%f, %f}, {%f, %f}}", &x, &y, &width, &height); err == nil {
		return x, y, width, height, true
	}
	if _, err := fmt.Sscanf(frame, "{{%f,%f},{%f,%f}}", &x, &y, &width, &height); err == nil {
		return x, y, width, height, true
	}
	return 0, 0, 0, 0, false
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
	c.resolveConfigTools(&cfg)
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
	if cfg, err := LoadConfig(c.Root); err == nil && cfg.SimulatorUDID != "" {
		removeSimulatorLock(cfg.SimulatorUDID, c.Root)
	}
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
	c.resolveConfigTools(&cfg)
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
	names := parseCrashNames(result.Stdout)
	fields := map[string]string{"count": strconv.Itoa(len(names))}

	if len(names) == 0 {
		return OK("crashes", fields).Write(c.Stdout)
	}

	if run, err := LoadRun(c.Root, ""); err == nil {
		crashDir := filepath.Join(run.Dir, "crashes")
		driver, _, err := c.router().Route(ctx, drivers.CapCrashFetch, targetFromConfig(cfg), "idb")
		if err != nil {
			return Fail("tool_missing", map[string]string{"tool": "idb"}).Write(c.Stdout)
		}
		cd, ok := driver.(drivers.CrashDriver)
		if !ok {
			return Fail("tool_missing", map[string]string{"tool": "idb"}).Write(c.Stdout)
		}
		entries, err := cd.CrashFetch(ctx, targetFromConfig(cfg), drivers.CrashSpec{BundleID: cfg.BundleID, OutDir: crashDir})
		if err != nil {
			return Fail("crashes_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		summarised := 0
		for idx, entry := range entries {
			summary, err := ParseIPS(entry.Body)
			if err != nil {
				continue
			}
			txtPath := filepath.Join(crashDir, fmt.Sprintf("%02d.txt", idx+1))
			_ = os.WriteFile(txtPath, []byte(summary.OneLiner()+"\n"), 0o644)
			summarised++
		}
		fields["fetched"] = strconv.Itoa(len(entries))
		fields["summarised"] = strconv.Itoa(summarised)
		fields["dir"] = crashDir
	}

	return OK("crashes", fields).Write(c.Stdout)
}

// parseCrashNames extracts crash report names from `idb crash list` stdout.
// idb emits one report identifier per line (the file name without the
// trailing `.ips`). Whitespace-only lines and ANSI-styled rows are
// tolerated.
func parseCrashNames(stdout string) []string {
	var names []string
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Some idb versions include a leading bullet/dash; strip it.
		trimmed = strings.TrimLeft(trimmed, "-* ")
		if trimmed == "" {
			continue
		}
		// idb's `crash list` includes a header line on some versions; skip
		// anything obviously not a crash identifier (starts with whitespace,
		// uppercase keyword we don't expect).
		if strings.EqualFold(trimmed, "no crashes") {
			continue
		}
		names = append(names, trimmed)
	}
	return names
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
	fields := map[string]string{"run": run.ID, "data": path, "file": path, "video": "missing"}
	if video, validation := reportVideo(run); video != "" {
		if validation.OK {
			fields["video"] = video
			fields["video_duration"] = validation.Duration.String()
			if mp4 := reportVideoMP4(run); mp4 != "" {
				fields["video_mp4"] = mp4
			}
		} else {
			fields["video"] = "invalid"
			fields["video_file"] = video
			fields["video_issue"] = validation.Issue
		}
	}
	if network := reportNetwork(run); network.HAR != "" {
		fields["network"] = network.HAR
		fields["network_requests"] = strconv.Itoa(network.Requests)
		fields["network_responses"] = strconv.Itoa(network.Responses)
		if network.Issue != "" {
			fields["network_issue"] = network.Issue
		}
	}
	fields["next"] = "author " + filepath.Join(run.Dir, "report.html") + " from report.json; the JSON manifest is not the deliverable"
	return OK("evidence.report", fields).Write(c.Stdout)
}

func (c CLI) evidenceStart(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTools(&cfg)
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	if issue := existingEvidenceIssue(run); issue != "" {
		return Fail("evidence_run_not_clean", map[string]string{"run": run.ID, "issue": issue, "next": "start a new run with mav open, or remove old evidence from the run directory"}).Write(c.Stdout)
	}
	path, pid, err := c.startVideoRecording(ctx, cfg, run)
	if err != nil {
		if err.Error() == "video_unsupported" {
			return Fail("video_unsupported", map[string]string{"target": "device", "next": "use simulator video or capture screenshots; device video is not supported in this PR"}).Write(c.Stdout)
		}
		return Fail("evidence_start_failed", map[string]string{"run": run.ID, "error": err.Error()}).Write(c.Stdout)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, "video.pid"), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return err
	}
	// Persist the wall-clock start so subsequent evidence steps can compute
	// video_offset_ms (P4.4 PTS sync). Best-effort; failure here only loses
	// the offset calibration, not the recording.
	_ = os.WriteFile(filepath.Join(run.Dir, "video.start.ms"), []byte(strconv.FormatInt(time.Now().UnixMilli(), 10)+"\n"), 0o644)
	appendCommand(run, "mav evidence start", CommandResult{})
	fields := map[string]string{"run": run.ID, "file": path, "pid": strconv.Itoa(pid)}
	if hasFlag(args, "--network") {
		var out bytes.Buffer
		networkArgs := []string{"--run", run.ID}
		if port := flagValue(args, "--port"); port != "" {
			networkArgs = append(networkArgs, "--port", port)
		}
		if err := commandOutputErr(c.withStdout(&out).networkStart(ctx, GlobalOptions{}, networkArgs), out.String(), "network_start_failed"); err != nil {
			_ = stopProcess(pid)
			_ = os.Remove(filepath.Join(run.Dir, "video.pid"))
			removeProcess(run, pid)
			return Fail("evidence_network_start_failed", map[string]string{
				"run":   run.ID,
				"video": path,
				"error": firstLine(out.String()),
			}).Write(c.Stdout)
		}
		fields["network"] = filepath.Join(run.Dir, "network.har")
	}
	return OK("evidence.start", fields).Write(c.Stdout)
}

func (c CLI) evidenceStep(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTools(&cfg)
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
	idx := len(steps) + 1
	file := filepath.Join(run.Dir, "steps", fmt.Sprintf("%02d_%s.png", idx, name))
	result, err := c.captureScreenshot(ctx, cfg, file)
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "axe|idb|xcrun"}).Write(c.Stdout)
	}
	if result.Err != nil {
		return Fail("evidence_step_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
	}
	step := EvidenceStep{Name: name, Note: flagValue(args, "--note"), File: file, Kind: "screenshot"}
	attachStepTimings(run, &step)
	attachStepTree(ctx, c, cfg, run, &step, idx, name)
	if err := AppendEvidenceStep(run, step); err != nil {
		return err
	}
	appendCommand(run, "mav evidence step --name "+name, result)
	fields := map[string]string{"run": run.ID, "name": name, "file": file}
	if step.TreePath != "" {
		fields["tree"] = step.TreePath
	}
	if step.DeltaPath != "" {
		fields["tree_delta"] = step.DeltaPath
	}
	return OK("evidence.step", fields).Write(c.Stdout)
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
	if mp4, err := c.transcodeEvidenceVideo(ctx, fields["file"]); err == nil {
		fields["video_mp4"] = mp4
	} else {
		fields["video_mp4_warn"] = err.Error()
	}
	if findRunningNetworkPID(run) > 0 {
		var out bytes.Buffer
		if err := c.withStdout(&out).networkStop(ctx, GlobalOptions{}, []string{"--run", run.ID}); err == nil {
			fields["network"] = filepath.Join(run.Dir, "network.har")
		} else {
			fields["network_warn"] = firstLine(out.String())
		}
	}
	if !hasFlag(args, "--no-capture") {
		cfg, cfgErr := LoadConfig(c.Root)
		if cfgErr == nil {
			c.resolveConfigTools(&cfg)
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

func (c CLI) transcodeEvidenceVideo(ctx context.Context, source string) (string, error) {
	if source == "" {
		return "", fmt.Errorf("video_source_missing")
	}
	out := strings.TrimSuffix(source, filepath.Ext(source)) + ".mp4"
	result := c.Runner.Run(ctx, "/usr/bin/avconvert",
		"-p", "PresetHighestQuality",
		"-s", source,
		"-o", out,
		"--replace",
	)
	if result.Err != nil || result.Code != 0 {
		issue := firstLine(result.Stderr)
		if issue == "" {
			issue = firstLine(result.Stdout)
		}
		if issue == "" && result.Err != nil {
			issue = result.Err.Error()
		}
		if issue == "" {
			issue = "avconvert_failed"
		}
		return "", fmt.Errorf("%s", issue)
	}
	if !fileExists(out) {
		return "", fmt.Errorf("video_mp4_not_written")
	}
	return out, nil
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
	if udid := targetUDID(cfg); udid != "" {
		out = append(out, "--udid", udid)
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
	if isPhysicalDevice(cfg) {
		return "", 0, fmt.Errorf("video_unsupported")
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
	value = strings.TrimSpace(transliterateLatin(strings.ToLower(value)))
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

func transliterateLatin(value string) string {
	var b strings.Builder
	for _, r := range value {
		if repl, ok := latinASCII[r]; ok {
			b.WriteString(repl)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var latinASCII = map[rune]string{
	'á': "a", 'à': "a", 'â': "a", 'ä': "a", 'ã': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'ç': "c", 'ć': "c", 'č': "c",
	'ď': "d", 'đ': "d",
	'é': "e", 'è': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ė': "e", 'ę': "e",
	'í': "i", 'ì': "i", 'î': "i", 'ï': "i", 'ī': "i", 'į': "i",
	'ñ': "n", 'ń': "n",
	'ó': "o", 'ò': "o", 'ô': "o", 'ö': "o", 'õ': "o", 'ø': "o", 'ō': "o", 'ő': "o",
	'ú': "u", 'ù': "u", 'û': "u", 'ü': "u", 'ū': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ý': "y", 'ÿ': "y",
	'ř': "r", 'ŕ': "r",
	'š': "s", 'ś': "s",
	'ť': "t",
	'ž': "z", 'ź': "z", 'ż': "z",
	'æ': "ae", 'œ': "oe", 'ß': "ss",
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
