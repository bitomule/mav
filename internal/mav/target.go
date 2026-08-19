package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// targetKind es el unico sitio que decide QUE ES un target. Devuelve el enum
// del router (drivers.TargetKind) en vez de un bool para que anadir un tercer
// kind sea anadir un case, no auditar cada if del CLI.
func targetKind(cfg Config) drivers.TargetKind {
	if cfg.TargetKind == "device" {
		return drivers.KindDevice
	}
	return drivers.KindSim
}

// targetKindLabel es la grafia PUBLICA de un TargetKind: es lo que sale en el
// campo target_kind de la salida del CLI, en MAV_TARGET_KIND y en el
// target_kind de .mav/config.yaml. Deliberadamente NO es string(kind)
// ("sim"), que es un token interno de routing: "simulator"/"device" son
// contrato con los agentes y con los config.yaml ya escritos en disco.
func targetKindLabel(kind drivers.TargetKind) string {
	switch kind {
	case drivers.KindDevice:
		return "device"
	default:
		return "simulator"
	}
}

func targetUDID(cfg Config) string {
	switch targetKind(cfg) {
	case drivers.KindDevice:
		return cfg.DeviceUDID
	default:
		return cfg.SimulatorUDID
	}
}

func targetName(cfg Config) string {
	switch targetKind(cfg) {
	case drivers.KindDevice:
		return cfg.DeviceName
	default:
		return cfg.SimulatorName
	}
}

func targetRuntime(cfg Config) string {
	switch targetKind(cfg) {
	case drivers.KindDevice:
		return ""
	default:
		return cfg.SimulatorRuntime
	}
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
	// resolveConfigTarget already applies the booted-simulator fallback to
	// cfg itself (see its doc comment), so cfg is fully resolved by now --
	// no separate fallback needed here just for the reported fields.
	udid := targetUDID(cfg)
	name := targetName(cfg)
	kind := targetKindLabel(targetKind(cfg))
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

// resolveBootedSimulator resolves which simulator is currently booted. It is
// the last-resort fallback (case 5 in the README's precedence table):
// targets that are pinned in config, set via MAV_TARGET_UDID, or resolved
// through a working target_command never reach this -- see
// resolveConfigTarget, which is the only caller and applies this to cfg
// itself, not just to a command's reported fields. `xcrun simctl list
// devices booted -j` costs ~0.75s regardless of how it's invoked (measured:
// calling simctl directly, bypassing xcrun's own dispatch, costs the same --
// the latency is inherent to CoreSimulator, not to xcrun), and hot-path
// usage means dozens of commands per navigation, so re-resolving on every
// command would add tens of seconds to a session for a field that rarely
// changes mid-run.
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
	if targetKind(*cfg) != drivers.KindSim {
		return ""
	}
	if os.Getenv("MAV_TARGET_KIND") != "" {
		return ""
	}
	warn := c.resolveConfigTargetCommand(cfg)
	// Case 5 of the README precedence table -- the pre-existing booted-
	// simulator fallback -- applies uniformly here, in cfg itself, whenever
	// nothing above (a pin, a working target_command) already filled
	// cfg.SimulatorUDID. This used to live only in withResolvedTarget,
	// which built the *reported* udid field from a freshly-loaded cfg of
	// its own without ever writing it back to the cfg a command actually
	// dispatches against -- so a command whose success output goes through
	// c.OK (like doctor) looked resolved while a command that reads
	// cfg.SimulatorUDID directly to build a driver invocation (like
	// `mav ui tree`'s axe call) still got sent out with an empty UDID.
	// Resolving it here instead means every caller of resolveConfigTarget
	// gets the same, real answer.
	if cfg.SimulatorUDID == "" {
		cfg.SimulatorUDID, cfg.SimulatorName = c.resolveBootedSimulator()
	}
	return warn
}

