package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func normalizedTargetKind(cfg Config) string {
	if cfg.TargetKind == "device" {
		return "device"
	}
	return "simulator"
}

func isPhysicalDevice(cfg Config) bool {
	return normalizedTargetKind(cfg) == "device"
}

func targetUDID(cfg Config) string {
	if isPhysicalDevice(cfg) {
		return cfg.DeviceUDID
	}
	return cfg.SimulatorUDID
}

func targetName(cfg Config) string {
	if isPhysicalDevice(cfg) {
		return cfg.DeviceName
	}
	return cfg.SimulatorName
}

func targetRuntime(cfg Config) string {
	if isPhysicalDevice(cfg) {
		return ""
	}
	return cfg.SimulatorRuntime
}

// OK is the CLI-bound counterpart of the package-level OK: it's the single
// place a command's success fields pick up which simulator or device they
// actually acted on. Route every success output through c.OK (not the bare
// OK) so nobody has to remember to add the field by hand -- in hot-path
// usage (an agent driving mav command-by-command, not just via `mav run`)
// that field is how the next call knows which target to keep using instead
// of guessing; guessing wrong with several agents on one machine means
// silently driving someone else's simulator while taps and assertions keep
// passing.
func (c CLI) OK(cmd string, fields map[string]string) Output {
	return OK(cmd, c.withResolvedTarget(fields))
}

// withResolvedTarget fills udid/target_kind/target_name into fields, unless
// the caller already set udid explicitly (e.g. sim.select reporting the
// simulator it just picked). Most project configs no longer pin
// simulator_udid (see config.go's MAV_TARGET_KIND/MAV_TARGET_UDID handling),
// so most commands actually run against "whatever simulator is booted" --
// resolving that concretely here, instead of leaving it implicit, is the
// point: it turns a silent default into something the caller can read off
// the very first response.
func (c CLI) withResolvedTarget(fields map[string]string) map[string]string {
	if fields == nil {
		fields = map[string]string{}
	}
	if _, ok := fields["udid"]; ok {
		return fields
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return fields
	}
	if warn := c.resolveConfigTarget(&cfg); warn != "" {
		fields["target_command_warn"] = warn
	}
	udid := targetUDID(cfg)
	name := targetName(cfg)
	kind := normalizedTargetKind(cfg)
	if udid == "" && kind == "simulator" {
		udid, name = c.resolveBootedSimulator()
	}
	if udid == "" {
		return fields
	}
	fields["udid"] = udid
	if _, ok := fields["target_kind"]; !ok {
		fields["target_kind"] = kind
	}
	if name != "" {
		if _, ok := fields["target_name"]; !ok {
			fields["target_name"] = name
		}
	}
	return fields
}

// bootedSimulatorCacheTTL bounds how long a cached "whatever's booted"
// resolution is trusted before resolveBootedSimulator pays the real cost
// again. It exists for the long-lived/resumed-run case (`mav run
// flow.yaml --run RUN_ID` hours later, or a run an agent comes back to after
// a break) where the booted simulator may genuinely have changed under a
// run whose cache would otherwise be trusted for the run's entire lifetime.
// It's deliberately generous: a hot-path navigation of dozens of commands
// normally completes in well under this window, so it shouldn't make any
// individual command pay the cost twice in practice.
const bootedSimulatorCacheTTL = 2 * time.Minute

type bootedSimulatorCache struct {
	UDID       string    `json:"udid"`
	Name       string    `json:"name"`
	ResolvedAt time.Time `json:"resolved_at"`
}

func bootedSimulatorCachePath(run RunState) string {
	return filepath.Join(run.Dir, "booted-simulator.json")
}

func readBootedSimulatorCache(run RunState) (bootedSimulatorCache, bool) {
	data, err := os.ReadFile(bootedSimulatorCachePath(run))
	if err != nil {
		return bootedSimulatorCache{}, false
	}
	var cache bootedSimulatorCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.UDID == "" {
		return bootedSimulatorCache{}, false
	}
	if time.Since(cache.ResolvedAt) >= bootedSimulatorCacheTTL {
		return bootedSimulatorCache{}, false
	}
	return cache, true
}

