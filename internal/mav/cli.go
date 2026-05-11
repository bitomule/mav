package mav

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type CLI struct {
	Runner Runner
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Root   string
	// RouteEdgeRetry overrides the same-driver retry count used by
	// `executeRouteEdge`. Zero means "use `DefaultEdgeRetry`". A
	// negative value means "no retry". `goScreen` sets this from
	// the `--edge-retry` flag before calling `navigateToScreen`.
	RouteEdgeRetry int
	// RouteEdgeTTL is the staleness threshold the route engine uses
	// when deciding whether a recorded edge is still trustworthy.
	// Zero means "use `DefaultEdgeTTL`". Set from `--edge-ttl` in
	// `goScreen`.
	RouteEdgeTTL time.Duration
	// RouteNoCoordTap disables the coord-tap fallback executed when
	// id/text-based taps have exhausted the driver chain without
	// changing the tree. Set from `--no-coord-tap` in `goScreen`
	// when an operator wants to keep stale-id failures loud
	// instead of silently masked by a coordinate retry.
	RouteNoCoordTap bool
}

type GlobalOptions struct {
	Verbose      bool
	Raw          bool
	Help         bool
	PreferDriver string
	// SkipAutoObserve suppresses the post-tap tree probe that
	// promotes pending map actions to edges. Set by callers that
	// manage the observation lifecycle themselves (route playback in
	// `mav go`, flow steps that bracket their own observations) so
	// they don't accidentally learn an edge to a transitional or
	// off-route screen.
	SkipAutoObserve bool
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
	case "map":
		return c.mapCommand(rest[1:])
	case "logs":
		return c.logs(opts, rest[1:])
	case "stop":
		return c.stop(ctx, opts, rest[1:])
	case "crashes":
		return c.crashes(ctx, opts, rest[1:])
	case "evidence":
		return c.evidence(opts, rest[1:])
	case "approach":
		return c.approachCommand(ctx, opts, rest[1:])
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
	case "auto", "axe", "appium":
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
  open        Build, install, launch, and start run logs.
  ui          Inspect and control the current UI.
  capture     Capture the current screen.
  run         Execute a native MAV YAML flow.
  go          Navigate from app launch to a mapped screen with evidence.
  map         Inspect and validate the learned app map.
  logs        Read captured run logs.
  stop        Stop run-owned background processes.
  crashes     List crashes for the configured app.
  evidence    Start/step/stop/report evidence.

Global flags:
  --raw       Emit raw underlying tool output where supported.
  --verbose   Print extra debug details where supported.
  --prefer-driver auto|axe|appium
              Prefer a UI driver for semantic tree/tap commands.
  --help,-h   Show help.
`
	case "setup":
		return "Usage:\n  mav setup [--non-interactive]\n  mav setup --install axe idb appium\n"
	case "install-skills":
		return "Usage: mav install-skills\n"
	case "sim":
		return `Usage:
  mav sim list
  mav sim select --device "iPhone 17 Pro Max" --ios 26 [--locale es_ES] [--language es]
  mav sim select --udid <simulator-udid>
  mav sim boot
`
	case "open":
		return `Usage:
  mav open [--device NAME] [--ios VERSION] [--udid UDID] [--locale LOCALE] [--language LANG] [--clear-state] [--warm-appium]
`
	case "ui":
		return `Usage:
  mav ui tree [--prefer-driver auto|axe|appium] [--include-system]
  mav ui tap --id ID [--prefer-driver auto|axe|appium]
  mav ui tap --x X --y Y
  mav ui tap --text TEXT [--prefer-driver auto|axe|appium]
  mav ui tap --value VALUE [--prefer-driver auto|appium]
  mav ui type TEXT [--prefer-driver auto|axe|appium]
  mav ui erase [--id ID | --text TEXT | --value VALUE | --focused true] [--prefer-driver appium]
  mav ui hideKeyboard
  mav ui swipe [--direction up|down|left|right]
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
		return "Usage: mav ui tree [--prefer-driver auto|axe|appium] [--include-system]\n\nPrints compact screen metadata followed by bounded node lines with id, label, role, value, enabled, subrole, title, pid, focused, and frame when available. --include-system lets Appium query the active foreground app when a system service, permission prompt, or cross-app view is in front.\n"
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
	case "map":
		return `Usage:
  mav map list
  mav map show <screen-id>
  mav map graph
  mav map verify
  mav map prune [--filter coordinate-edges|duplicate-selectors|low-confidence] [--apply-warnings] [--dry-run]
`
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
	for _, tool := range []string{"axe", "idb"} {
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
				if driverStatus.Version != "" {
					fields["xcuitest_driver_version"] = driverStatus.Version
					fields["predicate_supported"] = strconv.FormatBool(driverStatus.PredicateOK)
				}
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
		return c.setupProject(opts, args)
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
			if isAppiumXCUITestServerVersionMismatch(result.Stdout+"\n"+result.Stderr) && strings.Join(cmd, " ") == "appium driver install xcuitest" {
				fallback := []string{"appium", "driver", "install", "xcuitest@8"}
				if opts.Verbose {
					fmt.Fprintln(c.Stderr, strings.Join(fallback, " "))
				}
				result = c.Runner.Run(ctx, fallback[0], fallback[1:]...)
				if result.Err == nil {
					continue
				}
			}
			return false, Fail("setup_failed", map[string]string{"tool": "appium", "stderr": compactCommandOutput(result)}).Write(c.Stdout)
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

func isAppiumXCUITestServerVersionMismatch(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "xcuitest") &&
		strings.Contains(lower, "server version") &&
		strings.Contains(lower, "does not meet")
}

func compactCommandOutput(result CommandResult) string {
	text := strings.TrimSpace(result.Stderr)
	if text == "" {
		text = strings.TrimSpace(result.Stdout)
	}
	if text == "" && result.Err != nil {
		text = result.Err.Error()
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 500 {
		return text[:500] + "..."
	}
	return text
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
	if !exists(filepath.Join(c.Root, MapIndexFile)) && !exists(filepath.Join(c.Root, AppMapFile)) {
		_ = SaveAppMap(c.Root, DefaultAppMap(cfg.BundleID))
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
	if _, appiumErr := c.Runner.LookPath("appium"); appiumErr != nil {
		fields["multitouch"] = "missing"
		fields["multitouch_next"] = "mav setup --install appium"
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
	warmAppium := hasFlag(args, "--warm-appium")
	var appiumWarmup <-chan appiumWarmupResult
	probeLogPID, probeLogErr := c.startProbeLogs(ctx, cfg, run)
	if probeLogErr != nil {
		appendFile(run.LogsPath, "mav probe log capture failed: "+probeLogErr.Error()+"\n")
	}
	appPath, failedStep, failedResult := c.runLaunchRecipe(ctx, cfg, run, hasFlag(args, "--clear-state"))
	if failedStep != nil {
		fields := map[string]string{"run": run.ID, "logs": run.LogsPath, "step": failedStep.Name, "stderr": firstLine(failedResult.Stderr)}
		if fields["stderr"] == "" {
			fields["stderr"] = failedResult.Err.Error()
		}
		return Fail("launch_step_failed", fields).Write(c.Stdout)
	}
	if warmAppium {
		appiumWarmup = c.startAppiumWarmup(ctx, cfg, run, true)
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
	if appiumWarmup != nil {
		result := <-appiumWarmup
		fields["appium_warmup"] = result.Status
		if result.SessionID != "" {
			fields["appium_session"] = result.SessionID
		}
		if result.PID > 0 {
			fields["appium_pid"] = strconv.Itoa(result.PID)
		}
		if result.Issue != "" {
			fields["appium_warmup_issue"] = result.Issue
		}
		if result.Next != "" {
			fields["appium_warmup_next"] = result.Next
		}
	}
	fields["target"] = cfg.SimulatorName
	if fields["target"] == "" {
		fields["target"] = cfg.SimulatorUDID
	}
	if fields["target"] == "" {
		fields["target"] = "booted"
	}
	if m, err := EnsureAppMap(c.Root, cfg); err == nil {
		SetCurrentScreen(c.Root, m.Start, run.ID)
		ClearPendingMapAction(c.Root)
	}
	return OK("open", fields).Write(c.Stdout)
}

type appiumWarmupResult struct {
	Status    string
	SessionID string
	PID       int
	Issue     string
	Next      string
}

func (c CLI) startAppiumWarmup(ctx context.Context, cfg Config, run RunState, enabled bool) <-chan appiumWarmupResult {
	if !enabled {
		return nil
	}
	ch := make(chan appiumWarmupResult, 1)
	go func() {
		start := time.Now()
		if err := c.ensureAppiumAvailable(ctx); err != nil {
			result := appiumWarmupErrorResult(err)
			appendFile(run.LogsPath, fmt.Sprintf("mav appium warmup failed elapsed=%s issue=%s\n", time.Since(start), result.Issue))
			ch <- result
			return
		}
		session, err := c.ensureAppiumSession(ctx, cfg, run)
		if err != nil {
			result := appiumWarmupErrorResult(err)
			appendFile(run.LogsPath, fmt.Sprintf("mav appium warmup failed elapsed=%s issue=%s\n", time.Since(start), result.Issue))
			ch <- result
			return
		}
		appendFile(run.LogsPath, fmt.Sprintf("mav appium warmup ok elapsed=%s session=%s\n", time.Since(start), session.SessionID))
		ch <- appiumWarmupResult{Status: "ok", SessionID: session.SessionID, PID: session.PID}
	}()
	return ch
}

func appiumWarmupErrorResult(err error) appiumWarmupResult {
	result := appiumWarmupResult{Status: "failed", Issue: err.Error()}
	if appErr, ok := err.(appiumError); ok {
		result.Issue = appiumWarmupIssue(appErr)
		result.Next = appErr.Next
	}
	return result
}

func appiumWarmupIssue(err appiumError) string {
	message := strings.ToLower(err.Message)
	switch {
	case strings.Contains(message, "appium missing"):
		return "appium missing"
	case strings.Contains(message, "xcuitest") && strings.Contains(message, "missing"):
		return "xcuitest_driver_missing"
	case strings.Contains(message, "xcuitest") && strings.Contains(message, "incompatible"):
		return "xcuitest_driver_incompatible"
	case err.Code == "session_create_failed":
		return "session_create_failed"
	case err.Code == "appium_status_failed":
		return "appium_status_failed"
	case err.Message != "":
		return err.Message
	default:
		return err.Code
	}
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
	cfg.SimulatorUDID = sim.UDID
	cfg.SimulatorName = sim.Name
	cfg.SimulatorRuntime = sim.Runtime
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

func (c CLI) ui(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("ui_command_missing", map[string]string{"usage": "mav ui tree|tap|type|erase|hideKeyboard|swipe|pinch|rotate|twoFingerPan|actions|wait|scrollUntil"}).Write(c.Stdout)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
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
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe|appium"}).Write(c.Stdout)
	}
	includeSystem := hasFlag(args, "--include-system")
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
	state := c.observeUITree(cfg, result.Stdout, driver, false)
	if prefer == "auto" && driver != "appium" && shouldTryAppiumTreeFallback(result.Stdout, state) {
		if appium, appiumErr := c.describeUITree(ctx, cfg, "appium", true); appiumErr == nil && appium.Result.Err == nil && !isEmptyAXTree(appium.Result.Stdout) {
			appiumState := c.observeUITree(cfg, appium.Result.Stdout, appium.Driver, false)
			if shouldUseAppiumTreeFallback(result.Stdout, state, appiumState, cfg) {
				described = appium
				driver = appium.Driver
				result = appium.Result
				state = appiumState
			}
		}
	}
	if opts.Raw {
		fmt.Fprint(c.Stdout, result.Stdout)
		return nil
	}
	if isEmptyAXTree(result.Stdout) {
		return Fail("ui_tree_empty", map[string]string{"driver": driver, "reason": "simulator_accessibility_unavailable", "recovered": strconv.FormatBool(recovered)}).Write(c.Stdout)
	}
	state = c.observeUITree(cfg, result.Stdout, driver, true)
	fields := map[string]string{"driver": driver, "nodes": strconv.Itoa(state.Nodes), "screen": state.Screen}
	if state.ScreenSource != "" {
		fields["screen_source"] = state.ScreenSource
	}
	if state.ScreenSource == "recognized" || state.ScreenSource == "inferred" || state.ScreenSource == "start" {
		fields["recognized_screen"] = state.Screen
		fields["screen_confidence"] = screenConfidence(state.ScreenSource)
		fields["screen"] = "unknown"
	}
	if state.PreviousScreen != "" && state.PreviousScreen != state.Screen {
		fields["previous_screen"] = state.PreviousScreen
	}
	if state.MapPending {
		fields["map_pending"] = "true"
		if state.PendingFrom != "" {
			fields["previous_screen"] = state.PendingFrom
		}
		addIdentityMissingNext(fields, described)
	} else if state.ScreenSource == "identity_missing" {
		addIdentityMissingNext(fields, described)
	}
	if recovered {
		fields["recovered"] = "true"
	}
	if described.ActiveBundle != "" && described.ActiveBundle != cfg.BundleID {
		fields["active_bundle"] = described.ActiveBundle
		fields["system_source"] = strconv.FormatBool(described.SystemSource)
	}
	if err := OK("ui.tree", fields).Write(c.Stdout); err != nil {
		return err
	}
	return writeElementLines(c.Stdout, state.Elements)
}

type uiTreeState struct {
	Screen         string
	ScreenSource   string
	PreviousScreen string
	Driver         string
	Elements       []Element
	Nodes          int
	MapPending     bool
	PendingFrom    string
}

func (c CLI) observeUITree(cfg Config, raw, treeDriver string, persist bool) uiTreeState {
	state := uiTreeState{
		Driver:   treeDriver,
		Screen:   "unknown",
		Elements: ExtractElements(raw),
		Nodes:    countTreeNodes(raw),
	}
	if persist {
		if run, err := LoadRun(c.Root, ""); err == nil {
			if observed, err := ObserveScreenDetailedWithDriver(c.Root, cfg, run, raw, treeDriver); err == nil && observed.Screen != "" {
				state.Screen = observed.Screen
				state.ScreenSource = observed.Source
				state.PreviousScreen = observed.PreviousScreen
				state.Elements = observed.Elements
			}
		}
	} else if m, err := LoadAppMap(c.Root); err == nil {
		current := CurrentScreen(c.Root)
		_, hasPending := peekPendingMapAction(c.Root)
		screenID := ""
		source := ""
		if current != "" && !hasPending {
			if screen, ok := m.Screens[current]; ok && currentScreenMatches(screen, raw, state.Elements) {
				screenID = current
				source = "current"
			}
		}
		identity, hasIdentity := explicitScreenIdentity(state.Elements)
		if screenID != "" && hasIdentity && identity.ID != screenID {
			screenID = ""
			source = ""
		}
		if screenID == "" && hasIdentity {
			screenID = identity.ID
			source = identityScreenSource(m, identity.ID, state.Elements)
		}
		if screenID == "" {
			screenID = recognizeScreen(m, raw, state.Elements)
			if screenID != "" {
				source = "recognized"
			}
		}
		if screenID != "" {
			state.Screen = screenID
			state.ScreenSource = source
			state.PreviousScreen = current
		} else {
			state.ScreenSource = "identity_missing"
			state.PreviousScreen = current
		}
	}
	if state.Nodes == 0 {
		state.Nodes = strings.Count(raw, "\n")
	}
	if state.Screen == "unknown" {
		if pending, ok := peekPendingMapAction(c.Root); ok {
			state.MapPending = true
			state.PendingFrom = pending.From
		}
	}
	return state
}

func shouldFallbackToAppiumTree(raw string, state uiTreeState) bool {
	return isEmptyAXTree(raw) || state.Nodes <= 1 || len(state.Elements) == 0 || state.ScreenSource == "unmatched" || state.MapPending
}

func shouldTryAppiumTreeFallback(raw string, state uiTreeState) bool {
	return shouldFallbackToAppiumTree(raw, state) || state.ScreenSource == "identity_missing"
}

func shouldUseAppiumTreeFallback(raw string, state, appiumState uiTreeState, cfg Config) bool {
	if shouldFallbackToAppiumTree(raw, state) {
		return true
	}
	return state.ScreenSource == "identity_missing" && appiumStateHasExplicitScreenIdentity(appiumState, cfg)
}

// isWeakLaunchMatch reports whether the screen was recognised through the
// synthetic `start` screen's `kind: launch` recogniser (used as a permissive
// catch-all). That match is intentionally low-confidence and should give way
// to any explicit screen identity the Appium driver can surface.
func isWeakLaunchMatch(state uiTreeState) bool {
	return state.Screen == "start" && state.ScreenSource == "current"
}

// isUsableAppiumTree reports whether an Appium describe-tree response is
// useful for screen recognition: no transport error, non-empty AX-style
// output, more than the bare application root, and at least one extractable
// element. The same guard is applied to every Appium fallback call inside
// `waitForTreeReady`; centralising it keeps the call sites readable and the
// criteria in lockstep.
func isUsableAppiumTree(tree describedUITree) bool {
	return tree.Result.Err == nil &&
		!isEmptyAXTree(tree.Result.Stdout) &&
		countTreeNodes(tree.Result.Stdout) > 1 &&
		len(ExtractElements(tree.Result.Stdout)) > 0
}

// appiumStateExposesNonStartScreen reports whether the Appium-observed state
// represents a real screen — i.e. anything more specific than the synthetic
// `start` fallback or an unrecognised tree. Used by the weak-launch fallback
// to decide whether the Appium tree improves on what AXe already returned.
func appiumStateExposesNonStartScreen(state uiTreeState) bool {
	if state.Screen == "" || state.Screen == "unknown" || state.Screen == "start" {
		return false
	}
	return state.ScreenSource != "identity_missing" && state.ScreenSource != "unmatched"
}

func appiumStateHasExplicitScreenIdentity(state uiTreeState, cfg Config) bool {
	if state.Screen == "" || state.Screen == "unknown" || state.ScreenSource == "identity_missing" {
		return false
	}
	identity, ok := explicitScreenIdentity(state.Elements)
	return ok && identity.ID == state.Screen
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
	if prefer == "appium" {
		source, err := c.appiumSourceTree(ctx, cfg, includeSystem)
		if err != nil {
			return describedUITree{}, err
		}
		return describedUITree{Driver: "appium", Result: CommandResult{Stdout: source.Raw}, ActiveBundle: source.ActiveBundle, SystemSource: source.SystemSource}, nil
	}
	if includeSystem && prefer == "auto" {
		if source, err := c.appiumSourceTree(ctx, cfg, true); err == nil {
			return describedUITree{Driver: "appium", Result: CommandResult{Stdout: source.Raw}, ActiveBundle: source.ActiveBundle, SystemSource: source.SystemSource}, nil
		}
	}
	if hasTool(cfg, "axe") {
		return describedUITree{Driver: "axe", Result: c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "describe-ui")...)}, nil
	}
	if prefer == "axe" {
		return describedUITree{}, fmt.Errorf("tree_tool_missing")
	}
	if hasTool(cfg, "idb") {
		return describedUITree{Driver: "idb", Result: c.Runner.Run(ctx, "idb", idbTargetArgs(cfg, "ui", "describe-all", "--json", "--nested")...)}, nil
	}
	if prefer == "auto" && hasTool(cfg, "appium") {
		source, err := c.appiumSourceTree(ctx, cfg, includeSystem)
		if err != nil {
			return describedUITree{}, err
		}
		return describedUITree{Driver: "appium", Result: CommandResult{Stdout: source.Raw}, ActiveBundle: source.ActiveBundle, SystemSource: source.SystemSource}, nil
	}
	return describedUITree{}, fmt.Errorf("tree_tool_missing")
}

func (c CLI) recoverEmptyAXTree(ctx context.Context, cfg Config) error {
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
		if result.Err == nil && driver != "appium" {
			state := c.observeUITree(cfg, result.Stdout, driver, false)
			// AXe only exposes accessibility leaves, so it misses the
			// `accessibilityIdentifier` developers set on the non-leaf
			// container view of a screen. When that happens AXe falls all
			// the way back to the synthetic `start` launch recogniser (its
			// weakest signal). Probe Appium against the host app (NOT
			// system overlays — those would re-target a privacy or
			// SpringBoard bundle and lose the host tree) so the real
			// screen identity surfaces. Without this nudge `mav go` keeps
			// observing `start` and times out with `launch_tree_not_ready`
			// even when the target screen is already on display.
			if isWeakLaunchMatch(state) {
				if appium, err := c.describeUITree(ctx, cfg, "appium", false); err == nil && isUsableAppiumTree(appium) {
					appiumState := c.observeUITree(cfg, appium.Result.Stdout, appium.Driver, false)
					if appiumStateExposesNonStartScreen(appiumState) {
						result = appium.Result
						driver = appium.Driver
						state = appiumState
					}
				}
			}
			if shouldTryAppiumTreeFallback(result.Stdout, state) {
				if appium, appiumErr := c.describeUITree(ctx, cfg, "appium", true); appiumErr == nil && isUsableAppiumTree(appium) {
					appiumState := c.observeUITree(cfg, appium.Result.Stdout, appium.Driver, false)
					if !shouldUseAppiumTreeFallback(result.Stdout, state, appiumState, cfg) {
						time.Sleep(300 * time.Millisecond)
						continue
					}
					result = appium.Result
					driver = appium.Driver
				} else {
					time.Sleep(300 * time.Millisecond)
					continue
				}
			}
		}
		if result.Err == nil && !isEmptyAXTree(result.Stdout) && countTreeNodes(result.Stdout) > 1 && len(ExtractElements(result.Stdout)) > 0 {
			if _, mapErr := LoadAppMap(c.Root); mapErr == nil {
				state := c.observeUITree(cfg, result.Stdout, driver, false)
				if state.ScreenSource == "identity_missing" || state.ScreenSource == "unmatched" || state.MapPending {
					time.Sleep(300 * time.Millisecond)
					continue
				}
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
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe|appium"}).Write(c.Stdout)
	}
	autoObserve := !opts.SkipAutoObserve
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
		if prefer == "appium" {
			if err := c.uiTapAppium(ctx, cfg, id, text, value); err != nil {
				return c.writeAppiumTapError(err)
			}
			fields["driver"] = "appium"
			c.appendCurrentCommand(command+" --prefer-driver appium", CommandResult{})
			c.recordPendingTap(id, text, value, "", "", "appium")
			if autoObserve {
				c.observeAfterAction(ctx, cfg, "appium")
			}
			return OK("ui.tap", fields).Write(c.Stdout)
		}
		if !caps.Tools["axe"] {
			if prefer == "auto" {
				if err := c.uiTapAppium(ctx, cfg, id, text, value); err == nil {
					fields["driver"] = "appium"
					fields["attempted"] = "appium"
					c.appendCurrentCommand(command+" --prefer-driver appium", CommandResult{})
					c.recordPendingTap(id, text, value, "", "", "appium")
					return OK("ui.tap", fields).Write(c.Stdout)
				} else {
					return c.writeAppiumTapError(err)
				}
			}
			return Fail("tool_missing", map[string]string{"tool": "axe", "next": "use mav ui tap --x X --y Y when AXe is unavailable"}).Write(c.Stdout)
		}
		if value != "" {
			if prefer == "auto" {
				if err := c.uiTapAppium(ctx, cfg, id, text, value); err != nil {
					return c.writeAppiumTapError(err)
				}
				fields["driver"] = "appium"
				fields["attempted"] = "appium"
				c.appendCurrentCommand(command+" --prefer-driver appium", CommandResult{})
				c.recordPendingTap(id, text, value, "", "", "appium")
				return OK("ui.tap", fields).Write(c.Stdout)
			}
			return Fail("tap_target_missing", map[string]string{"usage": "mav ui tap --value VALUE requires --prefer-driver auto|appium"}).Write(c.Stdout)
		}
		if prefer == "auto" && id != "" && c.shouldRouteTextInputWrapperTapToAppium(ctx, cfg, id) {
			if err := c.uiTapAppium(ctx, cfg, id, text, value); err == nil {
				fields["driver"] = "appium"
				fields["attempted"] = "axe,appium"
				fields["fallback"] = "axe"
				fields["fallback_reason"] = "text_input_wrapper"
				c.appendCurrentCommand(command+" --prefer-driver appium", CommandResult{})
				c.recordPendingTap(id, text, value, "", "", "appium")
				return OK("ui.tap", fields).Write(c.Stdout)
			}
		}
		if prefer == "auto" && c.shouldRouteContainerTapToAppium(ctx, cfg, id, text) {
			if err := c.uiTapAppium(ctx, cfg, id, text, value); err == nil {
				fields["driver"] = "appium"
				fields["attempted"] = "axe,appium"
				fields["fallback"] = "axe"
				fields["fallback_reason"] = "container_tap"
				c.appendCurrentCommand(command+" --prefer-driver appium", CommandResult{})
				c.recordPendingTap(id, text, value, "", "", "appium")
				return OK("ui.tap", fields).Write(c.Stdout)
			} else {
				return c.writeAppiumTapErrorWithFields(err, map[string]string{"attempted": "axe,appium", "fallback": "axe", "fallback_reason": "container_tap"})
			}
		}
		axeArgs := axeTargetArgs(cfg, "tap")
		if id != "" {
			axeArgs = append(axeArgs, "--id", id)
		} else {
			axeArgs = append(axeArgs, "--label", text)
		}
		result := c.Runner.Run(ctx, "axe", axeArgs...)
		if result.Err != nil {
			diagnosticFields, hasTextDiagnostic := c.diagnoseTextTapFailure(ctx, cfg, text, result.Stderr)
			if prefer == "auto" {
				if err := c.uiTapAppium(ctx, cfg, id, text, value); err == nil {
					fields["driver"] = "appium"
					fields["attempted"] = "axe,appium"
					fields["fallback"] = "axe"
					fields["fallback_reason"] = "axe_no_match"
					c.appendCurrentCommand(command+" --prefer-driver appium", CommandResult{})
					c.recordPendingTap(id, text, value, "", "", "appium")
					return OK("ui.tap", fields).Write(c.Stdout)
				} else {
					if hasTextDiagnostic {
						return Fail("ui_tap_text_no_label_match", diagnosticFields).Write(c.Stdout)
					}
					return c.writeAppiumTapErrorWithFields(err, map[string]string{"attempted": "axe,appium", "fallback_reason": "axe_no_match"})
				}
			}
			if hasTextDiagnostic {
				return Fail("ui_tap_text_no_label_match", diagnosticFields).Write(c.Stdout)
			}
			return Fail("ui_tap_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
		}
		fields["driver"] = "axe"
		c.appendCurrentCommand(command, result)
		c.recordPendingTap(id, text, value, "", "", "axe")
		if autoObserve {
			c.observeAfterAction(ctx, cfg, prefer)
		}
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
		c.recordPendingTap("", "", "", x, y, "idb")
		if autoObserve {
			c.observeAfterAction(ctx, cfg, prefer)
		}
		return OK("ui.tap", map[string]string{"x": x, "y": y, "driver": "idb", "route_recorded": "false"}).Write(c.Stdout)
	}
	return Fail("tap_target_missing", map[string]string{"usage": "mav ui tap --id ID | --x X --y Y | --text TEXT | --value VALUE"}).Write(c.Stdout)
}

func (c CLI) shouldRouteTextInputWrapperTapToAppium(ctx context.Context, cfg Config, id string) bool {
	result := c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "describe-ui")...)
	if result.Err != nil {
		return false
	}
	return treeNodeWithIDHasTextInputDescendant(result.Stdout, id)
}

func treeNodeWithIDHasTextInputDescendant(raw, id string) bool {
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return false
	}
	return nodeWithIDHasTextInputDescendant(parsed, id)
}

func (c CLI) shouldRouteContainerTapToAppium(ctx context.Context, cfg Config, id, text string) bool {
	if id == "" && text == "" {
		return false
	}
	result := c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "describe-ui")...)
	if result.Err != nil {
		return false
	}
	var parsed any
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return false
	}
	return nodeMatchingTargetHasInteractiveContainerAncestor(parsed, id, text, nil)
}

