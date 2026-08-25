package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The VM tooling is named here and nowhere else on purpose. `vm: true` is
// the whole config surface: which tool leases the machine is mav's
// business, so the day it stops being crabbox-over-tart no config.yaml on
// anybody's disk has to change. Everything below is the only code that
// knows these names.
const (
	vmLeaseTool = "crabbox"
	vmHostTool  = "tart"
)

// vmImage is the image built by scripts/build-mav-vm-image.sh. It already
// carries mav, the patched cua-driver, axcli and mitmproxy, with the
// driver's TCC grants baked into the disk, which is the part that cannot be
// scripted at run time (see examples/macos-vm/README.md).
const vmImage = "mav-macos"

// vmIdleTimeout is how long a lease may go untouched before mav hands it
// back. Virtualization.framework and the macOS EULA both cap the machine at
// two concurrent macOS VMs, so a leaked lease is not untidiness, it is the
// next run failing to get a machine at all.
const vmIdleTimeout = 20 * time.Minute

const vmLeaseFile = "vm-lease.json"

// vmLease is the local record of the machine this project currently holds.
// It lives on disk and not in memory because every mav command is its own
// process: without it, `mav ui tap` would have no way to find the VM that
// `mav open` leased, and would lease a second one.
type vmLease struct {
	ID       string    `json:"id"`
	Target   string    `json:"target"`
	Port     string    `json:"port"`
	Key      string    `json:"key"`
	Image    string    `json:"image"`
	Acquired time.Time `json:"acquired"`
	LastUsed time.Time `json:"last_used"`
}

func vmLeasePath(root string) string { return filepath.Join(root, MavDir, vmLeaseFile) }

func readVMLease(root string) (vmLease, bool) {
	data, err := os.ReadFile(vmLeasePath(root))
	if err != nil {
		return vmLease{}, false
	}
	var lease vmLease
	if err := json.Unmarshal(data, &lease); err != nil || lease.ID == "" || lease.Target == "" {
		return vmLease{}, false
	}
	return lease, true
}

func writeVMLease(root string, lease vmLease) error {
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(lease)
	if err != nil {
		return err
	}
	return os.WriteFile(vmLeasePath(root), data, 0o644)
}

func removeVMLease(root string) { _ = os.Remove(vmLeasePath(root)) }

// touchVMLease pushes the idle deadline out. Every command that actually
// reaches the VM calls it, so "idle" means the agent stopped working, not
// that this particular command happened to be slow.
func touchVMLease(root string) {
	lease, ok := readVMLease(root)
	if !ok {
		return
	}
	lease.LastUsed = time.Now()
	_ = writeVMLease(root, lease)
}

func vmLeaseIdle(lease vmLease) bool {
	return time.Since(lease.LastUsed) > vmIdleTimeout
}

