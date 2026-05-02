package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	JSON    bool
	Verbose bool
	Raw     bool
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
		return c.help(opts)
	}
	switch rest[0] {
	case "doctor":
		return c.doctor(ctx, opts)
	case "setup":
		return c.setup(ctx, opts, rest[1:])
	case "discover":
		return c.discover(opts)
	case "sim":
		return c.sim(ctx, opts, rest[1:])
	case "open":
		return c.open(ctx, opts, rest[1:])
	case "ui":
		return c.ui(ctx, opts, rest[1:])
	case "capture":
		return c.capture(ctx, opts, rest[1:])
	case "preview":
		return c.preview(ctx, opts, rest[1:])
	case "go":
		return c.goScreen(ctx, opts, rest[1:])
	case "logs":
		return c.logs(opts, rest[1:])
	case "crashes":
		return c.crashes(ctx, opts, rest[1:])
	case "evidence":
		return c.evidence(opts, rest[1:])
	default:
		return Fail("unknown_command", map[string]string{"command": rest[0]}).Write(c.Stdout, opts.JSON)
	}
}

func parseGlobal(args []string) (GlobalOptions, []string) {
	opts := GlobalOptions{}
	rest := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--json":
			opts.JSON = true
		case "--verbose":
			opts.Verbose = true
		case "--raw":
			opts.Raw = true
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest
}

func (c CLI) help(opts GlobalOptions) error {
	_, err := fmt.Fprintln(c.Stdout, "mav commands: doctor setup discover sim open ui capture preview go logs crashes evidence")
	return err
}

func (c CLI) doctor(ctx context.Context, opts GlobalOptions) error {
	tools := []string{"go", "bazelisk", "xcrun", "axe", "idb", "maestro"}
	fields := map[string]string{}
	missing := []string{}
	for _, tool := range tools {
		_, err := c.Runner.LookPath(tool)
		if err == nil {
			fields[tool] = "ok"
		} else {
			fields[tool] = "missing"
			if tool != "go" && tool != "xcrun" {
				missing = append(missing, tool)
			}
		}
	}
	if len(missing) > 0 {
		fields["next"] = "mav setup --install " + strings.Join(missing, " ")
	}
	return OK("doctor", fields).Write(c.Stdout, opts.JSON)
}