// resolveConfigTargetCommand runs target_command (cases 3/4 of the
// precedence order) and applies a successful result to cfg, or explains why
// it didn't fire. Split out of resolveConfigTarget so the booted-simulator
// fallback above applies the same way regardless of which branch below
// returns -- a pin, an empty target_command, or a target_command that ran
// and failed all leave cfg.SimulatorUDID exactly as unresolved as each
// other from that fallback's point of view.
func (c CLI) resolveConfigTargetCommand(cfg *Config) string {
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

// invalidateTargetCommandCache drops the run-scoped target_command cache so
// the next resolveConfigTarget call in this same run pays the real cost
// again instead of trusting a resolution that dispatch just proved wrong.
// Best-effort: a missing file is exactly the state we want, so a failed
// remove (already gone, permissions) is not reported -- the next read will
// simply treat it as a cache miss either way (readTargetCommandCache already
// tolerates a missing file).
func invalidateTargetCommandCache(run RunState) {
	_ = os.Remove(targetCommandCachePath(run))
}

// isSimulatorBooted asks CoreSimulator directly (via `simctl list devices
// booted`) whether udid is booted right now. This is the ground truth used
// to decide whether a dispatch failure is the stale-cache problem
// staleSimulatorTargetRetry exists for, deliberately not axe/idb's stderr
// wording -- that text is theirs to change, and a wrong guess there would
// either miss a real staleness case or waste a ~15s re-resolve on a failure
// that has nothing to do with booting (a bad selector, a disconnected
// accessibility service). Costs the same ~0.75s as detectBootedSimulator
// (same underlying simctl call), but only runs after a command has already
// failed, never on the hot success path.
func isSimulatorBooted(runner Runner, udid string) bool {
	if runner == nil || udid == "" {
		// Unknown is treated as "booted" -- i.e. don't retry -- so a runner-
		// less CLI (tests with Runner left nil) or an unresolved udid never
		// triggers a pointless re-resolve.
		return true
	}
	result := runner.Run(context.Background(), "xcrun", "simctl", "list", "devices", "booted", "-j")
	if result.Err != nil {
		return true
	}
	var parsed struct {
		Devices map[string][]struct {
			UDID  string `json:"udid"`
			State string `json:"state"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return true
	}
	for _, devices := range parsed.Devices {
		for _, device := range devices {
			if device.UDID == udid && device.State == "Booted" {
				return true
			}
		}
	}
	return false
}

// staleSimulatorTargetRetry re-resolves cfg's target_command-sourced
// simulator after a dispatch failure that isSimulatorBooted has already
// confirmed is real staleness (the cached UDID is not currently booted).
// Eligibility beyond that check: target_command must actually be the thing
// that resolved this target -- not an explicit --target/MAV_TARGET_*, not a
// simulator_udid pin, not the plain booted fallback -- because it is the
// only precedence tier with anyone to re-ask (see targetCommandInEffect).
// Invalidates the run-scoped cache and calls resolveConfigTarget again,
// which is what actually re-runs target_command and, for a pool manager
// like simpool, boots the simulator and waits for it. Returns ok=false
// whenever retrying wouldn't help (no run to cache against, target_command
// not in effect, or re-resolution produced the same or no UDID) so the
// caller falls back to reporting the original failure untouched.
func (c CLI) staleSimulatorTargetRetry(cfg Config) (retried Config, previousUDID string, ok bool) {
	if targetKind(cfg) != drivers.KindSim || cfg.SimulatorUDID == "" {
		return Config{}, "", false
	}
	if !c.targetCommandInEffectForRun() {
		return Config{}, "", false
	}
	run, err := c.resolveRun("")
	if err != nil {
		return Config{}, "", false
	}
	previousUDID = cfg.SimulatorUDID
	invalidateTargetCommandCache(run)
	fresh, err := LoadConfig(c.Root)
	if err != nil {
		return Config{}, "", false
	}
	c.resolveConfigTools(&fresh)
	c.resolveConfigTarget(&fresh)
	if fresh.SimulatorUDID == "" || fresh.SimulatorUDID == previousUDID {
		return Config{}, "", false
	}
	return fresh, previousUDID, true
}

// dispatchWithStaleTargetRetry runs dispatch once against cfg into its own
// buffer (dispatch is expected to write exactly the shape any mav command
// writes: one leading "ok "/"fail " line via Output.Write, optionally
// followed by data lines like a tree dump) and, only when that first line
// is a failure AND isSimulatorBooted confirms the resolved simulator is
// genuinely not booted right now, either:
//
//   - retries the exact same dispatch once against a target_command-
//     re-resolved cfg, annotating the retried attempt's own first line with
//     target_command_restale so a re-resolve-and-retry is never silent -- a
//     silent one would hide that the simulator was going down underneath;
//   - or, when there is nobody to re-ask (target_command not in effect),
//     annotates the original failure with reason=simulator_not_booted so the
//     caller sees the real cause instead of a driver's raw, possibly
//     ambiguous stderr.
//
// Exactly one retry, never a loop: staleSimulatorTargetRetry is called once
// and its result dispatched once, whatever that second attempt produces
// (success or a fresh failure) is final.
func (c CLI) dispatchWithStaleTargetRetry(cfg Config, dispatch func(CLI, Config)) string {
	var buf bytes.Buffer
	dispatch(c.withStdout(&buf), cfg)
	out := buf.String()
	if !strings.HasPrefix(strings.TrimSpace(out), "fail ") {
		return out
	}
	if targetKind(cfg) != drivers.KindSim || cfg.SimulatorUDID == "" || isSimulatorBooted(c.Runner, cfg.SimulatorUDID) {
		return out
	}
	if !c.targetCommandInEffectForRun() {
		return insertFieldIntoFirstLine(out, "reason", "simulator_not_booted")
	}
	fresh, previousUDID, retried := c.staleSimulatorTargetRetry(cfg)
	if !retried {
		return insertFieldIntoFirstLine(out, "reason", "simulator_not_booted")
	}
	var buf2 bytes.Buffer
	dispatch(c.withStdout(&buf2), fresh)
	note := fmt.Sprintf(
		"target_command_restale: cached simulator %s was no longer booted; re-resolved target_command to %s and retried once",
		previousUDID, fresh.SimulatorUDID)
	return insertFieldIntoFirstLine(buf2.String(), "target_command_restale", note)
}

// insertFieldIntoFirstLine appends key=value to the end of output's first
// line (the "ok "/"fail " Output.Write line) and leaves everything after
// the first newline (data lines like a tree dump) untouched. Appending
// rather than re-parsing/re-sorting the existing fields keeps this safe
// regardless of what else is already on the line, quoted or not -- it never
// has to understand fields it didn't write.
func insertFieldIntoFirstLine(output, key, value string) string {
	line := output
	rest := ""
	if idx := strings.IndexByte(output, '\n'); idx >= 0 {
		line = output[:idx]
		rest = output[idx:]
	}
	return line + " " + key + "=" + quoteIfNeeded(value) + rest
}

func secondLine(s string) string {
	lines := strings.SplitN(s, "\n", 3)
	if len(lines) < 2 {
		return ""
	}
	return strings.TrimSpace(lines[1])
}

// targetCommandKeepAliveInterval is how often `mav run` reinvokes a
// configured target_command purely as a liveness signal while the run is in
// flight -- independent of, and more frequent than, the run-scoped
// target-command cache (see bootedSimulatorCacheTTL) that only re-resolves
// when some *other* step happens to ask.
//
// It exists to cover the one gap that cache leaves: a single long-running
// step -- a build inside a launch recipe, run through the same `exec` path
// as a flow's own exec steps -- can go minutes without mav dispatching any
// command that would touch target_command at all. A pool manager on the
// other end of target_command that expires its reservation by wall-clock
// TTL (simpool's `lease` is exactly this) has no way to know the run is
// still alive during that gap, and reclaims the slot out from under it --
// precisely the collision target_command exists to prevent in the first
// place. Comfortably under any TTL a pool manager plausibly uses (simpool's
// own default is 3 minutes) so a ping every interval never lets that TTL
// lapse, however long the step in between runs.
var targetCommandKeepAliveInterval = 60 * time.Second

// targetCommandInEffect reports whether target_command is actually what
// resolved raw's target, as opposed to being dead configuration a pin or an
// explicit MAV_TARGET_* env var already preempted (see
// resolveConfigTargetCommand). raw must be an unresolved Config -- i.e.
// straight from LoadConfig, before resolveConfigTarget has had a chance to
// fill SimulatorUDID in from target_command or the booted fallback --
// otherwise every run would look like target_command is in effect, since
// resolution always leaves SimulatorUDID non-empty on success.
func targetCommandInEffect(raw Config) bool {
	if targetKind(raw) != drivers.KindSim {
		return false
	}
	if raw.SimulatorUDID != "" {
		return false
	}
	return strings.TrimSpace(raw.TargetCommand) != ""
}

// targetCommandInEffectForRun re-loads config fresh (unresolved) to answer
// targetCommandInEffect for the CLI's project root. A second LoadConfig
// alongside mustLoadConfig's own is deliberate, not an oversight -- by the
// time mustLoadConfig returns, its Config has already been resolved in
// place, so the pin-vs-target_command distinction targetCommandInEffect
// needs is gone from that copy.
func (c CLI) targetCommandInEffectForRun() bool {
	raw, err := LoadConfig(c.Root)
	if err != nil {
		return false
	}
	return targetCommandInEffect(raw)
}

// startTargetCommandKeepAlive starts the background ping described at
// targetCommandKeepAliveInterval and returns a func that stops it; callers
// (runFlow) defer the stop so it never outlives the run. A no-op (returning
// an already-inert stop func) whenever target_command isn't actually in
// effect for this run, target is a physical device, or there's no Runner to
// dispatch through (e.g. a test CLI with Runner left nil) -- pinging a
// command that had no say in the run's target would just add noise, not
// safety.
//
// cfg must already be resolved (mustLoadConfig's return), since it supplies
// both the command to reinvoke and the original UDID every ping is compared
// against -- this never re-resolves or mutates cfg itself; see
// pingTargetCommandKeepAlive for why.
func (c CLI) startTargetCommandKeepAlive(run RunState, cfg Config, inEffect bool) func() {
	command := strings.TrimSpace(cfg.TargetCommand)
	originalUDID := targetUDID(cfg)
	if !inEffect || command == "" || c.Runner == nil || originalUDID == "" {
		return func() {}
	}
	root := cfg.Root
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(targetCommandKeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.pingTargetCommandKeepAlive(run, root, command, originalUDID)
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

// pingTargetCommandKeepAlive reinvokes target_command once, purely for its
// side effect on whatever pool manager it talks to -- it never changes
// which UDID the run dispatches against. The run already fixed that for its
// whole lifetime the moment bindFlowTarget captured it, and that has to
// stay true even if this ping resolves to something else: switching
// simulators mid-run would not prevent the collision a stolen slot
// represents, it would just relocate it somewhere a step already in flight
// isn't expecting. A mismatch or a failure is logged to the run's own logs
// (the same appendFile sink worker.go's own lease-expiry warning uses) as
// an actionable signal, never returned as an error -- symmetric with
// execTargetCommand's own single-command fallback: this must never fail or
// hang the run it's protecting.
func (c CLI) pingTargetCommandKeepAlive(run RunState, root, command, originalUDID string) {
	udid, _, warn := c.execTargetCommand(root, command)
	switch {
	case warn != "":
		appendFile(run.LogsPath, "mav target_command keepalive: "+warn+"\n")
	case udid != originalUDID:
		appendFile(run.LogsPath, fmt.Sprintf(
			"mav target_command keepalive: target_command now resolves to %s, but this run is pinned to %s and will keep using it -- next: something else may have taken this run's slot; check the pool manager behind target_command\n",
			udid, originalUDID))
	}
}
