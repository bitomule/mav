package mav

import (
	"github.com/bitomule/mav/internal/mav/drivers"
	"github.com/bitomule/mav/internal/mav/drivers/axe"
	"github.com/bitomule/mav/internal/mav/drivers/baguette"
	"github.com/bitomule/mav/internal/mav/drivers/idb"
	"github.com/bitomule/mav/internal/mav/drivers/simctl"
)

// RegisterDefaultDrivers wires the canonical driver portfolio into reg:
// AXe (fast a11y + semantic tap), idb (device coord tap / screenshot / logs /
// crashes / install), Baguette (sim multitouch + system UI + hardware buttons),
// simctl (sim lifecycle / video / locale / log stream).
//
// Appium has been removed (see the May 2026 plan revision). go-ios was
// evaluated and rejected: requires sudo for tunnel on iOS 17+, no gesture
// API, no HAR. idb stays as the canonical device driver.
//
// cli.go does not consult this registry yet for every operation; the
// migration off hasTool() happens incrementally as we move each subcommand.
func RegisterDefaultDrivers(reg *drivers.Registry, exec drivers.Executor) {
	reg.Register(axe.New(exec))
	reg.Register(simctl.New(exec))
	reg.Register(idb.New(exec))
	reg.Register(baguette.New(exec))
}
