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
	case "open":
		return c.open(ctx, opts, rest[1:])
	case "ui":
		return c.ui(ctx, opts, rest[1:])
	case "capture":
		return c.capture(ctx, opts, rest[1:])
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
	_, err := fmt.Fprintln(c.Stdout, "mav commands: doctor setup discover open ui capture go logs crashes evidence")
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

func (c CLI) open(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav discover"}).Write(c.Stdout, opts.JSON)
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
		launch := c.Runner.Run(ctx, "xcrun", "simctl", "launch", target, cfg.BundleID)
		appendCommand(run, "xcrun simctl launch "+target+" "+cfg.BundleID, launch)
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
	if !hasTool(cfg, "axe") {
		return Fail("tool_missing", map[string]string{"tool": "axe", "next": "mav setup --install axe"}).Write(c.Stdout, opts.JSON)
	}
	args := axeTargetArgs(cfg, "describe-ui")
	result := c.Runner.Run(ctx, "axe", args...)
	if opts.Raw {
		fmt.Fprint(c.Stdout, result.Stdout)
		return nil
	}
	if result.Err != nil {
		return Fail("ui_tree_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	nodes := strings.Count(result.Stdout, "\n")
	return OK("ui.tree", map[string]string{"nodes": strconv.Itoa(nodes), "screen": "unknown"}).Write(c.Stdout, opts.JSON)
}

func (c CLI) uiTap(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	if !hasTool(cfg, "axe") {
		return Fail("tool_missing", map[string]string{"tool": "axe"}).Write(c.Stdout, opts.JSON)
	}
	id := flagValue(args, "--id")
	text := flagValue(args, "--text")
	axeArgs := axeTargetArgs(cfg, "tap")
	if id != "" {
		axeArgs = append(axeArgs, "--id", id)
	} else if text != "" {
		axeArgs = append(axeArgs, "--label", text)
	} else {
		return Fail("tap_target_missing", map[string]string{"usage": "mav ui tap --id ID | --text TEXT"}).Write(c.Stdout, opts.JSON)
	}
	result := c.Runner.Run(ctx, "axe", axeArgs...)
	if result.Err != nil {
		return Fail("ui_tap_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	fields := map[string]string{}
	if id != "" {
		fields["id"] = id
	}
	if text != "" {
		fields["text"] = text
	}
	return OK("ui.tap", fields).Write(c.Stdout, opts.JSON)
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
	var result CommandResult
	if hasTool(cfg, "axe") {
		axeArgs := axeTargetArgs(cfg, "screenshot", "--output", path)
		result = c.Runner.Run(ctx, "axe", axeArgs...)
	} else if hasTool(cfg, "xcrun") {
		target := cfg.SimulatorUDID
		if target == "" {
			target = "booted"
		}
		result = c.Runner.Run(ctx, "xcrun", "simctl", "io", target, "screenshot", path)
	} else {
		return Fail("tool_missing", map[string]string{"tool": "axe|xcrun"}).Write(c.Stdout, opts.JSON)
	}
	if result.Err != nil {
		return Fail("capture_failed", map[string]string{"stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	return OK("capture", map[string]string{"file": path, "run": run.ID}).Write(c.Stdout, opts.JSON)
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
	flow := MaestroFlow(m, route, screenID)
	tmp := filepath.Join(os.TempDir(), "mav-"+run.ID+"-"+screenID+".yaml")
	if err := os.WriteFile(tmp, []byte(flow), 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)
	result := c.Runner.Run(ctx, "maestro", "test", tmp)
	if result.Err != nil {
		return Fail("go_failed", map[string]string{"screen": screenID, "stderr": firstLine(result.Stderr)}).Write(c.Stdout, opts.JSON)
	}
	return OK("go", map[string]string{"screen": screenID, "steps": strconv.Itoa(len(route))}).Write(c.Stdout, opts.JSON)
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
	idbArgs := []string{}
	if cfg.SimulatorUDID != "" {
		idbArgs = append(idbArgs, "--udid", cfg.SimulatorUDID)
	}
	idbArgs = append(idbArgs, "crash", "list")
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
	if len(args) == 0 || args[0] != "report" {
		return Fail("evidence_command_missing", map[string]string{"usage": "mav evidence report [--run RUN]"}).Write(c.Stdout, opts.JSON)
	}
	run, err := LoadRun(c.Root, flagValue(args[1:], "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout, opts.JSON)
	}
	path, err := GenerateReport(run)
	if err != nil {
		return Fail("report_failed", map[string]string{"error": err.Error()}).Write(c.Stdout, opts.JSON)
	}
	return OK("evidence.report", map[string]string{"run": run.ID, "file": path}).Write(c.Stdout, opts.JSON)
}

func axeTargetArgs(cfg Config, args ...string) []string {
	out := append([]string{}, args...)
	if cfg.SimulatorUDID != "" {
		out = append(out, "--udid", cfg.SimulatorUDID)
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
