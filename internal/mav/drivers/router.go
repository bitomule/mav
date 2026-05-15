package drivers

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Router selects the best driver for an operation. The decision rule, in order:
//
//  1. If prefer is non-empty and the named driver provides cap for target and
//     is healthy, use it. If the named driver exists but doesn't provide cap
//     (or is unhealthy), Route returns a structured error — the caller chose
//     it explicitly.
//  2. Otherwise, collect every driver that provides cap for target, filter to
//     healthy ones, and sort by Cost(cap, target) ascending. Stable secondary
//     sort by ID for determinism.
//  3. If no driver remains, return ErrNoDriver with the list of considered
//     drivers and why each was rejected.
type Router struct {
	registry *Registry
	health   map[string]HealthReport // cached probes; populated lazily
	probe    Probe
	disabled map[string]struct{} // driver IDs the user opted out (MAV_DRIVERS_DISABLE)
}

// NewRouter builds a router over the given registry, using probe to run health
// checks on demand. Pass nil for registry to use the process-wide default.
// disabled is an opt-out list (e.g. parsed from MAV_DRIVERS_DISABLE).
func NewRouter(registry *Registry, probe Probe, disabled []string) *Router {
	if registry == nil {
		registry = Default()
	}
	if probe == nil {
		probe = RealProbe()
	}
	set := make(map[string]struct{}, len(disabled))
	for _, id := range disabled {
		id = strings.TrimSpace(id)
		if id != "" {
			set[id] = struct{}{}
		}
	}
	return &Router{
		registry: registry,
		probe:    probe,
		health:   map[string]HealthReport{},
		disabled: set,
	}
}

// ErrNoDriver is returned when no driver can serve the request.
type ErrNoDriver struct {
	Capability Capability
	Target     Target
	Considered []DriverRejection
}

// DriverRejection records why a candidate driver was not picked. The caller
// (cli.go) folds these into the user-visible error so agents can self-diagnose.
type DriverRejection struct {
	ID     string
	Reason string
}

func (e *ErrNoDriver) Error() string {
	parts := make([]string, 0, len(e.Considered))
	for _, r := range e.Considered {
		parts = append(parts, fmt.Sprintf("%s: %s", r.ID, r.Reason))
	}
	return fmt.Sprintf("no driver for %s on %s (considered: %s)",
		e.Capability, e.Target.Kind, strings.Join(parts, "; "))
}

// Route picks the best driver for cap on target. prefer is an optional driver
// ID hint (from --prefer-driver). Returns the Driver and the rejection list of
// drivers that lost, useful for verbose logging.
func (r *Router) Route(ctx context.Context, cap Capability, target Target, prefer string) (Driver, []DriverRejection, error) {
	candidates := r.candidates(cap, target)

	// Honour explicit prefer when valid.
	if prefer != "" {
		if d := r.registry.Lookup(prefer); d != nil {
			if _, disabled := r.disabled[prefer]; disabled {
				return nil, nil, fmt.Errorf("driver %q disabled via MAV_DRIVERS_DISABLE", prefer)
			}
			if !d.Provides(target).Has(cap) {
				return nil, nil, fmt.Errorf("driver %q does not provide %s on %s", prefer, cap, target.Kind)
			}
			report := r.healthOf(ctx, d)
			if !report.IsHealthy() {
				return nil, nil, fmt.Errorf("driver %q unhealthy: %s", prefer, report.Detail)
			}
			return d, rejectionsExcept(candidates, prefer), nil
		}
		return nil, nil, fmt.Errorf("driver %q not registered", prefer)
	}

	// No prefer: filter healthy + non-disabled, sort by cost, return head.
	var healthy []Driver
	var rejected []DriverRejection
	for _, d := range candidates {
		if _, off := r.disabled[d.ID()]; off {
			rejected = append(rejected, DriverRejection{ID: d.ID(), Reason: "disabled"})
			continue
		}
		report := r.healthOf(ctx, d)
		if !report.IsHealthy() {
			rejected = append(rejected, DriverRejection{ID: d.ID(), Reason: string(report.State) + ": " + report.Detail})
			continue
		}
		healthy = append(healthy, d)
	}
	if len(healthy) == 0 {
		return nil, rejected, &ErrNoDriver{Capability: cap, Target: target, Considered: rejected}
	}
	sort.SliceStable(healthy, func(i, j int) bool {
		ci := healthy[i].Cost(cap, target)
		cj := healthy[j].Cost(cap, target)
		if ci != cj {
			return ci < cj
		}
		return healthy[i].ID() < healthy[j].ID()
	})
	picked := healthy[0]
	for _, d := range healthy[1:] {
		rejected = append(rejected, DriverRejection{ID: d.ID(), Reason: fmt.Sprintf("higher cost (%d)", d.Cost(cap, target))})
	}
	return picked, rejected, nil
}

// candidates returns every registered driver that declares cap for target,
// before any health/disabled filtering.
func (r *Router) candidates(cap Capability, target Target) []Driver {
	var out []Driver
	for _, d := range r.registry.All() {
		if d.Provides(target).Has(cap) {
			out = append(out, d)
		}
	}
	return out
}

// healthOf returns a cached or freshly computed health report for d.
func (r *Router) healthOf(ctx context.Context, d Driver) HealthReport {
	if report, ok := r.health[d.ID()]; ok {
		return report
	}
	report := d.Probe(ctx, r.probe)
	r.health[d.ID()] = report
	return report
}

// InvalidateHealth drops the cached health for one driver (or all if id == "").
// Used by `mav doctor` to force a fresh probe.
func (r *Router) InvalidateHealth(id string) {
	if id == "" {
		r.health = map[string]HealthReport{}
		return
	}
	delete(r.health, id)
}

func rejectionsExcept(drivers []Driver, exceptID string) []DriverRejection {
	var out []DriverRejection
	for _, d := range drivers {
		if d.ID() == exceptID {
			continue
		}
		out = append(out, DriverRejection{ID: d.ID(), Reason: "not preferred"})
	}
	return out
}
