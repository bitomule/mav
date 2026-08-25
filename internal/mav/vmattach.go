package mav

import (
	"context"
	"fmt"
	"path/filepath"
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
func (c CLI) withVM(ctx context.Context, command string) (CLI, func(), error) {
	noop := func() {}
	cfg, err := LoadConfig(c.Root)
	if err != nil || !cfg.VM {
		return c, noop, nil
	}
	// stop and the run worker both reach the guest but must never lease a
	// machine: stop kills the guest's processes, and the worker exists to
	// reap a run whose owner is gone. Leasing a machine in order to tidy up
	// after one is exactly backwards.
	reuseOnly := command == "stop" || command == "__worker"
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
	// The provisioner runs its own idle clock, shorter than mav's, and
	// reclaims a machine that has gone quiet. Touching it at the start of
	// every command is what keeps a run that is genuinely working from
	// being reclaimed mid-step.
	vmHeartbeat(ctx, host, lease)
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

// syncVMEvidence brings the guest's artifacts home right now, mid-command,
// for the one case the per-command sync cannot cover: a command that
// inspects a file it just caused the guest to write. A no-op outside VM
// mode, so callers do not have to branch.
func (c CLI) syncVMEvidence(ctx context.Context) {
	if c.vmRun == nil || c.vmRun.released {
		return
	}
	c.pullVMEvidence(ctx, c.vmRun)
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
		if err := c.vmAdoptApp(ctx, run, appPath); err != nil {
			return err
		}
	}
	return c.vmRun.vmMirrorDir(ctx, run.Dir)
}

// vmAdoptApp makes the copy in the guest launchable, and says so when it
// had to change it.
//
// A development-signed bundle carries an embedded provisioning profile and
// restricted entitlements (iCloud, push) tied to a team and a device list.
// In a clean VM AMFI kills it on launch with SIGKILL and no message, which
// surfaces as "the app is not running" three commands later. Re-signing
// ad-hoc lets it launch, and validating UI does not need those
// entitlements.
//
// It is a real trade and it is reported rather than done quietly: you are
// no longer running the exact binary you ship. Only the guest's copy is
// touched; the bundle on this machine is left alone.
func (c CLI) vmAdoptApp(ctx context.Context, run RunState, appPath string) error {
	profile := filepath.Join(appPath, "Contents", "embedded.provisionprofile")
	if probe := c.vmRun.Run(ctx, "test", "-e", profile); probe.Err != nil {
		return nil
	}
	if result := c.vmRun.Run(ctx, "rm", "-f", profile); result.Err != nil {
		return fmt.Errorf("vm_resign_failed detail=%s", firstLine(strings.TrimSpace(result.Stderr)))
	}
	if result := c.vmRun.Run(ctx, "codesign", "-f", "-s", "-", "--deep", appPath); result.Err != nil {
		return fmt.Errorf("vm_resign_failed detail=%s", firstLine(strings.TrimSpace(result.Stderr)))
	}
	// The flag lives on the runner, not on the CLI: CLI travels by value,
	// and the copy that re-signs is several frames below the one that
	// reports.
	c.vmRun.resigned = true
	appendFile(run.LogsPath, "mav vm: re-signed the guest's copy of "+filepath.Base(appPath)+
		" ad-hoc so it can launch there; its provisioning profile and the entitlements tied to it (iCloud, push) are gone from that copy\n")
	return nil
}