// vmToolingMissing lists the tools `vm: true` needs and this machine does
// not have. Returned as a list, not a bool, so the error can name what to
// install instead of failing deep inside a run with a bare "command not
// found" from a tool the user was never told about.
func vmToolingMissing(host Runner) []string {
	missing := []string{}
	for _, tool := range []string{vmLeaseTool, vmHostTool} {
		if _, err := host.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing
}

// vmImageReady reports whether the prepared image is on this machine. A
// missing image is worth catching here because the failure it causes
// otherwise, a VM that boots without mav, cua-driver or the TCC grants,
// surfaces as an unrelated tree or capture error minutes later.
func vmImageReady(ctx context.Context, host Runner) bool {
	result := host.Run(ctx, vmHostTool, "list", "--quiet")
	if result.Err != nil {
		// --quiet is not in every tart version; the table output carries
		// the name just the same.
		result = host.Run(ctx, vmHostTool, "list")
		if result.Err != nil {
			return false
		}
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		for _, field := range strings.Fields(line) {
			if field == vmImage {
				return true
			}
		}
	}
	return false
}

// vmMinHostVersion is not a preference, it is the line below which the
// hypervisor cannot be driven from a script at all. Earlier versions
// compute the terminal size unconditionally when running a command in the
// guest, which blows up with no TTY, and injecting the SSH key is the very
// first thing that needs it. The failure it produces says "failed to get
// terminal size", which points nowhere near "your hypervisor is too old",
// so it is checked up front instead.
const vmMinHostVersion = 2.29

// vmHostTooOld returns the version string when it is below the floor, or ""
// when it is fine or unreadable. Unreadable counts as fine: refusing to run
// because a version string could not be parsed would be worse than the
// failure this guards against, which at least happens for a reason.
func vmHostTooOld(ctx context.Context, host Runner) string {
	result := host.Run(ctx, vmHostTool, "--version")
	version := strings.TrimSpace(firstLine(result.Stdout))
	if result.Err != nil || version == "" {
		return ""
	}
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	value, err := strconv.ParseFloat(parts[0]+"."+parts[1], 64)
	if err != nil {
		return ""
	}
	if value < vmMinHostVersion {
		return version
	}
	return ""
}

// vmInstallHint is the single sentence every VM failure ends with. Users
// are never told to install the hypervisor by name: they are told to run
// the same command that installs everything else mav needs.
const vmInstallHint = "mav setup --install vm"

// acquireVMLease returns the machine this project drives, leasing a new one
// only when there is not already a live one to reuse. Reuse is the common
// case and the one that matters: an agent issues dozens of commands, each
// in its own process, and leasing per command would spend a VM boot on
// every tap.
func (c CLI) acquireVMLease(ctx context.Context, host Runner) (vmLease, error) {
	if missing := vmToolingMissing(host); len(missing) > 0 {
		return vmLease{}, fmt.Errorf("vm_tooling_missing missing=%s next=%s", strings.Join(missing, ","), vmInstallHint)
	}
	if old := vmHostTooOld(ctx, host); old != "" {
		return vmLease{}, fmt.Errorf("vm_tooling_outdated version=%s next=%s", old, vmInstallHint)
	}
	if lease, ok := readVMLease(c.Root); ok {
		switch {
		case vmLeaseIdle(lease):
			c.releaseVMLease(ctx, host)
		case vmLeaseAlive(ctx, host, lease):
			lease.LastUsed = time.Now()
			_ = writeVMLease(c.Root, lease)
			return lease, nil
		default:
			// The record outlived the machine (host rebooted, VM killed by
			// hand). Dropping it is the only way a next command can lease
			// again instead of dialing a target that is gone.
			removeVMLease(c.Root)
		}
	}
	if !vmImageReady(ctx, host) {
		return vmLease{}, fmt.Errorf("vm_image_missing image=%s next=%s", vmImage, vmInstallHint)
	}
	id, err := vmStartLease(ctx, host)
	if err != nil {
		return vmLease{}, err
	}
	lease, err := vmLeaseDetails(ctx, host, id)
	if err != nil {
		// A lease whose address cannot be read is unusable AND still holds
		// one of the two available slots, so it is handed back here instead
		// of left for the idle timeout.
		host.Run(ctx, vmLeaseTool, "stop", "--id", id)
		return vmLease{}, err
	}
	lease.Image = vmImage
	lease.Acquired = time.Now()
	lease.LastUsed = time.Now()
	if err := writeVMLease(c.Root, lease); err != nil {
		host.Run(ctx, vmLeaseTool, "stop", "--id", id)
		return vmLease{}, err
	}
	return lease, nil
}

func vmStartLease(ctx context.Context, host Runner) (string, error) {
	result := host.Run(ctx, vmLeaseTool, "warmup", "--provider", vmHostTool, "--"+vmHostTool+"-image", vmImage)
	if result.Err != nil {
		detail := firstLine(strings.TrimSpace(result.Stderr))
		if detail == "" {
			detail = firstLine(strings.TrimSpace(result.Stdout))
		}
		return "", fmt.Errorf("vm_lease_failed detail=%s next=%s", detail, vmInstallHint)
	}
	id := vmParseLeaseID(result.Stdout + "\n" + result.Stderr)
	if id == "" {
		return "", fmt.Errorf("vm_lease_id_missing next=%s", vmInstallHint)
	}
	return id, nil
}

// vmParseLeaseID pulls the lease slug out of the provisioner's output. It
// keeps the LAST match, not the first: warmup prints progress lines before
// the slug, and an early line that merely mentions a previous lease would
// otherwise win over the one just created.
func vmParseLeaseID(output string) string {
	id := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// First matching prefix wins within a line, strongest first: the
		// line that names the lease also carries several other key=value
		// pairs, and a looser pattern would happily read one of those.
		for _, prefix := range []string{"lease=", "--id=", "--id "} {
			idx := strings.Index(line, prefix)
			if idx < 0 {
				continue
			}
			if fields := strings.Fields(line[idx+len(prefix):]); len(fields) > 0 {
				id = strings.Trim(fields[0], "\"'`,.")
			}
			break
		}
	}
	return id
}