func writeBootedSimulatorCache(run RunState, udid, name string) {
	if udid == "" {
		return
	}
	data, err := json.Marshal(bootedSimulatorCache{UDID: udid, Name: name, ResolvedAt: time.Now()})
	if err != nil {
		return
	}
	_ = os.WriteFile(bootedSimulatorCachePath(run), data, 0o644)
}

// resolveBootedSimulator resolves which simulator is currently booted, for
// reporting only (targets that are pinned in config or MAV_TARGET_UDID never
// reach this -- see withResolvedTarget). `xcrun simctl list devices booted
// -j` costs ~0.75s regardless of how it's invoked (measured: calling simctl
// directly, bypassing xcrun's own dispatch, costs the same -- the latency is
// inherent to CoreSimulator, not to xcrun), and hot-path usage means dozens
// of commands per navigation, so re-resolving on every command would add
// tens of seconds to a session for a field that rarely changes mid-run.
//
// The resolution is cached in the run's own state dir and trusted for
// bootedSimulatorCacheTTL: mav already treats "one run, one simulator, never
// shared" as an invariant elsewhere (the simulator lock refuses a second run
// on the same device), so trusting it for a run's *normal* duration isn't
// a new risk -- the TTL exists only to bound the case where a run outlives
// that assumption (resumed hours later, simulator rebooted or switched
// outside mav in the meantime).
func (c CLI) resolveBootedSimulator() (string, string) {
	if c.Runner == nil {
		return "", ""
	}
	run, err := c.resolveRun("")
	if err != nil {
		// No run to cache against (e.g. a standalone command before `mav
		// open`); resolve fresh. This path is rare and never the hot loop.
		udid, name, _ := detectBootedSimulator(c.Runner)
		return udid, name
	}
	if cache, ok := readBootedSimulatorCache(run); ok {
		return cache.UDID, cache.Name
	}
	udid, name, _ := detectBootedSimulator(c.Runner)
	writeBootedSimulatorCache(run, udid, name)
	return udid, name
}

// resolveConfigTarget is the generic, simpool-agnostic answer to "which
// simulator when several are booted at once": if the repo configures
// target_command, mav runs it and uses its stdout as the UDID to pin for
// this command, without ever importing or knowing about whatever pool
// manager produced it (simpool is one possible value of that command, never
// a dependency).
//
// Precedence (documented in README/skill): an explicit --target flag on
// `mav run` and the MAV_TARGET_* env vars it sets on matrix children both
// short-circuit before this ever runs (LoadConfig already applied them, or
// MAV_TARGET_KIND is set directly); a simulator_udid pinned in config.yaml
// (via `mav sim select`) also wins, since it is already-resolved explicit
// state. target_command only fires for the case that used to silently mean
// "whatever's booted" -- it replaces that guess with a deterministic one,
// and falls straight back to the old booted behavior if it fails.
//
// Mutates cfg.SimulatorUDID/SimulatorName in place (so the caller's actual
// axe/idb dispatch is pinned, not just the reported field) and returns a
// non-empty, actionable warning in two cases -- never a panic, never a hang:
//
//   - target_command was configured but did not produce a usable UDID: a
//     bad command degrades to the pre-existing booted fallback.
//   - simulator_udid is pinned *and* target_command is configured: the pin
//     still wins (a pin is already-resolved explicit state, and inverting
//     that would make `mav sim select` unpredictable), but silently ignoring
//     target_command is exactly the failure mode this feature exists to
//     avoid -- a repo can carry a stale pin from before target_command was
//     added and never notice the field is dead configuration. Surfaced
//     through the same target_command_warn field as the failure case, since
//     both boil down to "target_command is configured but not in effect."
func (c CLI) resolveConfigTarget(cfg *Config) string {
	if isPhysicalDevice(*cfg) {
		return ""
	}
	if os.Getenv("MAV_TARGET_KIND") != "" {
		return ""
	}
	command := strings.TrimSpace(cfg.TargetCommand)
	if cfg.SimulatorUDID != "" {
		if command == "" {
			return ""
		}
		return fmt.Sprintf("target_command_ignored: simulator_udid=%s is pinned in .mav/config.yaml and wins over target_command (next: remove simulator_udid to let target_command route automatically, or remove target_command if the pin is intentional)", cfg.SimulatorUDID)
	}
	if command == "" {
		return ""
	}
	udid, name, warn := c.resolveTargetCommand(cfg.Root, command)
	if udid == "" {
		return warn
	}
	cfg.SimulatorUDID = udid
	cfg.SimulatorName = name
	return ""
}

