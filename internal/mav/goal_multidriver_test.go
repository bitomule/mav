package mav

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type goalCombinedDriver struct {
	portableTapDriver
	reads, checkedActions int
	completed             bool
	lastRef               string
}

type goalPointDriver struct {
	portableTapDriver
	tap   drivers.TapSpec
	swipe drivers.SwipeSpec
}

func (*goalPointDriver) Provides(drivers.Target) drivers.CapabilitySet {
	return drivers.NewSet(drivers.CapCoordTap, drivers.CapSwipe)
}
func (d *goalPointDriver) Tap(_ context.Context, _ drivers.Target, s drivers.TapSpec) (drivers.TapResult, error) {
	d.taps++
	d.tap = s
	return drivers.TapResult{}, nil
}
func (d *goalPointDriver) Swipe(_ context.Context, _ drivers.Target, s drivers.SwipeSpec) error {
	d.swipe = s
	return nil
}
func (*goalPointDriver) Pinch(context.Context, drivers.Target, drivers.PinchSpec) error {
	return errors.New("unused")
}
func (*goalPointDriver) Rotate(context.Context, drivers.Target, drivers.RotateSpec) error {
	return errors.New("unused")
}
func (*goalPointDriver) TwoFingerPan(context.Context, drivers.Target, drivers.TwoFingerPanSpec) error {
	return errors.New("unused")
}
func (*goalPointDriver) W3CActions(context.Context, drivers.Target, []byte) error {
	return errors.New("unused")
}
func TestGoalOrdinaryDriverTransformsFreshCoordinatesOnceAndRefusesUnknownGeometry(t *testing.T) {
	d := &goalPointDriver{}
	calls := 0
	a := goalDriverAdapter{driver: d, target: drivers.Target{Kind: drivers.KindSim}, transform: func(_ context.Context, _ []Element, x, y int) (int, int, error) { calls++; return x + 10, y + 20, nil }}
	el := Element{Role: "button", Frame: "{{0, 0}, {100, 40}}"}
	r := a.Execute(t.Context(), GoalObservation{Elements: []Element{el}}, GoalOperation{Kind: "tap", Target: &el})
	if r.Delivery != "completed" || calls != 1 || d.tap.X != 60 || d.tap.Y != 40 {
		t.Fatalf("%+v calls=%d spec=%+v", r, calls, d.tap)
	}
	el = Element{Role: "scroll-view", Frame: "{{0, 0}, {100, 200}}"}
	r = a.Execute(t.Context(), GoalObservation{Elements: []Element{el}}, GoalOperation{Kind: "scroll", Target: &el, Direction: "up"})
	if r.Delivery != "completed" || calls != 3 || d.swipe.StartX != 60 || d.swipe.StartY != 170 || d.swipe.EndY != 70 {
		t.Fatalf("%+v calls=%d spec=%+v", r, calls, d.swipe)
	}
	a.transform = func(context.Context, []Element, int, int) (int, int, error) {
		return 0, 0, errors.New("rotation unavailable")
	}
	r = a.Execute(t.Context(), GoalObservation{}, GoalOperation{Kind: "tap", Target: &el})
	if r.Delivery != "not_sent" || d.taps != 1 {
		t.Fatalf("%+v taps=%d", r, d.taps)
	}
}

func (*goalCombinedDriver) Provides(drivers.Target) drivers.CapabilitySet {
	return drivers.NewSet(drivers.CapTreeAX, drivers.CapCoordTap)
}
func (*goalCombinedDriver) Tree(context.Context, drivers.Target, drivers.TreeSpec) (drivers.TreeResult, error) {
	panic("combined driver must not read tree separately")
}
func (*goalCombinedDriver) ReferenceSnapshot(context.Context, drivers.Target) (drivers.ReferenceSnapshot, error) {
	panic("combined driver must not invalidate its tree refs")
}
func (d *goalCombinedDriver) ReferenceTree(context.Context, drivers.Target) (drivers.TreeResult, drivers.ReferenceSnapshot, error) {
	d.reads++
	d.lastRef = fmt.Sprintf("snapshot-%d:1", d.reads)
	s := drivers.ReferenceSnapshot{ID: fmt.Sprintf("snapshot-%d", d.reads), PID: 101, Context: []byte(`{"pid":101}`)}
	if d.completed {
		return drivers.TreeResult{JSON: []byte(`[{"role":"heading","label":"Details"}]`)}, s, nil
	}
	s.Elements = []drivers.ReferenceElement{{Ref: d.lastRef, Action: "tap", Role: "button", Label: "Details", Frame: "0,0,100,40"}}
	return drivers.TreeResult{JSON: []byte(`[{"role":"heading","label":"Home"},{"role":"button","label":"Details","frame":"0,0,100,40"}]`)}, s, nil
}
func (d *goalCombinedDriver) ExecuteReference(_ context.Context, _ drivers.Target, _ drivers.ReferenceSnapshot, op drivers.ReferenceOperation) (drivers.ReferenceResult, error) {
	d.checkedActions++
	if op.Ref != d.lastRef {
		return drivers.ReferenceResult{Delivery: "not_sent"}, fmt.Errorf("stale token used")
	}
	d.completed = true
	return drivers.ReferenceResult{Delivery: "completed"}, nil
}

func TestGoalEngineUsesFreshOpaqueTokensFromCombinedDriver(t *testing.T) {
	d := &goalCombinedDriver{}
	a := goalDriverAdapter{driver: d, target: drivers.Target{Kind: drivers.KindMac}}
	planner := &goalFixture{}
	r := (GoalEngine{Wait: func(context.Context) error { return nil }, Observer: a, Planner: planner, Executor: a, Verifier: goalCondition(t, `title:"Details"`), Capabilities: a.capabilities()}).Run(t.Context(), GoalRequest{Goal: "open Details", Mode: "navigation"})
	if r.Succeeded != "true" || d.checkedActions != 1 || d.taps != 0 || r.Metrics.Discarded != 0 || r.Steps[0].Operation.Ref != "snapshot-2:1" {
		t.Fatalf("result=%+v driver=%+v", r, d)
	}
}
