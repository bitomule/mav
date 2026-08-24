package mav

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	run    *RunState // nil = fall back to reading .mav/current-run from disk
}

type GlobalOptions struct {
	Verbose      bool
	Raw          bool
	Help         bool
	PreferDriver string
	Profile      string
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
	// --profile se propaga por el entorno en vez de hilarse por las ~50
	// llamadas a LoadConfig. Dos motivos: la precedencia documentada
	// (--profile gana a MAV_PROFILE) se cumple igual porque el flag pisa la
	// variable, y ademas los hijos -- flows, hijos de matrix, y los propios
	// comandos de la receta de lanzamiento -- heredan el perfil sin que haya
	// que acordarse de reenviarlo en cada sitio.
	if opts.Profile != "" {
		if err := os.Setenv("MAV_PROFILE", opts.Profile); err != nil {
			return err
		}
	}
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
		return c.help(opts, strings.Join(rest, " "))
	}
	stopLeaseHeartbeat := c.keepRunLeaseAlive(rest[0])
	defer stopLeaseHeartbeat()
	switch rest[0] {
	case "__worker":
		return c.runInternalWorker(ctx, rest[1:])
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
	case "app":
		return c.app(ctx, opts, rest[1:])
	case "openURL":
		return c.openURL(ctx, opts, rest[1:])
	case "location":
		return c.location(ctx, opts, rest[1:])
	case "clipboard":
		return c.clipboard(ctx, opts, rest[1:])
	case "time":
		return c.timeControl(ctx, opts, rest[1:])
	case "debug":
		return c.debug(ctx, opts, rest[1:])
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
		case "--profile":
			if i+1 < len(args) {
				opts.Profile = args[i+1]
				i++
			} else {
				rest = append(rest, arg)
			}
		default:
			if strings.HasPrefix(arg, "--prefer-driver=") {
				opts.PreferDriver = strings.TrimPrefix(arg, "--prefer-driver=")
			} else if strings.HasPrefix(arg, "--profile=") {
				opts.Profile = strings.TrimPrefix(arg, "--profile=")
			} else {
				rest = append(rest, arg)
			}
		}
	}
	return opts, rest
}

// preferDriverAuto es el centinela que significa "decide tu por coste".
const preferDriverAuto = "auto"

// normalizePreferDriver valida --prefer-driver contra el registry de drivers
// en vez de contra una lista congelada, de modo que un driver recien
// registrado es seleccionable sin tocar el parser. Devuelve "auto" o el id
// del driver.
func (c CLI) normalizePreferDriver(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return preferDriverAuto, nil
	}
	if value == preferDriverAuto {
		return value, nil
	}
	if c.driverRegistry().Lookup(value) == nil {
		return "", fmt.Errorf("prefer_driver_invalid")
	}
	return value, nil
}

// preferDriverUsage enumera los ids realmente registrados, para que el error
// no mienta cuando el portfolio de drivers cambia. Registry.All() ya viene
// ordenado por ID, asi que la salida es determinista.
func (c CLI) preferDriverUsage() string {
	ids := []string{preferDriverAuto}
	for _, d := range c.driverRegistry().All() {
		ids = append(ids, d.ID())
	}
	return "--prefer-driver " + strings.Join(ids, "|")
}

// routerPrefer traduce el valor normalizado al hint que espera
// Router.Route: "auto" es ausencia de hint.
func routerPrefer(prefer string) string {
	if prefer == preferDriverAuto {
		return ""
	}
	return prefer
}

func (c CLI) help(opts GlobalOptions, topic string) error {
	help := helpText(topic)
	_, err := fmt.Fprint(c.Stdout, help)
	return err
}

func helpText(topic string) string {
	topic = normalizeHelpTopic(topic)
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
  app         List or terminate apps.
  openURL     Open a URL on the target.
  location    Set or reset simulated location.
  clipboard   Copy or read target clipboard.
  time        Control app wall-clock time (simulator only).
  debug       Attach and control LLDB (simulator only).
  run         Execute a native MAV YAML flow.
  flow        Lint native MAV YAML flows.
  logs        Read captured run logs.
  stop        Stop a run immediately (normally automatic).
  crashes     List crashes for the configured app.
  evidence    Start/step/stop/report evidence.
  network     Start/stop a HAR network capture (sim only).

Global flags:
  --raw       Emit raw underlying tool output where supported.
  --verbose   Print extra debug details where supported.
  --prefer-driver auto|<driver-id>
              Prefer a registered driver. Use mav doctor to list them.
  --profile NAME
              Select a platform profile from .mav/config.yaml.
  --help,-h   Show help.
`
	case "setup":
		return "Usage:\n  mav setup [--non-interactive]\n  mav setup --install axe idb baguette simtime lldb-dap\n"
	case "install-skills":
		return "Usage: mav install-skills\n"
	case "sim":
		return `Usage:
  mav sim list
  mav sim select --device "iPhone 17 Pro Max" --ios 26 [--locale es_ES] [--language es] [--force]
  mav sim select --udid <simulator-udid> [--force]
  mav sim boot
`
	case "sim list":
		return "Usage: mav sim list\n\nLists available iOS simulators.\n"
	case "sim select":
		return `Usage:
  mav sim select --device "iPhone 17 Pro Max" --ios 26 [--locale es_ES] [--language es] [--force]
  mav sim select --udid <simulator-udid> [--force]

Selects the active simulator in .mav/config.yaml. --force ignores a fresh MAV simulator lock when you know the run is yours.
`
	case "sim boot":
		return "Usage: mav sim boot\n\nBoots the selected simulator.\n"
	case "device":
		return `Usage:
  mav device list
  mav device select --udid <device-udid>
  mav device select --name <device-name>
`
	case "device list":
		return "Usage: mav device list\n\nLists connected physical iOS devices.\n"
	case "device select":
		return `Usage:
  mav device select --udid <device-udid>
  mav device select --name <device-name>

Selects a physical iOS device and switches target_kind to device.
`
	case "open":
		return `Usage:
  mav open [--device NAME] [--ios VERSION] [--udid UDID] [--locale LOCALE] [--language LANG] [--clear-state] [--fixture NAME] [--time-control] [--no-relaunch] [--force]

--no-relaunch reuses the app already running on the selected target. It starts or reuses a MAV run without executing the launch recipe.
--force ignores a fresh MAV simulator lock when you know the run is yours.
`
	case "ui":
		return `Usage:
  mav ui tree [--prefer-driver auto|axe] [--include-system]
  mav ui tap --id ID [--verify] [--prefer-driver auto|<driver-id>]
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
	case "ui tap":
		return `Usage:
  mav ui tap --id ID [--verify] [--prefer-driver auto|<driver-id>]
  mav ui tap --x X --y Y
  mav ui tap --text TEXT [--prefer-driver auto|axe]
  mav ui tap --value VALUE

Prefer accessibility ids. Use coordinates only when the tree is insufficient and a screenshot makes the target unambiguous.
`
	case "ui type":
		return "Usage: mav ui type TEXT [--prefer-driver auto|axe]\n"
	case "ui erase":
		return "Usage: mav ui erase [--id ID | --text TEXT | --value VALUE | --focused true]\n\nSimulator-backed through baguette. Physical devices return erase_unsupported_on_device.\n"
	case "ui hideKeyboard":
		return "Usage: mav ui hideKeyboard\n\nSimulator-backed through baguette. Physical devices return hide_keyboard_unsupported_on_device.\n"
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
	case "ui actions":
		return "Usage: mav ui actions --file actions.json\n"
	case "ui wait":
		return `Usage:
  mav ui wait --id ID [--timeout 5s]
  mav ui wait --text TEXT [--timeout 5s]
  mav ui wait --value VALUE [--timeout 5s]
`
	case "ui scrollUntil":
		return "Usage: mav ui scrollUntil --id ID [--direction up] [--max-swipes 5]\n"
	case "run":
		return "Usage:\n  mav run flow.yaml [--param name=value]\n  mav run flow.yaml --run RUN_ID\n  mav run flow.yaml --target UDID --target \"Exact Name\" [--jobs N]\n"
	case "time":
		return "Usage:\n  mav time freeze --at 2032-01-15T10:00:00Z\n  mav time travel --by +8d\n  mav time scale --factor 60\n  mav time status\n  mav time reset\n"
	case "debug":
		return "Usage:\n  mav debug attach [--breakpoint File.swift:42]\n  mav debug wait [--timeout 10s]\n  mav debug state [--stack --locals --threads] [--raw]\n  mav debug break add File.swift:42\n  mav debug eval 'po store'\n  mav debug pause\n  mav debug step in|over|out|continue\n  mav debug detach [--kill]\n"
	case "flow":
		return "Usage:\n  mav flow lint flow.yaml [--raw]\n"
	case "flow lint":
		return "Usage: mav flow lint flow.yaml [--raw]\n\nParses and validates a native MAV YAML flow without running it.\n"
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
	case "evidence start":
		return "Usage: mav evidence start [--network] [--port PORT] [--run RUN_ID]\n"
	case "evidence step":
		return "Usage: mav evidence step --name NAME [--note NOTE] [--run RUN_ID]\n"
	case "evidence stop":
		return "Usage: mav evidence stop [--note NOTE] [--no-capture] [--run RUN_ID]\n"
	case "evidence report":
		return "Usage: mav evidence report [--run RUN_ID]\n\nWrites the verified evidence manifest for the run.\n"
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
	case "network start":
		return "Usage: mav network start [--har PATH] [--port PORT] [--run RUN_ID]\n\nStarts a simulator HAR capture through mitmproxy.\n"
	case "network stop":
		return "Usage: mav network stop [--run RUN_ID]\n\nStops the current HAR capture and summarizes the file.\n"
	case "network status":
		return "Usage: mav network status [--har PATH] [--run RUN_ID] [--raw]\n"
	default:
		return "Unknown help topic. Run: mav help\n"
	}
}

func normalizeHelpTopic(topic string) string {
	topic = strings.Join(strings.Fields(topic), " ")
	aliases := map[string]string{
		"tree":         "ui tree",
		"tap":          "ui tap",
		"type":         "ui type",
		"erase":        "ui erase",
		"hideKeyboard": "ui hideKeyboard",
		"swipe":        "ui swipe",
		"longPress":    "ui longPress",
		"pinch":        "ui pinch",
		"rotate":       "ui rotate",
		"twoFingerPan": "ui twoFingerPan",
		"wait":         "ui wait",
		"scrollUntil":  "ui scrollUntil",
		"actions":      "ui actions",
		"screenshot":   "capture",
	}
	if alias, ok := aliases[topic]; ok {
		return alias
	}
	return topic
}

