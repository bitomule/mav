package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitomule/mav/internal/mav/codes"
	"github.com/bitomule/mav/internal/mav/drivers"
)

// targetKind is the only place that decides WHAT a target IS. It returns
// the router's enum (drivers.TargetKind) instead of a bool so adding a
// third kind means adding a case, not auditing every if in the CLI.
func targetKind(cfg Config) drivers.TargetKind {
	switch cfg.TargetKind {
	case "device":
		return drivers.KindDevice
	case "macos":
		return drivers.KindMac
	default:
		return drivers.KindSim
	}
}

// targetKindLabel is the PUBLIC spelling of a TargetKind: it is what shows
// up in the CLI output's target_kind field, in MAV_TARGET_KIND and in the
// target_kind of .mav/config.yaml. It is deliberately NOT string(kind)
// ("sim"), which is an internal routing token: "simulator"/"device" are
// contract with the agents and with the config.yaml files already on disk.
func targetKindLabel(kind drivers.TargetKind) string {
	switch kind {
	case drivers.KindDevice:
		return "device"
	case drivers.KindMac:
		return "macos"
	default:
		return "simulator"
	}
}

func targetUDID(cfg Config) string {
	switch targetKind(cfg) {
	case drivers.KindDevice:
		return cfg.DeviceUDID
	case drivers.KindMac:
		// A macOS app has no UDID: the machine is this one. Returning
		// something here would make withResolvedTarget report a fake
		// identifier.
		return ""
	default:
		return cfg.SimulatorUDID
	}
}

func targetName(cfg Config) string {
	switch targetKind(cfg) {
	case drivers.KindDevice:
		return cfg.DeviceName
	case drivers.KindMac:
		return "localhost"
	default:
		return cfg.SimulatorName
	}
}

