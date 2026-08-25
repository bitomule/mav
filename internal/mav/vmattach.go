package mav

import (
	"context"
	"strings"
)

// vmProjectExcludes are what never crosses to the guest. .git because it is
// large and the guest never runs git; .mav/runs because it is the evidence
// channel and travels the other way -- pushing it would overwrite the
// guest's own in-flight artifacts with the host's stale copy.
var vmProjectExcludes = []string{".git", ".mav/runs", ".mav/vm-lease.json", ".claude"}

// commandNeedsVM says whether this command reaches the app under test.
// Everything that does runs on the leased machine; everything else --
// inspecting config, listing simulators, installing tooling -- must NOT
// lease one, or `mav doctor` would boot a VM just to tell you a VM is
// available.
func commandNeedsVM(command string) bool {
	switch command {
	case "open", "ui", "capture", "app", "openURL", "location", "clipboard", "time", "debug", "run", "logs", "crashes", "network", "evidence":
		return true
	}
	return false
}

// withVM returns a CLI whose Runner executes on the leased machine, plus
// the func that tears the attachment down. Called once per invocation,
// before dispatch, so no individual command has to know it is remote.
//
// stop is deliberately not in commandNeedsVM but still attaches here, with
// reuseOnly: it has to reach the guest to kill the guest's processes, but
// leasing a machine in order to stop a run that has none would be exactly
// backwards.
func (c CLI) withVM(ctx context.Context, command string) (CLI, func(), error) {
	noop := func() {}
	cfg, err := LoadConfig(c.Root)
	if err != nil || !cfg.VM {
		return c, noop, nil
	}
	reuseOnly := command == "stop"
	if !commandNeedsVM(command) && !reuseOnly {
		return c, noop, nil
	}
	if reuseOnly {
		if _, held := readVMLease(c.Root); !held {
			return c, noop, nil
		}
	}
	host := c.Runner
	lease, err := c.acquireVMLease(ctx, host)
	if err != nil {
		return c, noop, err
	}
	runner := newVMRunner(host, lease, c.Root)
	attached := c
	attached.Runner = runner
	attached.host = host
	attached.vmRun = runner
	return attached, func() { c.detachVM(ctx, runner) }, nil
}

// detachVM brings the guest's artifacts home and closes the connection. It
// runs on EVERY command, not just at the end of a flow: an agent driving
// mav command by command never calls anything that would be "the end", and
// evidence it cannot read until some later command happens to sync is
// evidence it will reason about stale.
func (c CLI) detachVM(ctx context.Context, runner *vmRunner) {
	if runner.released {
		return
	}
	defer runner.closeControlMaster()
	touchVMLease(c.Root)
	c.pullVMEvidence(ctx, runner)
}

func (c CLI) pullVMEvidence(ctx context.Context, runner *vmRunner) {
	run, err := c.resolveRun("")
	if err != nil {
		return
	}
	if pullErr := runner.pull(ctx, run.Dir); pullErr != nil {
		appendFile(run.LogsPath, "mav vm: pulling evidence back failed: "+pullErr.Error()+"\n")
	}
}

// releaseVM ends the work: evidence comes home first, then the machine goes
// back. The order is the whole contract of the feature and it only has one
// safe direction -- a lease handed back before the pull takes the artifacts
// with it, and there is no second chance to fetch them.
//
// Marking the runner released is what stops the deferred detach in Run from
// trying to rsync out of a machine that no longer exists; the flag lives on
// the runner, not the CLI, because CLI travels by value and the copy that
// releases is never the copy that detaches.
func (c CLI) releaseVM(ctx context.Context) bool {
	if c.vmRun == nil {
		return false
	}
	c.pullVMEvidence(ctx, c.vmRun)
	c.vmRun.closeControlMaster()
	c.vmRun.released = true
	return c.releaseVMLease(ctx, c.hostRunner())
}

// hostRunner is the Runner that reaches THIS machine, whether or not the
// invocation is attached to a VM. Lease bookkeeping runs through it: the
// leasing tool is installed here, not in the guest.
func (c CLI) hostRunner() Runner {
	if c.host != nil {
		return c.host
	}
	return c.Runner
}

// vmFailureFields splits a VM failure into a reason an agent can branch on
// and the full text a human needs, instead of one opaque string that is
// both and neither. Every one of them carries the same next step: there is
// exactly one command that fixes missing VM tooling.
func vmFailureFields(err error) map[string]string {
	message := err.Error()
	reason := message
	if idx := strings.IndexByte(message, ' '); idx > 0 {
		reason = message[:idx]
	}
	return map[string]string{"reason": reason, "error": message, "next": vmInstallHint}
}

// hostCLI is this CLI with the local Runner back. The launch recipe needs
// it: build and app_path resolve against the Xcode toolchain and the
// checkout, which live here, while install and launch have to happen where
// the app runs.
func (c CLI) hostCLI() CLI {
	if c.host == nil {
		return c
	}
	local := c
	local.Runner = c.host
	local.vmRun = nil
	return local
}

// vmPrepareGuest puts the checkout and the app bundle on the guest, at the
// same absolute paths they have here, and creates the run dir so drivers
// can write into it. Called from open once app_path has resolved, which is
// the first moment mav knows which bundle to ship.
func (c CLI) vmPrepareGuest(ctx context.Context, run RunState, appPath string) error {
	if c.vmRun == nil {
		return nil
	}
	if err := c.vmRun.push(ctx, c.Root, vmProjectExcludes); err != nil {
		return err
	}
	if strings.TrimSpace(appPath) != "" {
		if err := c.vmRun.push(ctx, appPath, nil); err != nil {
			return err
		}
	}
	return c.vmRun.vmMirrorDir(ctx, run.Dir)
}
