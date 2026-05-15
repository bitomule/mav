package mav

import (
	"github.com/bitomule/mav/internal/mav/drivers"
	"github.com/bitomule/mav/internal/mav/drivers/axe"
	"github.com/bitomule/mav/internal/mav/drivers/baguette"
	"github.com/bitomule/mav/internal/mav/drivers/idb"
	"github.com/bitomule/mav/internal/mav/drivers/network"
	"github.com/bitomule/mav/internal/mav/drivers/simctl"
)

// RegisterDefaultDrivers wires the canonical driver portfolio into reg:
// AXe (fast a11y + semantic tap), idb (device coord tap / screenshot / logs /
// crashes / install), Baguette (sim multitouch + system UI + hardware buttons),
// simctl (sim lifecycle / video / locale / log stream).
//
// idb is the canonical device driver. Sim-only multitouch / system-UI /
// hardware-button operations go through baguette. On device targets where
// baguette is unavailable, cli.go surfaces a structured error rather than
// silently falling back.
func RegisterDefaultDrivers(reg *drivers.Registry, exec drivers.Executor) {
	reg.Register(axe.New(exec))
	reg.Register(simctl.New(exec))
	reg.Register(idb.New(exec))
	reg.Register(baguette.New(exec))
	reg.Register(network.New(exec))
}