func nodeMatchingTargetHasInteractiveContainerAncestor(value any, id, text string, ancestors []map[string]any) bool {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			if nodeMatchingTargetHasInteractiveContainerAncestor(child, id, text, ancestors) {
				return true
			}
		}
	case map[string]any:
		if nodeMatchesTapTarget(node, id, text) && (isInteractiveTapContainer(nodeRole(node)) || isInteractiveTapContainer(nodeSubrole(node)) || hasInteractiveContainerAncestor(ancestors)) {
			return true
		}
		nextAncestors := append(append([]map[string]any{}, ancestors...), node)
		for _, child := range nodeChildren(node) {
			if nodeMatchingTargetHasInteractiveContainerAncestor(child, id, text, nextAncestors) {
				return true
			}
		}
	}
	return false
}

func nodeMatchesTapTarget(node map[string]any, id, text string) bool {
	if id != "" && nodeIdentifier(node) == id {
		return true
	}
	if text == "" {
		return false
	}
	return stringField(node, "AXLabel", "label", "name") == text ||
		stringField(node, "AXTitle", "title") == text ||
		stringField(node, "AXValue", "value") == text
}

func hasInteractiveContainerAncestor(ancestors []map[string]any) bool {
	for _, ancestor := range ancestors {
		if isInteractiveTapContainer(nodeRole(ancestor)) || isInteractiveTapContainer(nodeSubrole(ancestor)) {
			return true
		}
	}
	return false
}