func targetRuntime(cfg Config) string {
	switch targetKind(cfg) {
	case drivers.KindDevice, drivers.KindMac:
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
	// A resolution error here cannot fail the command: this only decorates
	// the `ok` line of a command that has already run, and by definition it
	// resolved its own target successfully to get this far (the real call
	// sites return c.failTargetCommand instead). Reporting it in the same
	// warn field is the honest thing left to do -- it means target_command
	// stopped working between dispatch and reporting.
	warn, resolveErr := c.resolveConfigTarget(&cfg)
	if resolveErr != nil {
		var tcErr *targetCommandError
		if errors.As(resolveErr, &tcErr) {
			warn = tcErr.message()
		} else {
			warn = resolveErr.Error()
		}
	}
	if warn != "" {
		fields["target_command_warn"] = warn
	}
	// resolveConfigTarget already applies the booted-simulator fallback to
	// cfg itself (see its doc comment), so cfg is fully resolved by now --
	// no separate fallback needed here just for the reported fields.
	// The active profile is reported for the same reason as
	// udid/target_kind: in hot use, an agent chains loose commands and
	// needs to read from the response what it is operating against, instead
	// of remembering it.
	if cfg.ActiveProfile != "" {
		if _, ok := fields["profile"]; !ok {
			fields["profile"] = cfg.ActiveProfile
		}
	}
	// Reported outside the profile branch: `vm` is a base-level field too,
	// and an agent chaining loose commands has no other way to tell whether
	// what it just drove was the VM's app or this machine's.
	if cfg.VM {
		if _, ok := fields["vm"]; !ok {
			fields["vm"] = "true"
		}
	}
	udid := targetUDID(cfg)
	name := targetName(cfg)
	kind := targetKindLabel(targetKind(cfg))
	// The kind is ALWAYS reported, also when there is no udid. The guard
	// below exists for the "could not resolve which simulator" case, but a
	// macOS target has no udid by definition, the machine is this one, and
	// returning early left the agent not knowing which platform it is
	// driving, which is exactly the datum that most changes what it can ask
	// for next.
	if _, ok := fields["target_kind"]; !ok {
		fields["target_kind"] = kind
	}
	if udid == "" {
		if name != "" {
			if _, ok := fields["target_name"]; !ok {
				fields["target_name"] = name
			}
		}
		return fields
	}
	fields["udid"] = udid
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
func (c CLI) resolveConfigTarget(cfg *Config) (string, error) {
	if targetKind(*cfg) != drivers.KindSim {
		return "", nil
	}
	if os.Getenv("MAV_TARGET_KIND") != "" {
		return "", nil
	}
	warn, err := c.resolveConfigTargetCommand(cfg)
	if err != nil {
		// Deliberately before the booted-simulator fallback below: a
		// required target_command that did not deliver must leave the
		// caller with nothing to dispatch against, not with a simulator
		// nobody chose. This early return is the whole fix.
		return "", err
	}
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
	return warn, nil
}

// resolveConfigTargetCommand runs target_command (cases 3/4 of the
// precedence order) and applies a successful result to cfg, or explains why
// it didn't fire. Split out of resolveConfigTarget so the booted-simulator
// fallback above applies the same way regardless of which branch below
// returns -- a pin, an empty target_command, or a target_command that ran
// and failed all leave cfg.SimulatorUDID exactly as unresolved as each
// other from that fallback's point of view.
func (c CLI) resolveConfigTargetCommand(cfg *Config) (string, error) {
	command := strings.TrimSpace(cfg.TargetCommand)
	if cfg.SimulatorUDID != "" {
		if command == "" {
			return "", nil
		}
		return fmt.Sprintf("target_command_ignored: simulator_udid=%s is pinned in .mav/config.yaml and wins over target_command (next: remove simulator_udid to let target_command route automatically, or remove target_command if the pin is intentional)", cfg.SimulatorUDID), nil
	}
	if command == "" {
		return "", nil
	}
	timeout, err := targetCommandTimeoutFor(*cfg)
	if err != nil {
		return "", err
	}
	udid, name, warn, err := c.resolveTargetCommand(cfg.Root, command, timeout, targetCommandRequired(*cfg))
	if err != nil {
		return "", err
	}
	if udid == "" {
		return warn, nil
	}
	cfg.SimulatorUDID = udid
	cfg.SimulatorName = name
	return "", nil
}

// targetCommandRequired answers whether a configured target_command is the
// only acceptable source of the simulator. Unset means yes. The strict
// behavior is the default on purpose: `target_command:` in config.yaml is
// the user saying "this is how the target is chosen", and answering its
// failure by driving whatever simulator happened to be booted contradicts
// that -- silently, since almost every call site discarded the warning and
// any caller redirecting stdout (a screenshot script piping `mav run` to
// /dev/null) never saw it at all. Making strictness opt-out rather than
// opt-in means nobody keeps the silent behavior by inaction; a project that
// genuinely wants best-effort routing says so once, in writing.
func targetCommandRequired(cfg Config) bool {
	if cfg.TargetCommandRequired == nil {
		return true
	}
	return *cfg.TargetCommandRequired
}

// targetCommandTimeoutFor resolves target_command_timeout, defaulting to
// defaultTargetCommandTimeout. An unparseable value is a hard error rather
// than a silent fall back to the default: quietly substituting a different
// value for the one the config states is the same class of bug this change
// exists to close.
func targetCommandTimeoutFor(cfg Config) (time.Duration, error) {
	raw := strings.TrimSpace(cfg.TargetCommandTimeout)
	if raw == "" {
		return defaultTargetCommandTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, &targetCommandError{
			code:    codes.TargetCommandTimeoutInvalid,
			command: strings.TrimSpace(cfg.TargetCommand),
			detail:  fmt.Sprintf("target_command_timeout=%q is not a positive Go duration", raw),
			next:    "set target_command_timeout to a Go duration such as 90s or 3m",
		}
	}
	return d, nil
}

// defaultTargetCommandTimeout bounds a single target_command invocation so
// a hanging pool manager can never hang mav itself. It was 10s, which no
// real consumer could meet: target_command is documented against simpool,
// simpool's `lease` blocks on `xcrun simctl bootstatus` before printing a
// UDID, and a cold lease takes about two minutes. A default the one known
// consumer cannot meet is a bug in the default, so it is now three minutes
// -- past the documented cold-lease cost, and still bounded. It matches
// rather than undercuts simpool's own default lease TTL (`simpool lease
// -ttl`, 3m0s), so a lease that burns the whole timeout can return with its
// own TTL already spent; the keepalive that renews it only starts once
// resolution has returned. Cutting the default below the TTL instead would
// leave under a minute of headroom over the documented cold-lease cost,
// which is the original bug again. A pool manager that is slower, or whose
// TTL is shorter, says so with target_command_timeout.
const defaultTargetCommandTimeout = 3 * time.Minute

// targetCommandError is a target_command that did not name a simulator. It
// carries the command, the timeout it was given and the underlying detail,
// so the failure a caller prints names all three -- "target_command failed"
// on its own sends the reader back to the config to guess which part.
type targetCommandError struct {
	code    codes.Code
	command string
	timeout time.Duration
	detail  string
	// next is the remediation used by the non-required (opt-out) wording,
	// where the sentence has to end in "falling back to the booted
	// simulator" rather than in the code's own remediation.
	next string
}

// Error is the bare code id, matching this package's convention that a
// command error's text IS its code (see outputErr/commandOutputErr): a flow
// step's error surfaces verbatim as the run's `code=` field, so anything
// longer would emit a quoted sentence where the vocabulary belongs. The
// detail travels in fields(); message() is for logs, which have no code
// field to put it in.
func (e *targetCommandError) Error() string { return e.code.ID }

func (e *targetCommandError) message() string {
	return fmt.Sprintf("%s: %s (target_command: %s)", e.code.ID, e.detail, e.command)
}

// warn renders the pre-existing warn-and-fall-back wording, emitted only
// when target_command_required is explicitly false.
func (e *targetCommandError) warn() string {
	return fmt.Sprintf("%s: %s (next: %s; falling back to the booted simulator)", e.code.ID, e.detail, e.next)
}

// fields spells out what failed for the structured `fail code=...` line,
// including fallback=none: the single most important thing for a reader who
// remembers the old behavior is that mav did not quietly pick a simulator
// of its own.
func (e *targetCommandError) fields() map[string]string {
	f := map[string]string{
		"target_command": e.command,
		"detail":         e.detail,
		"fallback":       "none",
	}
	if e.timeout > 0 {
		// Not `timeout`: a flow step merges these fields over its own
		// params, and `timeout` is a first-class param on waitFor and
		// scrollUntil. Two different timeouts under one key is worse than
		// a longer name.
		f["target_command_timeout"] = e.timeout.String()
	}
	return f
}

// targetCommandWarnText renders a resolution error for the places that
// report it rather than fail on it -- `mav doctor`, whose whole job is to
// diagnose, and teardown paths whose work cannot be retried later.
func targetCommandWarnText(err error) string {
	var tcErr *targetCommandError
	if errors.As(err, &tcErr) {
		return tcErr.message()
	}
	return err.Error()
}

// failTargetCommand writes the structured failure for a target_command that
// was required and did not deliver a simulator. Every command that resolves
// a target routes its resolution error through here, so the failure has the
// same shape wherever it surfaces.
func (c CLI) failTargetCommand(err error) error {
	var tcErr *targetCommandError
	if errors.As(err, &tcErr) {
		return FailCode(tcErr.code, tcErr.fields()).Write(c.Stdout)
	}
	return Fail("target_command_failed", map[string]string{"error": err.Error(), "fallback": "none"}).Write(c.Stdout)
}

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
// Failures are cached only when required is false. That asymmetry is
// deliberate. Negative caching exists to stop a run that *keeps going*
// after a failed resolution from paying the timeout again on every one of
// the dozens of commands that follow -- which is precisely the non-required
// case. When target_command is required the command fails and the process
// exits, so there is no such sequence to protect, and caching the failure
// would only mean the next invocation inside the TTL fails on evidence it
// never re-tested: a run started right after a slow cold lease would fail
// without ever retrying. That is the reported bug, not a mitigation of it.
func (c CLI) resolveTargetCommand(root, command string, timeout time.Duration, required bool) (string, string, string, error) {
	if c.Runner == nil {
		return "", "", "", nil
	}
	resolve := func() (string, string, string, error) {
		udid, name, cmdErr := c.execTargetCommand(root, command, timeout)
		if cmdErr != nil {
			if required {
				return "", "", "", cmdErr
			}
			return "", "", cmdErr.warn(), nil
		}
		return udid, name, "", nil
	}
	run, err := c.resolveRun("")
	if err != nil {
		// No run to cache against yet (e.g. a standalone command before
		// `mav open`); resolve fresh. Rare, never the hot loop.
		return resolve()
	}
	if cache, ok := readTargetCommandCache(run); ok {
		return cache.UDID, cache.Name, cache.Warn, nil
	}
	udid, name, warn, resolveErr := resolve()
	if resolveErr != nil {
		return "", "", "", resolveErr
	}
	writeTargetCommandCache(run, udid, name, warn)
	return udid, name, warn, nil
}

// execTargetCommand runs the configured command through /bin/bash -lc, the
// same shell invocation flows already use for `exec` steps, so target_command
// can be anything from a bare binary to a pipeline. Its only contract with
// mav is: print the UDID to use on stdout. It runs from the project root
// (like launch and exec-step commands) with MAV_ROOT exported, so a project
// can point target_command at a repo-relative script.
func (c CLI) execTargetCommand(root, command string, timeout time.Duration) (string, string, *targetCommandError) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	prefixed := shellEnvPrefix(map[string]string{"MAV_ROOT": root}) + " " + command
	timedOut := &targetCommandError{
		code:    codes.TargetCommandTimeout,
		command: command,
		timeout: timeout,
		detail:  fmt.Sprintf("no UDID after %s", timeout),
		next:    "raise target_command_timeout in .mav/config.yaml, or make target_command return faster",
	}
	// The deadline is enforced here rather than left to the runner, because
	// exec.CommandContext kills the direct child only. target_command runs
	// through `/bin/bash -lc`, and a grandchild that inherits the output
	// pipe keeps cmd.Wait blocked long after the shell is dead -- so
	// waiting on Run would mean waiting out the grandchild instead of the
	// timeout, which is exactly the hang the timeout exists to prevent.
	// Returning on ctx.Done() leaves that Run goroutine to finish into a
	// buffered channel nobody reads; it is bounded (one per target_command
	// invocation, and mav is about to exit or, in a run, ping again only
	// once a minute) and it is the price of not making every other command
	// mav runs -- launch recipes that legitimately background a helper
	// holding stdout -- subject to the same cutoff.
	done := make(chan CommandResult, 1)
	go func() { done <- c.Runner.Run(ctx, "/bin/bash", "-lc", prefixed) }()
	var result CommandResult
	select {
	case result = <-done:
	case <-ctx.Done():
		return "", "", timedOut
	}
	if result.Err != nil {
		// A timeout gets its own code: the command may well still be
		// working, and "raise the timeout" is a different next step from
		// "fix the command". ctx is the authority here, not the runner's
		// error text, which is only whatever the shell chose to say about
		// being killed.
		if ctx.Err() == context.DeadlineExceeded {
			return "", "", timedOut
		}
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = result.Err.Error()
		}
		return "", "", &targetCommandError{
			code:    codes.TargetCommandFailed,
			command: command,
			timeout: timeout,
			detail:  firstLine(detail),
			next:    "fix or remove target_command in .mav/config.yaml",
		}
	}
	udid := strings.TrimSpace(firstLine(result.Stdout))
	if udid == "" {
		return "", "", &targetCommandError{
			code:    codes.TargetCommandEmpty,
			command: command,
			timeout: timeout,
			detail:  "command produced no output",
			next:    "target_command must print a simulator UDID on stdout",
		}
	}
	name := strings.TrimSpace(secondLine(result.Stdout))
	return udid, name, nil
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
	if _, err := c.resolveConfigTarget(&fresh); err != nil {
		// Retrying did not produce a usable target either. ok=false leaves
		// the caller reporting the original dispatch failure, which is the
		// one the operator was already looking at.
		return Config{}, "", false
	}
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
	// A malformed target_command_timeout has already failed the command
	// that started this run, so the fallback here is unreachable in
	// practice; it exists so the keepalive never has to decide what to do
	// with an error it cannot report to anyone.
	timeout, err := targetCommandTimeoutFor(cfg)
	if err != nil {
		timeout = defaultTargetCommandTimeout
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(targetCommandKeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.pingTargetCommandKeepAlive(run, root, command, timeout, originalUDID)
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
func (c CLI) pingTargetCommandKeepAlive(run RunState, root, command string, timeout time.Duration, originalUDID string) {
	udid, _, cmdErr := c.execTargetCommand(root, command, timeout)
	switch {
	case cmdErr != nil:
		appendFile(run.LogsPath, "mav target_command keepalive: "+cmdErr.message()+"\n")
	case udid != originalUDID:
		appendFile(run.LogsPath, fmt.Sprintf(
			"mav target_command keepalive: target_command now resolves to %s, but this run is pinned to %s and will keep using it -- next: something else may have taken this run's slot; check the pool manager behind target_command\n",
			udid, originalUDID))
	}
}
