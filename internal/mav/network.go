package mav

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// network dispatches the `mav network` family. Currently start/stop only;
// future verbs (status, har-validate, ...) bolt on here.
func (c CLI) network(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("network_command_missing", map[string]string{"usage": "mav network start|stop"}).Write(c.Stdout)
	}
	switch args[0] {
	case "start":
		return c.networkStart(ctx, opts, args[1:])
	case "stop":
		return c.networkStop(ctx, opts, args[1:])
	default:
		return Fail("network_unknown_command", map[string]string{"command": args[0]}).Write(c.Stdout)
	}
}

// networkStart routes a CapNetworkCapture request through the router (mitmproxy
// on sim) and persists the PID of the background mitmdump process under the
// current run. Subsequent `mav network stop` finds the PID via processes.jsonl
// without the caller having to track it.
func (c CLI) networkStart(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", map[string]string{"next": "mav setup"}).Write(c.Stdout)
	}
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}

	target := targetFromConfig(cfg)
	if target.IsDevice() {
		return Fail("network_unsupported_on_device", map[string]string{
			"next": "point the device at an externally-running proxy; mav does not bundle device capture",
		}).Write(c.Stdout)
	}

	// Refuse to start a second capture for the same run; the PID file is
	// the source of truth so users get a deterministic error rather than
	// two competing mitmdumps fighting for the proxy plist.
	if pid := findRunningNetworkPID(run); pid > 0 {
		return Fail("network_already_running", map[string]string{
			"pid":  strconv.Itoa(pid),
			"next": "mav network stop",
		}).Write(c.Stdout)
	}

	spec := drivers.NetworkCaptureSpec{
		OutPath:    networkHARPath(run, args),
		ListenPort: portFlag(args),
	}

	driver, _, err := c.router().Route(ctx, drivers.CapNetworkCapture, target, "")
	if err != nil {
		return Fail("driver_unavailable", map[string]string{
			"capability": "network.capture",
			"error":      err.Error(),
			"next":       "mav setup --install mitmproxy",
		}).Write(c.Stdout)
	}
	nd, ok := driver.(drivers.NetworkDriver)
	if !ok {
		return Fail("driver_unavailable", map[string]string{
			"driver":     driver.ID(),
			"capability": "network.capture",
		}).Write(c.Stdout)
	}

	result, err := nd.NetworkStart(ctx, target, spec)
	if err != nil {
		return Fail("network_start_failed", map[string]string{
			"driver": driver.ID(),
			"error":  err.Error(),
		}).Write(c.Stdout)
	}

	appendProcess(run, "network", result.PID, fmt.Sprintf("%s --listen-port %d --hardump %s", driver.ID(), result.ListenPort, result.OutPath))

	_ = opts
	return OK("network.start", map[string]string{
		"driver":      driver.ID(),
		"pid":         strconv.Itoa(result.PID),
		"har":         result.OutPath,
		"listen_port": strconv.Itoa(result.ListenPort),
		"proxy_url":   result.ProxyURL,
		"run":         run.ID,
	}).Write(c.Stdout)
}

// networkStop terminates the background mitmdump for the current run.
// The HAR file flushes incrementally so the partial recording is usable
// even if mitmdump never receives the SIGTERM (e.g. a crashed run).
func (c CLI) networkStop(ctx context.Context, opts GlobalOptions, args []string) error {
	run, err := LoadRun(c.Root, flagValue(args, "--run"))
	if err != nil {
		return Fail("run_not_found", nil).Write(c.Stdout)
	}
	pid := findRunningNetworkPID(run)
	if pid <= 0 {
		return Fail("network_not_running", map[string]string{"next": "mav network start"}).Write(c.Stdout)
	}

	// Look up the mitmproxy driver explicitly via --prefer so that the
	// stop call always reaches the same driver that started -- protects
	// against ordering bugs if multiple NetworkDriver implementations
	// are ever registered side by side.
	driver, _, err := c.router().Route(ctx, drivers.CapNetworkCapture, targetFromConfig(loadOrDefaultConfig(c.Root)), "mitmproxy")
	if err != nil {
		return Fail("driver_unavailable", map[string]string{
			"capability": "network.capture",
			"error":      err.Error(),
		}).Write(c.Stdout)
	}
	nd, ok := driver.(drivers.NetworkDriver)
	if !ok {
		return Fail("driver_unavailable", map[string]string{
			"driver": driver.ID(),
		}).Write(c.Stdout)
	}
	if err := nd.NetworkStop(ctx, pid); err != nil {
		return Fail("network_stop_failed", map[string]string{
			"pid":   strconv.Itoa(pid),
			"error": err.Error(),
		}).Write(c.Stdout)
	}
	_ = opts
	return OK("network.stop", map[string]string{
		"pid": strconv.Itoa(pid),
		"run": run.ID,
	}).Write(c.Stdout)
}

// findRunningNetworkPID scans processes.jsonl for the latest "network" kind
// entry. Returns 0 if no capture is currently registered for this run.
func findRunningNetworkPID(run RunState) int {
	records := loadProcessRecords(run)
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Kind == "network" && records[i].PID > 0 {
			return records[i].PID
		}
	}
	return 0
}

// networkHARPath resolves --har: explicit value wins, otherwise default to
// <runDir>/network.har so the evidence report finds it without configuration.
func networkHARPath(run RunState, args []string) string {
	if v := flagValue(args, "--har"); v != "" {
		return v
	}
	return filepath.Join(run.Dir, "network.har")
}

// portFlag pulls the --port value, returning 0 (driver picks free port) when
// absent or malformed.
func portFlag(args []string) int {
	v := flagValue(args, "--port")
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

// loadOrDefaultConfig is a tiny shim so networkStop can route without
// hard-failing when the project lacks a config -- the stop call only needs
// the target kind to disambiguate sim vs device, and stop is intentionally
// resilient (we want to terminate a stale mitmdump even if the project
// state has drifted).
func loadOrDefaultConfig(root string) Config {
	cfg, err := LoadConfig(root)
	if err != nil || cfg.Root == "" {
		return DefaultConfig(root)
	}
	return cfg
}

// errNetworkNotSupported is the sentinel used internally so tests can assert
// the device branch without scraping error strings.
var errNetworkNotSupported = errors.New("network capture is sim-only")
