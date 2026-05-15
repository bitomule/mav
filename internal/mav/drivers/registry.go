package drivers

import (
	"sort"
	"sync"
)

// Registry holds the set of Driver implementations available to the router.
// Drivers register themselves via a package init() (e.g. drivers/axe/axe.go
// imports the parent package and calls drivers.Register(&Axe{})). Tests can
// create a fresh Registry with NewRegistry() to avoid leaking state.
type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{drivers: map[string]Driver{}}
}

// defaultRegistry is the process-wide registry used by Register / Lookup /
// All. Tests that need isolation should use their own *Registry.
var defaultRegistry = NewRegistry()

// Default returns the process-wide registry.
func Default() *Registry { return defaultRegistry }

// Register adds d to the registry. Panics if a driver with the same ID is
// already registered — duplicate registration almost always means an import
// loop or a stale entry, not a useful overwrite.
func (r *Registry) Register(d Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.drivers[d.ID()]; exists {
		panic("drivers: duplicate registration: " + d.ID())
	}
	r.drivers[d.ID()] = d
}

// Unregister removes a driver by ID. No-op if absent. Useful in tests.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.drivers, id)
}

// Lookup returns the driver with the given ID, or nil if not present.
func (r *Registry) Lookup(id string) Driver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.drivers[id]
}

// All returns all registered drivers sorted by ID for deterministic output
// (doctor, debug logs, etc.).
func (r *Registry) All() []Driver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Driver, 0, len(r.drivers))
	for _, d := range r.drivers {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// Register is the shortcut for Default().Register(d).
func Register(d Driver) { defaultRegistry.Register(d) }

// Unregister is the shortcut for Default().Unregister(id).
func Unregister(id string) { defaultRegistry.Unregister(id) }

// Lookup is the shortcut for Default().Lookup(id).
func Lookup(id string) Driver { return defaultRegistry.Lookup(id) }

// All is the shortcut for Default().All().
func All() []Driver { return defaultRegistry.All() }
