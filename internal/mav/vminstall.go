package mav

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// vmFormulas are what `mav setup --install vm` puts on the machine. They are listed
// here and never in an error message or a doc: somebody writing `vm: true`
// should not have to learn which project provides the hypervisor this
// month, only that one mav command installs whatever it is.
var vmFormulas = map[string]string{
	vmLeaseTool: "openclaw/tap/crabbox",
	vmHostTool:  "cirruslabs/cli/tart",
}

// setupVM is `mav setup --install vm`. It is a tool in that list and not a
// command of its own because there is only one place a user should look for
// "mav is missing something I need": everything else mav can install lives
// behind the same flag, and a second install command is a second thing to
// remember for no gain.
//
// Unlike the rest of that list it installs two tools and checks a version
// floor, which is why it has its own function instead of a row in the
// command table.
func (c CLI) setupVM(ctx context.Context) (map[string]string, error) {
	host := c.hostRunner()
	if _, err := host.LookPath("brew"); err != nil {
		return nil, Fail("vm_install_needs_brew", map[string]string{
			"next": "install Homebrew from https://brew.sh, then rerun " + vmInstallHint,
		}).Write(c.Stdout)
	}
	fields := map[string]string{}
	installed := []string{}
	for _, tool := range []string{vmLeaseTool, vmHostTool} {
		action := "install"
		if _, err := host.LookPath(tool); err == nil {
			// An outdated hypervisor is worse than a missing one: it fails
			// while injecting the SSH key, with a message about terminal
			// sizes that points nowhere near the cause.
			if tool != vmHostTool || vmHostTooOld(ctx, host) == "" {
				continue
			}
			action = "upgrade"
		}
		result := host.Run(ctx, "brew", action, vmFormulas[tool])
		if result.Err != nil {
			// The formula is named in the error and only there: it is what
			// the user would have to type by hand if this command cannot
			// do it for them.
			return nil, Fail("vm_install_failed", map[string]string{
				"error": firstLine(strings.TrimSpace(result.Stderr)),
				"next":  "brew " + action + " " + vmFormulas[tool],
			}).Write(c.Stdout)
		}
		installed = append(installed, tool)
	}
	// Deliberately a count and not the formula names. Nobody writing
	// `vm: true` should end up learning which project ships the hypervisor;
	// that is exactly the detail the config surface exists to hide.
	fields["vm_installed"] = strconv.Itoa(len(installed))
	if missing := vmToolingMissing(host); len(missing) > 0 {
		return nil, Fail("vm_tooling_missing", map[string]string{
			"missing": strings.Join(missing, ","),
			"next":    "open a new shell so the installed tools are on PATH, then rerun " + vmInstallHint,
		}).Write(c.Stdout)
	}
	fields["vm_tooling"] = "ok"
	// The image is a separate step and cannot be folded into this one: it
	// takes tens of minutes to build and it ends with two switches a human
	// has to flip by hand, because macOS 26 has no scriptable way to grant
	// Accessibility or Screen Recording. Reporting it as missing here is
	// the whole point -- the alternative is finding out mid-run.
	if vmImageReady(ctx, host) {
		fields["vm_image"] = vmImage
	} else {
		fields["vm_image"] = "missing"
		fields["vm_image_next"] = "scripts/build-mav-vm-image.sh"
	}
	return fields, nil
}

// vmDoctorFields is what `mav doctor` reports about VM mode. Nothing is
// added when the project does not use it: a repo driving a simulator should
// not have to read past VM lines that will never apply to it.
func (c CLI) vmDoctorFields(ctx context.Context, cfg Config, fields map[string]string) {
	if !cfg.VM {
		return
	}
	fields["vm"] = "on"
	host := c.hostRunner()
	missing := vmToolingMissing(host)
	if len(missing) > 0 {
		fields["vm_tooling"] = "missing"
		fields["vm_next"] = vmInstallHint
		// Deliberately not the tool names: doctor is read by agents, and
		// the actionable datum is the command that fixes it.
		return
	}
	if old := vmHostTooOld(ctx, host); old != "" {
		fields["vm_tooling"] = "outdated"
		fields["vm_next"] = vmInstallHint
		return
	}
	fields["vm_tooling"] = "ok"
	if vmImageReady(ctx, host) {
		fields["vm_image"] = vmImage
	} else {
		fields["vm_image"] = "missing"
		// Not vmInstallHint: the install command does not build the image,
		// so sending the reader there sends them to run something that
		// reports this same line back at them.
		fields["vm_next"] = vmImageHint
	}
	if lease, held := readVMLease(c.Root); held {
		fields["vm_lease"] = lease.ID
		fields["vm_lease_idle"] = time.Since(lease.LastUsed).Truncate(time.Second).String()
		// Only reported when a lease is already held. Checking the guest
		// means talking to it, and doctor leasing a machine to report that
		// leasing works would take one of the two slots the run needs.
		if lease.Verified {
			fields["vm_guest"] = "ok"
		} else {
			fields["vm_guest"] = "unchecked"
		}
	} else {
		fields["vm_lease"] = "none"
	}
}