func nodeSubrole(node map[string]any) string {
	return stringField(node, "AXSubrole", "subrole")
}

func isInteractiveTapContainer(role string) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	return strings.Contains(role, "cell") ||
		strings.Contains(role, "table") ||
		strings.Contains(role, "collection") ||
		strings.Contains(role, "sheet") ||
		strings.Contains(role, "tab")
}

func nodeWithIDHasTextInputDescendant(value any, id string) bool {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			if nodeWithIDHasTextInputDescendant(child, id) {
				return true
			}
		}
	case map[string]any:
		if nodeIdentifier(node) == id {
			return !isTextInputRole(nodeRole(node)) && hasTextInputDescendant(node)
		}
		for _, child := range nodeChildren(node) {
			if nodeWithIDHasTextInputDescendant(child, id) {
				return true
			}
		}
	}
	return false
}

func hasTextInputDescendant(node map[string]any) bool {
	for _, child := range nodeChildren(node) {
		childMap, ok := child.(map[string]any)
		if !ok {
			continue
		}
		if isTextInputRole(nodeRole(childMap)) || hasTextInputDescendant(childMap) {
			return true
		}
	}
	return false
}

func nodeChildren(node map[string]any) []any {
	for _, key := range []string{"children", "Children", "AXChildren"} {
		if value, ok := node[key]; ok {
			switch children := value.(type) {
			case []any:
				return children
			case []map[string]any:
				out := make([]any, 0, len(children))
				for _, child := range children {
					out = append(out, child)
				}
				return out
			}
		}
	}
	return nil
}

func nodeIdentifier(node map[string]any) string {
	return stringField(node, "AXIdentifier", "identifier", "AXUniqueId", "name")
}

func nodeRole(node map[string]any) string {
	return stringField(node, "role_description", "role", "type")
}

func isTextInputRole(role string) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	return strings.Contains(role, "textfield") ||
		strings.Contains(role, "text field") ||
		strings.Contains(role, "textview") ||
		strings.Contains(role, "text view") ||
		strings.Contains(role, "textarea") ||
		strings.Contains(role, "securetext")
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
		"next":          "--text matched AXValue but not AXLabel; use --prefer-driver appium for predicate matching, prefer --id, or tap coordinates from a capture",
	}
	if line := firstLine(stderr); line != "" {
		fields["stderr"] = line
	}
	return fields, true
}

func (c CLI) uiTapAppium(ctx context.Context, cfg Config, id, text, value string) error {
	// Some XCUIElement containers (tab bars, action sheets, alerts,
	// popovers) carry children whose `XCUIElement.tap()` / `click`
	// silently no-ops because UIKit handles their selection through
	// gesture recognisers rather than the standard hit-test path. For
	// any target that lives inside one of those containers, prefer a
	// coordinate-based tap (`mobile: tap` at the element's frame
	// centre) so the touch is delivered as a synthesised event the
	// system reliably routes to the responder.
	if tapped, err := c.uiTapAppiumCoordTapIfNeeded(ctx, cfg, id, text, value); tapped || err != nil {
		return err
	}
	if id != "" {
		return c.appiumClickByAccessibilityID(ctx, cfg, id)
	}
	if text != "" || value != "" {
		return c.appiumClickByPredicate(ctx, cfg, appiumTargetPredicate(text, value))
	}
	return appiumError{Code: "tap_target_missing", Message: "tap target missing"}
}

func (c CLI) uiTapAppiumCoordTapIfNeeded(ctx context.Context, cfg Config, id, text, value string) (bool, error) {
	if id == "" && text == "" && value == "" {
		return false, nil
	}
	source, err := c.appiumSourceTree(ctx, cfg, true)
	if err != nil {
		return false, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(source.Raw), &parsed); err != nil {
		return false, nil
	}
	frame, ok := findGestureContainerTargetFrame(parsed, id, text, value, "")
	if !ok {
		return false, nil
	}
	x, y, w, h, ok := parseElementFrame(frame)
	if !ok {
		return false, nil
	}
	if err := c.appiumMobileTap(ctx, cfg, int(x+w/2), int(y+h/2)); err != nil {
		return true, err
	}
	return true, nil
}

// findGestureContainerTargetFrame walks the tree looking for the first
// element whose label/id/value matches AND whose nearest ancestor is a
// container that handles taps via gesture recognisers (tab bars, action
// sheets, alert controllers, popovers). The string returned is the
// matched element's `AXFrame` / `frame`; the caller derives the centre.
//
// `containerKind` is the lower-cased role of the closest matching
// container ancestor; the empty string means we are not currently inside
// one. We track it explicitly (rather than re-walking on each match) so
// nested children only qualify when the chain is still under the
// container.
func findGestureContainerTargetFrame(value any, id, text, targetValue, containerKind string) (string, bool) {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			if frame, ok := findGestureContainerTargetFrame(child, id, text, targetValue, containerKind); ok {
				return frame, true
			}
		}
	case map[string]any:
		nextContainer := containerKind
		if nextContainer == "" {
			if kind := gestureContainerKindFromRole(nodeRole(node)); kind != "" {
				nextContainer = kind
			}
		}
		if nextContainer != "" && nodeMatchesAppiumTarget(node, id, text, targetValue) {
			if frame := stringField(node, "AXFrame", "frame"); frame != "" {
				return frame, true
			}
		}
		for _, child := range nodeChildren(node) {
			if frame, ok := findGestureContainerTargetFrame(child, id, text, targetValue, nextContainer); ok {
				return frame, true
			}
		}
	}
	return "", false
}

// gestureContainerKindFromRole maps an element role to the container
// classification used by `findGestureContainerTargetFrame`. Any role
// returned non-empty marks the subtree as gesture-handled, so taps on
// children should be coordinate-based.
//
// Match strategy: the role string is normalized (lower-cased,
// non-alphanumerics stripped) and checked against a closed set of
// suffixes derived from XCUIElement and AX role taxonomies. Suffix
// (rather than substring) matching avoids surprises like
// `XCUIElementTypeSheetButton` triggering the sheet path; the role
// names we care about always end on the role token itself.
//
// Coverage rationale:
//   - sheets / alert: UIAlertController action buttons rely on the
//     dimming-view tap recognizer instead of the button hit-test, so
//     `XCUIElement.click` on the inner buttons can silently no-op.
//   - popovers: UIPopoverPresentationController content is reachable
//     via its dimming view; `click` on inner buttons can no-op in
//     iOS 17+.
//
// Tab bars are deliberately NOT in this set: synthetic
// `mobile: tap` events at the button frame centre are dropped by
// the simulator immediately after a screen transition (notably
// post-login), whereas `XCUIElement.click` on the same button is
// routed through the test infrastructure and triggers the tab
// switch reliably. Routing tab bar children through coord-tap
// makes the first tap right after navigation no-op silently.
func gestureContainerKindFromRole(role string) string {
	normalized := normalizedRoleToken(role)
	// Iterate the ordered slice so adding a more specific suffix
	// later (e.g. another `*alert*` flavour) cannot become
	// non-deterministic by virtue of Go map iteration order.
	for _, entry := range gestureContainerRoleSuffixes {
		if strings.HasSuffix(normalized, entry.suffix) {
			return entry.kind
		}
	}
	return ""
}

