package mav

import (
	"context"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func TestRegisterDefaultDriversWiresAllBridges(t *testing.T) {
	reg := drivers.NewRegistry()
	RegisterDefaultDrivers(reg, NewExecutor(fakeRunner{}))

	want := []string{"axe", "baguette", "idb", "mitmproxy", "simctl"}
	got := reg.All()
	if len(got) != len(want) {
		t.Fatalf("expected %d drivers, got %d", len(want), len(got))
	}
	for i, d := range got {
		if d.ID() != want[i] {
			t.Errorf("[%d] got=%s want=%s", i, d.ID(), want[i])
		}
	}
}

// TestRouterPicksAxeForSemanticTapWhenInstalled exercises the bridge
// registration plus the router on a sim target: with axe installed and idb
// also installed (degraded), the router must pick axe (cost 0 vs idb cost 200).
func TestRouterPicksAxeForSemanticTapWhenInstalled(t *testing.T) {
	reg := drivers.NewRegistry()
	runner := fakeRunner{tools: map[string]bool{"axe": true, "idb": true, "xcrun": true}}
	RegisterDefaultDrivers(reg, NewExecutor(runner))

	router := drivers.NewRouter(reg, drivers.RealProbe(), nil)
	// We pass the runner's LookPath via a small adapter so the test doesn't
	// touch the real PATH.
	router = drivers.NewRouter(reg, NewExecutor(runner), nil)

	picked, _, err := router.Route(context.Background(), drivers.CapSemanticTap, drivers.Target{Kind: drivers.KindSim}, "")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if picked.ID() != "axe" {
		t.Fatalf("expected axe, got %s", picked.ID())
	}
}

// TestRouterFallsBackToIdbForCoordTapWhenAxeAbsent confirms the bridge driver
// idb still serves CapCoordTap on device when axe is missing -- this preserves
// today's behaviour until P3 replaces idb with goios.
func TestRouterFallsBackToIdbForCoordTapWhenAxeAbsent(t *testing.T) {
	reg := drivers.NewRegistry()
	runner := fakeRunner{tools: map[string]bool{"idb": true}}
	RegisterDefaultDrivers(reg, NewExecutor(runner))

	router := drivers.NewRouter(reg, NewExecutor(runner), nil)
	picked, _, err := router.Route(context.Background(), drivers.CapCoordTap, drivers.Target{Kind: drivers.KindDevice}, "")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if picked.ID() != "idb" {
		t.Fatalf("expected idb, got %s", picked.ID())
	}
}

func TestRouterPicksBaguetteForSimulatorSystemTreeAndKeyboard(t *testing.T) {
	reg := drivers.NewRegistry()
	runner := fakeRunner{tools: map[string]bool{"baguette": true}}
	RegisterDefaultDrivers(reg, NewExecutor(runner))

	router := drivers.NewRouter(reg, NewExecutor(runner), nil)
	target := drivers.Target{Kind: drivers.KindSim}
	for _, cap := range []drivers.Capability{drivers.CapTreeSystem, drivers.CapErase, drivers.CapHideKeyboard} {
		picked, _, err := router.Route(context.Background(), cap, target, "")
		if err != nil {
			t.Fatalf("route %s failed: %v", cap, err)
		}
		if picked.ID() != "baguette" {
			t.Fatalf("cap %s: expected baguette, got %s", cap, picked.ID())
		}
	}
}

func TestRouterPicksMitmproxyForSimulatorNetworkCapture(t *testing.T) {
	reg := drivers.NewRegistry()
	runner := fakeRunner{tools: map[string]bool{"mitmdump": true}}
	RegisterDefaultDrivers(reg, NewExecutor(runner))

	router := drivers.NewRouter(reg, NewExecutor(runner), nil)
	picked, _, err := router.Route(context.Background(), drivers.CapNetworkCapture, drivers.Target{Kind: drivers.KindSim}, "")
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if picked.ID() != "mitmproxy" {
		t.Fatalf("expected mitmproxy, got %s", picked.ID())
	}
}

func TestRouterReportsNoDriverWhenNothingInstalled(t *testing.T) {
	reg := drivers.NewRegistry()
	runner := fakeRunner{} // nothing on PATH
	RegisterDefaultDrivers(reg, NewExecutor(runner))

	router := drivers.NewRouter(reg, NewExecutor(runner), nil)
	_, _, err := router.Route(context.Background(), drivers.CapSemanticTap, drivers.Target{Kind: drivers.KindSim}, "")
	if err == nil {
		t.Fatal("expected error when no drivers healthy")
	}
}