// vmLeaseInspection is the subset of the provisioner's lease description
// mav needs. It is read as JSON rather than scraped from the human-readable
// output for the usual reason: that output is theirs to reword, and a
// silent parse failure here means dialling the wrong machine while
// reporting success.
type vmLeaseInspection struct {
	SSHUser string `json:"sshUser"`
	SSHHost string `json:"sshHost"`
	SSHPort string `json:"sshPort"`
	SSHKey  string `json:"sshKey"`
	Ready   bool   `json:"ready"`
}

func vmInspectLease(ctx context.Context, host Runner, id string) (vmLeaseInspection, error) {
	result := host.Run(ctx, vmLeaseTool, "inspect", "--id", id, "--json")
	if result.Err != nil {
		return vmLeaseInspection{}, fmt.Errorf("vm_lease_unreadable id=%s detail=%s", id, firstLine(strings.TrimSpace(result.Stderr)))
	}
	var inspection vmLeaseInspection
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &inspection); err != nil {
		return vmLeaseInspection{}, fmt.Errorf("vm_lease_unreadable id=%s detail=%s", id, err)
	}
	return inspection, nil
}

// vmLeaseDetails resolves how to reach the machine. The key and port come
// from the provisioner rather than from any convention of ours: it mints a
// per-lease key, and hardcoding a location for it would break the first
// time it decides to put it somewhere else.
func vmLeaseDetails(ctx context.Context, host Runner, id string) (vmLease, error) {
	inspection, err := vmInspectLease(ctx, host, id)
	if err != nil {
		return vmLease{}, err
	}
	if inspection.SSHHost == "" || inspection.SSHUser == "" {
		return vmLease{}, fmt.Errorf("vm_lease_unreadable id=%s detail=no ssh address", id)
	}
	return vmLease{
		ID:     id,
		Target: inspection.SSHUser + "@" + inspection.SSHHost,
		Port:   inspection.SSHPort,
		Key:    inspection.SSHKey,
	}, nil
}

// vmLeaseAlive distinguishes a lease record that still names a machine from
// one that outlived it. The record survives a host reboot or a VM killed by
// hand, and trusting it means every later command dialling an address that
// no longer answers, forever, with no path back to a working run.
func vmLeaseAlive(ctx context.Context, host Runner, lease vmLease) bool {
	inspection, err := vmInspectLease(ctx, host, lease.ID)
	return err == nil && inspection.Ready && inspection.SSHHost != ""
}

// vmHeartbeat pushes out the provisioner's OWN idle deadline, which is
// separate from mav's and shorter. A single long step -- a fixture seeding
// a large database, a flow waiting on the app -- can go minutes without mav
// sending anything, and the machine would be reclaimed out from under a run
// that is still very much alive.
func vmHeartbeat(ctx context.Context, host Runner, lease vmLease) {
	host.Run(ctx, vmLeaseTool, "heartbeat", "--id", lease.ID)
}

// releaseVMLease hands the machine back and forgets it. Idempotent by
// construction: releasing twice has to be harmless, because it is called
// from `mav stop`, from the end of a flow, and from the worker's
// lease-expiry reaper, and any two of those can run in either order.
func (c CLI) releaseVMLease(ctx context.Context, host Runner) bool {
	lease, ok := readVMLease(c.Root)
	if !ok {
		return false
	}
	removeVMLease(c.Root)
	if _, err := host.LookPath(vmLeaseTool); err != nil {
		return false
	}
	host.Run(ctx, vmLeaseTool, "stop", "--id", lease.ID)
	return true
}