func (c CLI) setup(ctx context.Context, opts GlobalOptions, args []string) error {
	install := flagValue(args, "--install")
	if install == "" {
		return Fail("setup_install_missing", map[string]string{"usage": "mav setup --install axe maestro idb"}).Write(c.Stdout, opts.JSON)
	}
	tools := strings.Fields(install)
	if len(tools) == 0 {
		return Fail("setup_install_missing", nil).Write(c.Stdout, opts.JSON)
	}
	commands := map[string][]string{
		"axe":     {"brew", "install", "cameroncooke/axe/axe"},
		"maestro": {"brew", "install", "maestro"},
		"idb":     {"brew", "install", "idb-companion"},
	}
	for _, tool := range tools {
		cmd, ok := commands[tool]
		if !ok {
			return Fail("setup_unknown_tool", map[string]string{"tool": tool}).Write(c.Stdout, opts.JSON)
		}
		if opts.Verbose {
			fmt.Fprintln(c.Stderr, strings.Join(cmd, " "))
		}
		result := c.Runner.Run(ctx, cmd[0], cmd[1:]...)
		if result.Err != nil {
			return Fail("setup_failed", map[string]string{"tool": tool, "stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
		}
	}
	return OK("setup", map[string]string{"installed": strings.Join(tools, ",")}).Write(c.Stdout, opts.JSON)
}

func (c CLI) discover(opts GlobalOptions) error {
	cfg, err := DiscoverConfig(c.Root, c.Runner)
	if saveErr := SaveConfig(c.Root, cfg); saveErr != nil {
		return saveErr
	}
	if !exists(filepath.Join(c.Root, AppMapFile)) {
		_ = SaveAppMap(c.Root, DefaultAppMap(cfg.BundleID))
	}
	fields := map[string]string{
		"config": filepath.Join(c.Root, ConfigFile),
		"target": cfg.AppTarget,
		"bundle": cfg.BundleID,
	}
	if err != nil {
		fields["warning"] = err.Error()
	}
	return OK("discover", fields).Write(c.Stdout, opts.JSON)
}

func (c CLI) sim(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("sim_command_missing", map[string]string{"usage": "mav sim list|select|boot"}).Write(c.Stdout, opts.JSON)
	}
	switch args[0] {
	case "list":
		sims, err := ListSimulators(c.Runner)
		if err != nil {
			return Fail("sim_list_failed", map[string]string{"error": err.Error()}).Write(c.Stdout, opts.JSON)
		}
		if opts.JSON {
			return json.NewEncoder(c.Stdout).Encode(map[string]any{"ok": true, "cmd": "sim.list", "simulators": sims})
		}
		for _, sim := range sims {
			fmt.Fprintf(c.Stdout, "sim udid=%s name=%q runtime=%s state=%s\n", sim.UDID, sim.Name, sim.Runtime, sim.State)
		}
		return nil
	case "select":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
		}
		sims, err := ListSimulators(c.Runner)
		if err != nil {
			return Fail("sim_list_failed", map[string]string{"error": err.Error()}).Write(c.Stdout, opts.JSON)
		}
		sim, ok := SelectSimulator(sims, flagValue(args[1:], "--device"), flagValue(args[1:], "--ios"), flagValue(args[1:], "--udid"))
		if !ok {
			return Fail("sim_not_found", map[string]string{"device": flagValue(args[1:], "--device"), "ios": flagValue(args[1:], "--ios"), "udid": flagValue(args[1:], "--udid")}).Write(c.Stdout, opts.JSON)
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
		return OK("sim.select", map[string]string{"udid": sim.UDID, "name": sim.Name, "runtime": sim.Runtime}).Write(c.Stdout, opts.JSON)
	case "boot":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
		}
		if cfg.SimulatorUDID == "" {
			return Fail("sim_not_selected", map[string]string{"next": "mav sim select --device 'iPhone' --ios 26"}).Write(c.Stdout, opts.JSON)
		}
		boot := c.Runner.Run(ctx, "xcrun", "simctl", "boot", cfg.SimulatorUDID)
		if boot.Err != nil && !strings.Contains(boot.Stderr, "Unable to boot device in current state") {
			return Fail("sim_boot_failed", map[string]string{"stderr": firstLine(boot.Stderr)}).Write(c.Stdout, opts.JSON)
		}
		status := c.Runner.Run(ctx, "xcrun", "simctl", "bootstatus", cfg.SimulatorUDID, "-b")
		if status.Err != nil {
			return Fail("sim_bootstatus_failed", map[string]string{"stderr": firstLine(status.Stderr)}).Write(c.Stdout, opts.JSON)
		}
		return OK("sim.boot", map[string]string{"udid": cfg.SimulatorUDID, "name": cfg.SimulatorName}).Write(c.Stdout, opts.JSON)
	default:
		return Fail("sim_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout, opts.JSON)
	}
}

func (c CLI) open(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
	}
	if err := c.applyOpenTargetOverrides(ctx, &cfg, args); err != nil {
		return Fail("sim_select_failed", map[string]string{"error": err.Error()}).Write(c.Stdout, opts.JSON)
	}
	run, err := NewRunState()
	if err != nil {
		return err
	}
	if err := SaveCurrentRun(c.Root, run); err != nil {
		return err
	}
	logPID, logErr := c.startLogs(ctx, cfg, run)
	if logErr != nil {
		appendFile(run.LogsPath, "mav log capture failed: "+logErr.Error()+"\n")
	}
	if cfg.AppTarget != "" && hasTool(cfg, "bazelisk") {
		build := c.Runner.Run(ctx, "bazelisk", "build", cfg.AppTarget)
		appendCommand(run, "bazelisk build "+cfg.AppTarget, build)
		if build.Err != nil {
			return Fail("build_failed", map[string]string{"run": run.ID, "logs": run.LogsPath, "stderr": firstLine(build.Stderr)}).Write(c.Stdout, opts.JSON)
		}
	}
	appPath := ""
	if cfg.AppTarget != "" && hasTool(cfg, "bazelisk") {
		cquery := c.Runner.Run(ctx, "bazelisk", "cquery", "--output=files", cfg.AppTarget)
		appendCommand(run, "bazelisk cquery --output=files "+cfg.AppTarget, cquery)
		if cquery.Err == nil {
			appPath = firstNonEmptyLine(cquery.Stdout)
		}
	}
	if appPath != "" && cfg.BundleID != "" && hasTool(cfg, "xcrun") {
		target := cfg.SimulatorUDID
		if target == "" {
			target = "booted"
		}
		install := c.Runner.Run(ctx, "xcrun", "simctl", "install", target, appPath)
		appendCommand(run, "xcrun simctl install "+target+" "+appPath, install)
		if install.Err != nil {
			return Fail("install_failed", map[string]string{"run": run.ID, "logs": run.LogsPath, "stderr": firstLine(install.Stderr)}).Write(c.Stdout, opts.JSON)
		}
		launchArgs := []string{"simctl", "launch", target, cfg.BundleID}
		launchArgs = append(launchArgs, simctlLaunchLanguageArgs(cfg)...)
		launch := c.Runner.Run(ctx, "xcrun", launchArgs...)
		appendCommand(run, "xcrun "+strings.Join(launchArgs, " "), launch)
		if launch.Err != nil {
			return Fail("launch_failed", map[string]string{"run": run.ID, "logs": run.LogsPath, "stderr": firstLine(launch.Stderr)}).Write(c.Stdout, opts.JSON)
		}
	}
	fields := map[string]string{"run": run.ID, "logs": run.LogsPath, "dir": run.Dir}
	if appPath != "" {
		fields["app"] = appPath
	}
	if logPID > 0 {
		fields["log_pid"] = strconv.Itoa(logPID)
	}
	fields["target"] = cfg.SimulatorName
	if fields["target"] == "" {
		fields["target"] = cfg.SimulatorUDID
	}
	if fields["target"] == "" {
		fields["target"] = "booted"
	}
	return OK("open", fields).Write(c.Stdout, opts.JSON)
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

func (c CLI) startLogs(ctx context.Context, cfg Config, run RunState) (int, error) {
	if cfg.LogStrategy == "idb-log" && hasTool(cfg, "idb") {
		args := []string{}
		if cfg.SimulatorUDID != "" {
			args = append(args, "--udid", cfg.SimulatorUDID)
		}
		args = append(args, "log", "--level", "debug")
		if cfg.ProcessName != "" {
			args = append(args, "--predicate", `process == "`+cfg.ProcessName+`"`)
		}
		return c.Runner.Start(ctx, run.LogsPath, "idb", args...)
	}
	if !hasTool(cfg, "xcrun") {
		return 0, fmt.Errorf("xcrun_missing")
	}
	target := cfg.SimulatorUDID
	if target == "" {
		target = "booted"
	}
	args := []string{"simctl", "spawn", target, "log", "stream", "--level", "debug", "--style", "compact"}
	if cfg.ProcessName != "" {
		args = append(args, "--predicate", `process == "`+cfg.ProcessName+`"`)
	}
	return c.Runner.Start(ctx, run.LogsPath, "xcrun", args...)
}

func (c CLI) ui(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("ui_command_missing", map[string]string{"usage": "mav ui tree|tap|type|swipe|wait"}).Write(c.Stdout, opts.JSON)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
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
	case "wait":
		return c.uiWait(ctx, opts, cfg, args[1:])
	default:
		return Fail("ui_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout, opts.JSON)
	}
}

func (c CLI) uiTree(ctx context.Context, opts GlobalOptions, cfg Config) error {
	var result CommandResult
	driver := ""
	if hasTool(cfg, "axe") {
		driver = "axe"
		result = c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "describe-ui")...)
	} else if hasTool(cfg, "idb") {
		driver = "idb"
		result = c.Runner.Run(ctx, "idb", idbTargetArgs(cfg, "ui", "describe-all", "--json", "--nested")...)
	} else {
		return Fail("tool_missing", map[string]string{"tool": "axe|idb", "next": "mav setup --install axe idb"}).Write(c.Stdout, opts.JSON)
	}
	if opts.Raw {
		fmt.Fprint(c.Stdout, result.Stdout)
		return nil
	}
	if result.Err != nil {
		return Fail("ui_tree_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	nodes := countTreeNodes(result.Stdout)
	if nodes == 0 {
		nodes = strings.Count(result.Stdout, "\n")
	}
	return OK("ui.tree", map[string]string{"driver": driver, "nodes": strconv.Itoa(nodes), "screen": "unknown"}).Write(c.Stdout, opts.JSON)
}

func (c CLI) uiTap(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	id := flagValue(args, "--id")
	text := flagValue(args, "--text")
	x := flagValue(args, "--x")
	y := flagValue(args, "--y")
	if x != "" && y != "" {
		if !hasTool(cfg, "idb") {
			return Fail("tool_missing", map[string]string{"tool": "idb"}).Write(c.Stdout, opts.JSON)
		}
		result := c.Runner.Run(ctx, "idb", idbTargetArgs(cfg, "ui", "tap", x, y)...)
		if result.Err != nil {
			return Fail("ui_tap_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
		}
		c.appendCurrentCommand("mav ui tap --x "+x+" --y "+y, result)
		return OK("ui.tap", map[string]string{"x": x, "y": y}).Write(c.Stdout, opts.JSON)
	}
	if !hasTool(cfg, "axe") {
		return Fail("tool_missing", map[string]string{"tool": "axe", "next": "use mav ui tap --x X --y Y when AXe is unavailable"}).Write(c.Stdout, opts.JSON)
	}
	axeArgs := axeTargetArgs(cfg, "tap")
	if id != "" {
		axeArgs = append(axeArgs, "--id", id)
	} else if text != "" {
		axeArgs = append(axeArgs, "--label", text)
	} else {
		return Fail("tap_target_missing", map[string]string{"usage": "mav ui tap --id ID | --text TEXT | --x X --y Y"}).Write(c.Stdout, opts.JSON)
	}
	result := c.Runner.Run(ctx, "axe", axeArgs...)
	if result.Err != nil {
		return Fail("ui_tap_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	command := "mav ui tap"
	fields := map[string]string{}
	if id != "" {
		fields["id"] = id
		command += " --id " + id
	}
	if text != "" {
		fields["text"] = text
		command += " --text " + text
	}
	c.appendCurrentCommand(command, result)
	return OK("ui.tap", fields).Write(c.Stdout, opts.JSON)
}

func (c CLI) appendCurrentCommand(command string, result CommandResult) {
	run, err := LoadRun(c.Root, "")
	if err == nil && run.ID != "" {
		appendCommand(run, command, result)
	}
}

func (c CLI) uiType(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	if len(args) == 0 {
		return Fail("type_text_missing", nil).Write(c.Stdout, opts.JSON)
	}
	axeArgs := axeTargetArgs(cfg, "type")
	axeArgs = append(axeArgs, strings.Join(args, " "))
	result := c.Runner.Run(ctx, "axe", axeArgs...)
	if result.Err != nil {
		return Fail("ui_type_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	return OK("ui.type", map[string]string{"chars": strconv.Itoa(len(strings.Join(args, " ")))}).Write(c.Stdout, opts.JSON)
}

func (c CLI) uiSwipe(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	direction := flagValue(args, "--direction")
	if direction == "" && len(args) > 0 {
		direction = args[0]
	}
	if direction == "" {
		direction = "up"
	}
	axeArgs := axeTargetArgs(cfg, "swipe", direction)
	result := c.Runner.Run(ctx, "axe", axeArgs...)
	if result.Err != nil {
		return Fail("ui_swipe_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	return OK("ui.swipe", map[string]string{"direction": direction}).Write(c.Stdout, opts.JSON)
}

func (c CLI) uiWait(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	id := flagValue(args, "--id")
	if id == "" {
		return Fail("wait_target_missing", map[string]string{"usage": "mav ui wait --id ID"}).Write(c.Stdout, opts.JSON)
	}
	timeout := 5 * time.Second
	if raw := flagValue(args, "--timeout"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		result := c.Runner.Run(ctx, "axe", axeTargetArgs(cfg, "describe-ui")...)
		if result.Err == nil && strings.Contains(result.Stdout, id) {
			return OK("ui.wait", map[string]string{"id": id}).Write(c.Stdout, opts.JSON)
		}
		if time.Now().After(deadline) {
			return Fail("ui_wait_timeout", map[string]string{"id": id}).Write(c.Stdout, opts.JSON)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (c CLI) capture(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
	}
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		run, err = NewRunState()
		if err != nil {
			return err
		}
		_ = SaveCurrentRun(c.Root, run)
	}
	path := filepath.Join(run.Dir, "screen.png")
	result, err := c.captureScreenshot(ctx, cfg, path)
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "axe|xcrun"}).Write(c.Stdout, opts.JSON)
	}
	if result.Err != nil {
		return Fail("capture_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	return OK("capture", map[string]string{"file": path, "run": run.ID}).Write(c.Stdout, opts.JSON)
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

func (c CLI) preview(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) > 0 && args[0] == "init" {
		return c.previewInit(opts, args[1:])
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
	}
	if cfg.PreviewTarget == "" || cfg.PreviewBundleID == "" {
		return Fail("preview_not_configured", map[string]string{"next": "set preview_target and preview_bundle_id in .mav/config.yaml"}).Write(c.Stdout, opts.JSON)
	}
	run, err := NewRunState()
	if err != nil {
		return err
	}
	if err := SaveCurrentRun(c.Root, run); err != nil {
		return err
	}
	logPID, logErr := c.startLogs(ctx, cfg, run)
	if logErr != nil {
		appendFile(run.LogsPath, "mav log capture failed: "+logErr.Error()+"\n")
	}
	build := c.Runner.Run(ctx, "bazelisk", "build", cfg.PreviewTarget)
	appendCommand(run, "bazelisk build "+cfg.PreviewTarget, build)
	if build.Err != nil {
		return Fail("preview_build_failed", map[string]string{"run": run.ID, "logs": run.LogsPath, "stderr": firstLine(build.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	cquery := c.Runner.Run(ctx, "bazelisk", "cquery", "--output=files", cfg.PreviewTarget)
	appendCommand(run, "bazelisk cquery --output=files "+cfg.PreviewTarget, cquery)
	appPath := firstNonEmptyLine(cquery.Stdout)
	if cquery.Err != nil || appPath == "" {
		return Fail("preview_app_not_found", map[string]string{"run": run.ID, "logs": run.LogsPath}).Write(c.Stdout, opts.JSON)
	}
	target := cfg.SimulatorUDID
	if target == "" {
		target = "booted"
	}
	install := c.Runner.Run(ctx, "xcrun", "simctl", "install", target, appPath)
	appendCommand(run, "xcrun simctl install "+target+" "+appPath, install)
	if install.Err != nil {
		return Fail("preview_install_failed", map[string]string{"run": run.ID, "logs": run.LogsPath, "stderr": firstLine(install.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	launchArgs := []string{"simctl", "launch", target, cfg.PreviewBundleID}
	if len(args) > 0 {
		launchArgs = append(launchArgs, "--args", "--mav-preview", args[0])
	}
	launch := c.Runner.Run(ctx, "xcrun", launchArgs...)
	appendCommand(run, "xcrun "+strings.Join(launchArgs, " "), launch)
	if launch.Err != nil {
		return Fail("preview_launch_failed", map[string]string{"run": run.ID, "logs": run.LogsPath, "stderr": firstLine(launch.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	fields := map[string]string{"run": run.ID, "logs": run.LogsPath, "app": appPath}
	if logPID > 0 {
		fields["log_pid"] = strconv.Itoa(logPID)
	}
	if len(args) > 0 {
		fields["view"] = args[0]
	}
	return OK("preview", fields).Write(c.Stdout, opts.JSON)
}

func (c CLI) previewInit(opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		cfg = DefaultConfig(c.Root)
	}
	dir := flagValue(args, "--dir")
	if dir == "" {
		dir = "MAVPreview"
	}
	bundleID := flagValue(args, "--bundle-id")
	if bundleID == "" {
		if cfg.BundleID != "" {
			bundleID = cfg.BundleID + ".mavpreview"
		} else {
			bundleID = "dev.mav.preview"
		}
	}
	target := "//" + filepath.ToSlash(dir) + ":MAVPreviewApp"
	buildPath := filepath.Join(c.Root, dir, "BUILD.bazel")
	swiftPath := filepath.Join(c.Root, dir, "PreviewHostApp.swift")
	plistPath := filepath.Join(c.Root, dir, "Info.plist")
	if !hasFlag(args, "--force") && (exists(buildPath) || exists(swiftPath) || exists(plistPath)) {
		return Fail("preview_host_exists", map[string]string{"dir": dir, "next": "rerun with --force"}).Write(c.Stdout, opts.JSON)
	}
	if err := os.MkdirAll(filepath.Join(c.Root, dir), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(buildPath, []byte(previewBuildTemplate(bundleID)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(swiftPath, []byte(previewSwiftTemplate()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(previewInfoPlistTemplate()), 0o644); err != nil {
		return err
	}
	cfg.PreviewTarget = target
	cfg.PreviewBundleID = bundleID
	if err := SaveConfig(c.Root, cfg); err != nil {
		return err
	}
	return OK("preview.init", map[string]string{"target": target, "bundle": bundleID, "dir": filepath.Join(c.Root, dir)}).Write(c.Stdout, opts.JSON)
}

func (c CLI) goScreen(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("screen_missing", map[string]string{"usage": "mav go SCREEN_ID"}).Write(c.Stdout, opts.JSON)
	}
	screenID := args[0]
	m, err := LoadAppMap(c.Root)
	if err != nil {
		return Fail("app_map_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
	}
	if err := ValidateAppMap(m); err != nil {
		return Fail("app_map_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout, opts.JSON)
	}
	route, err := Route(m, screenID)
	if err != nil {
		code := err.Error()
		next := "use mav ui tree and update .mav/app-map.yaml"
		return Fail(code, map[string]string{"screen": screenID, "next": next}).Write(c.Stdout, opts.JSON)
	}
	cfg, cfgErr := LoadConfig(c.Root)
	if cfgErr == nil && cfg.BundleID != "" && m.AppID == "" {
		m.AppID = cfg.BundleID
	}
	if cfgErr == nil && !hasTool(cfg, "maestro") {
		return Fail("tool_missing", map[string]string{"tool": "maestro", "next": "mav setup --install maestro"}).Write(c.Stdout, opts.JSON)
	}
	run, _ := LoadRun(c.Root, "")
	if run.ID == "" {
		run, _ = NewRunState()
		_ = SaveCurrentRun(c.Root, run)
	}
	evidence := hasFlag(args[1:], "--evidence") || hasFlag(args[1:], "--record")
	flow := MaestroFlow(m, route, screenID, MaestroFlowOptions{CaptureSteps: evidence})
	tmp := filepath.Join(os.TempDir(), "mav-"+run.ID+"-"+screenID+".yaml")
	if err := os.WriteFile(tmp, []byte(flow), 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)
	maestroArgs := []string{"test", tmp}
	if evidence {
		maestroDir := filepath.Join(run.Dir, "maestro")
		_ = os.MkdirAll(maestroDir, 0o755)
		maestroArgs = append([]string{"test", "--debug-output", maestroDir, "--flatten-debug-output"}, tmp)
	}
	record := hasFlag(args[1:], "--record") || flagValue(args[1:], "--video-seconds") != ""
	videoPID := 0
	videoPath := filepath.Join(run.Dir, "video.mov")
	if record && cfgErr == nil {
		if path, pid, err := c.startVideoRecording(ctx, cfg, run); err == nil {
			videoPath = path
			videoPID = pid
			time.Sleep(750 * time.Millisecond)
		}
	}
	result := c.Runner.Run(ctx, "maestro", maestroArgs...)
	if videoPID > 0 {
		_ = stopProcess(videoPID)
		_ = waitForFile(videoPath, 6*time.Second)
	}
	if evidence {
		collectMaestroScreenshots(c.Root, filepath.Join(run.Dir, "maestro"))
	}
	appendCommand(run, "maestro "+strings.Join(maestroArgs, " "), result)
	if result.Err != nil {
		return Fail("go_failed", map[string]string{"screen": screenID, "logs": run.LogsPath, "error": lastNonEmptyLine(result.Stderr + "\n" + result.Stdout)}).Write(c.Stdout, opts.JSON)
	}
	fields := map[string]string{"screen": screenID, "steps": strconv.Itoa(len(route))}
	if evidence {
		fields["evidence"] = filepath.Join(run.Dir, "maestro")
	}
	if record {
		fields["video"] = videoPath
	}
	return OK("go", fields).Write(c.Stdout, opts.JSON)
}

func (c CLI) logs(opts GlobalOptions, args []string) error {
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout, opts.JSON)
	}
	data, err := os.ReadFile(run.LogsPath)
	if err != nil {
		return Fail("logs_not_found", map[string]string{"run": run.ID, "file": run.LogsPath}).Write(c.Stdout, opts.JSON)
	}
	contains := flagValue(args, "--contains")
	level := flagValue(args, "--level")
	lines := filterLines(strings.Split(string(data), "\n"), contains, level)
	if opts.JSON {
		payload := map[string]any{"ok": true, "cmd": "logs", "run": run.ID, "file": run.LogsPath, "lines": lines, "matches": len(lines)}
		return json.NewEncoder(c.Stdout).Encode(payload)
	}
	if opts.Raw {
		for _, line := range lines {
			fmt.Fprintln(c.Stdout, line)
		}
		return nil
	}
	return OK("logs", map[string]string{"run": run.ID, "file": run.LogsPath, "matches": strconv.Itoa(len(lines))}).Write(c.Stdout, false)
}

func (c CLI) crashes(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
	}
	if !hasTool(cfg, "idb") {
		return Fail("tool_missing", map[string]string{"tool": "idb"}).Write(c.Stdout, opts.JSON)
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
		return Fail("crashes_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	count := 0
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return OK("crashes", map[string]string{"count": strconv.Itoa(count)}).Write(c.Stdout, opts.JSON)
}

func (c CLI) evidence(opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("evidence_command_missing", map[string]string{"usage": "mav evidence start|step|stop|report|video"}).Write(c.Stdout, opts.JSON)
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
	case "video":
		return c.evidenceVideo(opts, args[1:])
	default:
		return Fail("evidence_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout, opts.JSON)
	}
}

func (c CLI) evidenceReport(opts GlobalOptions, args []string) error {
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout, opts.JSON)
	}
	path, err := GenerateReport(run)
	if err != nil {
		return Fail("report_failed", map[string]string{"error": err.Error()}).Write(c.Stdout, opts.JSON)
	}
	return OK("evidence.report", map[string]string{"run": run.ID, "file": path}).Write(c.Stdout, opts.JSON)
}

func (c CLI) evidenceStart(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
	}
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout, opts.JSON)
	}
	path, pid, err := c.startVideoRecording(ctx, cfg, run)
	if err != nil {
		return Fail("evidence_start_failed", map[string]string{"run": run.ID, "error": err.Error()}).Write(c.Stdout, opts.JSON)
	}
	if err := os.WriteFile(filepath.Join(run.Dir, "video.pid"), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return err
	}
	appendCommand(run, "mav evidence start", CommandResult{})
	return OK("evidence.start", map[string]string{"run": run.ID, "file": path, "pid": strconv.Itoa(pid)}).Write(c.Stdout, opts.JSON)
}

func (c CLI) evidenceStep(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
	}
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout, opts.JSON)
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
		return Fail("tool_missing", map[string]string{"tool": "axe|idb|xcrun"}).Write(c.Stdout, opts.JSON)
	}
	if result.Err != nil {
		return Fail("evidence_step_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	step := EvidenceStep{Name: name, Note: flagValue(args, "--note"), File: file, Kind: "screenshot"}
	if err := AppendEvidenceStep(run, step); err != nil {
		return err
	}
	appendCommand(run, "mav evidence step --name "+name, result)
	return OK("evidence.step", map[string]string{"run": run.ID, "name": name, "file": file}).Write(c.Stdout, opts.JSON)
}

func (c CLI) evidenceStop(ctx context.Context, opts GlobalOptions, args []string) error {
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout, opts.JSON)
	}
	pid, err := readPID(filepath.Join(run.Dir, "video.pid"))
	if err != nil {
		return Fail("video_not_running", map[string]string{"run": run.ID}).Write(c.Stdout, opts.JSON)
	}
	_ = stopProcess(pid)
	_ = os.Remove(filepath.Join(run.Dir, "video.pid"))
	fields := map[string]string{"run": run.ID, "file": filepath.Join(run.Dir, "video.mov")}
	if !waitForFile(fields["file"], 6*time.Second) {
		return Fail("video_not_written", map[string]string{"run": run.ID, "file": fields["file"], "log": filepath.Join(run.Dir, "video.log")}).Write(c.Stdout, opts.JSON)
	}
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
	return OK("evidence.stop", fields).Write(c.Stdout, opts.JSON)
}

func (c CLI) evidenceVideo(opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("video_command_missing", map[string]string{"usage": "mav evidence video start|stop"}).Write(c.Stdout, opts.JSON)
	}
	run, err := LoadRun(c.Root, flagValue(args[1:], "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout, opts.JSON)
	}
	switch args[0] {
	case "record":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
		}
		seconds := flagValue(args[1:], "--seconds")
		if seconds == "" {
			seconds = "3"
		}
		videoPath := filepath.Join(run.Dir, "video.mov")
		target := cfg.SimulatorUDID
		if target == "" {
			target = "booted"
		}
		cmd := []string{"-s", "INT", seconds, "xcrun", "simctl", "io", target, "recordVideo", "--codec=h264", videoPath}
		result := c.Runner.Run(context.Background(), "timeout", cmd...)
		appendCommand(run, "timeout "+strings.Join(cmd, " "), result)
		if result.Err != nil && !exists(videoPath) {
			return Fail("video_record_failed", map[string]string{"run": run.ID, "logs": run.LogsPath, "stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
		}
		return OK("evidence.video.record", map[string]string{"run": run.ID, "file": videoPath, "seconds": seconds}).Write(c.Stdout, opts.JSON)
	case "start":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
		}
		videoPath, pid, err := c.startVideoRecording(context.Background(), cfg, run)
		if err != nil {
			return Fail("video_start_failed", map[string]string{"error": err.Error()}).Write(c.Stdout, opts.JSON)
		}
		_ = os.WriteFile(filepath.Join(run.Dir, "video.pid"), []byte(strconv.Itoa(pid)+"\n"), 0o644)
		return OK("evidence.video.start", map[string]string{"run": run.ID, "file": videoPath, "pid": strconv.Itoa(pid)}).Write(c.Stdout, opts.JSON)
	case "stop":
		pid, err := readPID(filepath.Join(run.Dir, "video.pid"))
		if err != nil {
			return Fail("video_not_running", map[string]string{"run": run.ID}).Write(c.Stdout, opts.JSON)
		}
		_ = stopProcess(pid)
		_ = os.Remove(filepath.Join(run.Dir, "video.pid"))
		videoPath := filepath.Join(run.Dir, "video.mov")
		if !waitForFile(videoPath, 6*time.Second) {
			return Fail("video_not_written", map[string]string{"run": run.ID, "file": videoPath, "log": filepath.Join(run.Dir, "video.log")}).Write(c.Stdout, opts.JSON)
		}
		return OK("evidence.video.stop", map[string]string{"run": run.ID, "file": videoPath}).Write(c.Stdout, opts.JSON)
	default:
		return Fail("video_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout, opts.JSON)
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
	return videoPath, pid, err
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

func collectMaestroScreenshots(root, targetDir string) {
	_ = os.MkdirAll(targetDir, 0o755)
	for _, pattern := range []string{"mav_step_*.png", "mav_final_*.png"} {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, src := range matches {
			dst := filepath.Join(targetDir, filepath.Base(src))
			_ = os.Rename(src, dst)
		}
	}
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
	if result.Stdout != "" {
		appendFile(run.LogsPath, result.Stdout)
	}
	if result.Stderr != "" {
		appendFile(run.LogsPath, result.Stderr)
	}
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
