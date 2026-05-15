package mav

import (
	"github.com/bitomule/mav/internal/mav/drivers"
	"github.com/bitomule/mav/internal/mav/drivers/appium"
	"github.com/bitomule/mav/internal/mav/drivers/axe"
	"github.com/bitomule/mav/internal/mav/drivers/idb"
	"github.com/bitomule/mav/internal/mav/drivers/simctl"
)

// RegisterDefaultDrivers builds the bridge driver set used during P2 and wires
// it into reg. cli.go does not consult the registry yet (P3 starts that
// migration), so calling this is a no-op for end-user behaviour today. P3/P4
// swap idb and appium for goios and baguette by editing the body of this
// function only -- the rest of mav keeps using the Registry/Router.
func RegisterDefaultDrivers(reg *drivers.Registry, exec drivers.Executor) {
	reg.Register(axe.New(exec))
	reg.Register(simctl.New(exec))
	reg.Register(idb.New(exec))    // gone in P3
	reg.Register(appium.New(exec)) // gone in P4
}
