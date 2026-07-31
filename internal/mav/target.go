package mav

import (
	"encoding/json"
	"os"
	"path/filepath"
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