// targetCommandTimeout bounds a single target_command invocation so a
// hanging pool manager can never hang mav itself -- it degrades to the
// booted fallback instead.
const targetCommandTimeout = 10 * time.Second

type targetCommandCache struct {
	UDID       string    `json:"udid"`
	Name       string    `json:"name"`
	Warn       string    `json:"warn,omitempty"`
	ResolvedAt time.Time `json:"resolved_at"`
}

func targetCommandCachePath(run RunState) string {
	return filepath.Join(run.Dir, "target-command.json")
}

func readTargetCommandCache(run RunState) (targetCommandCache, bool) {
	data, err := os.ReadFile(targetCommandCachePath(run))
	if err != nil {
		return targetCommandCache{}, false
	}
	var cache targetCommandCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return targetCommandCache{}, false
	}
	if cache.UDID == "" && cache.Warn == "" {
		return targetCommandCache{}, false
	}
	if time.Since(cache.ResolvedAt) >= bootedSimulatorCacheTTL {
		return targetCommandCache{}, false
	}
	return cache, true
}

func writeTargetCommandCache(run RunState, udid, name, warn string) {
	data, err := json.Marshal(targetCommandCache{UDID: udid, Name: name, Warn: warn, ResolvedAt: time.Now()})
	if err != nil {
		return
	}
	_ = os.WriteFile(targetCommandCachePath(run), data, 0o644)
}

// resolveTargetCommand runs cfg.TargetCommand at most once per run, cached
// in the run's own state dir under the same TTL as resolveBootedSimulator
// (see bootedSimulatorCacheTTL) and for the same reason: a hot-path
// navigation issues dozens of commands, and target_command is expected to
// shell out to an external pool manager whose own cost (and, if it fails,
// whose own latency to fail) mav should not multiply by every command.
// Failures are cached too, so a broken target_command degrades to the
// booted fallback once per TTL window instead of paying its timeout on
// every single command.
func (c CLI) resolveTargetCommand(root, command string) (string, string, string) {
	if c.Runner == nil {
		return "", "", ""
	}
	run, err := c.resolveRun("")
	if err != nil {
		// No run to cache against yet (e.g. a standalone command before
		// `mav open`); resolve fresh. Rare, never the hot loop.
		return c.execTargetCommand(root, command)
	}
	if cache, ok := readTargetCommandCache(run); ok {
		return cache.UDID, cache.Name, cache.Warn
	}
	udid, name, warn := c.execTargetCommand(root, command)
	writeTargetCommandCache(run, udid, name, warn)
	return udid, name, warn
}

// execTargetCommand runs the configured command through /bin/bash -lc, the
// same shell invocation flows already use for `exec` steps, so target_command
// can be anything from a bare binary to a pipeline. Its only contract with
// mav is: print the UDID to use on stdout. It runs from the project root
// (like launch and exec-step commands) with MAV_ROOT exported, so a project
// can point target_command at a repo-relative script.
func (c CLI) execTargetCommand(root, command string) (string, string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), targetCommandTimeout)
	defer cancel()
	prefixed := shellEnvPrefix(map[string]string{"MAV_ROOT": root}) + " " + command
	result := c.Runner.Run(ctx, "/bin/bash", "-lc", prefixed)
	if result.Err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = result.Err.Error()
		}
		return "", "", fmt.Sprintf("target_command_failed: %s (next: fix or remove target_command in .mav/config.yaml; falling back to the booted simulator)", firstLine(detail))
	}
	udid := strings.TrimSpace(firstLine(result.Stdout))
	if udid == "" {
		return "", "", "target_command_empty: command produced no output (next: target_command must print a simulator UDID on stdout; falling back to the booted simulator)"
	}
	name := strings.TrimSpace(secondLine(result.Stdout))
	return udid, name, ""
}

func secondLine(s string) string {
	lines := strings.SplitN(s, "\n", 3)
	if len(lines) < 2 {
		return ""
	}
	return strings.TrimSpace(lines[1])
}
