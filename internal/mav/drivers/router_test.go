package drivers

import (
	"context"
	"errors"
	"testing"
)

// fakeDriver is a Driver that lets each test dial in capabilities, cost, and
// health. It implements TapDriver so router_test can also exercise typed lookup.
type fakeDriver struct {
	id     string
	caps   CapabilitySet
	cost   map[Capability]int
	health HealthReport
}

func (f *fakeDriver) ID() string                                 { return f.id }
func (f *fakeDriver) Provides(_ Target) CapabilitySet            { return f.caps }
func (f *fakeDriver) Cost(c Capability, _ Target) int            { return f.cost[c] }
func (f *fakeDriver) Probe(context.Context, Probe) HealthReport  { return f.health }
func (f *fakeDriver) Warm(context.Context, Target) <-chan error  { ch := make(chan error); close(ch); return ch }
func (f *fakeDriver) Tap(context.Context, Target, TapSpec) (TapResult, error) {
	return TapResult{}, nil
}

func okDriver(id string, cost int, caps ...Capability) *fakeDriver {
	costs := map[Capability]int{}
	for _, c := range caps {
		costs[c] = cost
	}
	return &fakeDriver{
		id:     id,
		caps:   NewSet(caps...),
		cost:   costs,
		health: HealthReport{State: HealthOK},
	}
}

