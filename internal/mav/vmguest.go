package mav

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// vmGuestTools are what the image has to carry for mav to drive an app
// inside it. The list is short on purpose: it is not "everything mav might
// use", it is the set whose absence turns into a failure that names
// something else entirely three commands later.
var vmGuestTools = []string{"mav", "cua-driver", "axcli", "mitmdump"}

// vmGuestDaemonWait bounds how long the driver daemon gets to come up. Cold
// it takes a few seconds; polling instead of sleeping a fixed amount means a
// warm machine pays nothing and a cold one is not cut off early. A var so a
// test can shrink it: the wait is the point of the loop, not of the tests
// that exercise what the loop decides.
var vmGuestDaemonWait = 20 * time.Second

// verifyGuest checks the machine is the one mav expects, once, when the
// lease is created.
//
// This is the same class of problem as an outdated hypervisor, one level
// down: an image built before a driver existed, or one whose permission
// switches were never flipped, fails deep inside a run with an error about
// a window or an element. Measured on a real image: with the driver absent
// from the guest's PATH, the first symptom was `ui tree` reporting that the
// app was not running.
//
// It also leaves the daemon up, and that is not a side effect worth hiding:
// starting it is per-machine setup, so doing it here is the difference
// between paying for it once and paying for it on the first `ui tree` of
// every run.
func (c CLI) verifyGuest(ctx context.Context, runner *vmRunner) error {
	missing := []string{}
	for _, tool := range vmGuestTools {
		if _, err := runner.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("vm_image_incomplete missing=%s next=%s", strings.Join(missing, ","), vmImageHint)
	}
	if grants := c.guestDriverGrants(ctx, runner); len(grants) > 0 {
		// Named rather than summarised: these are the switches a human has
		// to flip by hand while the image is being built, because macOS 26
		// has no scriptable way to grant them, so whoever reads this needs
		// to know which one was left off.
		return fmt.Errorf("vm_image_ungranted missing=%s next=%s", strings.Join(grants, ","), vmImageHint)
	}
	return nil
}

// guestDriverGrants returns the permissions the guest's driver does NOT
// have. It starts the daemon first because the answer is only meaningful
// from a running one: asked with no daemon, the driver reports that it does
// not know rather than guessing, and reading that as "granted" would defeat
// the whole check.
func (c CLI) guestDriverGrants(ctx context.Context, runner *vmRunner) []string {
	runner.Run(ctx, "open", "-n", "-g", "-a", "CuaDriver", "--args", "serve")
	deadline := time.Now().Add(vmGuestDaemonWait)
	for {
		result := runner.Run(ctx, "cua-driver", "permissions", "status", "--json")
		missing := macMissingPermissions(result.Stdout)
		// "unreadable" alone means the daemon has not answered yet, which
		// is a reason to wait, not a verdict. Anything else is an answer.
		if len(missing) != 1 || missing[0] != "unreadable" {
			return missing
		}
		if !time.Now().Before(deadline) {
			return missing
		}
		time.Sleep(time.Second)
	}
}
