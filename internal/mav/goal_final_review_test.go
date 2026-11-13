package mav

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func TestGoalNavigationCountsUnreferencedWizardTitlesAsProgress(t *testing.T) {
	f := &goalFixture{o: goalStart()}
	f.o.Elements[1] = Element{ID: "next", Label: "Next", Role: "button", Frame: "0,50,100,40"}
	f.o.Refs = map[int]string{1: "next-ref"}
	f.o.RefActions = map[int]map[string]bool{1: {"tap": true}}
	f.onAct = func(f *goalFixture, _ GoalObservation, _ GoalOperation) GoalReceipt {
		f.o.Elements = append([]Element{}, f.o.Elements...)
		f.o.Elements[0].Label = fmt.Sprintf("Step %d", f.actions)
		if f.actions == 3 {
			f.o.Elements[0].Label = "Details"
		}
		return GoalReceipt{Delivery: "completed"}
	}
	r := runGoalFixture(t, f, `title:"Details"`)
	if r.Succeeded != "true" || f.actions != 3 {
		t.Fatalf("unreferenced route changes lost despite stable Next target: outcome=%s actions=%d steps=%+v", r.Outcome, f.actions, r.Steps)
	}
	for i, step := range r.Steps {
		if !step.Changed {
			t.Fatalf("wizard step %d changed its route but reported no progress: %+v", i+1, step)
		}
	}
}

// The adapter's first two reads observe a stable landscape screen. Only the
// additional screen-size probe made by the real runGoal transform fails.
// This leaves the angle known, with no validated portrait dimensions.
type goalFinalRotationDriver struct {
	portableTapDriver
	reads, failedSizeProbes int
}

func (*goalFinalRotationDriver) Provides(drivers.Target) drivers.CapabilitySet {
	return drivers.NewSet(drivers.CapTreeAX, drivers.CapCoordTap)
}

func (d *goalFinalRotationDriver) Tree(context.Context, drivers.Target, drivers.TreeSpec) (drivers.TreeResult, error) {
	d.reads++
	if d.reads == 3 {
		d.failedSizeProbes++
		return drivers.TreeResult{}, errors.New("screen extent unavailable")
	}
	return drivers.TreeResult{JSON: []byte(`[{"role":"application","label":"Example","frame":"{{0, 0}, {874, 402}}"},{"role":"heading","label":"Home"},{"identifier":"next","role":"button","label":"Next","frame":"{{100, 50}, {100, 40}}"}]`)}, nil
}

func TestGoalRunRefusesKnownRotationWithoutValidatedScreenSize(t *testing.T) {
	fakeJev(t, "1", 10)
	const udid = "AAAAAAAA-0000-0000-0000-000000000001"
	runner := &sequenceRecordingRunner{
		out: map[string]string{"defaults read com.apple.iphonesimulator DevicePreferences": devicePreferencesDump},
	}
	d := &goalFinalRotationDriver{}
	registry := drivers.NewRegistry()
	registry.Register(d)
	root := t.TempDir()
	c := CLI{Root: root, Runner: runner, driverSession: &driverSession{registry: registry}}
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = udid
	r := c.runGoal(t.Context(), GlobalOptions{PreferDriver: "portable"}, cfg,
		GoalRequest{Goal: "open Details", Mode: "navigation", MaxSteps: 1}, goalCondition(t, `title:"Details"`))
	if d.failedSizeProbes != 1 || countCalls(runner.commands, "defaults read") == 0 {
		t.Fatalf("fixture did not exercise known-angle missing-size path: probes=%d commands=%v reads=%d", d.failedSizeProbes, runner.commands, d.reads)
	}
	if d.taps != 0 || r.Outcome != "blocked" || len(r.Steps) != 1 || r.Steps[0].Receipt.Delivery != "not_sent" {
		t.Fatalf("input dispatched without a validated coordinate transform: taps=%d result=%+v", d.taps, r)
	}
}