func TestRouterPicksLowestCostHealthy(t *testing.T) {
	reg := NewRegistry()
	reg.Register(okDriver("expensive", 100, CapSemanticTap))
	reg.Register(okDriver("cheap", 0, CapSemanticTap))
	reg.Register(okDriver("medium", 50, CapSemanticTap))

	router := NewRouter(reg, nil, nil)
	d, _, err := router.Route(context.Background(), CapSemanticTap, Target{Kind: KindSim}, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID() != "cheap" {
		t.Fatalf("expected cheap, got %s", d.ID())
	}
}

func TestRouterTieBreaksByIDForDeterminism(t *testing.T) {
	reg := NewRegistry()
	reg.Register(okDriver("bbb", 10, CapSemanticTap))
	reg.Register(okDriver("aaa", 10, CapSemanticTap))

	router := NewRouter(reg, nil, nil)
	d, _, err := router.Route(context.Background(), CapSemanticTap, Target{Kind: KindSim}, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID() != "aaa" {
		t.Fatalf("expected aaa (alphabetical tiebreak), got %s", d.ID())
	}
}

func TestRouterPreferHonoured(t *testing.T) {
	reg := NewRegistry()
	reg.Register(okDriver("cheap", 0, CapSemanticTap))
	reg.Register(okDriver("special", 100, CapSemanticTap))

	router := NewRouter(reg, nil, nil)
	d, rejected, err := router.Route(context.Background(), CapSemanticTap, Target{Kind: KindSim}, "special")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID() != "special" {
		t.Fatalf("expected special, got %s", d.ID())
	}
	if len(rejected) != 1 || rejected[0].ID != "cheap" {
		t.Fatalf("expected cheap in rejected list, got %+v", rejected)
	}
}

func TestRouterPreferUnregisteredErrors(t *testing.T) {
	reg := NewRegistry()
	reg.Register(okDriver("cheap", 0, CapSemanticTap))
	router := NewRouter(reg, nil, nil)
	_, _, err := router.Route(context.Background(), CapSemanticTap, Target{Kind: KindSim}, "ghost")
	if err == nil || !contains(err.Error(), "not registered") {
		t.Fatalf("expected not registered error, got %v", err)
	}
}

func TestRouterPreferLacksCapErrors(t *testing.T) {
	reg := NewRegistry()
	reg.Register(okDriver("typer", 0, CapType)) // does not provide CapSemanticTap
	router := NewRouter(reg, nil, nil)
	_, _, err := router.Route(context.Background(), CapSemanticTap, Target{Kind: KindSim}, "typer")
	if err == nil || !contains(err.Error(), "does not provide") {
		t.Fatalf("expected does-not-provide error, got %v", err)
	}
}

func TestRouterPreferUnhealthyErrors(t *testing.T) {
	reg := NewRegistry()
	broken := okDriver("broken", 0, CapSemanticTap)
	broken.health = HealthReport{State: HealthBroken, Detail: "missing tool"}
	reg.Register(broken)
	router := NewRouter(reg, nil, nil)
	_, _, err := router.Route(context.Background(), CapSemanticTap, Target{Kind: KindSim}, "broken")
	if err == nil || !contains(err.Error(), "unhealthy") {
		t.Fatalf("expected unhealthy error, got %v", err)
	}
}

func TestRouterSkipsUnhealthyWhenAutoRouting(t *testing.T) {
	reg := NewRegistry()
	broken := okDriver("broken", 0, CapSemanticTap)
	broken.health = HealthReport{State: HealthMissing, Detail: "tool absent"}
	reg.Register(broken)
	reg.Register(okDriver("backup", 50, CapSemanticTap))

	router := NewRouter(reg, nil, nil)
	d, rejected, err := router.Route(context.Background(), CapSemanticTap, Target{Kind: KindSim}, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID() != "backup" {
		t.Fatalf("expected backup, got %s", d.ID())
	}
	found := false
	for _, r := range rejected {
		if r.ID == "broken" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected broken in rejected list, got %+v", rejected)
	}
}

func TestRouterDisabledViaEnv(t *testing.T) {
	reg := NewRegistry()
	reg.Register(okDriver("primary", 0, CapSemanticTap))
	reg.Register(okDriver("backup", 50, CapSemanticTap))

	router := NewRouter(reg, nil, []string{"primary"})
	d, _, err := router.Route(context.Background(), CapSemanticTap, Target{Kind: KindSim}, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID() != "backup" {
		t.Fatalf("expected backup (primary disabled), got %s", d.ID())
	}

	// Explicit prefer to a disabled driver errors out.
	_, _, err = router.Route(context.Background(), CapSemanticTap, Target{Kind: KindSim}, "primary")
	if err == nil || !contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func TestRouterNoCandidateReturnsErrNoDriver(t *testing.T) {
	reg := NewRegistry()
	reg.Register(okDriver("noop", 0, CapType))

	router := NewRouter(reg, nil, nil)
	_, _, err := router.Route(context.Background(), CapPinch, Target{Kind: KindSim}, "")
	var nodErr *ErrNoDriver
	if !errors.As(err, &nodErr) {
		t.Fatalf("expected *ErrNoDriver, got %T %v", err, err)
	}
	if nodErr.Capability != CapPinch {
		t.Fatalf("wrong cap in error: %s", nodErr.Capability)
	}
}

func TestRouterProvidesPerTargetKind(t *testing.T) {
	// A driver that only provides CapPinch on sim, not on device.
	d := &fakeDriver{
		id:     "simonly",
		caps:   NewSet(CapPinch),
		cost:   map[Capability]int{CapPinch: 0},
		health: HealthReport{State: HealthOK},
	}
	reg := NewRegistry()
	reg.Register(d)

	router := NewRouter(reg, nil, nil)
	_, _, err := router.Route(context.Background(), CapPinch, Target{Kind: KindSim}, "")
	if err != nil {
		t.Fatalf("sim route should succeed, got %v", err)
	}
	// Now mark the driver as not providing CapPinch on device by replacing its Provides.
	d.caps = NewSet() // emulate: empty caps when target is device — done crudely for unit test
	_, _, err = router.Route(context.Background(), CapPinch, Target{Kind: KindDevice}, "")
	var nodErr *ErrNoDriver
	if !errors.As(err, &nodErr) {
		t.Fatalf("expected ErrNoDriver on device, got %v", err)
	}
}

func TestRegistryRejectsDuplicates(t *testing.T) {
	reg := NewRegistry()
	reg.Register(okDriver("dup", 0, CapTap))
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate Register")
		}
	}()
	reg.Register(okDriver("dup", 0, CapTap))
}

func TestRegistryAllSorted(t *testing.T) {
	reg := NewRegistry()
	reg.Register(okDriver("zeta", 0, CapTap))
	reg.Register(okDriver("alpha", 0, CapTap))
	reg.Register(okDriver("mu", 0, CapTap))
	got := reg.All()
	want := []string{"alpha", "mu", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("len got=%d want=%d", len(got), len(want))
	}
	for i, d := range got {
		if d.ID() != want[i] {
			t.Fatalf("[%d] got=%s want=%s", i, d.ID(), want[i])
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
