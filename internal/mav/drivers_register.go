package mav

import (
	"github.com/bitomule/mav/internal/mav/drivers"
	"github.com/bitomule/mav/internal/mav/drivers/axe"
	"github.com/bitomule/mav/internal/mav/drivers/baguette"
	"github.com/bitomule/mav/internal/mav/drivers/idb"
	"github.com/bitomule/mav/internal/mav/drivers/macos"
	"github.com/bitomule/mav/internal/mav/drivers/network"
	"github.com/bitomule/mav/internal/mav/drivers/simctl"
	"github.com/bitomule/mav/internal/mav/drivers/simtime"
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
	reg.Register(simtime.New(exec))
	// macOS: cua-driver gives the tree, window capture and background input
	// in a single tool; axcli the input that does not steal focus;
	// screencapture the video and the whole-screen capture as a last
	// resort. The split among the three comes from their Cost tables, not
	// from any special case in the router.
	reg.Register(macos.NewCua(exec))
	reg.Register(macos.NewAxcli(exec))
	reg.Register(macos.NewScreencapture(exec))
	reg.Register(macos.NewSystem(exec))
}