// normalizedRoleToken strips spaces, punctuation, and case from a role
// string so suffix comparisons are robust across Appium (`XCUIElementTypeTabBar`)
// and AX (`AXTabBar`, `AX Tab Bar`) taxonomies.
func normalizedRoleToken(role string) string {
	var b strings.Builder
	b.Grow(len(role))
	for _, r := range role {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// gestureContainerRoleSuffixes pairs normalized role suffixes with
// the container kind they map to. The list is ordered most-specific
// first so a future addition like `bar` (which is a suffix of
// `actionsheet`-adjacent names) cannot accidentally win when both
// match. The current entries are mutually disjoint.
var gestureContainerRoleSuffixes = []struct {
	suffix string
	kind   string
}{
	{"uialertcontroller", "alert"},
	{"popoverpresentation", "popover"},
	{"actionsheet", "sheet"},
	{"popover", "popover"},
	{"alert", "alert"},
	{"sheet", "sheet"},
}

func nodeMatchesAppiumTarget(node map[string]any, id, text, targetValue string) bool {
	if id != "" && nodeIdentifier(node) == id {
		return true
	}
	if text != "" {
		return stringField(node, "AXLabel", "label", "name") == text ||
			stringField(node, "AXTitle", "title") == text ||
			stringField(node, "AXValue", "value") == text
	}
	if targetValue != "" {
		return stringField(node, "AXValue", "value") == targetValue
	}
	return false
}

func appiumTargetPredicate(text, value string) string {
	if value != "" && text == "" {
		escaped := strings.ReplaceAll(value, "'", "\\'")
		return "value == '" + escaped + "'"
	}
	escaped := strings.ReplaceAll(text, "'", "\\'")
	return "value == '" + escaped + "' OR name == '" + escaped + "' OR label == '" + escaped + "'"
}

func (c CLI) writeAppiumTapError(err error) error {
	return c.writeAppiumTapErrorWithFields(err, nil)
}

func (c CLI) writeAppiumTapErrorWithFields(err error, extra map[string]string) error {
	if appErr, ok := err.(appiumError); ok {
		fields := map[string]string{"tool": "appium"}
		for key, value := range extra {
			fields[key] = value
		}
		if appErr.Message != "" {
			fields["stderr"] = appErr.Message
		}
		if appErr.Next != "" {
			fields["next"] = appErr.Next
		}
		return Fail(appErr.Code, fields).Write(c.Stdout)
	}
	fields := map[string]string{"tool": "appium", "stderr": err.Error()}
	for key, value := range extra {
		fields[key] = value
	}
	return Fail("ui_tap_failed", fields).Write(c.Stdout)
}

func (c CLI) recordPendingTap(id, text, value, x, y, driver string) {
	from := CurrentScreen(c.Root)
	if from == "" {
		return
	}
	if id == "" && text == "" && value == "" {
		ClearPendingMapAction(c.Root)
		return
	}
	SetPendingMapAction(c.Root, pendingMapAction{From: from, ID: id, Text: text, Value: value, X: x, Y: y, Driver: driver})
}

// observeAfterAction fetches a fresh UI tree after a successful tap
// (or other interaction that sets a pending map action) and runs the
// observer so the edge gets promoted to the map immediately. Without
// this, edges only land when a separate `mav ui tree` command is run
// later, which never happens during long flows where one tap is
// immediately followed by another. The cost is one extra source
// fetch per tap (~ 1s on Appium); the benefit is a populated app map
// that lets `mav go` actually navigate.
//
// The function tolerates failures silently — observation is best
// effort, never the reason a `mav ui tap` would fail.
func (c CLI) observeAfterAction(ctx context.Context, cfg Config, prefer string) {
	if _, hasPending := peekPendingMapAction(c.Root); !hasPending {
		return
	}
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil || described.Result.Err != nil {
		return
	}
	if isEmptyAXTree(described.Result.Stdout) {
		return
	}
	_ = c.observeUITree(cfg, described.Result.Stdout, described.Driver, true)
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
	prefer, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe|appium"}).Write(c.Stdout)
	}
	before, focusKnown := c.focusedTextInput(ctx, cfg)
	if focusKnown && before == nil {
		return Fail("type_no_focused_field", map[string]string{"next": "No text input has keyboard focus. Use 'mav ui tap --prefer-driver appium' on the field, then retry type."}).Write(c.Stdout)
	}
	text := strings.Join(args, " ")
	driver := "axe"
	if prefer == "appium" {
		if err := c.appiumTypeActiveElement(ctx, cfg, text); err != nil {
			if appErr, ok := err.(appiumError); ok && appErr.Code == "type_no_focused_field" {
				return Fail("type_no_focused_field", map[string]string{"next": "No text input has keyboard focus. Tap the field with Appium and retry type."}).Write(c.Stdout)
			}
			return Fail("ui_type_failed", map[string]string{"tool": "appium", "stderr": err.Error()}).Write(c.Stdout)
		}
		driver = "appium"
	} else {
		axeArgs := axeTargetArgs(cfg, "type")
		axeArgs = append(axeArgs, text)
		result := c.Runner.Run(ctx, "axe", axeArgs...)
		if result.Err != nil {
			if prefer == "auto" {
				return Fail("ui_type_failed", map[string]string{"stderr": firstLine(result.Stderr), "next": "retry with --prefer-driver appium for Appium/XCUITest text entry"}).Write(c.Stdout)
			}
			return Fail("ui_type_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout)
		}
	}
	fields := map[string]string{
		"chars":      strconv.Itoa(len(text)),
		"chars_sent": strconv.Itoa(len(text)),
		"driver":     driver,
	}
	if before != nil {
		if after, ok := c.focusedTextInput(ctx, cfg); ok && after != nil {
			fields["chars_received"] = strconv.Itoa(receivedCharDelta(before.Value, after.Value, text))
			if prefer == "auto" && strings.ContainsAny(text, "@#&") && !strings.HasSuffix(after.Value, text) {
				fields["warning"] = "typed_text_may_be_corrupted"
				fields["next"] = "retry with --prefer-driver appium"
			}
		}
	}
	return OK("ui.type", fields).Write(c.Stdout)
}

func (c CLI) uiErase(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	prefer, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe|appium"}).Write(c.Stdout)
	}
	if prefer == "axe" {
		return Fail("ui_erase_failed", map[string]string{"next": "erase requires Appium; retry with --prefer-driver appium"}).Write(c.Stdout)
	}
	id := flagValue(args, "--id")
	text := flagValue(args, "--text")
	value := flagValue(args, "--value")
	focused := flagValue(args, "--focused") == "true" || hasFlag(args, "--focused")
	if err := c.appiumClearElement(ctx, cfg, id, text, value, focused); err != nil {
		fields := map[string]string{"tool": "appium"}
		if appErr, ok := err.(appiumError); ok && appErr.Next != "" {
			fields["next"] = appErr.Next
		}
		fields["stderr"] = err.Error()
		return Fail("ui_erase_failed", fields).Write(c.Stdout)
	}
	fields := map[string]string{"driver": "appium"}
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
	beforeVisible, beforeKnown := c.keyboardVisible(ctx, cfg)
	if err := c.appiumHideKeyboard(ctx, cfg); err != nil {
		return Fail("ui_hide_keyboard_failed", map[string]string{"tool": "appium", "stderr": err.Error()}).Write(c.Stdout)
	}
	if !beforeKnown || !beforeVisible {
		return OK("ui.hideKeyboard", map[string]string{"driver": "appium", "verified": strconv.FormatBool(beforeKnown)}).Write(c.Stdout)
	}
	for _, retry := range []struct {
		name string
		fn   func() error
	}{
		{name: "tapOutside", fn: func() error {
			return c.appiumHideKeyboardWithArgs(ctx, cfg, []any{map[string]any{"strategy": "tapOutside"}})
		}},
		{name: "tap_top_center", fn: func() error {
			x, y := c.keyboardDismissTapPoint(ctx, cfg)
			return c.appiumMobileTap(ctx, cfg, x, y)
		}},
	} {
		time.Sleep(250 * time.Millisecond)
		if visible, ok := c.keyboardVisible(ctx, cfg); ok && !visible {
			return OK("ui.hideKeyboard", map[string]string{"driver": "appium", "verified": "true"}).Write(c.Stdout)
		}
		_ = retry.fn()
		time.Sleep(250 * time.Millisecond)
		if visible, ok := c.keyboardVisible(ctx, cfg); ok && !visible {
			return OK("ui.hideKeyboard", map[string]string{"driver": "appium", "verified": "true", "retry": retry.name}).Write(c.Stdout)
		}
	}
	return Fail("ui_hide_keyboard_failed", map[string]string{
		"tool":   "appium",
		"reason": "keyboard_still_visible",
		"next":   "tap an empty area above the keyboard or use a downward swipe before tapping controls hidden by the keyboard",
	}).Write(c.Stdout)
}

func (c CLI) keyboardVisible(ctx context.Context, cfg Config) (bool, bool) {
	source, err := c.appiumSourceTree(ctx, cfg, true)
	if err != nil {
		return false, false
	}
	return elementsContainKeyboard(ExtractElements(source.Raw)), true
}

func elementsContainKeyboard(elements []Element) bool {
	for _, el := range elements {
		text := strings.ToLower(strings.Join([]string{el.ID, el.Label, el.Role, el.Value, el.Title}, " "))
		if strings.Contains(text, "keyboard") || strings.Contains(text, "xcuiuielementtypekeyboard") {
			return true
		}
	}
	return false
}

func (c CLI) keyboardDismissTapPoint(ctx context.Context, cfg Config) (int, int) {
	source, err := c.appiumSourceTree(ctx, cfg, true)
	if err != nil {
		return 200, 80
	}
	width := 0.0
	for _, el := range ExtractElements(source.Raw) {
		if x, _, w, _, ok := parseElementFrame(el.Frame); ok && x == 0 && w > width {
			width = w
		}
	}
	if width <= 0 {
		return 200, 80
	}
	return int(width / 2), 80
}

func (c CLI) focusedTextInput(ctx context.Context, cfg Config) (*Element, bool) {
	source, err := c.appiumSourceTree(ctx, cfg, true)
	if err != nil {
		return nil, false
	}
	elements := ExtractElements(source.Raw)
	hasFocusMetadata := false
	for _, el := range elements {
		if el.Focused != "" || strings.Contains(strings.ToLower(el.Value), "keyboardfocused") {
			hasFocusMetadata = true
		}
		if isTextInputRole(el.Role) && (el.Focused == "true" || strings.Contains(strings.ToLower(el.Value), "keyboardfocused")) {
			copy := el
			return &copy, true
		}
	}
	if !hasFocusMetadata {
		return nil, false
	}
	return nil, true
}

func receivedCharDelta(before, after, typed string) int {
	if after == before {
		return 0
	}
	if strings.HasSuffix(after, typed) && len(after) >= len(before)+len(typed) {
		return len(typed)
	}
	if strings.HasPrefix(after, before) && len(after) >= len(before) {
		return len(after) - len(before)
	}
	return len([]rune(after))
}

func (c CLI) uiSwipe(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	prefer, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe|appium"}).Write(c.Stdout)
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
	if prefer == "appium" {
		actions, fields, err := buildSwipeActions(startX, startY, endX, endY)
		if err != nil {
			return Fail("gesture_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
		}
		if err := c.performAppiumActions(ctx, cfg, actions); err != nil {
			return c.writeAppiumGestureError(err)
		}
		waitForGestureCompletion(fields)
		fields["direction"] = direction
		fields["driver"] = "appium"
		if customCoordinates {
			fields["direction"] = "custom"
			fields["start_x"] = startX
			fields["start_y"] = startY
			fields["end_x"] = endX
			fields["end_y"] = endY
		}
		return OK("ui.swipe", fields).Write(c.Stdout)
	}
	driver := "axe"
	var result CommandResult
	if hasTool(cfg, "axe") {
		axeArgs := axeTargetArgs(cfg, "swipe", "--start-x", startX, "--start-y", startY, "--end-x", endX, "--end-y", endY)
		result = c.Runner.Run(ctx, "axe", axeArgs...)
	} else if hasTool(cfg, "idb") {
		if prefer == "axe" {
			return Fail("tool_missing", map[string]string{"tool": "axe", "next": "install AXe or use --prefer-driver auto|appium"}).Write(c.Stdout)
		}
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
	_ = cfg
	prefer, err := normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe|appium"}).Write(c.Stdout)
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
		return Fail("prefer_driver_invalid", map[string]string{"usage": "--prefer-driver auto|axe|appium"}).Write(c.Stdout)
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
	if hasTool(cfg, "axe") {
		return c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "screenshot", "--output", path)...), nil
	}
	if hasTool(cfg, "idb") {
		return c.Runner.Run(ctx, "idb", idbTargetArgs(cfg, "screenshot", path)...), nil
	}
	if hasTool(cfg, "xcrun") {
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
		c.observeFlowStepScreen(ctx, opts, run, step)
		appendFlowStep(run, index+1, step.Action, elapsed, "ok", fields)
	}
	_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
	return OK("run", map[string]string{"name": flow.Name, "run": run.ID, "steps": strconv.Itoa(len(flow.Steps)), "elapsed": time.Since(start).String()}).Write(c.Stdout)
}

func (c CLI) observeFlowStepScreen(ctx context.Context, opts GlobalOptions, run RunState, step FlowStep) {
	if !flowStepCanChangeOrConfirmScreen(step.Action) || run.ID == "" {
		return
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return
	}
	prefer, err := flowStepPreferDriver(opts, step)
	if err != nil {
		return
	}
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil || described.Result.Err != nil {
		return
	}
	driver := described.Driver
	raw := described.Result.Stdout
	if prefer == "auto" && driver != "appium" {
		state := c.observeUITree(cfg, raw, driver, false)
		if shouldTryAppiumTreeFallback(raw, state) {
			if appium, appiumErr := c.describeUITree(ctx, cfg, "appium", true); appiumErr == nil && appium.Result.Err == nil && !isEmptyAXTree(appium.Result.Stdout) {
				appiumState := c.observeUITree(cfg, appium.Result.Stdout, appium.Driver, false)
				if shouldUseAppiumTreeFallback(raw, state, appiumState, cfg) {
					driver = appium.Driver
					raw = appium.Result.Stdout
				}
			}
		}
	}
	if isEmptyAXTree(raw) {
		return
	}
	_, _ = ObserveScreenDetailedWithDriver(c.Root, cfg, run, raw, driver)
}

func flowStepCanChangeOrConfirmScreen(action string) bool {
	switch action {
	case "tap", "wait", "assert", "waitUntil", "scrollUntil", "tree", "go":
		return true
	default:
		return false
	}
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
	case "go":
		screen := step.Params["screen"]
		if screen == "" {
			return nil, fmt.Errorf("screen_missing")
		}
		fields, err := c.navigateToScreen(ctx, screen)
		return fields, err
	case "tree":
		err := c.withStdout(io.Discard).ui(ctx, GlobalOptions{PreferDriver: prefer}, []string{"tree"})
		return map[string]string{"driver": prefer}, outputErr(err, "tree_failed")
	case "tap":
		args := flowArgs(step.Params, "--id", "id", "--text", "text", "--value", "value", "--x", "x", "--y", "y")
		var out bytes.Buffer
		// Flow runner has its own post-step observation pass that
		// promotes edges with the best-fit driver (axe-then-appium
		// fallback). Suppress the in-tap auto-observe so we don't
		// run a less-thorough probe first and clear the pending
		// action when the host tree isn't yet recognisable.
		err := c.withStdout(&out).uiTap(ctx, GlobalOptions{PreferDriver: prefer, SkipAutoObserve: true}, mustLoadConfig(c.Root), args)
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
		err := c.withStdout(&out).uiType(ctx, GlobalOptions{PreferDriver: prefer}, mustLoadConfig(c.Root), []string{text})
		fields := map[string]string{"chars": strconv.Itoa(len(text))}
		if prefer != "" {
			fields["driver"] = prefer
		}
		return fields, commandOutputErr(err, out.String(), "type_failed")
	case "erase":
		args := flowArgs(step.Params, "--id", "id", "--text", "text", "--value", "value", "--focused", "focused")
		var out bytes.Buffer
		err := c.withStdout(&out).uiErase(ctx, GlobalOptions{PreferDriver: prefer}, mustLoadConfig(c.Root), args)
		return copyParams(step.Params), commandOutputErr(err, out.String(), "erase_failed")
	case "hideKeyboard":
		var out bytes.Buffer
		err := c.withStdout(&out).uiHideKeyboard(ctx, GlobalOptions{}, mustLoadConfig(c.Root), nil)
		return map[string]string{"driver": "appium"}, commandOutputErr(err, out.String(), "hide_keyboard_failed")
	case "swipe":
		args := flowArgs(step.Params, "--direction", "direction")
		err := c.withStdout(io.Discard).uiSwipe(ctx, GlobalOptions{PreferDriver: prefer}, mustLoadConfig(c.Root), args)
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
	case "approach":
		name := step.Params["name"]
		if name == "" {
			return nil, fmt.Errorf("approach_name_missing")
		}
		return c.runApproachStep(ctx, run, name)
	default:
		return nil, fmt.Errorf("unknown_step")
	}
}

// runDiscoveryFallback runs the live-UI discovery walker bounded
// by the supplied options (today: defaults; future: --discover-*
// CLI flags). Returns the result so callers can persist the path
// or surface diagnostics on failure.
func (c CLI) runDiscoveryFallback(ctx context.Context, cfg Config, run RunState, target string) (DiscoverResult, error) {
	runner := cliDiscoverRunner{cli: c, cfg: cfg, run: run}
	return Discover(ctx, runner, target, DiscoverOptions{})
}

// cliDiscoverRunner adapts the CLI's tap + observation primitives
// to the `DiscoverRunner` interface used by the discovery
// algorithm. Created on the fly inside `goScreen` so the algorithm
// can stay file-local and unit-testable without dragging in the
// real simulator.
type cliDiscoverRunner struct {
	cli CLI
	cfg Config
	run RunState
}

func (r cliDiscoverRunner) CurrentScreen(ctx context.Context) (string, []Element, error) {
	tree, err := r.cli.waitForTreeReady(ctx, r.cfg, discoverTreeReadyMaxAge)
	if err != nil {
		return "", nil, err
	}
	observed, _ := ObserveScreenDetailedWithDriver(r.cli.Root, r.cfg, r.run, tree.Raw, tree.Driver)
	return observed.Screen, observed.Elements, nil
}

func (r cliDiscoverRunner) Tap(ctx context.Context, sel ApproachStep) (string, []Element, error) {
	args := []string{}
	if sel.ID != "" {
		args = append(args, "--id", sel.ID)
	}
	if sel.Text != "" {
		args = append(args, "--text", sel.Text)
	}
	if sel.Value != "" {
		args = append(args, "--value", sel.Value)
	}
	if sel.X != "" && sel.Y != "" {
		args = append(args, "--x", sel.X, "--y", sel.Y)
	}
	if len(args) == 0 {
		return "", nil, fmt.Errorf("discover_tap_empty_selector")
	}
	var out bytes.Buffer
	tapOpts := GlobalOptions{PreferDriver: sel.Driver, SkipAutoObserve: true}
	if err := r.cli.withStdout(&out).uiTap(ctx, tapOpts, r.cfg, args); err != nil {
		return "", nil, err
	}
	if code, ok := outputFailureCode(out.String()); ok {
		return "", nil, fmt.Errorf("%s", code)
	}
	tree, err := r.cli.waitForTreeReady(ctx, r.cfg, discoverTreeReadyMaxAge)
	if err != nil {
		return "", nil, err
	}
	observed, _ := ObserveScreenDetailedWithDriver(r.cli.Root, r.cfg, r.run, tree.Raw, tree.Driver)
	return observed.Screen, observed.Elements, nil
}

func (r cliDiscoverRunner) Back(ctx context.Context) (string, []Element, error) {
	// Best-effort hardware-back gesture (left-edge swipe).
	swipeArgs := []string{"--direction", "right"}
	_ = r.cli.withStdout(&bytes.Buffer{}).uiSwipe(ctx, GlobalOptions{}, r.cfg, swipeArgs)
	tree, err := r.cli.waitForTreeReady(ctx, r.cfg, discoverTreeReadyMaxAge)
	if err != nil {
		return "", nil, err
	}
	observed, _ := ObserveScreenDetailedWithDriver(r.cli.Root, r.cfg, r.run, tree.Raw, tree.Driver)
	return observed.Screen, observed.Elements, nil
}

// approachCommand routes `mav approach <subcommand>` to the right
// handler. Three subcommands today: `list`, `show`, and `extract`.
// `extract` is the user-facing tool that derives a reusable
// approach from a successful `mav run`.
func (c CLI) approachCommand(ctx context.Context, opts GlobalOptions, args []string) error {
	_ = ctx
	_ = opts
	if len(args) == 0 {
		return Fail("approach_subcommand_missing", map[string]string{"usage": "mav approach list|show|extract"}).Write(c.Stdout)
	}
	switch args[0] {
	case "list":
		return c.approachList(args[1:])
	case "show":
		return c.approachShow(args[1:])
	case "extract":
		return c.approachExtract(args[1:])
	default:
		return Fail("approach_subcommand_unknown", map[string]string{"subcommand": args[0], "usage": "mav approach list|show|extract"}).Write(c.Stdout)
	}
}