func (c CLI) doctor(ctx context.Context, opts GlobalOptions) error {
	_ = opts
	cfg, _ := LoadConfig(c.Root)
	if cfg.Root == "" {
		cfg = DefaultConfig(c.Root)
	}
	c.resolveConfigTools(&cfg)
	c.resolveConfigTarget(&cfg)
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
	if targetKind(cfg) == drivers.KindSim && cfg.SimulatorUDID != "" {
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
	return c.OK("doctor", fields).Write(c.Stdout)
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
		"simtime":   {"brew", "install", "mobai-app/tap/simtime"},
		// Drivers de macOS. cua-driver se instala con su propio script, que
		// es lo que deja /Applications/CuaDriver.app en su sitio -- y esa app
		// es la que tiene los permisos, asi que no vale con dejar un binario
		// suelto en el PATH. axcli no publica formula aguas arriba, asi que va
		// por el tap de mav como formula espejo: apunta al release del autor
		// original, no a un fork.
		"cua-driver": {"sh", "-c", "curl -fsSL https://cua.ai/driver/install.sh | bash"},
		"axcli":      {"brew", "install", "bitomule/tap/axcli"},
	}
	for _, tool := range tools {
		if tool == "simtime" {
			if _, lookErr := c.Runner.LookPath("simtime"); lookErr != nil {
				cmd := commands[tool]
				if opts.Verbose {
					fmt.Fprintln(c.Stderr, strings.Join(cmd, " "))
				}
				result := c.Runner.Run(ctx, cmd[0], cmd[1:]...)
				if result.Err != nil {
					return Fail("setup_failed", map[string]string{"tool": tool, "stderr": firstLine(result.Stderr)}).Write(c.Stdout)
				}
			}
			if err := c.verifySimtime(ctx); err != nil {
				return Fail("setup_failed", map[string]string{
					"tool": "simtime", "stderr": err.Error(),
					"next": "brew reinstall mobai-app/tap/simtime",
				}).Write(c.Stdout)
			}
			continue
		}
		if tool == "lldb" || tool == "lldb-dap" {
			ok, err := c.setupLLDBDAP(ctx)
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
	return c.OK("setup", map[string]string{"installed": strings.Join(tools, ",")}).Write(c.Stdout)
}

func (c CLI) verifySimtime(ctx context.Context) error {
	binary, err := c.Runner.LookPath("simtime")
	if err != nil {
		return fmt.Errorf("simtime binary is not on PATH")
	}
	probe := c.Runner.Run(ctx, binary, "--help")
	if probe.Err != nil {
		return fmt.Errorf("simtime --help failed: %s", firstLine(probe.Stderr))
	}
	resolved, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return fmt.Errorf("resolve simtime binary: %w", err)
	}
	dylib := filepath.Join(filepath.Dir(filepath.Dir(resolved)), "libexec", "libsimtime.dylib")
	if info, statErr := os.Stat(dylib); statErr != nil || info.IsDir() {
		return fmt.Errorf("libsimtime.dylib is missing at %s", dylib)
	}
	return nil
}

func (c CLI) setupLLDBDAP(ctx context.Context) (bool, error) {
	result := c.Runner.Run(ctx, "xcrun", "--find", "lldb-dap")
	if result.Err != nil || strings.TrimSpace(result.Stdout) == "" {
		return false, Fail("setup_failed", map[string]string{
			"tool":   "lldb-dap",
			"stderr": firstNonEmpty(firstLine(result.Stderr), "lldb-dap is not bundled with the selected Xcode"),
			"next":   "install/select a full Xcode with xcode-select, then rerun mav setup --install lldb-dap",
		}).Write(c.Stdout)
	}
	probe := c.Runner.Run(ctx, "xcrun", "lldb-dap", "--help")
	if probe.Err != nil {
		return false, Fail("setup_failed", map[string]string{
			"tool": "lldb-dap", "stderr": firstLine(probe.Stderr),
			"next": "select a healthy Xcode with sudo xcode-select -s /Applications/Xcode.app",
		}).Write(c.Stdout)
	}
	return true, nil
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
	return c.OK("install-skills", map[string]string{"skill": "mav", "scope": "global"}).Write(c.Stdout)
}

func (c CLI) setupProject(opts GlobalOptions, args []string) error {
	cfg, err := SetupConfig(c.Root, c.Runner)
	// Sin overlay: esta rama termina en SaveConfig, y guardar una config con
	// perfil aplicado aplanaria el perfil sobre la base.
	if existing, loadErr := LoadConfigRaw(c.Root); loadErr == nil {
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
	return c.OK("setup", fields).Write(c.Stdout)
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
		if err := c.OK("sim.list", map[string]string{"count": strconv.Itoa(len(sims))}).Write(c.Stdout); err != nil {
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
		c.resolveConfigTarget(&cfg)
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
		if err := persistTargetSelection(c.Root, cfg); err != nil {
			return err
		}
		return c.OK("sim.select", map[string]string{"udid": sim.UDID, "name": sim.Name, "runtime": sim.Runtime}).Write(c.Stdout)
	case "boot":
		cfg, err := LoadConfig(c.Root)
		if err != nil {
			return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
		}
		c.resolveConfigTools(&cfg)
		c.resolveConfigTarget(&cfg)
		if targetKind(cfg) != drivers.KindSim {
			return Fail("sim_not_applicable", map[string]string{"target": "device", "next": "select a simulator with mav sim select"}).Write(c.Stdout)
		}
		if cfg.SimulatorUDID == "" {
			return Fail("sim_not_selected", map[string]string{"next": "mav sim select --device 'iPhone' --ios 26"}).Write(c.Stdout)
		}
		target := targetFromConfig(cfg)
		driver, _, routeErr := c.router().Route(ctx, drivers.CapBoot, target, "")
		if routeErr != nil {
			return Fail("sim_boot_failed", map[string]string{"stderr": firstLine(routeErr.Error())}).Write(c.Stdout)
		}
		lifecycle, ok := driver.(drivers.LifecycleDriver)
		if !ok {
			return Fail("sim_boot_failed", map[string]string{"stderr": "driver " + driver.ID() + " does not implement lifecycle"}).Write(c.Stdout)
		}
		if bootErr := lifecycle.Boot(ctx, target); bootErr != nil {
			return Fail("sim_boot_failed", map[string]string{"stderr": firstLine(bootErr.Error())}).Write(c.Stdout)
		}
		status := c.Runner.Run(ctx, "xcrun", "simctl", "bootstatus", cfg.SimulatorUDID, "-b")
		if status.Err != nil {
			return Fail("sim_bootstatus_failed", map[string]string{"stderr": firstLine(status.Stderr)}).Write(c.Stdout)
		}
		return c.OK("sim.boot", map[string]string{"udid": cfg.SimulatorUDID, "name": cfg.SimulatorName}).Write(c.Stdout)
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
		if err := c.OK("device.list", map[string]string{"count": strconv.Itoa(len(devices))}).Write(c.Stdout); err != nil {
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
		if err := persistTargetSelection(c.Root, cfg); err != nil {
			return err
		}
		return c.OK("device.select", map[string]string{"udid": device.UDID, "name": device.Name}).Write(c.Stdout)
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
	c.resolveConfigTarget(&cfg)
	if err := c.applyOpenTargetOverrides(ctx, &cfg, args); err != nil {
		return Fail("sim_select_failed", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	if targetKind(cfg) == drivers.KindSim && cfg.SimulatorUDID != "" && !hasFlag(args, "--force") {
		if lock, locked := simulatorLockedByOther(cfg.SimulatorUDID, c.Root); locked {
			return Fail("sim_locked", map[string]string{"udid": cfg.SimulatorUDID, "run": lock.RunID, "project": lock.Project, "next": "select another simulator or rerun with --force"}).Write(c.Stdout)
		}
	}
	noRelaunch := hasFlag(args, "--no-relaunch")
	if noRelaunch && hasFlag(args, "--clear-state") {
		return Fail("open_flags_invalid", map[string]string{"usage": "--no-relaunch cannot be combined with --clear-state"}).Write(c.Stdout)
	}
	fixture := flagValue(args, "--fixture")
	// Mismo trato que --clear-state, y por el mismo motivo: --no-relaunch se
	// salta la receta entera, asi que el fixture no llegaria a correr nunca.
	// Aceptar el flag y no ejecutarlo dejaria al agente validando contra datos
	// que nadie sembro, con las aserciones pasando o fallando por el motivo
	// equivocado.
	if noRelaunch && fixture != "" {
		return Fail("open_flags_invalid", map[string]string{"usage": "--no-relaunch cannot be combined with --fixture"}).Write(c.Stdout)
	}
	// A run bound via withRun (i.e. this open is a step inside a flow that
	// already allocated its own run) is authoritative: open neither reads
	// nor kills whatever .mav/current-run happens to name, and never
	// overwrites it. Only a standalone `mav open` -- no bound run -- touches
	// the pointer, preserving its existing kill-the-previous-run semantics.
	var previousRunID string
	var run RunState
	bound, hasBoundRun := c.boundRun()
	if hasBoundRun {
		run = bound
	} else if noRelaunch {
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
	if !hasBoundRun {
		if err := SaveCurrentRun(c.Root, run); err != nil {
			return err
		}
	}
	if targetKind(cfg) == drivers.KindDevice && !hasTool(cfg, "idb") {
		return Fail("tool_missing", map[string]string{"tool": "idb", "target": "device", "next": "mav setup --install idb"}).Write(c.Stdout)
	}
	if err := c.ensureOpenSimulatorBooted(ctx, cfg, run); err != nil {
		return Fail("sim_boot_failed", map[string]string{"run": run.ID, "logs": run.LogsPath, "stderr": err.Error()}).Write(c.Stdout)
	}
	// A second `open` step later in the same bound-run flow would otherwise
	// leave the first probe-logs `log stream` running forever: with the run
	// reused (not recreated), nothing else ever stops it, and a duplicate
	// log stream would start writing into the same logs.txt. This applies
	// regardless of --no-relaunch: startProbeLogs below always starts a new
	// stream, so a stale one from an earlier open in this same run must
	// always be stopped first, not just on relaunch. Deliberately scoped to
	// just probe-logs -- not a full `stop` of the run -- so this doesn't
	// tear down the video recording, worker, or sim-lock that
	// evidence.start / other earlier steps may have set up for this run.
	if hasBoundRun {
		stopProbeLogs(run)
	}
	probeLogPID, probeLogErr := c.startProbeLogs(ctx, cfg, run)
	if probeLogErr != nil {
		appendFile(run.LogsPath, "mav probe log capture failed: "+probeLogErr.Error()+"\n")
	}
	appPath := ""
	clearStateWarn := ""
	if !noRelaunch {
		var failedStep *launchStep
		var failedResult CommandResult
		appPath, failedStep, failedResult, clearStateWarn = c.runLaunchRecipe(ctx, cfg, run, hasFlag(args, "--clear-state"), fixture)
		if failedStep != nil {
			fields := map[string]string{"run": run.ID, "logs": run.LogsPath, "step": failedStep.Name, "stderr": firstLine(failedResult.Stderr)}
			if fields["stderr"] == "" && failedResult.Err != nil {
				fields["stderr"] = failedResult.Err.Error()
			}
			return Fail("launch_step_failed", fields).Write(c.Stdout)
		}
		writeRunFixture(run, fixture)
	}
	if hasFlag(args, "--time-control") {
		if targetKind(cfg) != drivers.KindSim {
			return Fail("time_control_unsupported_on_device", nil).Write(c.Stdout)
		}
		driver, _, routeErr := c.router().Route(ctx, drivers.CapWallClock, targetFromConfig(cfg), "")
		if routeErr != nil {
			return Fail("tool_missing", map[string]string{"tool": "simtime", "next": "mav setup --install simtime"}).Write(c.Stdout)
		}
		clock, ok := driver.(drivers.WallClockDriver)
		if !ok {
			return Fail("time_control_unavailable", nil).Write(c.Stdout)
		}
		if injectErr := clock.InjectTimeControl(ctx, targetFromConfig(cfg)); injectErr != nil {
			return Fail("time_control_inject_failed", map[string]string{"stderr": firstLine(injectErr.Error())}).Write(c.Stdout)
		}
		_ = os.WriteFile(filepath.Join(run.Dir, "time-control.enabled"), []byte(cfg.BundleID+"\n"), 0o600)
		if hasFlag(args, "--preserve-time") {
			_ = os.WriteFile(filepath.Join(run.Dir, "time-control.preserve"), []byte("true\n"), 0o600)
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
	if targetKind(cfg) == drivers.KindSim && cfg.SimulatorUDID != "" && probeLogPID > 0 {
		if err := writeSimulatorLock(cfg.SimulatorUDID, run, c.Root, probeLogPID); err == nil {
			fields["sim_lock"] = simLockPath(cfg.SimulatorUDID)
		}
	}
	fields["target_kind"] = targetKindLabel(targetKind(cfg))
	if clearStateWarn != "" {
		fields["clear_state_warn"] = clearStateWarn
	}
	if fixture != "" {
		fields["fixture"] = fixture
	}
	fields["session"] = "direct"
	if _, ok := c.Runner.(ExecRunner); ok {
		if session, workerErr := startRunWorker(c.Root, run); workerErr == nil {
			fields["session"] = session
		} else {
			appendFile(run.LogsPath, "mav worker fallback: "+workerErr.Error()+"\n")
		}
	}
	fields["target"] = targetName(cfg)
	if fields["target"] == "" {
		fields["target"] = targetUDID(cfg)
	}
	if fields["target"] == "" {
		fields["target"] = "booted"
	}
	return c.OK("open", fields).Write(c.Stdout)
}

func (c CLI) timeControl(ctx context.Context, opts GlobalOptions, args []string) error {
	_ = opts
	if len(args) == 0 {
		return Fail("time_command_missing", map[string]string{"usage": "mav time freeze|travel|scale|status|reset"}).Write(c.Stdout)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTarget(&cfg)
	if targetKind(cfg) != drivers.KindSim {
		return Fail("time_control_unsupported_on_device", nil).Write(c.Stdout)
	}
	run, runErr := c.resolveRun("")
	if runErr != nil || !exists(filepath.Join(run.Dir, "time-control.enabled")) {
		return Fail("time_control_not_loaded", map[string]string{"next": "mav open --time-control"}).Write(c.Stdout)
	}
	driver, _, routeErr := c.router().Route(ctx, drivers.CapWallClock, targetFromConfig(cfg), "")
	if routeErr != nil {
		return Fail("tool_missing", map[string]string{"tool": "simtime", "next": "mav setup --install simtime"}).Write(c.Stdout)
	}
	clock, ok := driver.(drivers.WallClockDriver)
	if !ok {
		return Fail("time_control_unavailable", nil).Write(c.Stdout)
	}
	fields := map[string]string{"driver": driver.ID()}
	switch args[0] {
	case "freeze":
		at := flagValue(args[1:], "--at")
		if at == "" && len(args) > 1 {
			at = args[1]
		}
		if at == "" {
			return Fail("time_at_missing", nil).Write(c.Stdout)
		}
		status, err := clock.FreezeTime(ctx, targetFromConfig(cfg), at)
		if err != nil {
			return Fail("time_freeze_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		fields["at"], fields["status"] = at, status
	case "travel":
		by := flagValue(args[1:], "--by")
		if by == "" && len(args) > 1 {
			by = args[1]
		}
		if by == "" {
			return Fail("time_by_missing", nil).Write(c.Stdout)
		}
		status, err := clock.TravelTime(ctx, targetFromConfig(cfg), by)
		if err != nil {
			return Fail("time_travel_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		fields["by"], fields["status"] = by, status
	case "scale":
		raw := flagValue(args[1:], "--factor")
		if raw == "" && len(args) > 1 {
			raw = args[1]
		}
		factor, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil || factor <= 0 {
			return Fail("time_factor_invalid", map[string]string{"factor": raw}).Write(c.Stdout)
		}
		status, err := clock.ScaleTime(ctx, targetFromConfig(cfg), factor)
		if err != nil {
			return Fail("time_scale_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		fields["factor"], fields["status"] = raw, status
	case "status":
		status, err := clock.TimeStatus(ctx, targetFromConfig(cfg))
		if err != nil {
			return Fail("time_status_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		fields["status"] = status
	case "reset":
		if err := clock.ResetTime(ctx, targetFromConfig(cfg)); err != nil {
			return Fail("time_reset_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		fields["reset"] = "true"
	default:
		return Fail("time_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
	return c.OK("time."+args[0], fields).Write(c.Stdout)
}

func (c CLI) ensureOpenSimulatorBooted(ctx context.Context, cfg Config, run RunState) error {
	if targetKind(cfg) != drivers.KindSim || cfg.SimulatorUDID == "" || !hasTool(cfg, "xcrun") {
		return nil
	}
	boot := c.Runner.Run(ctx, "xcrun", "simctl", "boot", cfg.SimulatorUDID)
	appendCommand(run, "xcrun simctl boot "+cfg.SimulatorUDID, boot)
	if boot.Err != nil && !strings.Contains(boot.Stderr, "Unable to boot device in current state") {
		return fmt.Errorf("%s", firstLine(boot.Stderr))
	}
	status := c.Runner.Run(ctx, "xcrun", "simctl", "bootstatus", cfg.SimulatorUDID, "-b")
	appendCommand(run, "xcrun simctl bootstatus "+cfg.SimulatorUDID+" -b", status)
	if status.Err != nil {
		return fmt.Errorf("%s", firstLine(status.Stderr))
	}
	return nil
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
	return persistTargetSelection(c.Root, *cfg)
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
	// Guarantee a watchdog worker exists for this run before starting a
	// long-lived log stream, even when the caller reaches startProbeLogs
	// without ever going through open() -- see ensureRunWorker.
	defer c.ensureRunWorker(run)
	predicate := probeLogPredicate(cfg)
	if targetKind(cfg) == drivers.KindMac {
		// En el Mac el stream es el del propio host: la misma linea que en
		// simulador quitando el `simctl spawn <udid>` de delante. El predicado
		// y el formato no cambian, asi que todo lo que consume logs.txt
		// aguas abajo sigue funcionando igual.
		args := []string{"stream", "--style", "compact", "--level", "debug", "--predicate", predicate}
		pid, err := c.Runner.Start(ctx, run.LogsPath, "log", args...)
		if err == nil {
			appendProcess(run, "probe-logs", pid, "log "+strings.Join(args, " "))
		}
		return pid, err
	}
	if targetKind(cfg) == drivers.KindDevice {
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

// ui is the standalone-command entry point for `mav ui <verb>` -- the
// hot-path surface this exists for (an agent driving `mav ui tap`, `mav ui
// tree`, ... command-by-command, not through `mav run`). That distinction
// matters: `mav run` flows already survive a target_command-backed pool
// slot being reclaimed mid-run via startTargetCommandKeepAlive's periodic
// ping, but nothing pings on their behalf between two standalone commands,
// so a simulator that goes down inside the target-command cache's TTL (see
// bootedSimulatorCacheTTL) between two `mav ui` calls has no earlier warning
// -- dispatchWithStaleTargetRetry is what recovers from it here, once,
// after the fact.
func (c CLI) ui(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("ui_command_missing", map[string]string{"usage": "mav ui tree|tap|doubleTap|type|erase|hideKeyboard|swipe|drag|dragPath|toggle|press|longPress|pinch|rotate|twoFingerPan|actions|wait|scrollUntil"}).Write(c.Stdout)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTools(&cfg)
	c.resolveConfigTarget(&cfg)
	var dispatchErr error
	out := c.dispatchWithStaleTargetRetry(cfg, func(callee CLI, cfg Config) {
		dispatchErr = callee.dispatchUICommand(ctx, opts, cfg, args)
	})
	if dispatchErr != nil {
		return dispatchErr
	}
	_, writeErr := io.WriteString(c.Stdout, out)
	return writeErr
}

// dispatchUICommand is the actual `mav ui <verb>` switch, split out of ui so
// dispatchWithStaleTargetRetry can run it twice (original attempt, then one
// retry) against two different cfg values without re-parsing args or
// re-resolving the target a caller already resolved.
func (c CLI) dispatchUICommand(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	switch args[0] {
	case "tree":
		return c.uiTree(ctx, opts, cfg, args[1:])
	case "tap":
		return c.uiTap(ctx, opts, cfg, args[1:])
	case "doubleTap":
		return c.uiDoubleTap(ctx, opts, cfg, args[1:])
	case "type":
		return c.uiType(ctx, opts, cfg, args[1:])
	case "erase":
		return c.uiErase(ctx, opts, cfg, args[1:])
	case "hideKeyboard":
		return c.uiHideKeyboard(ctx, opts, cfg, args[1:])
	case "swipe":
		return c.uiSwipe(ctx, opts, cfg, args[1:])
	case "drag":
		return c.uiDrag(ctx, opts, cfg, args[1:])
	case "dragPath":
		return c.uiDragPath(ctx, opts, cfg, args[1:])
	case "toggle":
		return c.uiToggle(ctx, opts, cfg, args[1:])
	case "press":
		return c.uiPress(ctx, opts, cfg, args[1:])
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
	prefer, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": c.preferDriverUsage()}).Write(c.Stdout)
	}
	includeSystem := hasFlag(args, "--include-system")
	if includeSystem && targetKind(cfg) != drivers.KindSim {
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
	if err := c.OK("ui.tree", fields).Write(c.Stdout); err != nil {
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
		if targetKind(cfg) != drivers.KindSim {
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
	// Sin gate por herramienta: quien decide es el router. Preguntar antes por
	// `hasTool(cfg, "axe")` duplicaba la decision fuera de el, y eso no se
	// notaba mientras todos los targets se servian con axe -- en cuanto
	// aparecio uno que no (macOS), este `if` mandaba a axe de todas formas y
	// el arbol moria pidiendo un --udid que no existe. Si no hay driver para
	// esta capacidad en este target, el propio ErrNoDriver lo explica mejor de
	// lo que puede hacerlo un booleano.
	{
		driver, _, err := c.router().Route(ctx, drivers.CapTreeAX, target, routerPrefer(prefer))
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
		result := c.runIDBCommand(ctx, idbTargetArgs(cfg, "ui", "describe-all", "--json", "--nested")...)
		return describedUITree{Driver: "idb", Result: result}, nil
	}
	return describedUITree{}, fmt.Errorf("tree_tool_missing")
}

func (c CLI) recoverEmptyAXTree(ctx context.Context, cfg Config) error {
	if targetKind(cfg) != drivers.KindSim {
		return fmt.Errorf("device_accessibility_recovery_unavailable")
	}
	if !hasTool(cfg, "xcrun") || cfg.SimulatorUDID == "" {
		return fmt.Errorf("simulator_recovery_unavailable")
	}
	run, _ := c.resolveRun("")
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

// tapToolMissingFields explica que falta para poder pulsar, y en macOS eso NO
// es lo mismo que en iOS.
//
// El mensaje de iOS sugiere caer a coordenadas, que alli es un apano razonable.
// En el Mac seria el consejo contrario al correcto: pulsar por coordenadas es
// justo el camino que puede aterrizar en otra aplicacion si la ventana se
// movio. Lo que falta ahi es un driver que entregue por PID.
// snapshotForVerification lee el arbol para poder compararlo despues de una
// accion. Devuelve nil si no se puede leer: la verificacion es un extra, y no
// poder hacerla nunca debe convertir un tap correcto en un fallo.
func (c CLI) snapshotForVerification(ctx context.Context, cfg Config) []Element {
	described, err := c.describeUITree(ctx, cfg, "auto", false)
	if err != nil || described.Result.Err != nil {
		return nil
	}
	return ExtractElements(described.Result.Stdout)
}

// verifyTapChangedSomething responde a la pregunta que la capa de evidencia de
// mav no puede permitirse dejar sin respuesta: el tap hizo algo?
//
// Existe porque un tap puede reportar exito sin haber hecho nada. AXPress
// devuelve OK y no hace nada sobre vistas renderizadas por un navegador, y un
// evento sintetico puede acabar en una ventana que ya no esta donde estaba.
// Reportar "ok" ahi es peor que fallar: el agente sigue construyendo sobre una
// pantalla que no cambio, y el fallo aparece tres pasos despues sin relacion
// aparente con la causa.
//
// Devuelve "changed", "unchanged" o "unknown". OJO con "unchanged": no siempre
// significa que el tap fallara -- hay acciones que legitimamente no cambian el
// arbol -- asi que es una senal para que decida quien lee, no un veredicto.
func (c CLI) verifyTapChangedSomething(ctx context.Context, cfg Config, before []Element) string {
	if before == nil {
		return "unknown"
	}
	after := c.snapshotForVerification(ctx, cfg)
	if after == nil {
		return "unknown"
	}
	delta := TreeDiff(before, after)
	if len(delta.Added) == 0 && len(delta.Removed) == 0 && len(delta.Changed) == 0 {
		return "unchanged"
	}
	return "changed"
}

func tapToolMissingFields(cfg Config) map[string]string {
	if targetKind(cfg) == drivers.KindMac {
		return map[string]string{
			"tool": "axcli",
			"next": "mav setup --install axcli; taps on macOS need PID-targeted delivery, coordinates can land on another app",
		}
	}
	return map[string]string{"tool": "axe", "next": "use mav ui tap --x X --y Y when AXe is unavailable"}
}

func (c CLI) uiTap(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	selector, selectorErr := selectorFromCLI(args)
	if selectorErr != nil {
		return Fail("selector_invalid", map[string]string{"error": selectorErr.Error()}).Write(c.Stdout)
	}
	id := selector.ID
	text := selector.Text
	value := selector.Value
	x := flagValue(args, "--x")
	y := flagValue(args, "--y")
	prefer, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": c.preferDriverUsage()}).Write(c.Stdout)
	}
	caps := c.resolveCapabilities(ctx, cfg)
	if !selector.IsZero() {
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
			return Fail("tool_missing", tapToolMissingFields(cfg)).Write(c.Stdout)
		}
		if isSimpleSemanticSelector(selector) && value != "" {
			return Fail("tap_target_missing", map[string]string{"usage": "mav ui tap --value VALUE requires another predicate or a stable --id"}).Write(c.Stdout)
		}
		if !isSimpleSemanticSelector(selector) {
			matched, matchErr := c.resolveSelector(ctx, cfg, selector, prefer)
			if matchErr != nil {
				return Fail(matchErr.Error(), selectorDiagnosticFields(selector, matched)).Write(c.Stdout)
			}
			fields["matched_id"] = matched.ID
			fields["matched_text"] = elementText(matched)
			fields["role"] = matched.Role
			mx, my, mw, mh, ok := parseElementFrame(matched.Frame)
			if !ok {
				return Fail("selector_frame_missing", selectorDiagnosticFields(selector, matched)).Write(c.Stdout)
			}
			return c.uiTap(ctx, opts, cfg, append(onlyFastPathArgs(args),
				"--x", strconv.Itoa(int(mx+mw/2)), "--y", strconv.Itoa(int(my+mh/2))))
		}
		target := targetFromConfig(cfg)
		driver, _, err := c.router().Route(ctx, drivers.CapSemanticTap, target, routerPrefer(prefer))
		if err != nil {
			return Fail("tool_missing", tapToolMissingFields(cfg)).Write(c.Stdout)
		}
		td, ok := driver.(drivers.TapDriver)
		if !ok {
			return Fail("tool_missing", map[string]string{"tool": "axe"}).Write(c.Stdout)
		}
		// El arbol de ANTES solo se lee cuando se pide verificacion, porque
		// leerlo cuesta segundos y esto es el bucle en caliente.
		verify := hasFlag(args, "--verify")
		var before []Element
		if verify {
			before = c.snapshotForVerification(ctx, cfg)
		}
		_, tapErr := td.Tap(ctx, target, drivers.TapSpec{Selector: drivers.ElementSelector{ID: id, Text: text}})
		result := CommandResult{}
		if tapErr != nil {
			result = CommandResult{Stderr: tapErr.Error(), Err: tapErr}
			diagnosticFields, hasTextDiagnostic := c.diagnoseTextTapFailure(ctx, cfg, text, result.Stderr)
			if hasTextDiagnostic {
				return Fail("ui_tap_text_no_label_match", diagnosticFields).Write(c.Stdout)
			}
			tapFields := map[string]string{"stderr": firstLine(result.Stderr)}
			// Un locator ambiguo no es un fallo del tap: es que el selector
			// describe varias cosas y la herramienta se niega a elegir por su
			// cuenta, que es lo correcto. Pero el mensaje es suyo y no dice
			// que hacer, asi que el agente se queda mirando.
			if strings.Contains(result.Stderr, "must be unique") {
				tapFields["next"] = "the selector matches more than one element; use --id, or a longer --text that only matches one"
			}
			return Fail("ui_tap_failed", tapFields).Write(c.Stdout)
		}
		fields["driver"] = driver.ID()
		if verify {
			fields["verified"] = c.verifyTapChangedSomething(ctx, cfg, before)
		}
		c.appendCurrentCommand(command, result)
		return c.writeFastPathResult(ctx, cfg, args, "ui.tap", fields)
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
		// CapCoordTap: en sim axe/baguette/idb empatan a coste 50 y el desempate
		// por ID daria axe, no idb -- sin este prefer cambiaria el driver= de salida.
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
			tapFields := map[string]string{"stderr": firstLine(result.Stderr)}
			// Un locator ambiguo no es un fallo del tap: es que el selector
			// describe varias cosas y la herramienta se niega a elegir por su
			// cuenta, que es lo correcto. Pero el mensaje es suyo y no dice
			// que hacer, asi que el agente se queda mirando.
			if strings.Contains(result.Stderr, "must be unique") {
				tapFields["next"] = "the selector matches more than one element; use --id, or a longer --text that only matches one"
			}
			return Fail("ui_tap_failed", tapFields).Write(c.Stdout)
		}
		c.appendCurrentCommand("mav ui tap --x "+x+" --y "+y, result)
		return c.writeFastPathResult(ctx, cfg, args, "ui.tap", map[string]string{"x": x, "y": y, "driver": driver.ID(), "route_recorded": "false"})
	}
	return Fail("tap_target_missing", map[string]string{"usage": "mav ui tap --id ID | --x X --y Y | --text TEXT"}).Write(c.Stdout)
}

func isSimpleSemanticSelector(selector Selector) bool {
	copy := selector
	copy.ID, copy.Text, copy.Value = "", "", ""
	return copy.IsZero()
}

func selectorDiagnosticFields(selector Selector, matched Element) map[string]string {
	fields := map[string]string{}
	if selector.ID != "" {
		fields["id"] = selector.ID
	}
	if selector.Text != "" {
		fields["text"] = selector.Text
	}
	if selector.TextContains != "" {
		fields["text_contains"] = selector.TextContains
	}
	if selector.Role != "" {
		fields["role"] = selector.Role
	}
	if matched.ID != "" {
		fields["candidate_id"] = matched.ID
	}
	return fields
}

func (c CLI) resolveSelector(ctx context.Context, cfg Config, selector Selector, prefer string) (Element, error) {
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil || described.Result.Err != nil {
		return Element{}, fmt.Errorf("tree_failed")
	}
	matches, err := MatchElements(ExtractElementsRaw(described.Result.Stdout), selector)
	if err != nil {
		return Element{}, err
	}
	if len(matches) == 0 {
		return Element{}, fmt.Errorf("selector_not_found")
	}
	if len(matches) > 1 {
		return matches[0], fmt.Errorf("selector_ambiguous")
	}
	return matches[0], nil
}

func fastPathArgs(args []string) []string {
	out := make([]string, 0, len(args))
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "--wait-") || arg == "--observe" {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				skipNext = true
			}
			continue
		}
		out = append(out, arg)
	}
	return out
}

func onlyFastPathArgs(args []string) []string {
	out := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// --verify se conserva: la caida a coordenadas es justo el camino con
		// mas probabilidad de pulsar donde no toca, asi que perder ahi la
		// verificacion seria perderla donde mas hace falta.
		if !strings.HasPrefix(arg, "--wait-") && arg != "--observe" && arg != "--verify" {
			continue
		}
		out = append(out, arg)
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			out = append(out, args[i+1])
			i++
		}
	}
	return out
}

func (c CLI) writeFastPathResult(ctx context.Context, cfg Config, args []string, command string, fields map[string]string) error {
	waitID := flagValue(args, "--wait-id")
	waitText := flagValue(args, "--wait-text")
	waitValue := flagValue(args, "--wait-value")
	waitChanged := flagValue(args, "--wait-changed-from")
	waitStable := hasFlag(args, "--wait-stable")
	observe := flagValue(args, "--observe")
	if observe == "" {
		observe = "none"
	}
	switch observe {
	case "none", "agent", "tree", "delta":
	default:
		return Fail("observe_invalid", map[string]string{"observe": observe}).Write(c.Stdout)
	}
	timeout := parseFlowDuration(flagValue(args, "--wait-timeout"), 5*time.Second)
	if timeout <= 0 {
		return Fail("wait_timeout_invalid", nil).Write(c.Stdout)
	}
	if waitID != "" || waitText != "" || waitValue != "" || waitChanged != "" || waitStable {
		deadline := time.Now().Add(timeout)
		var lastHash string
		var lastElements []Element
		stableCount := 0
		for {
			if waitChanged != "" {
				ok, err := c.screenshotChangedFrom(ctx, waitChanged)
				if err == nil && ok {
					break
				}
			} else {
				described, err := c.describeUITree(ctx, cfg, "auto", false)
				if err == nil && described.Result.Err == nil {
					elements := ExtractElementsRaw(described.Result.Stdout)
					lastElements = elements
					if waitStable {
						hash := hashElements(elements)
						if hash == lastHash && hash != "" {
							stableCount++
						} else {
							stableCount = 0
							lastHash = hash
						}
						if stableCount >= 1 {
							break
						}
					} else if flowConditionMatchesElements(elements, FlowCondition{ID: waitID, Text: waitText, Value: waitValue}) {
						break
					}
				}
			}
			if time.Now().After(deadline) {
				diagnostics := map[string]string{"action": command, "timeout": timeout.String()}
				if path := c.captureFastPathFailure(ctx, cfg); path != "" {
					diagnostics["screenshot"] = path
				}
				if len(lastElements) > 0 {
					if run, runErr := c.currentOrNewRun(); runErr == nil {
						treePath := filepath.Join(run.Dir, "failure-fast-path-tree.json")
						data, _ := json.MarshalIndent(Compact(lastElements), "", "  ")
						_ = os.WriteFile(treePath, data, 0o644)
						diagnostics["tree"] = treePath
					}
					var candidates []string
					for _, element := range AgentTree(lastElements, AgentTreeOptions{}) {
						if text := firstNonEmpty(element.ID, element.Label, element.Title); text != "" {
							candidates = append(candidates, text)
						}
						if len(candidates) == 5 {
							break
						}
					}
					diagnostics["candidates"] = strings.Join(candidates, ",")
				}
				return Fail("wait_timeout", diagnostics).Write(c.Stdout)
			}
			time.Sleep(200 * time.Millisecond)
		}
		fields["wait"] = "ok"
		fields["wait_timeout"] = timeout.String()
	}
	if observe == "none" {
		return c.OK(command, fields).Write(c.Stdout)
	}
	described, err := c.describeUITree(ctx, cfg, "auto", false)
	if err != nil || described.Result.Err != nil {
		return Fail("tree_failed", fields).Write(c.Stdout)
	}
	elements := ExtractElementsRaw(described.Result.Stdout)
	fields["observe"] = observe
	fields["nodes"] = strconv.Itoa(len(elements))
	if err := c.OK(command, fields).Write(c.Stdout); err != nil {
		return err
	}
	switch observe {
	case "agent":
		return writeAgentElementLines(c.Stdout, AgentTree(elements, AgentTreeOptions{}))
	case "tree":
		return writeElementLines(c.Stdout, Compact(elements))
	case "delta":
		run, _ := c.resolveRun("")
		previous := loadFastPathTree(run)
		delta := TreeDiff(previous, elements)
		data, _ := json.Marshal(delta)
		_, err := fmt.Fprintf(c.Stdout, "tree_delta json=%s\n", quoteIfNeeded(string(data)))
		if run.Dir != "" {
			_ = saveFastPathTree(run, elements)
		}
		return err
	}
	return nil
}

func hashElements(elements []Element) string {
	data, _ := json.Marshal(elements)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (c CLI) captureFastPathFailure(ctx context.Context, cfg Config) string {
	run, err := c.currentOrNewRun()
	if err != nil {
		return ""
	}
	path := filepath.Join(run.Dir, "failure-fast-path.png")
	result, err := c.captureScreenshot(ctx, cfg, path)
	if err != nil || result.Err != nil {
		return ""
	}
	return path
}

func loadFastPathTree(run RunState) []Element {
	if run.Dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(run.Dir, "fast-path-tree.json"))
	if err != nil {
		return nil
	}
	var elements []Element
	_ = json.Unmarshal(data, &elements)
	return elements
}

func saveFastPathTree(run RunState, elements []Element) error {
	data, err := json.Marshal(elements)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(run.Dir, "fast-path-tree.json"), data, 0o644)
}

func (c CLI) diagnoseTextTapFailure(ctx context.Context, cfg Config, text, stderr string) (map[string]string, bool) {
	if text == "" {
		return nil, false
	}
	target := targetFromConfig(cfg)
	driver, _, err := c.router().Route(ctx, drivers.CapTreeAX, target, "")
	if err != nil {
		return nil, false
	}
	treeDriver, ok := driver.(drivers.TreeDriver)
	if !ok {
		return nil, false
	}
	tree, err := treeDriver.Tree(ctx, target, drivers.TreeSpec{})
	if err != nil {
		return nil, false
	}
	labelMatches := 0
	valueMatches := 0
	for _, el := range ExtractElements(string(tree.JSON)) {
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
	run, err := c.resolveRun("")
	if err == nil && run.ID != "" {
		appendCommand(run, command, result)
	}
}

func (c CLI) uiType(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	if len(args) == 0 {
		return Fail("type_text_missing", nil).Write(c.Stdout)
	}
	_, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": c.preferDriverUsage()}).Write(c.Stdout)
	}
	selector, selectorErr := selectorFromCLI(args)
	if selectorErr != nil {
		return Fail("selector_invalid", map[string]string{"error": selectorErr.Error()}).Write(c.Stdout)
	}
	textArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--wait-") || args[i] == "--observe" || isSelectorCLIFlag(args[i]) {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
			}
			continue
		}
		textArgs = append(textArgs, args[i])
	}
	text := strings.Join(textArgs, " ")
	if text == "" {
		return Fail("type_text_missing", nil).Write(c.Stdout)
	}
	if !selector.IsZero() {
		var tapOutput bytes.Buffer
		if err := c.withStdout(&tapOutput).uiTap(ctx, opts, cfg, selectorCLIArgs(selector)); err != nil || strings.HasPrefix(strings.TrimSpace(tapOutput.String()), "fail ") {
			return Fail("ui_type_target_failed", map[string]string{"result": firstLine(tapOutput.String())}).Write(c.Stdout)
		}
	}
	target := targetFromConfig(cfg)
	// CapType: axe gana el desempate por coste igualmente, pero sin este
	// prefer el router tambien sondearia baguette, cuyo Probe ejecuta un
	// subproceso real (`baguette list --json`) en cada `mav ui type`.
	driver, _, err := c.router().Route(ctx, drivers.CapType, target, "axe")
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "axe", "next": "mav setup --install axe"}).Write(c.Stdout)
	}
	td, ok := driver.(drivers.TypeDriver)
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
	return c.writeFastPathResult(ctx, cfg, args, "ui.type", fields)
}

func isSelectorCLIFlag(value string) bool {
	switch value {
	case "--id", "--text", "--text-contains", "--text-starts-with", "--text-regex",
		"--value", "--value-contains", "--role", "--enabled", "--selected", "--focused",
		"--visible", "--index", "--bounds", "--near-id", "--near-text",
		"--near-direction", "--near-distance", "--where-json":
		return true
	default:
		return false
	}
}

func (c CLI) uiErase(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
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
	return c.OK("ui.erase", fields).Write(c.Stdout)
}

func (c CLI) uiHideKeyboard(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	_ = args
	if targetKind(cfg) != drivers.KindSim {
		return Fail("hide_keyboard_unsupported_on_device", map[string]string{"next": "device hide-keyboard is not supported; tap outside the field"}).Write(c.Stdout)
	}
	target := targetFromConfig(cfg)
	if err := baguetteHideKeyboard(ctx, c.router(), target); err != nil {
		return Fail("ui_hide_keyboard_failed", map[string]string{"driver": "baguette", "stderr": err.Error()}).Write(c.Stdout)
	}
	return c.OK("ui.hideKeyboard", map[string]string{"driver": "baguette"}).Write(c.Stdout)
}

func (c CLI) uiSwipe(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	prefer, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": c.preferDriverUsage()}).Write(c.Stdout)
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
	switch {
	case prefer != "" && prefer != "auto":
		// Un --prefer-driver explicito manda. Aceptar el flag y luego
		// sobreescribirlo con "axe" seria configuracion muerta del mismo tipo
		// que target_command_ignored existe para hacer visible: el usuario
		// pide un driver, mav dice que si, y corre otro sin decir nada.
		preferred = prefer
	case hasTool(cfg, "axe"):
		preferred = "axe"
	}
	// CapSwipe: mismo coste de Probe que CapType, mas cfg.Tools["axe"]=true sin
	// axe en PATH debe seguir siendo un error duro en vez de un fallback
	// silencioso a baguette/idb (ver TestUISwipePreferAxeDoesNotFallbackToIDB).
	driver, _, err := c.router().Route(ctx, drivers.CapSwipe, target, preferred)
	if err != nil {
		if prefer == "axe" {
			return Fail("tool_missing", map[string]string{"tool": "axe", "next": "install AXe or use --prefer-driver auto"}).Write(c.Stdout)
		}
		if prefer != "" && prefer != "auto" {
			return Fail("prefer_driver_unusable", map[string]string{"driver": prefer, "capability": string(drivers.CapSwipe), "next": "use --prefer-driver auto to let mav route ui swipe"}).Write(c.Stdout)
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
	return c.writeFastPathResult(ctx, cfg, args, "ui.swipe", fields)
}

func (c CLI) uiDoubleTap(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	if targetKind(cfg) != drivers.KindSim {
		return Fail("double_tap_unsupported_on_device", nil).Write(c.Stdout)
	}
	x, y, err := c.actionCoordinates(ctx, cfg, opts, args)
	if err != nil {
		return Fail(err.Error(), nil).Write(c.Stdout)
	}
	target := targetFromConfig(cfg)
	driver, _, err := c.router().Route(ctx, drivers.CapDoubleTap, target, "")
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "baguette"}).Write(c.Stdout)
	}
	advanced, ok := driver.(drivers.AdvancedGestureDriver)
	if !ok {
		return Fail("double_tap_unsupported", nil).Write(c.Stdout)
	}
	duration := parseFlowDuration(flagValue(args, "--duration"), 80*time.Millisecond)
	screenWidth, screenHeight := c.targetScreenSize(ctx, cfg)
	if sendErr := c.sendWorkerGestureWithRestart(target.UDID, tapWorkerEvents(x, y, screenWidth, screenHeight, int(duration.Milliseconds()), 2)); sendErr == nil {
		return c.writeFastPathResult(ctx, cfg, args, "ui.doubleTap", map[string]string{
			"x": strconv.Itoa(x), "y": strconv.Itoa(y), "driver": "baguette", "session": "worker",
		})
	}
	if err := advanced.DoubleTap(ctx, target, drivers.TapSpec{X: x, Y: y, Duration: int(duration.Milliseconds())}); err != nil {
		return Fail("ui_double_tap_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
	}
	return c.writeFastPathResult(ctx, cfg, args, "ui.doubleTap", map[string]string{
		"x": strconv.Itoa(x), "y": strconv.Itoa(y), "driver": driver.ID(), "session": "direct",
	})
}

func (c CLI) actionCoordinates(ctx context.Context, cfg Config, opts GlobalOptions, args []string) (int, int, error) {
	if xRaw, yRaw := flagValue(args, "--x"), flagValue(args, "--y"); xRaw != "" && yRaw != "" {
		x, xErr := strconv.Atoi(xRaw)
		y, yErr := strconv.Atoi(yRaw)
		if xErr != nil || yErr != nil {
			return 0, 0, fmt.Errorf("gesture_invalid")
		}
		return x, y, nil
	}
	selector, err := selectorFromCLI(args)
	if err != nil || selector.IsZero() {
		return 0, 0, fmt.Errorf("selector_invalid")
	}
	prefer, _ := c.normalizePreferDriver(opts.PreferDriver)
	matched, err := c.resolveSelector(ctx, cfg, selector, prefer)
	if err != nil {
		return 0, 0, err
	}
	x, y, width, height, ok := parseElementFrame(matched.Frame)
	if !ok {
		return 0, 0, fmt.Errorf("selector_frame_missing")
	}
	return int(x + width/2), int(y + height/2), nil
}

func (c CLI) uiDrag(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
		return Fail("drag_unsupported_on_device", nil).Write(c.Stdout)
	}
	sx, err1 := strconv.Atoi(flagValue(args, "--start-x"))
	sy, err2 := strconv.Atoi(flagValue(args, "--start-y"))
	ex, err3 := strconv.Atoi(flagValue(args, "--end-x"))
	ey, err4 := strconv.Atoi(flagValue(args, "--end-y"))
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return Fail("drag_invalid", map[string]string{"usage": "mav ui drag --start-x X --start-y Y --end-x X --end-y Y"}).Write(c.Stdout)
	}
	duration := parseFlowDuration(flagValue(args, "--duration"), 500*time.Millisecond)
	target := targetFromConfig(cfg)
	screenWidth, screenHeight := c.targetScreenSize(ctx, cfg)
	if sendErr := c.sendWorkerGestureWithRestart(target.UDID, dragPathWorkerEvents([]drivers.PathPoint{
		{X: sx, Y: sy}, {X: ex, Y: ey, DurationMs: int(duration.Milliseconds())},
	}, screenWidth, screenHeight)); sendErr == nil {
		return c.writeFastPathResult(ctx, cfg, args, "ui.drag", map[string]string{"driver": "baguette", "session": "worker"})
	}
	driver, _, err := c.router().Route(ctx, drivers.CapDrag, target, "")
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "baguette"}).Write(c.Stdout)
	}
	advanced, ok := driver.(drivers.AdvancedGestureDriver)
	if !ok {
		return Fail("drag_unsupported", nil).Write(c.Stdout)
	}
	if err := advanced.Drag(ctx, target, drivers.DragSpec{
		StartX: sx, StartY: sy, EndX: ex, EndY: ey, DurationMs: int(duration.Milliseconds()),
	}); err != nil {
		return Fail("ui_drag_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
	}
	return c.writeFastPathResult(ctx, cfg, args, "ui.drag", map[string]string{"driver": driver.ID(), "session": "direct"})
}

func (c CLI) uiDragPath(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
		return Fail("drag_path_unsupported_on_device", nil).Write(c.Stdout)
	}
	var points []drivers.PathPoint
	for _, raw := range repeatedFlagValues(args, "--point") {
		point, err := parseDragPathPoint(raw)
		if err != nil {
			return Fail("drag_path_invalid", map[string]string{"point": raw}).Write(c.Stdout)
		}
		points = append(points, point)
	}
	if len(points) < 2 {
		return Fail("drag_path_invalid", map[string]string{"usage": "mav ui dragPath --point x,y --point x,y:duration"}).Write(c.Stdout)
	}
	target := targetFromConfig(cfg)
	screenWidth, screenHeight := c.targetScreenSize(ctx, cfg)
	if err := c.sendWorkerGestureWithRestart(target.UDID, dragPathWorkerEvents(points, screenWidth, screenHeight)); err == nil {
		return c.writeFastPathResult(ctx, cfg, args, "ui.dragPath", map[string]string{
			"driver": "baguette", "session": "worker", "points": strconv.Itoa(len(points)),
		})
	}
	driver, _, err := c.router().Route(ctx, drivers.CapDragPath, target, "")
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "baguette"}).Write(c.Stdout)
	}
	advanced, ok := driver.(drivers.AdvancedGestureDriver)
	if !ok {
		return Fail("drag_path_unsupported", nil).Write(c.Stdout)
	}
	if err := advanced.DragPath(ctx, target, drivers.DragPathSpec{Points: points}); err != nil {
		return Fail("ui_drag_path_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
	}
	return c.writeFastPathResult(ctx, cfg, args, "ui.dragPath", map[string]string{
		"driver": driver.ID(), "points": strconv.Itoa(len(points)), "session": "direct",
	})
}

func (c CLI) sendWorkerGestureWithRestart(udid string, events []workerGestureEvent) error {
	run, err := c.resolveRun("")
	if err != nil {
		return err
	}
	if !workerPing(run) {
		if _, err := startRunWorker(c.Root, run); err != nil {
			return err
		}
	}
	if err := sendWorkerGesture(run, udid, events); err == nil {
		return nil
	}
	if err := sendWorkerGesture(run, udid, events); err == nil {
		return nil
	}
	_, _ = sendWorkerRequest(run, workerRequest{Command: "stop"})
	_ = os.Remove(workerSocket(run))
	if _, err := startRunWorker(c.Root, run); err != nil {
		return err
	}
	return sendWorkerGesture(run, udid, events)
}

// fallbackScreenWidth/Height are used only when the target's real point
// size cannot be determined (e.g. the AX tree is unavailable). They match
// the iPhone 17 Pro simulator and are not accurate for other targets.
const fallbackScreenWidth, fallbackScreenHeight = 402, 874

// targetScreenSizeCache memoizes the resolved screen size per target UDID
// for the life of the process. A target's screen size never changes, so
// this avoids paying for a full AX-tree fetch on every single doubleTap/
// drag/dragPath gesture in a flow - only the first such gesture per target
// fetches the tree; the rest reuse the cached value.
var (
	targetScreenSizeCacheMu sync.Mutex
	targetScreenSizeCache   = map[string][2]int{}
)

// targetScreenSize returns the target's screen size in points, derived from
// the maximum element bounds in the current accessibility tree (baguette
// input events must be sized to the real screen to land taps/drags
// correctly - it normalizes x/width and y/height). Falls back to the
// iPhone 17 Pro's point size if the tree cannot be fetched.
func (c CLI) targetScreenSize(ctx context.Context, cfg Config) (int, int) {
	udid := targetFromConfig(cfg).UDID
	if udid != "" {
		targetScreenSizeCacheMu.Lock()
		cached, ok := targetScreenSizeCache[udid]
		targetScreenSizeCacheMu.Unlock()
		if ok {
			return cached[0], cached[1]
		}
	}
	described, err := c.describeUITree(ctx, cfg, "auto", false)
	if err != nil || described.Result.Err != nil {
		return fallbackScreenWidth, fallbackScreenHeight
	}
	elements := ExtractElementsRaw(described.Result.Stdout)
	width, height := 0.0, 0.0
	for _, el := range elements {
		x, y, w, h, ok := parseElementFrame(el.Frame)
		if !ok {
			continue
		}
		width = math.Max(width, x+w)
		height = math.Max(height, y+h)
	}
	if width <= 0 || height <= 0 {
		return fallbackScreenWidth, fallbackScreenHeight
	}
	resolvedWidth, resolvedHeight := int(width), int(height)
	if udid != "" {
		targetScreenSizeCacheMu.Lock()
		targetScreenSizeCache[udid] = [2]int{resolvedWidth, resolvedHeight}
		targetScreenSizeCacheMu.Unlock()
	}
	return resolvedWidth, resolvedHeight
}

func tapWorkerEvents(x, y, width, height, durationMs, count int) []workerGestureEvent {
	var events []workerGestureEvent
	for i := 0; i < count; i++ {
		down, _ := json.Marshal(map[string]any{"type": "touch1-down", "x": x, "y": y, "width": width, "height": height})
		up, _ := json.Marshal(map[string]any{"type": "touch1-up", "x": x, "y": y, "width": width, "height": height})
		events = append(events, workerGestureEvent{JSON: string(down)})
		events = append(events, workerGestureEvent{JSON: string(up), DelayMs: durationMs})
	}
	return events
}

func dragPathWorkerEvents(points []drivers.PathPoint, width, height int) []workerGestureEvent {
	events := make([]workerGestureEvent, 0, len(points)+1)
	for i, point := range points {
		kind := "touch1-move"
		if i == 0 {
			kind = "touch1-down"
		}
		body, _ := json.Marshal(map[string]any{
			"type": kind, "x": point.X, "y": point.Y, "width": width, "height": height,
		})
		events = append(events, workerGestureEvent{JSON: string(body), DelayMs: point.DurationMs})
	}
	last := points[len(points)-1]
	body, _ := json.Marshal(map[string]any{
		"type": "touch1-up", "x": last.X, "y": last.Y, "width": width, "height": height,
	})
	events = append(events, workerGestureEvent{JSON: string(body)})
	return events
}

func repeatedFlagValues(args []string, name string) []string {
	var values []string
	for i := 0; i < len(args); i++ {
		if args[i] == name && i+1 < len(args) {
			values = append(values, args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(args[i], name+"=") {
			values = append(values, strings.TrimPrefix(args[i], name+"="))
		}
	}
	return values
}

func parseDragPathPoint(raw string) (drivers.PathPoint, error) {
	duration := 0
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) == 2 {
		parsed := parseFlowDuration(parts[1], 0)
		if parsed <= 0 {
			return drivers.PathPoint{}, fmt.Errorf("duration_invalid")
		}
		duration = int(parsed.Milliseconds())
	}
	coords := strings.Split(parts[0], ",")
	if len(coords) != 2 {
		return drivers.PathPoint{}, fmt.Errorf("coordinates_invalid")
	}
	x, xErr := strconv.Atoi(coords[0])
	y, yErr := strconv.Atoi(coords[1])
	if xErr != nil || yErr != nil {
		return drivers.PathPoint{}, fmt.Errorf("coordinates_invalid")
	}
	return drivers.PathPoint{X: x, Y: y, DurationMs: duration}, nil
}

func (c CLI) uiToggle(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	selector, err := selectorFromCLI(args)
	if err != nil || selector.IsZero() {
		return Fail("selector_invalid", nil).Write(c.Stdout)
	}
	desired := strings.ToLower(flagValue(args, "--state"))
	if desired != "" && desired != "on" && desired != "off" {
		return Fail("toggle_state_invalid", map[string]string{"state": desired}).Write(c.Stdout)
	}
	prefer, _ := c.normalizePreferDriver(opts.PreferDriver)
	matched, err := c.resolveSelector(ctx, cfg, selector, prefer)
	if err != nil {
		return Fail(err.Error(), selectorDiagnosticFields(selector, matched)).Write(c.Stdout)
	}
	current, known := elementToggleState(matched)
	if desired != "" && known && (desired == "on") == current {
		return c.writeFastPathResult(ctx, cfg, args, "ui.toggle", map[string]string{
			"state": desired, "changed": "false",
		})
	}
	x, y, width, height, ok := parseElementFrame(matched.Frame)
	if !ok {
		return Fail("selector_frame_missing", nil).Write(c.Stdout)
	}
	tapArgs := []string{"--x", strconv.Itoa(int(x + width/2)), "--y", strconv.Itoa(int(y + height/2))}
	var out bytes.Buffer
	if err := c.withStdout(&out).uiTap(ctx, opts, cfg, tapArgs); err != nil || strings.HasPrefix(strings.TrimSpace(out.String()), "fail ") {
		return Fail("ui_toggle_failed", map[string]string{"result": firstLine(out.String())}).Write(c.Stdout)
	}
	return c.writeFastPathResult(ctx, cfg, args, "ui.toggle", map[string]string{
		"state": desired, "changed": "true",
	})
}

func elementToggleState(el Element) (bool, bool) {
	for _, raw := range []string{el.Value, el.Selected} {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "on", "yes", "checked", "selected":
			return true, true
		case "0", "false", "off", "no", "unchecked":
			return false, true
		}
	}
	return false, false
}

func (c CLI) uiPress(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
		return Fail("press_unsupported_on_device", nil).Write(c.Stdout)
	}
	button := strings.ToLower(flagValue(args, "--button"))
	mapping := map[string]drivers.HardwareButton{
		"home": drivers.BtnHome, "lock": drivers.BtnLock,
		"volume_up": drivers.BtnVolumeUp, "volume_down": drivers.BtnVolumeDown,
	}
	btn, ok := mapping[button]
	if !ok {
		return Fail("button_invalid", map[string]string{"button": button}).Write(c.Stdout)
	}
	target := targetFromConfig(cfg)
	driver, _, err := c.router().Route(ctx, drivers.CapHardwareBtn, target, "")
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "baguette"}).Write(c.Stdout)
	}
	hardware, ok := driver.(drivers.HardwareButtonDriver)
	if !ok {
		return Fail("press_unsupported", nil).Write(c.Stdout)
	}
	if err := hardware.PressButton(ctx, target, btn); err != nil {
		return Fail("ui_press_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
	}
	return c.OK("ui.press", map[string]string{"button": button, "driver": driver.ID()}).Write(c.Stdout)
}

func (c CLI) uiLongPress(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
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
	return c.OK("ui.longPress", fields).Write(c.Stdout)
}

func (c CLI) uiPinch(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
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
	return c.OK("ui.pinch", fields).Write(c.Stdout)
}

func (c CLI) uiRotate(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
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
	return c.OK("ui.rotate", fields).Write(c.Stdout)
}

func (c CLI) uiTwoFingerPan(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
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
	return c.OK("ui.twoFingerPan", fields).Write(c.Stdout)
}

func (c CLI) uiActions(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = opts
	if targetKind(cfg) != drivers.KindSim {
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
	return c.OK("ui.actions", map[string]string{"driver": "baguette", "file": path}).Write(c.Stdout)
}

func (c CLI) writeGestureError(err error) error {
	return Fail("ui_gesture_failed", map[string]string{"driver": "baguette", "stderr": err.Error()}).Write(c.Stdout)
}

func (c CLI) uiWait(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	_ = cfg
	prefer, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": c.preferDriverUsage()}).Write(c.Stdout)
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
	return c.OK("ui.wait", fields).Write(c.Stdout)
}

func (c CLI) uiScrollUntil(ctx context.Context, opts GlobalOptions, args []string) error {
	prefer, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": c.preferDriverUsage()}).Write(c.Stdout)
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
	return c.OK("ui.scrollUntil", fields).Write(c.Stdout)
}

func (c CLI) app(ctx context.Context, opts GlobalOptions, args []string) error {
	_ = opts
	if len(args) == 0 {
		return Fail("app_command_missing", map[string]string{"usage": "mav app list|kill"}).Write(c.Stdout)
	}
	cfg := c.mustLoadConfig()
	target := targetFromConfig(cfg)
	switch args[0] {
	case "list":
		driver, _, err := c.router().Route(ctx, drivers.CapAppList, target, "")
		if err != nil {
			return Fail("app_list_unsupported", nil).Write(c.Stdout)
		}
		utility, ok := driver.(drivers.DeviceUtilityDriver)
		if !ok {
			return Fail("app_list_unsupported", nil).Write(c.Stdout)
		}
		raw, err := utility.ListApps(ctx, target)
		if err != nil {
			return Fail("app_list_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		if opts.Raw {
			_, err = fmt.Fprint(c.Stdout, raw)
			return err
		}
		return c.OK("app.list", map[string]string{"driver": driver.ID(), "bytes": strconv.Itoa(len(raw))}).Write(c.Stdout)
	case "kill":
		bundle := flagValue(args[1:], "--bundle")
		if bundle == "" {
			bundle = cfg.BundleID
		}
		driver, _, err := c.router().Route(ctx, drivers.CapTerminate, target, "")
		if err != nil {
			return Fail("app_kill_unsupported", nil).Write(c.Stdout)
		}
		utility, ok := driver.(drivers.DeviceUtilityDriver)
		if !ok {
			return Fail("app_kill_unsupported", nil).Write(c.Stdout)
		}
		if err := utility.Terminate(ctx, target, bundle); err != nil {
			return Fail("app_kill_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		return c.OK("app.kill", map[string]string{"bundle": bundle, "driver": driver.ID()}).Write(c.Stdout)
	default:
		return Fail("app_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
}

func (c CLI) openURL(ctx context.Context, opts GlobalOptions, args []string) error {
	_ = opts
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return Fail("url_missing", map[string]string{"usage": "mav openURL URL"}).Write(c.Stdout)
	}
	cfg := c.mustLoadConfig()
	target := targetFromConfig(cfg)
	driver, _, err := c.router().Route(ctx, drivers.CapOpenURL, target, "")
	if err != nil {
		return Fail("open_url_unsupported", nil).Write(c.Stdout)
	}
	utility, ok := driver.(drivers.DeviceUtilityDriver)
	if !ok {
		return Fail("open_url_unsupported", nil).Write(c.Stdout)
	}
	if err := utility.OpenURL(ctx, target, args[0]); err != nil {
		return Fail("open_url_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
	}
	return c.OK("openURL", map[string]string{"url": args[0], "driver": driver.ID()}).Write(c.Stdout)
}

func (c CLI) location(ctx context.Context, opts GlobalOptions, args []string) error {
	_ = opts
	if len(args) == 0 {
		return Fail("location_command_missing", map[string]string{"usage": "mav location set LAT LON | reset"}).Write(c.Stdout)
	}
	cfg := c.mustLoadConfig()
	target := targetFromConfig(cfg)
	if targetKind(cfg) == drivers.KindDevice && len(args) > 0 && args[0] == "reset" {
		return Fail("location_reset_unsupported_on_device", nil).Write(c.Stdout)
	}
	driver, _, err := c.router().Route(ctx, drivers.CapLocation, target, "")
	if err != nil {
		return Fail("location_unsupported", nil).Write(c.Stdout)
	}
	utility, ok := driver.(drivers.DeviceUtilityDriver)
	if !ok {
		return Fail("location_unsupported", nil).Write(c.Stdout)
	}
	switch args[0] {
	case "set":
		if len(args) < 3 {
			return Fail("location_invalid", nil).Write(c.Stdout)
		}
		latitude, latErr := strconv.ParseFloat(args[1], 64)
		longitude, lonErr := strconv.ParseFloat(args[2], 64)
		if latErr != nil || lonErr != nil || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
			return Fail("location_invalid", nil).Write(c.Stdout)
		}
		if err := utility.SetLocation(ctx, target, latitude, longitude); err != nil {
			return Fail("location_set_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		return c.OK("location.set", map[string]string{"latitude": args[1], "longitude": args[2], "driver": driver.ID()}).Write(c.Stdout)
	case "reset":
		if err := utility.ResetLocation(ctx, target); err != nil {
			code := "location_reset_failed"
			if targetKind(cfg) == drivers.KindDevice {
				code = "location_reset_unsupported_on_device"
			}
			return Fail(code, map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		return c.OK("location.reset", map[string]string{"driver": driver.ID()}).Write(c.Stdout)
	default:
		return Fail("location_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
}

func (c CLI) clipboard(ctx context.Context, opts GlobalOptions, args []string) error {
	_ = opts
	if targetKind(c.mustLoadConfig()) != drivers.KindSim {
		return Fail("clipboard_unsupported_on_device", nil).Write(c.Stdout)
	}
	if len(args) == 0 {
		return Fail("clipboard_command_missing", map[string]string{"usage": "mav clipboard copy TEXT | read"}).Write(c.Stdout)
	}
	cfg := c.mustLoadConfig()
	target := targetFromConfig(cfg)
	driver, _, err := c.router().Route(ctx, drivers.CapClipboard, target, "")
	if err != nil {
		return Fail("clipboard_unsupported", nil).Write(c.Stdout)
	}
	utility, ok := driver.(drivers.DeviceUtilityDriver)
	if !ok {
		return Fail("clipboard_unsupported", nil).Write(c.Stdout)
	}
	switch args[0] {
	case "copy":
		if len(args) < 2 {
			return Fail("clipboard_text_missing", nil).Write(c.Stdout)
		}
		text := strings.Join(args[1:], " ")
		if err := utility.ClipboardWrite(ctx, target, text); err != nil {
			return Fail("clipboard_copy_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		return c.OK("clipboard.copy", map[string]string{"chars": strconv.Itoa(len(text)), "driver": driver.ID()}).Write(c.Stdout)
	case "read":
		text, err := utility.ClipboardRead(ctx, target)
		if err != nil {
			return Fail("clipboard_read_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
		}
		if opts.Raw {
			_, err = fmt.Fprint(c.Stdout, text)
			return err
		}
		return c.OK("clipboard.read", map[string]string{"value": text, "driver": driver.ID()}).Write(c.Stdout)
	default:
		return Fail("clipboard_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
}

func (c CLI) capture(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTools(&cfg)
	c.resolveConfigTarget(&cfg)
	run, err := c.resolveRun(flagValue(args, "--run"))
	if err != nil {
		run, err = NewProjectRunState(c.Root)
		if err != nil {
			return err
		}
		_ = SaveCurrentRun(c.Root, run)
	}
	prefer, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": c.preferDriverUsage()}).Write(c.Stdout)
	}
	path := uniqueCapturePath(run, flagValue(args, "--name"))
	result, err := c.captureScreenshotWith(ctx, cfg, path, routerPrefer(prefer))
	if err != nil {
		return Fail("tool_missing", map[string]string{"tool": "axe|idb|xcrun"}).Write(c.Stdout)
	}
	if result.Err != nil {
		fields := map[string]string{"stderr": firstLine(result.Stderr)}
		addSandboxNext(fields, result.Stderr)
		return Fail("capture_failed", fields).Write(c.Stdout)
	}
	_ = os.WriteFile(filepath.Join(run.Dir, "latest_capture.txt"), []byte(path+"\n"), 0o644)
	return c.OK("capture", map[string]string{"file": path, "run": run.ID}).Write(c.Stdout)
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
	return c.captureScreenshotWith(ctx, cfg, path, "")
}

// captureScreenshotWith es captureScreenshot honrando --prefer-driver. La
// version sin prefer existe porque la mayoria de llamadas son internas (fast
// path de fallo, pasos de un flow) y ahi no hay flag del usuario que respetar.
func (c CLI) captureScreenshotWith(ctx context.Context, cfg Config, path, prefer string) (CommandResult, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CommandResult{}, err
	}
	target := targetFromConfig(cfg)
	// Solo se nombra driver para device, donde idb es el canonico por decision
	// explicita. Para lo demas decide el router por coste: la cascada de
	// hasTool que habia aqui replicaba eso peor, porque miraba presencia en
	// PATH en vez de salud, y ademas dejaba fuera a cualquier target que no
	// fuera iOS -- en macOS acababa nombrando a axe, que ni siquiera provee la
	// capacidad alli, y la captura moria con capture_tool_missing.
	if prefer == "" && targetKind(cfg) == drivers.KindDevice {
		prefer = "idb"
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
	if os.Getenv("MAV_MATRIX_CHILD") == "" && len(repeatedFlagValues(args[1:], "--target")) > 0 {
		return c.runFlowMatrix(ctx, opts, args)
	}
	flow, err := LoadFlow(args[0])
	if err != nil {
		return Fail("flow_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	// runFlow never reads .mav/current-run: an explicit --run reuses that run
	// (e.g. a second flow continuing evidence collection on a run a caller
	// already opened); otherwise it always creates a fresh run, so two
	// concurrent `mav run` invocations in the same repo never adopt each
	// other's run. The run is bound to c so every step this flow dispatches
	// (including `open`) resolves against it instead of the disk pointer.
	var run RunState
	requestedRunID := flagValue(args[1:], "--run")
	if requestedRunID != "" {
		run, err = c.resolveRun(requestedRunID)
		// LoadRun never stats an explicit id: it happily returns a RunState
		// pointing at a directory that was never created (a typo'd id,
		// a --run for a run that was never opened). Left unchecked, the
		// flow would "succeed" against a directory nothing ever writes to
		// -- exit 0, zero logs, zero commands.jsonl, zero run.json -- and
		// still clobber .mav/current-run with the bogus id on the way out.
		if err == nil {
			if _, statErr := os.Stat(run.Dir); statErr != nil {
				return Fail("run_not_found", map[string]string{"run": requestedRunID}).Write(c.Stdout)
			}
		}
	} else {
		run, err = NewProjectRunState(c.Root)
	}
	if err != nil {
		return err
	}
	c = c.withRun(run)
	// Publish current-run as soon as this run exists, not only on a clean
	// exit: a flow killed mid-run (Ctrl-C, SIGKILL, a hung step) must still
	// leave a pointer manual follow-up commands (mav logs/stop without
	// --run) can find, matching the pre-M1 behavior where `open` published
	// the pointer immediately. Guarded by publishCurrentRunIfSafe so it
	// never steals the pointer from a different run that's still live (see
	// its doc comment).
	c.publishCurrentRunIfSafe(run)
	bindings := flowExecBindings{}
	if err := bindFlowParameters(flow, args[1:], bindings); err != nil {
		return Fail("flow_params_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	cfg := c.mustLoadConfig()
	bindFlowTarget(cfg, bindings)
	stopTargetCommandKeepAlive := c.startTargetCommandKeepAlive(run, cfg, c.targetCommandInEffectForRun())
	defer stopTargetCommandKeepAlive()
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
			runData, _ := json.MarshalIndent(map[string]any{
				"id": run.ID, "name": flow.Name, "status": "failed", "step": index + 1,
				"action": step.Action, "code": err.Error(), "elapsed": time.Since(start).String(),
				"outputs": flowVariableOutputs(bindings),
			}, "", "  ")
			_ = os.WriteFile(filepath.Join(run.Dir, "run.json"), runData, 0o644)
			c.cleanupFailedFlow(ctx, run, failFields)
			return Fail(err.Error(), failFields).Write(c.Stdout)
		}
		appendFlowStep(run, index+1, step.Action, elapsed, "ok", fields)
	}
	for _, step := range flow.Steps {
		if step.Action == "open" && step.Params["timeControl"] == "true" && step.Params["preserve"] != "true" {
			_ = c.withStdout(io.Discard).timeControl(ctx, GlobalOptions{}, []string{"reset"})
			break
		}
	}
	_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
	outputs := flowVariableOutputs(bindings)
	if len(outputs) > 0 {
		data, _ := json.MarshalIndent(outputs, "", "  ")
		_ = os.WriteFile(filepath.Join(run.Dir, "outputs.json"), data, 0o644)
	}
	runData, _ := json.MarshalIndent(map[string]any{
		"id": run.ID, "name": flow.Name, "status": "passed", "steps": len(flow.Steps),
		"elapsed": time.Since(start).String(), "outputs": outputs,
	}, "", "  ")
	_ = os.WriteFile(filepath.Join(run.Dir, "run.json"), runData, 0o644)
	fields := map[string]string{"name": flow.Name, "run": run.ID, "dir": run.Dir, "steps": strconv.Itoa(len(flow.Steps)), "elapsed": time.Since(start).String()}
	if len(outputs) > 0 {
		fields["outputs"] = filepath.Join(run.Dir, "outputs.json")
	}
	// Re-publish current-run now that the run is fully done (already
	// published on entry above; see that comment). Reasserts ownership of
	// the pointer in case whatever it named when this run started has since
	// gone quiet, without ever stealing it from a run that's still live.
	c.publishCurrentRunIfSafe(run)
	return c.OK("run", fields).Write(c.Stdout)
}

func bindFlowParameters(flow Flow, args []string, bindings flowExecBindings) error {
	values := map[string]string{}
	for name, param := range flow.Params {
		values[name] = param.Default
	}
	for i := 0; i < len(args); i++ {
		var raw string
		switch {
		case args[i] == "--param":
			if i+1 >= len(args) {
				return fmt.Errorf("param_value_missing")
			}
			i++
			raw = args[i]
		case strings.HasPrefix(args[i], "--param="):
			raw = strings.TrimPrefix(args[i], "--param=")
		default:
			continue
		}
		pair := strings.SplitN(raw, "=", 2)
		if len(pair) != 2 || pair[0] == "" {
			return fmt.Errorf("param_invalid")
		}
		if _, ok := flow.Params[pair[0]]; !ok {
			return fmt.Errorf("param_unknown name=%s", pair[0])
		}
		values[pair[0]] = pair[1]
	}
	for name, param := range flow.Params {
		if param.Required && values[name] == "" {
			return fmt.Errorf("param_required name=%s", name)
		}
		bindings["params."+name] = newFlowExecBinding(values[name])
	}
	return nil
}

func bindFlowTarget(cfg Config, bindings flowExecBindings) {
	target := targetFromConfig(cfg)
	bindings["target.udid"] = newFlowExecBinding(target.UDID)
	bindings["target.name"] = newFlowExecBinding(target.Name)
	bindings["target.runtime"] = newFlowExecBinding(target.Runtime)
	bindings["target.kind"] = newFlowExecBinding(string(target.Kind))
}

func flowVariableOutputs(bindings flowExecBindings) map[string]string {
	out := map[string]string{}
	for key, binding := range bindings {
		if strings.HasPrefix(key, "vars.") {
			out[strings.TrimPrefix(key, "vars.")] = binding.Raw
		}
	}
	return out
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

func substituteExecBindingsInAfter(after *FlowAfter, bindings flowExecBindings) (*FlowAfter, error) {
	if after == nil {
		return nil, nil
	}
	prepared := *after
	var err error
	prepared.Observe, err = substituteExecBindings(after.Observe, bindings)
	if err != nil {
		return nil, err
	}
	if after.Wait != nil {
		wait := *after.Wait
		for _, field := range []*string{&wait.ID, &wait.Text, &wait.TextContains, &wait.Value, &wait.ChangedFrom, &wait.Timeout} {
			*field, err = substituteExecBindings(*field, bindings)
			if err != nil {
				return nil, err
			}
		}
		for i := range wait.Any {
			wait.Any[i], err = substituteExecBindingsInCondition(wait.Any[i], bindings)
			if err != nil {
				return nil, err
			}
		}
		for i := range wait.All {
			wait.All[i], err = substituteExecBindingsInCondition(wait.All[i], bindings)
			if err != nil {
				return nil, err
			}
		}
		if wait.Not != nil {
			value, subErr := substituteExecBindingsInCondition(*wait.Not, bindings)
			if subErr != nil {
				return nil, subErr
			}
			wait.Not = &value
		}
		prepared.Wait = &wait
	}
	return &prepared, nil
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
	prepared.Where, err = substituteBindingsInSelector(step.Where, bindings)
	if err != nil {
		return FlowStep{}, err
	}
	prepared.After, err = substituteExecBindingsInAfter(step.After, bindings)
	if err != nil {
		return FlowStep{}, err
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
	if step.All != nil {
		prepared.All = make([]FlowCondition, len(step.All))
	}
	for i := range step.All {
		prepared.All[i], err = substituteExecBindingsInCondition(step.All[i], bindings)
		if err != nil {
			return FlowStep{}, err
		}
	}
	if step.Not != nil {
		value, subErr := substituteExecBindingsInCondition(*step.Not, bindings)
		if subErr != nil {
			return FlowStep{}, subErr
		}
		prepared.Not = &value
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
	selector, err := substituteBindingsInSelector(condition.Selector(), bindings)
	if err != nil {
		return FlowCondition{}, err
	}
	condition.ID = selector.ID
	condition.Text = selector.Text
	condition.TextContains = selector.TextContains
	condition.TextStartsWith = selector.TextStartsWith
	condition.TextRegex = selector.TextRegex
	condition.Value = selector.Value
	condition.ValueContains = selector.ValueContains
	condition.Role = selector.Role
	condition.Bounds = selector.Bounds
	condition.Near = selector.Near
	condition.ParentOf = selector.ParentOf
	return condition, nil
}

func substituteBindingsInSelector(selector Selector, bindings flowExecBindings) (Selector, error) {
	var err error
	fields := []*string{
		&selector.ID, &selector.Text, &selector.TextContains, &selector.TextStartsWith,
		&selector.TextRegex, &selector.Value, &selector.ValueContains, &selector.Role,
		&selector.Bounds,
	}
	for _, field := range fields {
		*field, err = substituteExecBindings(*field, bindings)
		if err != nil {
			return Selector{}, err
		}
	}
	if selector.Near != nil {
		near := *selector.Near
		near.Where, err = substituteBindingsInSelector(near.Where, bindings)
		if err != nil {
			return Selector{}, err
		}
		selector.Near = &near
	}
	if selector.ParentOf != nil {
		parent, subErr := substituteBindingsInSelector(*selector.ParentOf, bindings)
		if subErr != nil {
			return Selector{}, subErr
		}
		selector.ParentOf = &parent
	}
	return selector, nil
}

func substituteExecBindings(value string, bindings flowExecBindings) (string, error) {
	out := value
	searchFrom := 0
	for {
		if searchFrom >= len(out) {
			return out, nil
		}
		start := strings.Index(out[searchFrom:], "${")
		if start < 0 {
			return out, nil
		}
		start += searchFrom
		end := strings.Index(out[start:], "}")
		if end < 0 {
			return "", fmt.Errorf("exec_binding_invalid")
		}
		end += start
		expr := out[start+len("${") : end]
		replacement, known, err := resolveFlowBinding(expr, bindings)
		if err != nil {
			return "", err
		}
		if !known {
			// Not a recognized flow binding (e.g. a shell variable meant
			// for an exec step) - leave the literal ${...} untouched.
			searchFrom = end + 1
			continue
		}
		out = out[:start] + replacement + out[end+1:]
		searchFrom = start + len(replacement)
	}
}

// resolveFlowBinding resolves ${expr} against known flow bindings. The
// second return value reports whether expr was recognized as a binding at
// all; unrecognized expressions (e.g. plain shell variables used in an exec
// step) are left untouched by the caller instead of failing the flow.
func resolveFlowBinding(expr string, bindings flowExecBindings) (string, bool, error) {
	if strings.HasPrefix(expr, "exec.") {
		value, err := resolveExecBinding(strings.TrimPrefix(expr, "exec."), bindings)
		return value, true, err
	}
	if binding, ok := bindings[expr]; ok {
		return binding.Raw, true, nil
	}
	if strings.HasPrefix(expr, "params.") || strings.HasPrefix(expr, "vars.") || strings.HasPrefix(expr, "target.") {
		return "", true, fmt.Errorf("flow_binding_missing name=%s", expr)
	}
	return "", false, nil
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

func (c CLI) flowStepPreferDriver(opts GlobalOptions, step FlowStep) (string, error) {
	prefer := opts.PreferDriver
	if step.Params != nil && step.Params["prefer-driver"] != "" {
		prefer = step.Params["prefer-driver"]
	}
	return c.normalizePreferDriver(prefer)
}

func (c CLI) executeFlowStepBoundWithOptions(ctx context.Context, opts GlobalOptions, run RunState, index int, step FlowStep, bindings flowExecBindings) (map[string]string, error) {
	policy := step.OnFailure
	if policy.Strategy == "" {
		if step.Params["optional"] == "true" {
			policy.Strategy = "skip"
		} else {
			policy.Strategy = "abort"
		}
	}
	attempts := 1
	if policy.Strategy == "retry" {
		attempts = policy.MaxAttempts
		if attempts <= 0 {
			attempts = 3
		}
	}
	delay := parseFlowDuration(policy.Delay, 300*time.Millisecond)
	if delay < 0 {
		delay = 0
	}
	backoff := policy.Backoff
	if backoff == 0 {
		backoff = 1
	}
	var fields map[string]string
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		fields, err = c.executeFlowStepBoundOnceWithOptions(ctx, opts, run, index, step, bindings)
		if err == nil {
			if fields == nil {
				fields = map[string]string{}
			}
			fields["attempts"] = strconv.Itoa(attempt)
			if step.After != nil {
				after, afterErr := substituteExecBindingsInAfter(step.After, bindings)
				if afterErr != nil {
					return fields, afterErr
				}
				afterFields, afterErr := c.executeFlowAfter(ctx, run, after)
				for key, value := range afterFields {
					fields["after_"+key] = value
				}
				err = afterErr
			}
			if err == nil {
				return fields, nil
			}
		}
		if policy.Strategy == "skip" {
			if fields == nil {
				fields = map[string]string{}
			}
			fields["skipped"] = "true"
			fields["error"] = err.Error()
			return fields, nil
		}
		if policy.Strategy != "retry" || !retryPolicyMatches(policy, err) || attempt == attempts {
			break
		}
		appendFlowStep(run, index, step.Action+".retry", 0, "retry", map[string]string{
			"attempt": strconv.Itoa(attempt), "code": err.Error(),
		})
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fields, ctx.Err()
		case <-timer.C:
		}
		delay = time.Duration(float64(delay) * backoff)
	}
	return fields, err
}

func retryPolicyMatches(policy FailurePolicy, err error) bool {
	if err == nil || len(policy.RetryOn) == 0 {
		return err != nil
	}
	for _, code := range policy.RetryOn {
		if err.Error() == code || strings.Contains(err.Error(), code) {
			return true
		}
	}
	return false
}

func (c CLI) executeFlowStepBoundOnceWithOptions(ctx context.Context, opts GlobalOptions, run RunState, index int, step FlowStep, bindings flowExecBindings) (map[string]string, error) {
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
	case "extract":
		fields, value, err := c.extractFlowValue(ctx, prepared)
		if err != nil {
			return fields, err
		}
		name := prepared.Params["name"]
		if !validExecBindingName(name) {
			return fields, fmt.Errorf("extract_name_invalid")
		}
		bindings["vars."+name] = newFlowExecBinding(value)
		fields["name"] = name
		return fields, nil
	default:
		return c.executeFlowStepWithOptions(ctx, opts, run, index, prepared)
	}
}

func (c CLI) extractFlowValue(ctx context.Context, step FlowStep) (map[string]string, string, error) {
	cfg := c.mustLoadConfig()
	described, err := c.describeUITree(ctx, cfg, "auto", false)
	if err != nil || described.Result.Err != nil {
		return nil, "", fmt.Errorf("tree_failed")
	}
	matches, err := MatchElements(ExtractElementsRaw(described.Result.Stdout), flowStepSelector(step))
	if err != nil {
		return nil, "", err
	}
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("selector_not_found")
	}
	if len(matches) > 1 {
		return map[string]string{"matches": strconv.Itoa(len(matches))}, "", fmt.Errorf("selector_ambiguous")
	}
	el := matches[0]
	var value string
	switch step.Params["field"] {
	case "", "text":
		value = elementText(el)
	case "id":
		value = el.ID
	case "value":
		value = el.Value
	case "role":
		value = el.Role
	case "frame":
		value = el.Frame
	default:
		return nil, "", fmt.Errorf("extract_field_invalid")
	}
	if pattern := step.Params["regex"]; pattern != "" {
		rx, compileErr := regexp.Compile(pattern)
		if compileErr != nil {
			return nil, "", fmt.Errorf("extract_regex_invalid")
		}
		match := rx.FindStringSubmatch(value)
		if len(match) == 0 {
			return nil, "", fmt.Errorf("extract_regex_no_match")
		}
		if len(match) > 1 {
			value = match[1]
		} else {
			value = match[0]
		}
	}
	return map[string]string{"field": step.Params["field"], "value": value}, value, nil
}

func flowStepSelector(step FlowStep) Selector {
	if !step.Where.IsZero() {
		return step.Where
	}
	return selectorFromLegacy(step.Params)
}

func (c CLI) executeFlowAfter(ctx context.Context, run RunState, after *FlowAfter) (map[string]string, error) {
	fields := map[string]string{}
	if after == nil {
		return fields, nil
	}
	if after.Wait != nil {
		params := map[string]string{
			"id": after.Wait.ID, "text": after.Wait.Text,
			"value": after.Wait.Value, "changedFrom": after.Wait.ChangedFrom,
			"timeout": after.Wait.Timeout,
		}
		all := after.Wait.All
		if after.Wait.TextContains != "" {
			all = append(all, FlowCondition{TextContains: after.Wait.TextContains})
		}
		if after.Wait.Stable {
			all = append(all, FlowCondition{Stable: true})
		}
		if err := c.waitForConditionSet(ctx, params, after.Wait.Any, all, after.Wait.Not, "auto"); err != nil {
			return fields, err
		}
		fields["wait"] = "ok"
	}
	switch after.Observe {
	case "", "none":
	case "agent", "tree", "delta":
		cfg := c.mustLoadConfig()
		described, err := c.describeUITree(ctx, cfg, "auto", false)
		if err != nil || described.Result.Err != nil {
			return fields, fmt.Errorf("tree_failed")
		}
		elements := ExtractElementsRaw(described.Result.Stdout)
		fields["observe"] = after.Observe
		fields["nodes"] = strconv.Itoa(len(elements))
		if after.Observe == "delta" {
			previous := loadFastPathTree(run)
			delta := TreeDiff(previous, elements)
			data, _ := json.Marshal(delta)
			path := filepath.Join(run.Dir, fmt.Sprintf("step-%d-delta.json", len(LoadEvidenceSteps(run))+1))
			_ = os.WriteFile(path, data, 0o644)
			fields["delta"] = path
			_ = saveFastPathTree(run, elements)
		}
	default:
		return fields, fmt.Errorf("observe_invalid")
	}
	return fields, nil
}

func (c CLI) executeFlowStep(ctx context.Context, run RunState, index int, step FlowStep) (map[string]string, error) {
	return c.executeFlowStepWithOptions(ctx, GlobalOptions{}, run, index, step)
}

func (c CLI) executeFlowStepWithOptions(ctx context.Context, opts GlobalOptions, run RunState, index int, step FlowStep) (map[string]string, error) {
	prefer, preferErr := c.flowStepPreferDriver(opts, step)
	if preferErr != nil {
		return copyParams(step.Params), preferErr
	}
	switch step.Action {
	case "open":
		args := flowArgs(step.Params, "--device", "device", "--ios", "ios", "--udid", "udid", "--locale", "locale", "--language", "language", "--fixture", "fixture")
		if step.Params["clearState"] == "true" {
			args = append(args, "--clear-state")
		}
		if step.Params["timeControl"] == "true" {
			args = append(args, "--time-control")
			if step.Params["preserve"] == "true" {
				args = append(args, "--preserve-time")
			}
		}
		var out bytes.Buffer
		err := c.withStdout(&out).open(ctx, GlobalOptions{}, args)
		return map[string]string{"run": run.ID}, commandOutputErr(err, out.String(), "open_failed")
	case "when":
		return c.executeWhenFlowStepWithOptions(ctx, opts, run, index, step)
	case "whileNotVisible":
		return c.executeWhileNotVisibleFlowStepBoundWithOptions(ctx, opts, run, index, step, nil)
	case "tree":
		cfg := c.mustLoadConfig()
		described, err := c.describeUITree(ctx, cfg, prefer, false)
		if err != nil || described.Result.Err != nil {
			return map[string]string{"driver": prefer}, fmt.Errorf("tree_failed")
		}
		elements := ExtractElementsRaw(described.Result.Stdout)
		selector := flowStepSelector(step)
		if !selector.IsZero() {
			elements, err = MatchElements(elements, selector)
			if err != nil {
				return nil, err
			}
		}
		max := len(elements)
		if raw := step.Params["max"]; raw != "" {
			if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed >= 0 && parsed < max {
				max = parsed
			}
		}
		fields := map[string]string{"driver": described.Driver, "nodes": strconv.Itoa(max)}
		treeData, _ := json.MarshalIndent(elements[:max], "", "  ")
		treePath := filepath.Join(run.Dir, fmt.Sprintf("step-%d-tree.json", index))
		_ = os.WriteFile(treePath, treeData, 0o644)
		fields["tree"] = treePath
		if step.Params["since"] != "" {
			previous := loadFastPathTree(run)
			delta := TreeDiff(previous, elements[:max])
			data, _ := json.Marshal(delta)
			path := filepath.Join(run.Dir, fmt.Sprintf("step-%d-tree-delta.json", index))
			_ = os.WriteFile(path, data, 0o644)
			fields["delta"] = path
		}
		_ = saveFastPathTree(run, elements[:max])
		return fields, nil
	case "tap":
		args := append(selectorCLIArgs(flowStepSelector(step)), flowArgs(step.Params, "--x", "x", "--y", "y")...)
		var out bytes.Buffer
		err := c.withStdout(&out).uiTap(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), args)
		cmdErr := commandOutputErr(err, out.String(), "tap_failed")
		if cmdErr != nil && step.Params["optional"] == "true" {
			fields := copyParams(step.Params)
			fields["skipped"] = "true"
			return fields, nil
		}
		return copyParams(step.Params), cmdErr
	case "doubleTap":
		args := append(selectorCLIArgs(flowStepSelector(step)), flowArgs(step.Params, "--x", "x", "--y", "y", "--duration", "duration")...)
		var out bytes.Buffer
		err := c.withStdout(&out).uiDoubleTap(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), args)
		return copyParams(step.Params), commandOutputErr(err, out.String(), "double_tap_failed")
	case "type":
		text := step.Params["text"]
		var out bytes.Buffer
		// Only an explicit selector targets a tap before typing; the legacy
		// params fallback would resurrect "text" (the content to type) as a
		// tap target here.
		args := append([]string{text}, selectorCLIArgs(step.Where)...)
		err := c.withStdout(&out).uiType(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), args)
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
		args := flowArgs(step.Params, "--direction", "direction", "--start-x", "startX", "--start-y", "startY", "--end-x", "endX", "--end-y", "endY")
		err := c.withStdout(io.Discard).uiSwipe(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "swipe_failed")
	case "drag":
		args := flowArgs(step.Params, "--start-x", "startX", "--start-y", "startY", "--end-x", "endX", "--end-y", "endY", "--duration", "duration")
		err := c.withStdout(io.Discard).uiDrag(ctx, GlobalOptions{}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "drag_failed")
	case "dragPath":
		args := []string{}
		for _, point := range step.Points {
			raw := strconv.Itoa(point.X) + "," + strconv.Itoa(point.Y)
			duration := point.Duration
			if duration == "" && point.DurationMs > 0 {
				duration = strconv.Itoa(point.DurationMs) + "ms"
			}
			if duration != "" {
				raw += ":" + duration
			}
			args = append(args, "--point", raw)
		}
		err := c.withStdout(io.Discard).uiDragPath(ctx, GlobalOptions{}, c.mustLoadConfig(), args)
		return map[string]string{"points": strconv.Itoa(len(step.Points))}, outputErr(err, "drag_path_failed")
	case "toggle":
		args := append(selectorCLIArgs(flowStepSelector(step)), flowArgs(step.Params, "--state", "state")...)
		err := c.withStdout(io.Discard).uiToggle(ctx, GlobalOptions{PreferDriver: prefer}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "toggle_failed")
	case "press":
		args := flowArgs(step.Params, "--button", "button")
		err := c.withStdout(io.Discard).uiPress(ctx, GlobalOptions{}, c.mustLoadConfig(), args)
		return copyParams(step.Params), outputErr(err, "press_failed")
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
		all := step.All
		if !step.Where.IsZero() {
			all = append(all, flowConditionFromSelector(step.Where))
		}
		err := c.waitForConditionSet(ctx, step.Params, nil, all, step.Not, prefer)
		return copyParams(step.Params), err
	case "waitUntil":
		all := step.All
		if !step.Where.IsZero() {
			all = append(all, flowConditionFromSelector(step.Where))
		}
		err := c.waitForConditionSet(ctx, step.Params, step.Any, all, step.Not, prefer)
		return map[string]string{"conditions": strconv.Itoa(len(step.Any))}, err
	case "assertCount":
		return c.assertFlowCount(ctx, step, prefer)
	case "scrollUntil":
		return c.scrollUntilFlowConditionWithSelector(ctx, step.Params, flowStepSelector(step), prefer)
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
	case "app.list":
		err := c.withStdout(io.Discard).app(ctx, GlobalOptions{}, []string{"list"})
		return map[string]string{}, outputErr(err, "app_list_failed")
	case "app.kill":
		args := []string{"kill"}
		if bundle := step.Params["bundle"]; bundle != "" {
			args = append(args, "--bundle", bundle)
		}
		err := c.withStdout(io.Discard).app(ctx, GlobalOptions{}, args)
		return copyParams(step.Params), outputErr(err, "app_kill_failed")
	case "openURL":
		err := c.withStdout(io.Discard).openURL(ctx, GlobalOptions{}, []string{step.Params["url"]})
		return copyParams(step.Params), outputErr(err, "open_url_failed")
	case "location.set":
		err := c.withStdout(io.Discard).location(ctx, GlobalOptions{}, []string{"set", step.Params["latitude"], step.Params["longitude"]})
		return copyParams(step.Params), outputErr(err, "location_set_failed")
	case "location.reset":
		err := c.withStdout(io.Discard).location(ctx, GlobalOptions{}, []string{"reset"})
		return map[string]string{}, outputErr(err, "location_reset_failed")
	case "clipboard.copy":
		err := c.withStdout(io.Discard).clipboard(ctx, GlobalOptions{}, []string{"copy", step.Params["text"]})
		return map[string]string{"chars": strconv.Itoa(len(step.Params["text"]))}, outputErr(err, "clipboard_copy_failed")
	case "clipboard.read":
		var out bytes.Buffer
		err := c.withStdout(&out).clipboard(ctx, GlobalOptions{Raw: true}, []string{"read"})
		return map[string]string{"value": out.String()}, outputErr(err, "clipboard_read_failed")
	case "time.freeze":
		err := c.withStdout(io.Discard).timeControl(ctx, GlobalOptions{}, []string{"freeze", "--at", step.Params["at"]})
		return copyParams(step.Params), outputErr(err, "time_freeze_failed")
	case "time.travel":
		err := c.withStdout(io.Discard).timeControl(ctx, GlobalOptions{}, []string{"travel", "--by", step.Params["by"]})
		return copyParams(step.Params), outputErr(err, "time_travel_failed")
	case "time.scale":
		err := c.withStdout(io.Discard).timeControl(ctx, GlobalOptions{}, []string{"scale", "--factor", step.Params["factor"]})
		return copyParams(step.Params), outputErr(err, "time_scale_failed")
	case "time.status":
		var out bytes.Buffer
		err := c.withStdout(&out).timeControl(ctx, GlobalOptions{}, []string{"status"})
		return map[string]string{"status": out.String()}, outputErr(err, "time_status_failed")
	case "time.reset":
		err := c.withStdout(io.Discard).timeControl(ctx, GlobalOptions{}, []string{"reset"})
		return map[string]string{"reset": "true"}, outputErr(err, "time_reset_failed")
	case "debug.attach":
		args := []string{"attach"}
		if breakpoint := step.Params["breakpoint"]; breakpoint != "" {
			args = append(args, "--breakpoint", breakpoint)
		}
		err := c.withStdout(io.Discard).debug(ctx, GlobalOptions{}, args)
		return copyParams(step.Params), outputErr(err, "debug_attach_failed")
	case "debug.wait":
		args := flowArgs(step.Params, "--timeout", "timeout")
		args = append([]string{"wait"}, args...)
		err := c.withStdout(io.Discard).debug(ctx, GlobalOptions{}, args)
		return copyParams(step.Params), outputErr(err, "debug_wait_failed")
	case "debug.break":
		err := c.withStdout(io.Discard).debug(ctx, GlobalOptions{}, []string{"break", "add", step.Params["breakpoint"]})
		return copyParams(step.Params), outputErr(err, "debug_break_failed")
	case "debug.eval":
		err := c.withStdout(io.Discard).debug(ctx, GlobalOptions{}, []string{"eval", step.Params["expression"]})
		return copyParams(step.Params), outputErr(err, "debug_eval_failed")
	case "debug.step":
		kind := firstNonEmpty(step.Params["kind"], step.Params["debugDirection"], step.Params["direction"])
		err := c.withStdout(io.Discard).debug(ctx, GlobalOptions{}, []string{"step", kind})
		return copyParams(step.Params), outputErr(err, "debug_step_failed")
	case "debug.detach":
		args := []string{"detach"}
		if step.Params["kill"] == "true" {
			args = append(args, "--kill")
		}
		err := c.withStdout(io.Discard).debug(ctx, GlobalOptions{}, args)
		return copyParams(step.Params), outputErr(err, "debug_detach_failed")
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
	prefer, preferErr := c.flowStepPreferDriver(opts, step)
	if preferErr != nil {
		return copyParams(step.Params), preferErr
	}
	matched, err := c.evaluateConditionSet(ctx, step.Params, step.Any, step.All, step.Not, prefer)
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
	prefer, preferErr := c.flowStepPreferDriver(opts, step)
	if preferErr != nil {
		return copyParams(step.Params), preferErr
	}
	matched, err := c.evaluateConditionSet(ctx, step.Params, step.Any, step.All, step.Not, prefer)
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
	prefer, preferErr := c.flowStepPreferDriver(opts, step)
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
		matched, err := c.evaluateConditionSet(ctx, step.Params, step.Any, step.All, step.Not, prefer)
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
	run, err := c.resolveRun("")
	if err == nil && run.ID != "" {
		return run, nil
	}
	run, err = NewProjectRunState(c.Root)
	if err != nil {
		return RunState{}, err
	}
	return run, SaveCurrentRun(c.Root, run)
}

// withRun binds run to this CLI value so every command it dispatches to
// resolves against it instead of the on-disk .mav/current-run pointer. Since
// CLI is passed by value, the binding is local to the copy it's called on
// and everything that copy calls transitively (withStdout, flow steps, ...).
func (c CLI) withRun(run RunState) CLI {
	c.run = &run
	return c
}

// boundRun reports the run bound via withRun, if any.
func (c CLI) boundRun() (RunState, bool) {
	if c.run == nil {
		return RunState{}, false
	}
	return *c.run, true
}

// publishCurrentRunIfSafe points .mav/current-run at run, unless it already
// names a *different* run that still has live processes recorded against it
// -- e.g. another agent's manual `mav open` session in the same repo.
// Overwriting that pointer wouldn't kill anything (M1 already stopped
// runFlow from doing that), but it would silently redirect the other
// agent's next `mav ui tap` / `mav logs` / `mav stop` (all resolveRun("")
// when called without --run) onto this run instead -- a quieter version of
// the same cross-agent corruption M1 exists to close. Safe to call
// repeatedly and a no-op once run is already published.
func (c CLI) publishCurrentRunIfSafe(run RunState) {
	if existing, err := LoadRun(c.Root, ""); err == nil && existing.ID != "" && existing.ID != run.ID {
		for _, record := range loadProcessRecords(existing) {
			if processAlive(record.PID) {
				return
			}
		}
	}
	_ = SaveCurrentRun(c.Root, run)
}

// resolveRun is the single place that decides which run a command targets.
// An explicit id (--run) always wins; otherwise a bound run is used without
// touching disk; only with neither does it fall back to the manual
// .mav/current-run pointer.
func (c CLI) resolveRun(id string) (RunState, error) {
	if id != "" {
		return LoadRun(c.Root, id)
	}
	if bound, ok := c.boundRun(); ok {
		return bound, nil
	}
	return LoadRun(c.Root, "")
}

func (c CLI) withStdout(stdout io.Writer) CLI {
	c.Stdout = stdout
	return c
}

func (c CLI) keepRunLeaseAlive(command string) func() {
	switch command {
	case "__worker", "stop":
		return func() {}
	}
	renew := func() {
		run, err := c.resolveRun("")
		if err == nil {
			_, _ = sendWorkerRequest(run, workerRequest{Command: "renew"})
		}
	}
	// Fire-and-forget: the worker may be mid-request for tens of seconds
	// (debug wait/eval, long gesture sequences), and this runs before every
	// command dispatches. Renewing synchronously here would stall unrelated
	// mav invocations behind whatever the worker happens to be doing.
	go renew()
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(workerHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				renew()
			case <-done:
				return
			}
		}
	}()
	return func() {
		close(done)
		renew()
	}
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
	c.resolveConfigTarget(&cfg)
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

// stopVideoRecording terminates a still-registered video recorder and clears
// its pid marker. stop() deliberately leaves an active recording running
// while video.pid exists, so it doesn't race a caller that's about to
// finalize it through evidenceStop's wait/validate/transcode sequence.
// Abandonment and failure cleanup call this afterward, because for them
// there is no such caller coming -- if they don't kill it here, nothing
// ever will.
func stopVideoRecording(run RunState) {
	pid, err := readPID(filepath.Join(run.Dir, "video.pid"))
	if err != nil {
		return
	}
	_ = stopProcess(pid)
	_ = os.Remove(filepath.Join(run.Dir, "video.pid"))
	removeProcess(run, pid)
}

// reapAbandonedRun stops every process ever registered for run, including an
// in-flight video recording that generic stop() skips (see stop and
// stopVideoRecording). Use this specifically when the run's owner is gone or
// has given up -- worker lease expiry (nobody has touched the run in a
// while) and failed-flow cleanup -- so a recorder started by `mav evidence
// start` whose caller never comes back to stop it doesn't run forever and
// hold a simulator the run's owner can no longer release.
func (c CLI) reapAbandonedRun(ctx context.Context, run RunState) {
	_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
	stopVideoRecording(run)
}

// ensureRunWorker best-effort starts (or confirms) the run's watchdog worker,
// so a background recorder registered without ever going through `mav open`
// -- a bare `mav evidence start`, `mav network start`, or a probe-logs
// capture on its own -- still gets a lease-expiry safety net that reaps it
// via reapAbandonedRun if its caller never comes back. Errors are swallowed:
// the caller's real job already succeeded, and the recorder was already
// meant to keep running fire-and-forget either way.
func (c CLI) ensureRunWorker(run RunState) {
	if _, ok := c.Runner.(ExecRunner); !ok {
		return
	}
	_, _ = startRunWorker(c.Root, run)
}

func (c CLI) cleanupFailedFlow(ctx context.Context, run RunState, fields map[string]string) {
	c.reapAbandonedRun(ctx, run)
	if cfg, err := LoadConfig(c.Root); err == nil {
		c.resolveConfigTools(&cfg)
		c.resolveConfigTarget(&cfg)
		path := filepath.Join(run.Dir, "failure.png")
		if result, err := c.captureScreenshot(ctx, cfg, path); err == nil && result.Err == nil {
			fields["screenshot"] = path
		}
	}
	_, _ = GenerateReport(run)
	// Same best-effort publication as the success path (see runFlow): a
	// failed run should still be reachable by manual mav commands without
	// --run. publishCurrentRunIfSafe still won't steal the pointer from a
	// different run that's still live.
	c.publishCurrentRunIfSafe(run)
}

func (c CLI) captureEvidenceStep(ctx context.Context, run RunState, name, note string) (map[string]string, error) {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return nil, fmt.Errorf("config_not_found")
	}
	c.resolveConfigTools(&cfg)
	c.resolveConfigTarget(&cfg)
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
	return c.waitForConditionSet(ctx, params, any, nil, nil, prefer)
}

func (c CLI) waitForConditionSet(ctx context.Context, params map[string]string, any, all []FlowCondition, not *FlowCondition, prefer string) error {
	timeout := parseFlowDuration(params["timeout"], 5*time.Second)
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	for {
		ok, err := c.evaluateConditionSet(ctx, params, any, all, not, prefer)
		if err != nil {
			if err.Error() != "tree_failed" && err.Error() != "ui_not_stable" {
				return err
			}
			ok = false
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
	return c.scrollUntilFlowConditionWithSelector(ctx, params, selectorFromLegacy(params), prefer)
}

func (c CLI) scrollUntilFlowConditionWithSelector(ctx context.Context, params map[string]string, selector Selector, prefer string) (map[string]string, error) {
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
	condition := flowConditionFromSelector(selector)
	for i := 0; i <= maxSwipes; i++ {
		ok, err := c.evaluateSingleConditionWithPrefer(ctx, condition, prefer)
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
	return c.evaluateConditionSet(ctx, params, any, nil, nil, prefer)
}

func (c CLI) evaluateConditionSet(ctx context.Context, params map[string]string, any, all []FlowCondition, not *FlowCondition, prefer string) (bool, error) {
	base := FlowCondition{
		Text: params["text"], ID: params["id"], Value: params["value"],
		ChangedFrom: params["changedFrom"],
	}
	if !base.Selector().IsZero() || base.ChangedFrom != "" {
		all = append(append([]FlowCondition{}, all...), base)
	}
	if len(any) > 0 {
		anyMatched := false
		for _, condition := range any {
			ok, err := c.evaluateSingleConditionWithPrefer(ctx, condition, prefer)
			if err != nil {
				return false, err
			}
			if ok {
				anyMatched = true
				break
			}
		}
		if !anyMatched {
			return false, nil
		}
	}
	if len(all) > 0 {
		for _, condition := range all {
			ok, err := c.evaluateSingleConditionWithPrefer(ctx, condition, prefer)
			if err != nil || !ok {
				return false, err
			}
		}
	}
	if not != nil {
		ok, err := c.evaluateSingleConditionWithPrefer(ctx, *not, prefer)
		if err != nil {
			return false, err
		}
		if ok {
			return false, nil
		}
	}
	if len(any) > 0 || len(all) > 0 || not != nil {
		return true, nil
	}
	// No conditions at all (base params, any, all, not are all empty).
	return false, nil
}

func (c CLI) evaluateSingleCondition(ctx context.Context, condition FlowCondition) (bool, error) {
	return c.evaluateSingleConditionWithPrefer(ctx, condition, "auto")
}

func (c CLI) evaluateSingleConditionWithPrefer(ctx context.Context, condition FlowCondition, prefer string) (bool, error) {
	if len(condition.Any) > 0 || len(condition.All) > 0 || condition.Not != nil {
		return c.evaluateConditionSet(ctx, nil, condition.Any, condition.All, condition.Not, prefer)
	}
	if condition.ChangedFrom != "" {
		return c.screenshotChangedFrom(ctx, condition.ChangedFrom)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return false, fmt.Errorf("config_not_found")
	}
	c.resolveConfigTools(&cfg)
	c.resolveConfigTarget(&cfg)
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil {
		return false, fmt.Errorf("tree_failed")
	}
	result := described.Result
	if result.Err != nil {
		return false, fmt.Errorf("tree_failed")
	}
	elements := ExtractElementsRaw(result.Stdout)
	if condition.Stable {
		first := hashElements(elements)
		time.Sleep(200 * time.Millisecond)
		secondTree, secondErr := c.describeUITree(ctx, cfg, prefer, false)
		if secondErr != nil || secondTree.Result.Err != nil {
			return false, fmt.Errorf("tree_failed")
		}
		return first != "" && first == hashElements(ExtractElementsRaw(secondTree.Result.Stdout)), nil
	}
	return flowConditionMatchesElements(elements, condition), nil
}

func flowConditionMatchesElements(elements []Element, condition FlowCondition) bool {
	selector := condition.Selector()
	if selector.IsZero() {
		return false
	}
	matches, err := MatchElements(elements, selector)
	return err == nil && len(matches) > 0
}

func (c CLI) assertFlowCount(ctx context.Context, step FlowStep, prefer string) (map[string]string, error) {
	expected, err := strconv.Atoi(step.Params["count"])
	if err != nil || expected < 0 {
		return nil, fmt.Errorf("assert_count_invalid")
	}
	cfg := c.mustLoadConfig()
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil || described.Result.Err != nil {
		return nil, fmt.Errorf("tree_failed")
	}
	matches, err := MatchElements(ExtractElementsRaw(described.Result.Stdout), flowStepSelector(step))
	if err != nil {
		return nil, err
	}
	fields := map[string]string{"expected": strconv.Itoa(expected), "actual": strconv.Itoa(len(matches))}
	if len(matches) != expected {
		return fields, fmt.Errorf("assert_count_failed")
	}
	return fields, nil
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
	run, err := c.resolveRun("")
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
	c.resolveConfigTarget(&cfg)
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
	run, err := c.resolveRun(flagValue(args, "--run"))
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
	return c.OK("logs", fields).Write(c.Stdout)
}

func (c CLI) stop(ctx context.Context, opts GlobalOptions, args []string) error {
	run, err := c.resolveRun(flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	if exists(filepath.Join(run.Dir, "time-control.enabled")) && !exists(filepath.Join(run.Dir, "time-control.preserve")) {
		_ = c.withStdout(io.Discard).timeControl(ctx, GlobalOptions{}, []string{"reset"})
		_ = os.Remove(filepath.Join(run.Dir, "time-control.enabled"))
	}
	records := loadProcessRecords(run)
	if workerPing(run) {
		_, _ = sendWorkerRequest(run, workerRequest{Command: "stop"})
	}
	stopped := 0
	failed := 0
	for _, record := range records {
		if record.PID <= 0 {
			continue
		}
		if record.PID == os.Getpid() {
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
	return c.OK("stop", fields).Write(c.Stdout)
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

// stopProbeLogs kills only the "probe-logs" log-stream processes recorded
// for run. Used by open when it's about to start a fresh probe-logs capture
// on a run it's reusing (a flow's second open step), so the previous log
// stream doesn't keep writing into the same logs.txt forever.
func stopProbeLogs(run RunState) {
	for _, record := range loadProcessRecords(run) {
		if record.Kind != "probe-logs" || record.PID <= 0 {
			continue
		}
		_ = stopProcess(record.PID)
		removeProcess(run, record.PID)
	}
}

func (c CLI) crashes(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTools(&cfg)
	c.resolveConfigTarget(&cfg)
	if targetKind(cfg) == drivers.KindSim && !opts.Raw {
		return c.crashesFromDiagnosticReports("")
	}
	if targetKind(cfg) == drivers.KindMac {
		// En el Mac los .ips estan en el disco local y son el MISMO formato
		// JSON que introdujeron iOS 15 y macOS 12, asi que ParseIPS sirve sin
		// tocar nada. No hay idb de por medio ni nada que pueda fallar aparte
		// del propio filesystem, asi que tampoco hay modo --raw que ofrecer.
		return c.crashesFromDiagnosticReports("")
	}
	if !hasTool(cfg, "idb") {
		return Fail("tool_missing", map[string]string{"tool": "idb"}).Write(c.Stdout)
	}
	idbArgs := idbTargetArgs(cfg, "crash", "list")
	if cfg.BundleID != "" {
		idbArgs = append(idbArgs, "--bundle-id", cfg.BundleID)
	}
	result := c.runIDBCommand(ctx, idbArgs...)
	if opts.Raw {
		fmt.Fprint(c.Stdout, result.Stdout)
		return nil
	}
	if result.Err != nil {
		if targetKind(cfg) == drivers.KindSim {
			return c.crashesFromDiagnosticReports(firstLine(result.Stderr))
		}
		fields := map[string]string{"stderr": firstLine(result.Stderr)}
		addSandboxNext(fields, result.Stderr)
		return Fail("crashes_failed", fields).Write(c.Stdout)
	}
	names := parseCrashNames(result.Stdout)
	fields := map[string]string{"count": strconv.Itoa(len(names))}
	if result.IDBCompanionRefreshed {
		fields["idb_repaired"] = "true"
	}

	if len(names) == 0 {
		return c.OK("crashes", fields).Write(c.Stdout)
	}

	if run, err := c.resolveRun(""); err == nil {
		crashDir := filepath.Join(run.Dir, "crashes")
		driver, _, err := c.router().Route(ctx, drivers.CapCrashFetch, targetFromConfig(cfg), "")
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

	return c.OK("crashes", fields).Write(c.Stdout)
}

func (c CLI) crashesFromDiagnosticReports(idbError string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	// crashes() already resolved its own cfg before calling here, but this
	// reloads a fresh copy straight off disk -- re-resolve it too, or
	// diagnosticCrashRoots below silently narrows its search whenever
	// target_command is configured (cfg.SimulatorUDID would read empty
	// instead of falling back like every other caller now does).
	c.resolveConfigTarget(&cfg)
	since := time.Now().Add(-15 * time.Minute)
	var crashDir string
	if run, err := c.resolveRun(""); err == nil {
		if info, statErr := os.Stat(run.Dir); statErr == nil {
			since = info.ModTime()
		}
		crashDir = filepath.Join(run.Dir, "crashes")
	}
	matches := findRecentIPSFiles(diagnosticCrashRoots(cfg), crashNameNeedles(cfg), since)
	fields := map[string]string{
		"count":  strconv.Itoa(len(matches)),
		"source": "diagnostic_reports",
		"since":  since.Format(time.RFC3339),
	}
	if idbError != "" {
		fields["idb_error"] = idbError
		fields["degraded"] = "true"
		fields["next"] = "idb companion unavailable; run idb list-targets to refresh stale companions, or restart idb_companion"
	}
	if crashDir != "" && len(matches) > 0 {
		copied, summarised := copyDiagnosticCrashes(matches, crashDir)
		fields["fetched"] = strconv.Itoa(copied)
		fields["summarised"] = strconv.Itoa(summarised)
		fields["dir"] = crashDir
	}
	return c.OK("crashes", fields).Write(c.Stdout)
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
	run, err := c.resolveRun(flagValue(args, "--run"))
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
	return c.OK("evidence.report", fields).Write(c.Stdout)
}

func (c CLI) evidenceStart(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTools(&cfg)
	c.resolveConfigTarget(&cfg)
	run, err := c.resolveRun(flagValue(args, "--run"))
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
	return c.OK("evidence.start", fields).Write(c.Stdout)
}

func (c CLI) evidenceStep(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	c.resolveConfigTools(&cfg)
	c.resolveConfigTarget(&cfg)
	run, err := c.resolveRun(flagValue(args, "--run"))
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
	return c.OK("evidence.step", fields).Write(c.Stdout)
}

func (c CLI) evidenceStop(ctx context.Context, opts GlobalOptions, args []string) error {
	run, err := c.resolveRun(flagValue(args, "--run"))
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
			c.resolveConfigTarget(&cfg)
			file := filepath.Join(run.Dir, "steps", fmt.Sprintf("%02d_final.png", len(LoadEvidenceSteps(run))+1))
			if result, err := c.captureScreenshot(ctx, cfg, file); err == nil && result.Err == nil {
				_ = AppendEvidenceStep(run, EvidenceStep{Name: "final", Note: flagValue(args, "--note"), File: file, Kind: "screenshot"})
				fields["screenshot"] = file
			}
		}
	}
	appendCommand(run, "mav evidence stop", CommandResult{})
	return c.OK("evidence.stop", fields).Write(c.Stdout)
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
	if targetKind(cfg) != drivers.KindSim {
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
		c.ensureRunWorker(run)
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
	if result.IDBCompanionRefreshed {
		record["idb_repaired"] = true
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
