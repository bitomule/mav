package mav

import (
	"github.com/bitomule/mav/internal/mav/drivers"
	"github.com/bitomule/mav/internal/mav/drivers/axe"
	"github.com/bitomule/mav/internal/mav/drivers/baguette"
	"github.com/bitomule/mav/internal/mav/drivers/coredevice"
	"github.com/bitomule/mav/internal/mav/drivers/idb"
	"github.com/bitomule/mav/internal/mav/drivers/macos"
	"github.com/bitomule/mav/internal/mav/drivers/network"
	"github.com/bitomule/mav/internal/mav/drivers/simctl"
	"github.com/bitomule/mav/internal/mav/drivers/simtime"
)

// RegisterDefaultDrivers wires native drivers into the capability router.
// AXe 2 is canonical for iOS UI and capture; CoreDevice handles physical app
// lifecycle. Baguette and idb remain optional providers for their capabilities.
func RegisterDefaultDrivers(reg *drivers.Registry, exec drivers.Executor) {
	reg.Register(axe.New(exec))
	reg.Register(coredevice.New(exec))
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