func (c CLI) approachList(args []string) error {
	_ = args
	all, err := LoadAllApproaches(c.Root)
	if err != nil {
		return Fail("approach_list_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	fields := map[string]string{"count": strconv.Itoa(len(all))}
	if err := OK("approach.list", fields).Write(c.Stdout); err != nil {
		return err
	}
	for _, a := range all {
		_, _ = fmt.Fprintf(c.Stdout, "approach name=%s steps=%d anchor=%s last_success=%s failures=%d\n",
			a.Name, len(a.Steps), a.Anchor(), a.LastSuccessAt, a.FailureCount)
	}
	return nil
}

func (c CLI) approachShow(args []string) error {
	name := flagValue(args, "--name")
	if name == "" && len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		return Fail("approach_name_missing", map[string]string{"usage": "mav approach show NAME"}).Write(c.Stdout)
	}
	a, err := LoadApproach(c.Root, name)
	if err != nil {
		return Fail("approach_not_found", map[string]string{"name": name}).Write(c.Stdout)
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return Fail("approach_show_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	_, _ = fmt.Fprintln(c.Stdout, string(data))
	return nil
}

// approachExtract derives a named approach from the tap-like
// commands recorded in a past run's `commands.jsonl`. The user runs
// a focused flow once (e.g. `mav run cold_login.yaml`), then
// extracts the approach for reuse: `mav approach extract --from-run
// <id> --name cold_login --anchor start`.
//
// Selectors are read verbatim from the recorded command stream —
// the same id/text/value/x/y that drove the original tap. The
// anchor (first step's `Anchor` field) defaults to the map's
// `Start` screen and can be overridden via `--anchor`.
func (c CLI) approachExtract(args []string) error {
	runID := flagValue(args, "--from-run")
	if runID == "" {
		return Fail("approach_extract_run_missing", map[string]string{"usage": "mav approach extract --from-run RUN_ID --name NAME [--anchor SCREEN]"}).Write(c.Stdout)
	}
	name := flagValue(args, "--name")
	if name == "" {
		return Fail("approach_extract_name_missing", map[string]string{"usage": "mav approach extract --from-run RUN_ID --name NAME [--anchor SCREEN]"}).Write(c.Stdout)
	}
	run, err := LoadRun(c.Root, runID)
	if err != nil || run.Dir == "" {
		return Fail("run_not_found", map[string]string{"run": runID}).Write(c.Stdout)
	}
	steps, warmup, err := extractApproachSteps(filepath.Join(run.Dir, "commands.jsonl"))
	if err != nil {
		return Fail("approach_extract_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if len(steps) == 0 {
		return Fail("approach_extract_empty", map[string]string{"run": runID, "next": "ensure the run contains at least one successful `mav ui tap`"}).Write(c.Stdout)
	}
	anchor := flagValue(args, "--anchor")
	if anchor == "" {
		if m, err := LoadAppMap(c.Root); err == nil {
			anchor = m.Start
		}
	}
	steps[0].Anchor = anchor
	approach := Approach{
		Name:        name,
		Description: "Extracted from run " + runID,
		Steps:       steps,
		Warmup:      warmup,
		RecordedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := SaveApproach(c.Root, approach); err != nil {
		return Fail("approach_save_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	fields := map[string]string{
		"name":   name,
		"steps":  strconv.Itoa(len(steps)),
		"anchor": anchor,
		"run":    runID,
	}
	if unfilled := countUnfilledTypeSteps(steps); unfilled > 0 {
		// Don't fail the extract — credentials by design don't
		// leak into commands.jsonl, so the operator MUST fill
		// them in by hand. The CLI surface tells them where to
		// look and what to edit.
		fields["type_steps_unfilled"] = strconv.Itoa(unfilled)
		fields["next"] = "edit `.mav/map/approaches/" + approachFileName(name) + "` and set `type: \"...\"` on each step where `type_chars` is set; the actual text was never recorded for privacy"
	}
	return OK("approach.extract", fields).Write(c.Stdout)
}

// countUnfilledTypeSteps tells the extractor how many type steps
// it left as placeholders. Operators who run extract without
// reading the warning would otherwise hit
// `approach_step_type_unfilled` at playback time with no context;
// surfacing the count up front saves a debug round.
func countUnfilledTypeSteps(steps []ApproachStep) int {
	n := 0
	for _, s := range steps {
		if s.IsType() && s.Type == "" {
			n++
		}
	}
	return n
}

// extractApproachSteps reads a run's `commands.jsonl` and returns
// a flat slice of `ApproachStep`s, one per successful tap-or-type
// command. Non-tap/non-type commands (delay, capture, evidence)
// are skipped: approaches are pure transition sequences, the
// engine handles settle-time via the step's `Wait` field.
//
// Type actions land as type steps with `Type=""` and `TypeChars`
// set to whatever the run log recorded (`chars=N`). The actual
// text is intentionally NOT recovered — commands.jsonl never
// stores it because credentials and search terms shouldn't end up
// on disk by default. The CLI extract handler surfaces the empty
// Type as a warning so the operator knows to fill it in.
//
// The second return value is the warmup duration that should
// apply BEFORE the first step fires — extracted from any
// `delay`/`sleep`/`wait` entries that precede the first tap or
// type in the run log. Operators usually insert a leading
// `delay: 4s` to let WebView/SDK init finish before the first
// synthetic tap; this preserves that pause through extraction.
func extractApproachSteps(commandsLog string) ([]ApproachStep, string, error) {
	data, err := os.ReadFile(commandsLog)
	if err != nil {
		return nil, "", err
	}
	var steps []ApproachStep
	var warmup string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry["status"] != "ok" {
			continue
		}
		switch entry["action"] {
		case "tap":
			step := ApproachStep{
				ID:     stringFromAny(entry["id"]),
				Text:   stringFromAny(entry["text"]),
				Value:  stringFromAny(entry["value"]),
				X:      stringFromAny(entry["x"]),
				Y:      stringFromAny(entry["y"]),
				Driver: stringFromAny(entry["prefer-driver"]),
			}
			if step.Driver == "" {
				step.Driver = stringFromAny(entry["driver"])
			}
			if step.ID == "" && step.Text == "" && step.Value == "" && step.X == "" {
				continue
			}
			steps = append(steps, step)
		case "type":
			step := ApproachStep{
				Driver:    stringFromAny(entry["driver"]),
				TypeChars: intFromAny(entry["chars"]),
			}
			steps = append(steps, step)
		case "delay", "sleep":
			// Attach the delay to the PRECEDING step's
			// `Wait` field. `playApproachStep` already
			// honours `Wait` as the post-action settle time,
			// so this preserves the original flow's pacing
			// without inventing a "pure delay" step type.
			//
			// A delay BEFORE the first tap is the leading
			// settle-time operators insert to let the
			// launch surface render before any synthetic
			// tap fires (WebView CMP banners, OS splashes,
			// third-party SDK init). Capture it as the
			// approach-level `Warmup`.
			if d := stringFromAny(entry["duration"]); d != "" {
				if len(steps) > 0 {
					steps[len(steps)-1].Wait = d
				} else if warmup == "" {
					warmup = d
				}
			}
		case "wait":
			// `wait` actions encode "block until X is
			// visible, with a cap of `timeout`". The
			// `timeout` is the operator-declared upper bound
			// for how long the screen might legitimately
			// take to settle — that's the safe pessimistic
			// bound to reuse on blind replay, where the
			// actual `elapsed` can vary by machine.
			// Approach playback does a flat `time.Sleep`
			// (no element-visibility predicate), so we err
			// toward the timeout rather than the historical
			// elapsed.
			//
			// Same attach-to-preceding rule as delays; pre-
			// first-step waits become the approach Warmup.
			d := stringFromAny(entry["timeout"])
			if d == "" {
				d = stringFromAny(entry["elapsed"])
			}
			if d != "" {
				if len(steps) > 0 && steps[len(steps)-1].Wait == "" {
					steps[len(steps)-1].Wait = d
				} else if len(steps) == 0 && warmup == "" {
					warmup = d
				}
			}
		}
	}
	return steps, warmup, nil
}

// intFromAny coerces the JSON number/string representations of an
// int into a Go int. The commands.jsonl format leaves "chars" as a
// string in some flows and a number in others, so the extractor
// tolerates both.
func intFromAny(v any) int {
	switch t := v.(type) {
	case nil:
		return 0
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, err := strconv.Atoi(t)
		if err != nil {
			return 0
		}
		return n
	default:
		s := fmt.Sprint(t)
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0
		}
		return n
	}
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// applyBlindStartApproach plays the freshest non-failed approach
// anchored on the configured start screen WITHOUT requiring the
// post-launch tree probe to succeed first. This is the recovery
// path for AX-opaque launch surfaces — Iubenda CMP WebViews, OS
// consent prompts, splash screens — where the accessibility tree
// is empty or unrecognised but the app DOES respond to coord-taps
// and other selectors we already recorded.
//
// Returns `(observation, true)` when at least one approach
// completed without an early `playApproachStep` error AND the
// subsequent tree probe succeeded. Returns `(_, false)` to let the
// caller surface the original `launch_tree_not_ready` error.
//
// Distinct from `applyMatchingApproaches` (which gates on observed
// screen): this variant only checks the anchor against the
// configured start screen, since we have no current observation
// to compare to.
func (c CLI) applyBlindStartApproach(ctx context.Context, cfg Config, run RunState, startScreen string, ttl time.Duration) (ScreenObservation, bool) {
	if startScreen == "" {
		return ScreenObservation{}, false
	}
	approaches, err := LoadAllApproaches(c.Root)
	if err != nil || len(approaches) == 0 {
		return ScreenObservation{}, false
	}
	matches := MatchingApproaches(approaches, startScreen, ttl, time.Now().UTC())
	if len(matches) == 0 {
		return ScreenObservation{}, false
	}
	for _, a := range matches {
		if _, err := c.runApproachStep(ctx, run, a.Name); err != nil {
			continue
		}
		// Try to observe the tree post-replay. If the approach
		// dismissed the AX-blind surface (CMP banner, splash),
		// the tree should now be readable.
		tree, treeErr := c.waitForTreeReady(ctx, cfg, 8*time.Second)
		if treeErr != nil {
			continue
		}
		observed, _ := ObserveScreenDetailedWithDriver(c.Root, cfg, run, tree.Raw, tree.Driver)
		return observed, true
	}
	return ScreenObservation{}, false
}

// applyMatchingApproaches plays the first approach whose anchor
// matches the observed screen, re-observes the resulting tree, and
// reports the new screen state. Returns `(observation, true)` when
// at least one approach ran (the caller should treat the new
// `observation` as the authoritative current screen and recompute
// the route from there). Returns `(_, false)` when no approach
// matched OR every matching approach failed during playback.
//
// Multiple approaches with the same anchor are tried in declaration
// order (fresh-first via `MatchingApproaches`). The first successful
// playback short-circuits — a single approach is enough to move the
// engine onto a different screen. Failed approaches are demoted via
// `markApproachFailure` and the next one is tried.
func (c CLI) applyMatchingApproaches(ctx context.Context, cfg Config, run RunState, currentScreen string, ttl time.Duration) (ScreenObservation, bool) {
	approaches, err := LoadAllApproaches(c.Root)
	if err != nil || len(approaches) == 0 {
		return ScreenObservation{}, false
	}
	matches := MatchingApproaches(approaches, currentScreen, ttl, time.Now().UTC())
	if len(matches) == 0 {
		return ScreenObservation{}, false
	}
	for _, a := range matches {
		if _, err := c.runApproachStep(ctx, run, a.Name); err != nil {
			continue
		}
		// Re-observe so the caller has the new screen to BFS
		// from. waitForTreeReady is bounded to keep the
		// approach replay snappy — if the tree never settles
		// we surface the failure via the normal route_failure
		// path.
		tree, treeErr := c.waitForTreeReady(ctx, cfg, 5*time.Second)
		if treeErr != nil {
			continue
		}
		observed, _ := ObserveScreenDetailedWithDriver(c.Root, cfg, run, tree.Raw, tree.Driver)
		return observed, true
	}
	return ScreenObservation{}, false
}

// runApproachStep loads a named approach from disk and replays its
// steps as a unit. Failures inside the approach are reported with
// the approach name so operators can identify which one broke.
// `LastSuccessAt` is stamped on the approach record on a clean run;
// `FailureCount` advances on any error.
func (c CLI) runApproachStep(ctx context.Context, run RunState, name string) (map[string]string, error) {
	approach, err := LoadApproach(c.Root, name)
	if err != nil {
		return map[string]string{"approach": name}, fmt.Errorf("approach_not_found")
	}
	if len(approach.Steps) == 0 {
		return map[string]string{"approach": name}, fmt.Errorf("approach_empty")
	}
	cfg, cfgErr := LoadConfig(c.Root)
	if cfgErr != nil {
		return map[string]string{"approach": name}, fmt.Errorf("config_not_found")
	}
	// Pre-first-step warmup: a duration the original run paused
	// BEFORE its first tap, captured by `approach extract`.
	// Typically the `delay: 4s` operators insert to let the
	// launch surface render before any synthetic tap fires.
	if approach.Warmup != "" {
		time.Sleep(parseFlowDuration(approach.Warmup, 0))
	}
	executed := 0
	for i, step := range approach.Steps {
		if err := c.playApproachStep(ctx, cfg, step); err != nil {
			markApproachFailure(c.Root, approach)
			return map[string]string{
				"approach":    name,
				"step":        strconv.Itoa(i + 1),
				"total_steps": strconv.Itoa(len(approach.Steps)),
				"executed":    strconv.Itoa(executed),
			}, fmt.Errorf("approach_step_failed: %v", err)
		}
		executed++
		_ = run // run reserved for evidence integration in P3
	}
	markApproachSuccess(c.Root, approach)
	return map[string]string{
		"approach": name,
		"steps":    strconv.Itoa(executed),
	}, nil
}

// playApproachStep replays a single `ApproachStep`. Tap steps
// compose the equivalent `mav ui tap` arguments and delegate to
// `uiTap`; type steps delegate to `uiType`. The post-action
// auto-observe is suppressed because the route engine owns the
// observation lifecycle during approach playback.
//
// A type step with an empty `Type` payload is rejected with
// `approach_step_type_unfilled` — that's `approach extract`'s
// metadata-only placeholder reminding the operator to supply the
// text before using the approach (commands.jsonl doesn't record
// the actual text by design).
func (c CLI) playApproachStep(ctx context.Context, cfg Config, step ApproachStep) error {
	if step.IsType() {
		if step.Type == "" {
			return fmt.Errorf("approach_step_type_unfilled")
		}
		typeOpts := GlobalOptions{PreferDriver: step.Driver}
		var out bytes.Buffer
		if err := c.withStdout(&out).uiType(ctx, typeOpts, cfg, []string{step.Type}); err != nil {
			return err
		}
		if code, ok := outputFailureCode(out.String()); ok {
			return fmt.Errorf("%s", code)
		}
		if step.Wait != "" {
			time.Sleep(parseFlowDuration(step.Wait, time.Second))
		}
		return nil
	}
	args := []string{}
	if step.ID != "" {
		args = append(args, "--id", step.ID)
	}
	if step.Text != "" {
		args = append(args, "--text", step.Text)
	}
	if step.Value != "" {
		args = append(args, "--value", step.Value)
	}
	if step.X != "" && step.Y != "" {
		args = append(args, "--x", step.X, "--y", step.Y)
	}
	if len(args) == 0 {
		return fmt.Errorf("approach_step_empty")
	}
	driver := step.Driver
	tapOpts := GlobalOptions{PreferDriver: driver, SkipAutoObserve: true}
	var out bytes.Buffer
	if err := c.withStdout(&out).uiTap(ctx, tapOpts, cfg, args); err != nil {
		return err
	}
	if code, ok := outputFailureCode(out.String()); ok {
		return fmt.Errorf("%s", code)
	}
	if step.Wait != "" {
		time.Sleep(parseFlowDuration(step.Wait, time.Second))
	}
	return nil
}

// markApproachSuccess stamps the run-time bookkeeping on a clean
// playback so subsequent calls to `MatchingApproaches` rank it as
// fresh.
func markApproachSuccess(root string, a Approach) {
	a.LastSuccessAt = time.Now().UTC().Format(time.RFC3339)
	a.LastFailureAt = ""
	a.FailureCount = 0
	_ = SaveApproach(root, a)
}

// markApproachFailure increments the failure counter and records
// the timestamp. Two consecutive failures (without an intervening
// success) demote the approach the same way edges get demoted.
func markApproachFailure(root string, a Approach) {
	a.FailureCount++
	a.LastFailureAt = time.Now().UTC().Format(time.RFC3339)
	_ = SaveApproach(root, a)
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
		c.observeFlowStepScreen(ctx, opts, run, child)
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
		c.observeFlowStepScreen(ctx, opts, run, child)
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
			c.observeFlowStepScreen(ctx, opts, run, child)
			appendFlowStep(run, index, "whileNotVisible."+child.Action, elapsed, "ok", childFields)
			if time.Since(start) >= timeout {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (c CLI) goScreen(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("screen_missing", map[string]string{"usage": "mav go SCREEN_ID"}).Write(c.Stdout)
	}
	screenID := args[0]
	ttl, ttlErr := parseEdgeTTLFlag(args)
	if ttlErr != nil {
		return Fail("edge_ttl_invalid", map[string]string{"value": flagValue(args, "--edge-ttl"), "next": "use a duration like '14d', '24h', or '0' to disable"}).Write(c.Stdout)
	}
	if flagValue(args, "--edge-ttl") != "" {
		c.RouteEdgeTTL = ttl
		if ttl == 0 {
			// "Explicitly zero" must override the default, so
			// translate to the negative sentinel mirrored by
			// `effectiveEdgeTTL`.
			c.RouteEdgeTTL = -1
		}
	}
	if retry, retryErr := parseEdgeRetryFlag(args); retryErr != nil {
		return Fail("edge_retry_invalid", map[string]string{"value": flagValue(args, "--edge-retry"), "next": "use a non-negative integer (0 disables retry)"}).Write(c.Stdout)
	} else if flagValue(args, "--edge-retry") != "" {
		// Only stash the override when the flag was supplied;
		// otherwise leave `RouteEdgeRetry == 0` so the default
		// kicks in (a non-zero override of "0" expresses
		// "disable").
		c.RouteEdgeRetry = retry
		if retry == 0 {
			c.RouteEdgeRetry = -1
		}
	}
	if hasFlag(args, "--no-coord-tap") {
		c.RouteNoCoordTap = true
	}
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
	if _, ok := m.Screens[screenID]; !ok {
		return Fail("screen_not_found", map[string]string{
			"screen": screenID,
			"next":   "add or configure a stable screen identifier, then explore with mav ui tree/tap",
		}).Write(c.Stdout)
	}
	route, routeErr := RouteFromWithTTL(m, m.Start, screenID, ttl, time.Now().UTC())
	cfg, cfgErr := LoadConfig(c.Root)
	if cfgErr != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	openArgs := []string{}
	needsDriver := ""
	if routeErr == nil {
		needsDriver = routeRequiredDriver(m, screenID, route)
	}
	if needsDriver == "appium" {
		if err := c.ensureAppiumAvailable(ctx); err != nil {
			fields := map[string]string{"screen": screenID, "required_driver": "appium"}
			if appErr, ok := err.(appiumError); ok {
				if appErr.Message != "" {
					fields["issue"] = appErr.Message
				}
				if appErr.Next != "" {
					fields["next"] = appErr.Next
				}
			} else {
				fields["issue"] = err.Error()
			}
			return Fail("required_driver_missing", fields).Write(c.Stdout)
		}
		openArgs = append(openArgs, "--warm-appium")
	}
	if err := c.withStdout(io.Discard).open(ctx, GlobalOptions{}, openArgs); err != nil {
		return Fail("open_failed", map[string]string{"screen": screenID}).Write(c.Stdout)
	}
	run, runErr := LoadRun(c.Root, "")
	if runErr != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	if needsDriver == "appium" {
		if session, err := readAppiumSession(run); err != nil || session.SessionID == "" {
			_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
			return Fail("required_driver_missing", map[string]string{
				"screen":          screenID,
				"required_driver": "appium",
				"issue":           "appium_warmup_failed",
				"next":            "run mav open --warm-appium and inspect the run appium.log",
				"run":             run.ID,
			}).Write(c.Stdout)
		}
	}
	evidenceStarted := time.Now()
	if err := c.withStdout(io.Discard).evidenceStart(ctx, GlobalOptions{}, []string{"--run", run.ID}); err != nil {
		return Fail("evidence_start_failed", map[string]string{"run": run.ID, "screen": screenID}).Write(c.Stdout)
	}
	if tree, err := c.waitForTreeReady(ctx, cfg, 8*time.Second); err == nil {
		observed, _ := ObserveScreenDetailedWithDriver(c.Root, cfg, run, tree.Raw, tree.Driver)
		if refreshed, err := LoadAppMap(c.Root); err == nil {
			m = refreshed
		}
		// Approach playback: replay any named approach whose
		// anchor matches the observed screen before BFS engages.
		// Approaches collapse known long deterministic chains
		// (login, onboarding, paywall) so the route engine only
		// has to BFS the interesting suffix.
		if observed.Screen != "" && observed.Screen != "unknown" && !hasFlag(args, "--no-approach") {
			if newObserved, ok := c.applyMatchingApproaches(ctx, cfg, run, observed.Screen, ttl); ok {
				observed = newObserved
				if refreshed, err := LoadAppMap(c.Root); err == nil {
					m = refreshed
				}
			}
		}
		if observed.Screen == screenID {
			route = nil
			routeErr = nil
		} else if observed.Screen != "" && observed.Screen != "unknown" {
			if observedRoute, err := RouteFromWithTTL(m, observed.Screen, screenID, ttl, time.Now().UTC()); err == nil {
				route = observedRoute
				routeErr = nil
				if required := routeRequiredDriver(m, screenID, route); required != "" {
					needsDriver = required
				}
			} else {
				route = nil
				routeErr = err
			}
		} else {
			route = nil
			routeErr = fmt.Errorf("route_not_found")
		}
	} else {
		// AX-blind launch fallback: when the post-launch tree
		// probe times out, look for a start-anchored approach
		// (the very purpose of approaches is to coord-tap
		// through screens the AX hierarchy can't see — Iubenda
		// CMP WebViews, OS-level consent prompts, etc.). Play
		// it blind, then retry tree-ready. Only surface
		// `launch_tree_not_ready` when even the blind replay
		// doesn't unlock a readable tree.
		if !hasFlag(args, "--no-approach") {
			if recoveredObserved, ok := c.applyBlindStartApproach(ctx, cfg, run, m.Start, ttl); ok {
				observed := recoveredObserved
				if refreshed, err := LoadAppMap(c.Root); err == nil {
					m = refreshed
				}
				if observed.Screen == screenID {
					route = nil
					routeErr = nil
				} else if observed.Screen != "" && observed.Screen != "unknown" {
					if observedRoute, err := RouteFromWithTTL(m, observed.Screen, screenID, ttl, time.Now().UTC()); err == nil {
						route = observedRoute
						routeErr = nil
						if required := routeRequiredDriver(m, screenID, route); required != "" {
							needsDriver = required
						}
					} else {
						route = nil
						routeErr = err
					}
				} else {
					route = nil
					routeErr = fmt.Errorf("route_not_found")
				}
			} else {
				_, _ = c.captureEvidenceStep(ctx, run, "launch-not-ready", "App did not expose a ready accessibility tree after launch")
				_ = c.withStdout(io.Discard).evidenceStop(ctx, GlobalOptions{}, []string{"--run", run.ID, "--note", "Launch tree was not ready"})
				_ = c.withStdout(io.Discard).evidenceReport(GlobalOptions{}, []string{"--run", run.ID})
				_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
				return Fail("launch_tree_not_ready", map[string]string{"run": run.ID, "screen": screenID, "report": filepath.Join(run.Dir, "report.html")}).Write(c.Stdout)
			}
		} else {
			_, _ = c.captureEvidenceStep(ctx, run, "launch-not-ready", "App did not expose a ready accessibility tree after launch")
			_ = c.withStdout(io.Discard).evidenceStop(ctx, GlobalOptions{}, []string{"--run", run.ID, "--note", "Launch tree was not ready"})
			_ = c.withStdout(io.Discard).evidenceReport(GlobalOptions{}, []string{"--run", run.ID})
			_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
			return Fail("launch_tree_not_ready", map[string]string{"run": run.ID, "screen": screenID, "report": filepath.Join(run.Dir, "report.html")}).Write(c.Stdout)
		}
	}
	if routeErr != nil {
		// Live-discovery fallback: only when the failure is a
		// typed `*RouteFailure` reporting an unreachable
		// subgraph or unknown target. We skip discovery for
		// plain string errors coming from launch/observation
		// fallbacks — those failure modes call for evidence,
		// not random taps.
		shouldDiscover := false
		var rfCandidate *RouteFailure
		if errors.As(routeErr, &rfCandidate) {
			// `unknown_target` is the canonical "the map has no
			// record of this screen at all" signal — that's
			// when live discovery is most likely to help and
			// least likely to do harm. `unreachable_subgraph`
			// means the map HAS the screen but no path; that's
			// often a sign the operator should add an edge by
			// hand. Discovery is opt-in for that case via
			// `--discover-on-unreachable`.
			switch rfCandidate.Reason {
			case RouteFailureUnknownTarget:
				shouldDiscover = true
			case RouteFailureUnreachableSubgraph:
				shouldDiscover = hasFlag(args, "--discover-on-unreachable")
			}
		}
		if shouldDiscover && !hasFlag(args, "--no-discover") {
			if rResult, rErr := c.runDiscoveryFallback(ctx, cfg, run, screenID); rErr == nil && rResult.Reached {
				_ = PersistDiscoveredPath(c.Root, rResult.Path)
				route = nil
				routeErr = nil
				fields, navErr := c.navigateToScreen(ctx, screenID)
				if navErr == nil {
					_, _ = c.captureEvidenceStep(ctx, run, "discovered", "Reached "+screenID+" via live discovery")
					if time.Since(evidenceStarted) < 1200*time.Millisecond {
						time.Sleep(1200*time.Millisecond - time.Since(evidenceStarted))
					}
					_ = c.withStdout(io.Discard).evidenceStop(ctx, GlobalOptions{}, []string{"--run", run.ID, "--note", "Navigated via discovery"})
					_ = c.withStdout(io.Discard).evidenceReport(GlobalOptions{}, []string{"--run", run.ID})
					_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
					fields["run"] = run.ID
					fields["report"] = filepath.Join(run.Dir, "report.html")
					fields["discovered"] = "true"
					fields["discover_steps"] = strconv.Itoa(len(rResult.Path))
					return OK("go", fields).Write(c.Stdout)
				}
			}
		}
		fields := map[string]string{"screen": screenID}
		code := routeErr.Error()
		if code == "screen_not_found" || code == "route_not_found" || code == "route_start_not_found" {
			fields["next"] = "add or configure a stable screen identifier, then explore with mav ui tree/tap"
		}
		// Surface the structured detail so operators (and the P3
		// evidence renderer) see why the BFS gave up and which
		// edges were considered. Each field is namespaced so the
		// existing flat CLI output stays parseable.
		var rf *RouteFailure
		if errors.As(routeErr, &rf) {
			fields["reason"] = string(rf.Reason)
			if rf.NearestKnownScreen != "" {
				fields["nearest_reachable"] = rf.NearestKnownScreen
			}
			if n := len(rf.SkippedEdges); n > 0 {
				fields["skipped_edges"] = strconv.Itoa(n)
				if summary := summariseEdgeSkips(rf.SkippedEdges); summary != "" {
					fields["skipped_reasons"] = summary
				}
			}
			// Reason-specific operator hints replace the generic
			// "add a stable identifier" message when we have
			// concrete information about what went wrong.
			if hint := routeFailureHint(rf); hint != "" {
				fields["next"] = hint
			}
		}
		_ = c.withStdout(io.Discard).evidenceStop(ctx, GlobalOptions{}, []string{"--run", run.ID, "--note", "No route to " + screenID})
		_ = c.withStdout(io.Discard).evidenceReport(GlobalOptions{}, []string{"--run", run.ID})
		_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
		fields["run"] = run.ID
		fields["report"] = filepath.Join(run.Dir, "report.html")
		return Fail(code, fields).Write(c.Stdout)
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
	if needsDriver != "" {
		fields["required_driver"] = needsDriver
	}
	return OK("go", fields).Write(c.Stdout)
}

func routeRequiredDriver(m AppMap, target string, route []Edge) string {
	for _, edge := range route {
		if edge.Driver == "appium" {
			return "appium"
		}
		if screen := m.Screens[edge.To]; screen.Driver == "appium" {
			return "appium"
		}
	}
	if screen := m.Screens[target]; screen.Driver == "appium" {
		return "appium"
	}
	return ""
}

func (c CLI) mapCommand(args []string) error {
	if len(args) == 0 {
		return Fail("map_command_missing", map[string]string{"usage": "mav map list|show|graph|verify|prune"}).Write(c.Stdout)
	}
	m, err := LoadAppMap(c.Root)
	if err != nil {
		return Fail("app_map_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	switch args[0] {
	case "list":
		if err := OK("map.list", map[string]string{"screens": strconv.Itoa(len(m.Screens)), "start": m.Start, "current": CurrentScreen(c.Root)}).Write(c.Stdout); err != nil {
			return err
		}
		for _, id := range sortedScreenKeys(m.Screens) {
			screen := m.Screens[id]
			fmt.Fprintf(c.Stdout, "screen id=%s title=%s edges=%d recognizers=%d\n", id, quoteIfNeeded(screen.Title), len(screen.Edges), len(screen.Recognizers))
		}
		return nil
	case "show":
		if len(args) < 2 {
			return Fail("screen_missing", map[string]string{"usage": "mav map show SCREEN_ID"}).Write(c.Stdout)
		}
		screen, ok := m.Screens[args[1]]
		if !ok {
			return Fail("screen_not_found", map[string]string{"screen": args[1]}).Write(c.Stdout)
		}
		if err := OK("map.show", map[string]string{"screen": screen.ID, "edges": strconv.Itoa(len(screen.Edges)), "recognizers": strconv.Itoa(len(screen.Recognizers))}).Write(c.Stdout); err != nil {
			return err
		}
		for _, edge := range screen.Edges {
			fmt.Fprintf(c.Stdout, "edge from=%s to=%s id=%s text=%s value=%s driver=%s confidence=%s failures=%d\n",
				quoteIfNeeded(edgeFrom(screen.ID, edge)), quoteIfNeeded(edge.To), quoteIfNeeded(edge.ID), quoteIfNeeded(edge.Text), quoteIfNeeded(edge.Value), quoteIfNeeded(edge.Driver), quoteIfNeeded(edge.Confidence), edge.FailureCount)
		}
		return nil
	case "graph":
		fmt.Fprintln(c.Stdout, "digraph mav_map {")
		for _, id := range sortedScreenKeys(m.Screens) {
			screen := m.Screens[id]
			if len(screen.Edges) == 0 {
				fmt.Fprintf(c.Stdout, "  %q;\n", id)
			}
			for _, edge := range screen.Edges {
				label := edge.ID
				if label == "" {
					label = edge.Text
				}
				if label == "" {
					label = edge.Value
				}
				if label == "" && edge.X != "" && edge.Y != "" {
					label = edge.X + "," + edge.Y
				}
				fmt.Fprintf(c.Stdout, "  %q -> %q [label=%q];\n", edgeFrom(id, edge), edge.To, label)
			}
		}
		fmt.Fprintln(c.Stdout, "}")
		return nil
	case "verify":
		fields := verifyMapFields(m)
		if err := ValidateAppMap(m); err != nil {
			fields["error"] = err.Error()
			return Fail("app_map_invalid", fields).Write(c.Stdout)
		}
		if fields["warnings"] != "" && fields["warnings"] != "0" {
			return Fail("app_map_warnings", fields).Write(c.Stdout)
		}
		return OK("map.verify", fields).Write(c.Stdout)
	case "prune":
		pruned := 0
		filter := flagValue(args[1:], "--filter")
		applyWarnings := hasFlag(args[1:], "--apply-warnings") || hasFlag(args[1:], "--all")
		dryRun := hasFlag(args[1:], "--dry-run")
		if filter == "" && applyWarnings {
			filter = "warnings"
		}
		for id, screen := range m.Screens {
			kept := screen.Edges[:0]
			seenSelectors := map[string]bool{}
			for _, edge := range screen.Edges {
				if shouldPruneMapEdge(id, edge, filter, seenSelectors) {
					pruned++
					continue
				}
				if edge.ID != "" || edge.Text != "" || edge.Value != "" {
					seenSelectors[edge.ID+"\x00"+edge.Text+"\x00"+edge.Value] = true
				}
				kept = append(kept, edge)
			}
			screen.Edges = kept
			m.Screens[id] = screen
		}
		if !dryRun {
			if err := SaveAppMap(c.Root, m); err != nil {
				return Fail("map_prune_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
			}
		}
		fields := map[string]string{"pruned": strconv.Itoa(pruned)}
		if filter != "" {
			fields["filter"] = filter
		}
		if dryRun {
			fields["dry_run"] = "true"
		}
		return OK("map.prune", fields).Write(c.Stdout)
	default:
		return Fail("map_unknown_command", map[string]string{"command": args[0], "usage": "mav map list|show|graph|verify|prune"}).Write(c.Stdout)
	}
}

func shouldPruneMapEdge(from string, edge Edge, filter string, seenSelectors map[string]bool) bool {
	if filter == "" {
		return edge.Confidence == "low"
	}
	switch filter {
	case "warnings", "all":
		return edge.Confidence == "low" || edge.X != "" || edge.Y != "" || edge.From != "" && edge.From != from || duplicateEdgeSelector(edge, seenSelectors)
	case "coordinate-edges", "coordinate_edges":
		return edge.X != "" || edge.Y != ""
	case "duplicate-selectors", "duplicate_selectors":
		return duplicateEdgeSelector(edge, seenSelectors)
	case "low-confidence", "low_confidence":
		return edge.Confidence == "low"
	default:
		return edge.Confidence == "low"
	}
}

func duplicateEdgeSelector(edge Edge, seenSelectors map[string]bool) bool {
	if edge.ID == "" && edge.Text == "" && edge.Value == "" {
		return false
	}
	key := edge.ID + "\x00" + edge.Text + "\x00" + edge.Value
	return seenSelectors[key]
}

func edgeFrom(defaultFrom string, edge Edge) string {
	if edge.From != "" {
		return edge.From
	}
	return defaultFrom
}

func verifyMapFields(m AppMap) map[string]string {
	warnings := 0
	coordinateEdges := 0
	lowConfidence := 0
	duplicateSelectors := 0
	fromMismatches := 0
	identityMissing := 0
	seenSelectors := map[string]string{}
	for from, screen := range m.Screens {
		if !screenHasExplicitScreenIdentity(screen) {
			identityMissing++
			warnings++
		}
		for _, edge := range screen.Edges {
			if edge.From != "" && edge.From != from {
				fromMismatches++
				warnings++
			}
			if edge.X != "" || edge.Y != "" {
				coordinateEdges++
				warnings++
			}
			if edge.Confidence == "low" {
				lowConfidence++
				warnings++
			}
			key := from + "\x00" + edge.ID + "\x00" + edge.Text + "\x00" + edge.Value
			if edge.ID != "" || edge.Text != "" || edge.Value != "" {
				if prev, ok := seenSelectors[key]; ok && prev != edge.To {
					duplicateSelectors++
					warnings++
				}
				seenSelectors[key] = edge.To
			}
		}
	}
	return map[string]string{
		"screens":             strconv.Itoa(len(m.Screens)),
		"warnings":            strconv.Itoa(warnings),
		"coordinate_edges":    strconv.Itoa(coordinateEdges),
		"low_confidence":      strconv.Itoa(lowConfidence),
		"duplicate_selectors": strconv.Itoa(duplicateSelectors),
		"from_mismatches":     strconv.Itoa(fromMismatches),
		"identity_missing":    strconv.Itoa(identityMissing),
	}
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
	if CurrentScreen(c.Root) == screenID {
		return map[string]string{"screen": screenID, "steps": "0", "already_at_target": "true"}, nil
	}
	if len(routeOverride) == 0 {
		SetCurrentScreen(c.Root, m.Start, run.ID)
	}
	ClearPendingMapAction(c.Root)
	currentFrom := CurrentScreen(c.Root)
	if currentFrom == "" {
		currentFrom = m.Start
	}
	// Last-edge stats kept around so the success path can surface
	// the driver that actually delivered the route (especially
	// `coord` to signal a P1.c fallback fired). They are
	// overwritten on each edge so the LAST one is reported, which
	// is the one observers most often care about.
	var lastEdgeDriver string
	var lastEdgeRetried bool
	for _, edge := range route {
		from := edge.From
		if from == "" {
			from = currentFrom
		}
		beforeTree, beforeErr := c.waitForTreeReady(ctx, cfg, 5*time.Second)
		if beforeErr != nil {
			return map[string]string{"requested": screenID, "stuck_at": from, "edge_target": edge.To}, fmt.Errorf("tree_not_ready")
		}
		afterTree, usedDriver, retry, err := c.executeRouteEdge(ctx, cfg, edge, beforeTree.Raw)
		lastEdgeDriver = usedDriver
		lastEdgeRetried = retry
		if err != nil {
			markRouteEdgeFailure(c.Root, from, edge)
			fields := map[string]string{"requested": screenID, "stuck_at": from, "edge_target": edge.To}
			if usedDriver != "" {
				fields["driver"] = usedDriver
			}
			if retry {
				fields["retried_driver"] = "true"
			}
			return fields, err
		}
		observed, observeErr := ObserveScreenDetailedWithDriver(c.Root, cfg, run, afterTree.Raw, afterTree.Driver)
		if observeErr != nil {
			return map[string]string{"requested": screenID, "stuck_at": from, "edge_target": edge.To}, fmt.Errorf("map_observe_failed")
		}
		if observed.Screen != edge.To {
			markRouteEdgeFailure(c.Root, from, edge)
			return map[string]string{"requested": screenID, "stuck_at": observed.Screen, "edge_target": edge.To, "driver": usedDriver}, fmt.Errorf("route_target_not_observed")
		}
		SetCurrentScreen(c.Root, edge.To, run.ID)
		ClearPendingMapAction(c.Root)
		currentFrom = edge.To
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
	fields := map[string]string{"screen": screenID, "steps": strconv.Itoa(len(route))}
	if lastEdgeDriver != "" {
		fields["driver"] = lastEdgeDriver
	}
	if lastEdgeRetried {
		fields["retried_driver"] = "true"
	}
	return fields, nil
}

// DefaultEdgeRetry is how many times `executeRouteEdge` re-attempts
// the SAME edge with the SAME driver when the tap visibly succeeds
// but the tree doesn't change (the iOS 26 simulator routinely drops
// the first synthetic tap on a tab bar right after a screen
// transition). 1 retry catches the drop without making genuine
// no-op edges twice as expensive.
const DefaultEdgeRetry = 1

// edgeRetryWait is the brief pause between same-edge retries.
// Empirically iOS 26 settles within 200ms after a transition.
const edgeRetryWait = 350 * time.Millisecond

func (c CLI) executeRouteEdge(ctx context.Context, cfg Config, edge Edge, beforeTree string) (readyUITree, string, bool, error) {
	return c.executeRouteEdgeWithRetry(ctx, cfg, edge, beforeTree, c.effectiveEdgeRetry())
}

// effectiveEdgeRetry resolves the same-edge retry count from the CLI
// field. Zero means "use the package default"; a negative override
// means "no retries". Centralised so callers don't have to repeat
// the precedence rules.
func (c CLI) effectiveEdgeRetry() int {
	switch {
	case c.RouteEdgeRetry < 0:
		return 0
	case c.RouteEdgeRetry == 0:
		return DefaultEdgeRetry
	default:
		return c.RouteEdgeRetry
	}
}

// executeRouteEdgeWithRetry is the same loop as `executeRouteEdge`
// but retries the SAME driver up to `maxSameEdgeRetry` times when the
// tap succeeded but the tree did not change. Same-edge retries do not
// count toward `markRouteEdgeFailure`; only a final unsuccessful
// outcome does.
//
// After exhausting all (driver × retry) attempts via id/text/value
// selectors, if the edge carries fresh coordinates and the
// `--no-coord-tap` opt-out is not set, one last attempt is made with
// `--x`/`--y` only. This catches edges whose id was renamed in
// product code between map seeding and replay — the coord-tap lands
// the touch, and the post-tap auto-observe (when re-enabled by the
// caller) refreshes the edge selector on the next non-suppressed
// observation cycle.
func (c CLI) executeRouteEdgeWithRetry(ctx context.Context, cfg Config, edge Edge, beforeTree string, maxSameEdgeRetry int) (readyUITree, string, bool, error) {
	drivers := routeEdgeDrivers(edge)
	var lastDriver string
	var lastTapErr error
	tapSucceeded := false
	retried := false
	for i, driver := range drivers {
		lastDriver = driver
		if i > 0 {
			retried = true
		}
		for attempt := 0; attempt <= maxSameEdgeRetry; attempt++ {
			if err := c.tapRouteEdge(ctx, cfg, edge, driver); err != nil {
				lastTapErr = err
				break // give up on this driver; move to the next
			}
			tapSucceeded = true
			ClearPendingMapAction(c.Root)
			if edge.Wait != "" {
				time.Sleep(parseFlowDuration(edge.Wait, 0))
			}
			afterTree, afterErr := c.waitForTreeReady(ctx, cfg, 5*time.Second)
			if afterErr != nil {
				return readyUITree{}, lastDriver, retried, fmt.Errorf("tree_not_ready")
			}
			if sameTreeForRoute(beforeTree, afterTree.Raw) {
				if attempt < maxSameEdgeRetry {
					// Same-driver retry path. iOS 26 sim drops the
					// first tap post-transition; the second always
					// lands. Pause briefly to let any in-flight
					// animation settle before re-tapping.
					retried = true
					time.Sleep(edgeRetryWait)
					continue
				}
				// Exhausted retries on this driver — fall through
				// to the next driver in the fallback chain.
				break
			}
			markRouteEdgeSuccess(c.Root, edge.From, edge)
			return afterTree, lastDriver, retried, nil
		}
	}
	// Id/text-driver chain exhausted. Coord-tap fallback: only run
	// when allowed, the edge has coordinates, and the edge is not
	// `low` confidence / not stale (a stale coord on a refactored
	// screen would tap the wrong button).
	if c.shouldCoordTapFallback(edge) {
		if afterTree, ok, err := c.attemptCoordTapFallback(ctx, cfg, edge, beforeTree); err != nil {
			return readyUITree{}, "coord", true, err
		} else if ok {
			markRouteEdgeSuccess(c.Root, edge.From, edge)
			return afterTree, "coord", true, nil
		}
	}
	ClearPendingMapAction(c.Root)
	if !tapSucceeded && lastTapErr != nil {
		return readyUITree{}, lastDriver, retried, fmt.Errorf("tap_failed")
	}
	return readyUITree{}, lastDriver, retried, fmt.Errorf("route_no_screen_change")
}

// shouldCoordTapFallback gates the coord-only retry. The combined
// rule is: an operator opt-out short-circuits everything; the edge
// must carry coords AND a semantic selector (id/text/value) — a
// coord-only edge has already been tried with its native selector by
// the driver loop, so a second attempt would just repeat the same
// failure; and it must not be `low` confidence or stale (a stale
// coord on a refactored screen would tap the wrong button).
func (c CLI) shouldCoordTapFallback(edge Edge) bool {
	if c.RouteNoCoordTap {
		return false
	}
	if edge.X == "" || edge.Y == "" {
		return false
	}
	if edge.ID == "" && edge.Text == "" && edge.Value == "" {
		return false
	}
	if edge.Confidence == "low" {
		return false
	}
	if IsEdgeStale(edge, c.effectiveEdgeTTL(), time.Now().UTC()) {
		return false
	}
	return true
}

// effectiveEdgeTTL mirrors `effectiveEdgeRetry`: zero means "use
// `DefaultEdgeTTL`", a positive value is honoured verbatim, and a
// negative value disables the staleness gate. Centralised so the
// rule travels with the field.
func (c CLI) effectiveEdgeTTL() time.Duration {
	switch {
	case c.RouteEdgeTTL < 0:
		return 0
	case c.RouteEdgeTTL == 0:
		return DefaultEdgeTTL
	default:
		return c.RouteEdgeTTL
	}
}

// attemptCoordTapFallback fires `mav ui tap --x --y` against the
// edge's recorded coordinates, observes the post-tap tree, and
// reports whether the screen changed. Returns `(_, false, nil)` if
// the coord-tap landed but produced no observable transition (so the
// caller can record `route_no_screen_change` without crediting the
// edge with a success).
func (c CLI) attemptCoordTapFallback(ctx context.Context, cfg Config, edge Edge, beforeTree string) (readyUITree, bool, error) {
	args := []string{"--x", edge.X, "--y", edge.Y}
	// Coord-tap uses no semantic selector, so the SkipAutoObserve
	// flag is the only signal we need to forward — the in-tap
	// observation would race with our own waitForTreeReady probe
	// below.
	tapOpts := GlobalOptions{PreferDriver: "", SkipAutoObserve: true}
	var out bytes.Buffer
	if err := c.withStdout(&out).uiTap(ctx, tapOpts, cfg, args); err != nil {
		return readyUITree{}, false, fmt.Errorf("tap_failed")
	}
	if code, ok := outputFailureCode(out.String()); ok {
		return readyUITree{}, false, errors.New(code)
	}
	ClearPendingMapAction(c.Root)
	if edge.Wait != "" {
		time.Sleep(parseFlowDuration(edge.Wait, 0))
	}
	afterTree, afterErr := c.waitForTreeReady(ctx, cfg, 5*time.Second)
	if afterErr != nil {
		return readyUITree{}, false, fmt.Errorf("tree_not_ready")
	}
	if sameTreeForRoute(beforeTree, afterTree.Raw) {
		return readyUITree{}, false, nil
	}
	return afterTree, true, nil
}

func (c CLI) tapRouteEdge(ctx context.Context, cfg Config, edge Edge, driver string) error {
	args := []string{}
	if edge.ID != "" {
		args = append(args, "--id", edge.ID)
	}
	if edge.Text != "" {
		args = append(args, "--text", edge.Text)
	}
	if edge.Value != "" {
		args = append(args, "--value", edge.Value)
	}
	if edge.X != "" && edge.Y != "" {
		args = append(args, "--x", edge.X, "--y", edge.Y)
	}
	if edge.ID == "" && edge.Text == "" && edge.Value == "" {
		driver = ""
	}
	// Suppress auto-observe so route playback retains exclusive
	// control over the post-tap tree probe (`executeRouteEdge` does
	// its own `waitForTreeReady`, and a premature observe here would
	// learn an edge to a transitional or off-route screen).
	tapOpts := GlobalOptions{PreferDriver: driver, SkipAutoObserve: true}
	var out bytes.Buffer
	if err := c.withStdout(&out).uiTap(ctx, tapOpts, cfg, args); err != nil {
		return err
	}
	if code, ok := outputFailureCode(out.String()); ok {
		return errors.New(code)
	}
	return nil
}

func outputFailureCode(output string) (string, bool) {
	fields := strings.Fields(output)
	if len(fields) == 0 || fields[0] != "fail" {
		return "", false
	}
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "code=") {
			return strings.TrimPrefix(field, "code="), true
		}
	}
	return "command_failed", true
}

func routeEdgeDrivers(edge Edge) []string {
	driver := edge.Driver
	if edge.ID == "" && edge.Text == "" && edge.Value == "" {
		return []string{""}
	}
	if driver == "" || driver == "auto" {
		return []string{"", "appium"}
	}
	drivers := []string{driver}
	switch driver {
	case "axe":
		drivers = append(drivers, "appium")
	case "appium":
		drivers = append(drivers, "axe")
	}
	return uniqueStrings(drivers)
}

func markRouteEdgeFailure(root, from string, edge Edge) {
	if from == "" {
		return
	}
	screen, err := LoadScreen(root, from)
	if err != nil {
		return
	}
	for i := range screen.Edges {
		if sameRouteEdge(screen.Edges[i], edge) {
			screen.Edges[i].FailureCount++
			screen.Edges[i].LastFailure = time.Now().Format(time.RFC3339)
			if screen.Edges[i].FailureCount >= 2 {
				screen.Edges[i].Confidence = "low"
			}
			_ = SaveScreen(root, screen)
			return
		}
	}
}

// routeFailureHint returns a one-line operator hint tailored to the
// `RouteFailure` reason. The goal is "the user reads the failure
// output and knows what to do next" — exactly the P3 deliverable
// described in the follow-up plan.
//
// The hints are intentionally concrete: nearest reachable screen,
// the dominant skip reason, the suggested `mav` command to run.
func routeFailureHint(rf *RouteFailure) string {
	if rf == nil {
		return ""
	}
	switch rf.Reason {
	case RouteFailureUnknownTarget:
		return "the map does not know screen `" + rf.Target + "` — visit it once with `mav ui tap` so the observer records it, then retry"
	case RouteFailureStartNotFound:
		return "configured start screen `" + rf.Start + "` is missing from the map — check `.mav/map/index.json` or rerun `mav setup`"
	case RouteFailureUnreachableSubgraph:
		dominant := dominantSkipReason(rf.SkippedEdges)
		switch dominant {
		case EdgeSkipLowConfidence:
			return "every known edge is marked `low` confidence (>=2 consecutive failures) — re-record the path or pass `--edge-retry 0` to surface the underlying failure"
		case EdgeSkipStale:
			return "the only known path is older than the edge TTL — raise `--edge-ttl` or re-seed the map by re-running the flow that originally captured it"
		}
		if rf.NearestKnownScreen != "" {
			return "the closest reachable screen is `" + rf.NearestKnownScreen + "` — add a transition from there to `" + rf.Target + "` via `mav ui tap` and the observer will record it"
		}
		return "the start screen has no outgoing edges — run the flow that seeds your map (or tap manually with `mav ui tap`) before retrying"
	}
	return ""
}

// dominantSkipReason returns the EdgeSkipReason that appears most
// often in a slice, or empty when the slice is empty / tied. Tied
// counts return empty so the hint falls back to the more generic
// "nearest reachable" advice instead of misleadingly picking one
// of two equally-bad explanations.
func dominantSkipReason(skips []EdgeSkip) EdgeSkipReason {
	if len(skips) == 0 {
		return ""
	}
	counts := map[EdgeSkipReason]int{}
	for _, s := range skips {
		counts[s.Why]++
	}
	var best EdgeSkipReason
	bestCount := 0
	tied := false
	for reason, count := range counts {
		switch {
		case count > bestCount:
			best = reason
			bestCount = count
			tied = false
		case count == bestCount:
			tied = true
		}
	}
	if tied {
		return ""
	}
	return best
}

// summariseEdgeSkips compacts a slice of `EdgeSkip` records into a
// small, sortable "low_confidence=3,stale=1" style string. Renders
// cleanly inside CLI failure output and inside the P3 evidence
// HTML; typed callers can still walk the original slice via
// `errors.As(*RouteFailure)` for richer detail.
func summariseEdgeSkips(skips []EdgeSkip) string {
	if len(skips) == 0 {
		return ""
	}
	counts := map[EdgeSkipReason]int{}
	for _, s := range skips {
		counts[s.Why]++
	}
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		reasons = append(reasons, string(reason))
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, reason+"="+strconv.Itoa(counts[EdgeSkipReason(reason)]))
	}
	return strings.Join(parts, ",")
}

// parseEdgeRetryFlag reads `--edge-retry` from a flag list. Returns
// the supplied non-negative integer, or `DefaultEdgeRetry` when the
// flag is absent. A negative value is rejected.
func parseEdgeRetryFlag(args []string) (int, error) {
	raw := strings.TrimSpace(flagValue(args, "--edge-retry"))
	if raw == "" {
		return DefaultEdgeRetry, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("edge_retry_invalid: %q", raw)
	}
	return n, nil
}

// parseEdgeTTLFlag reads `--edge-ttl` from a flag list, accepts the
// usual `time.ParseDuration` syntax plus a `Nd` (days) shorthand for
// ergonomics, and falls back to `DefaultEdgeTTL` when the flag is
// absent. A value of `0` disables the staleness gate (route engine
// treats all edges as fresh).
func parseEdgeTTLFlag(args []string) (time.Duration, error) {
	raw := strings.TrimSpace(flagValue(args, "--edge-ttl"))
	if raw == "" {
		return DefaultEdgeTTL, nil
	}
	if strings.HasSuffix(raw, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err != nil || days < 0 {
			return 0, fmt.Errorf("edge_ttl_invalid: %q", raw)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("edge_ttl_invalid: %q", raw)
	}
	return d, nil
}

// markRouteEdgeSuccess stamps `LastSuccessAt` on the matching edge
// and resets failure bookkeeping so a previously-flaky edge that's
// working again earns its way back to "high" confidence on the next
// observation. Called from `executeRouteEdge` on the happy path.
func markRouteEdgeSuccess(root, from string, edge Edge) {
	if from == "" {
		return
	}
	screen, err := LoadScreen(root, from)
	if err != nil {
		return
	}
	for i := range screen.Edges {
		if sameRouteEdge(screen.Edges[i], edge) {
			screen.Edges[i].LastSuccessAt = time.Now().UTC().Format(time.RFC3339)
			screen.Edges[i].FailureCount = 0
			screen.Edges[i].LastFailure = ""
			if screen.Edges[i].Confidence == "low" {
				screen.Edges[i].Confidence = "high"
			}
			_ = SaveScreen(root, screen)
			return
		}
	}
}

func sameRouteEdge(a, b Edge) bool {
	return a.To == b.To && a.ID == b.ID && a.Text == b.Text && a.Value == b.Value && a.X == b.X && a.Y == b.Y
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
		if err := c.withStdout(io.Discard).uiSwipe(ctx, GlobalOptions{PreferDriver: prefer}, mustLoadConfig(c.Root), []string{"--direction", direction}); err != nil {
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
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil {
		return false, fmt.Errorf("tree_failed")
	}
	result := described.Result
	if result.Err != nil {
		return false, fmt.Errorf("tree_failed")
	}
	raw := result.Stdout
	hostElements := ExtractElements(raw)
	if flowConditionMatchesElements(hostElements, condition) {
		return true, nil
	}
	// First miss only ruled out the host-app tree. Targets that live in
	// modal service overlays (PHPicker, Safari/Mail, privacy alerts,
	// springboard) are reachable only when we ask Appium for the
	// active-bundle tree, so retry with `includeSystem=true` regardless
	// of whether the caller fixed `prefer` to Appium.
	//
	// Skip the second probe when the first one is already known to
	// have answered the same question:
	//   - `described.SystemSource` means the first probe already swung
	//     to an active system bundle.
	//   - A rich host tree (per `IsRichHostTree`) keeps
	//     `appiumSourceTree(includeSystem=true)` on the host source via
	//     the early-exit there, so the retry would issue a second
	//     identical `mobile: source` for nothing. Skipping it halves
	//     Appium load on tight `whileNotVisible` polls against
	//     unfindable targets. Using the shared helper rather than
	//     comparing the threshold directly keeps both call sites in
	//     lockstep if the policy ever changes.
	if described.SystemSource {
		return false, nil
	}
	if described.Driver == "appium" && IsRichHostTree(len(hostElements)) {
		return false, nil
	}
	appium, appiumErr := c.describeUITree(ctx, cfg, "appium", true)
	if appiumErr == nil && appium.Result.Err == nil {
		return flowConditionMatchesElements(ExtractElements(appium.Result.Stdout), condition), nil
	}
	return false, nil
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
	if cfg.SimulatorUDID != "" {
		out = append(out, "--udid", cfg.SimulatorUDID)
	}
	return out
}

func idbTargetArgs(cfg Config, args ...string) []string {
	out := append([]string{}, args...)
	if cfg.SimulatorUDID != "" {
		out = append(out, "--udid", cfg.SimulatorUDID)
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

func buildSwipeActions(startX, startY, endX, endY string) ([]map[string]any, map[string]string, error) {
	sx, err := parseRequiredFloat(startX, "start_x")
	if err != nil {
		return nil, nil, err
	}
	sy, err := parseRequiredFloat(startY, "start_y")
	if err != nil {
		return nil, nil, err
	}
	ex, err := parseRequiredFloat(endX, "end_x")
	if err != nil {
		return nil, nil, err
	}
	ey, err := parseRequiredFloat(endY, "end_y")
	if err != nil {
		return nil, nil, err
	}
	duration := 500 * time.Millisecond
	durationMS := int(duration / time.Millisecond)
	fields := map[string]string{"duration": duration.String()}
	return []map[string]any{
		touchPointerActions("finger1", point{X: sx, Y: sy}, point{X: ex, Y: ey}, durationMS, 0),
	}, fields, nil
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
