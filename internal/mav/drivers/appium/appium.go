// Package appium wraps Appium/WebDriverAgent. This driver exists as a bridge
// for the duration of P2 only: P4 deletes it entirely and replaces the
// multitouch/system-UI/text path with the baguette driver on simulator and
// with go-ios on device. Do not add features here.
package appium

import (
	"context"
	"errors"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key. Treated as a transitional name; gone in P4.
const ID = "appium"

// Driver wraps the Appium server + WDA session.
type Driver struct {
	exec drivers.Executor
}

// New constructs a Driver.
func New(exec drivers.Executor) *Driver { return &Driver{exec: exec} }

func (d *Driver) ID() string { return ID }

// Provides covers everything Appium is currently responsible for in cli.go:
// multitouch gestures, system UI tree, text editing, hide-keyboard, and W3C
// actions dispatch.
func (d *Driver) Provides(_ drivers.Target) drivers.CapabilitySet {
	return drivers.NewSet(
		drivers.CapPinch,
		drivers.CapRotate,
		drivers.CapTwoFingerPan,
		drivers.CapW3CActions,
		drivers.CapHideKeyboard,
		drivers.CapErase,
		drivers.CapType,
		drivers.CapTreeSystem,
		drivers.CapSemanticTap,
	)
}

// Cost is set high across the board: appium is the last-resort path during P2
// and scheduled for deletion in P4.
func (d *Driver) Cost(_ drivers.Capability, _ drivers.Target) int { return 200 }

// Probe is intentionally conservative: report Missing unless both node and
// appium are on PATH. Matches today's setup expectations.
func (d *Driver) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	if _, err := p.LookPath("appium"); err != nil {
		return drivers.HealthReport{State: drivers.HealthMissing, Detail: "appium not on PATH"}
	}
	if _, err := p.LookPath("node"); err != nil {
		return drivers.HealthReport{State: drivers.HealthBroken, Detail: "appium installed but node not on PATH"}
	}
	return drivers.HealthReport{State: drivers.HealthDegraded, Detail: "appium scheduled for removal in P4"}
}

func (d *Driver) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// --- functional methods (stubbed, never implemented; deleted in P4) -----

var errNotYet = errors.New("appium: bridge driver, not implemented (gone in P4)")

func (d *Driver) Tap(context.Context, drivers.Target, drivers.TapSpec) (drivers.TapResult, error) {
	return drivers.TapResult{}, errNotYet
}
func (d *Driver) Tree(context.Context, drivers.Target, drivers.TreeSpec) (drivers.TreeResult, error) {
	return drivers.TreeResult{}, errNotYet
}
func (d *Driver) Type(context.Context, drivers.Target, drivers.TextSpec) error  { return errNotYet }
func (d *Driver) Erase(context.Context, drivers.Target, drivers.TextSpec) error { return errNotYet }
func (d *Driver) HideKeyboard(context.Context, drivers.Target) error            { return errNotYet }
func (d *Driver) Pinch(context.Context, drivers.Target, drivers.PinchSpec) error {
	return errNotYet
}
func (d *Driver) Rotate(context.Context, drivers.Target, drivers.RotateSpec) error {
	return errNotYet
}
func (d *Driver) TwoFingerPan(context.Context, drivers.Target, drivers.TwoFingerPanSpec) error {
	return errNotYet
}
func (d *Driver) W3CActions(context.Context, drivers.Target, []byte) error { return errNotYet }
func (d *Driver) Swipe(context.Context, drivers.Target, drivers.SwipeSpec) error {
	return errNotYet
}
