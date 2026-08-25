package mav

import (
	"context"
	"strings"
	"time"
)

// Time control on macOS is SYSTEM-wide, not per process, and that
// difference with iOS is not an implementation detail but the central fact.
//
// On the simulator, simtime interposes the clock the app sees and does not
// touch the machine. On macOS that equivalent does not exist: the only
// per-process path is libfaketime with DYLD_INSERT_LIBRARIES, which the
// hardened runtime blocks in any app signed for distribution, that is, it
// works on your Debug build and on nothing else. What does always work is
// changing the machine's clock, and that is why it is what mav offers, with
// the gate closed by default.
//
// `freeze` and `scale` map to nothing: a system clock runs, and there is no
// way to stop it or speed it up.

// macClockInVM says whether this is a Virtualization.framework guest.
//
// It is the gate: changing the clock of a dedicated VM is cheap and
// reversible; doing it on somebody's Mac expires their certificates, breaks
// their sessions and shuffles their files by date. With --system-clock it
// can be forced, because some people know what they are doing, but they
// have to say so.
func (c CLI) macClockInVM(ctx context.Context) bool {
	res := c.Runner.Run(ctx, "sysctl", "-n", "kern.hv_vmm_present")
	return strings.TrimSpace(res.Stdout) == "1"
}

// macTimeTravel takes the machine's clock to an instant.
//
// Network time sync is turned off first because otherwise the system undoes
// the change whenever it feels like checking the time, and the symptom
// would be a test that passes or fails depending on when it runs.
func (c CLI) macTimeTravel(ctx context.Context, at time.Time) error {
	if res := c.Runner.Run(ctx, "sudo", "systemsetup", "-setusingnetworktime", "off"); res.Err != nil {
		return res.Err
	}
	local := at.Local()
	if res := c.Runner.Run(ctx, "sudo", "systemsetup", "-setdate", local.Format("01:02:06")); res.Err != nil {
		return res.Err
	}
	res := c.Runner.Run(ctx, "sudo", "systemsetup", "-settime", local.Format("15:04:05"))
	return res.Err
}

// macTimeReset returns the clock to the real world's.
func (c CLI) macTimeReset(ctx context.Context) error {
	res := c.Runner.Run(ctx, "sudo", "systemsetup", "-setusingnetworktime", "on")
	return res.Err
}

// macTimeStatus reports what there is, which for a system clock is the time
// and whether somebody is syncing it underneath.
func (c CLI) macTimeStatus(ctx context.Context) map[string]string {
	fields := map[string]string{"clock": "system"}
	if res := c.Runner.Run(ctx, "date", "-u", "+%Y-%m-%dT%H:%M:%SZ"); strings.TrimSpace(res.Stdout) != "" {
		fields["now"] = strings.TrimSpace(res.Stdout)
	}
	res := c.Runner.Run(ctx, "systemsetup", "-getusingnetworktime")
	if strings.Contains(strings.ToLower(res.Stdout), "on") {
		fields["network_time"] = "on"
	} else if strings.Contains(strings.ToLower(res.Stdout), "off") {
		fields["network_time"] = "off"
	}
	return fields
}
