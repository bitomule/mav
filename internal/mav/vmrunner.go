package mav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// vmRunner is a Runner that executes on the leased machine instead of this
// one. It is the whole of "mav drives a VM": every driver, every launch
// command and every log stream already goes through Runner, so swapping
// this in relocates all of them at once, and nothing above has to learn
// that a VM exists.
//
// The remote project root is the SAME absolute path as the local one. That
// looks odd until you consider the alternative: mav computes artifact paths
// everywhere (run dirs, screenshots, tree dumps, HAR files), and any other
// choice would mean translating every one of them at every call site, with
// a silent wrong-file bug waiting on each one it missed. Mirroring the path
// makes translation unnecessary by construction.
type vmRunner struct {
	host   Runner
	lease  vmLease
	root   string
	socket string

	// released is set once the machine has been handed back, so a deferred
	// evidence pull that runs afterwards knows there is nothing to pull
	// from instead of hanging on a dead address.
	released bool

	mu    sync.Mutex
	paths map[string]string
}

// vmRemoteEnv is the PATH a non-interactive SSH session does NOT get. The
// image writes it into .zshenv, but that only helps a shell that reads it;
// an `ssh host cmd` invocation runs cmd directly, so mav exports it itself
// rather than depending on which shell the guest happens to pick.
const vmRemoteEnv = `export PATH=/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$PATH;`

func newVMRunner(host Runner, lease vmLease, root string) *vmRunner {
	return &vmRunner{
		host:   host,
		lease:  lease,
		root:   root,
		socket: vmControlSocket(lease),
		paths:  map[string]string{},
	}
}

// vmControlSocket is where SSH keeps its multiplexed connection. It lives
// under /tmp with a hashed name, not under the run dir, for a boring reason
// that costs a day when you hit it: a unix socket path is capped at 104
// bytes on macOS, and a project root plus a run id blows through that.
//
// Multiplexing is not an optimisation here, it is what makes the feature
// usable: a single `mav ui tap` fans out into several remote calls, and a
// fresh TCP+auth handshake on each one adds up to seconds per command.
func vmControlSocket(lease vmLease) string {
	sum := sha256.Sum256([]byte(lease.ID + "@" + lease.Target))
	return filepath.Join(os.TempDir(), "mav-vm-"+hex.EncodeToString(sum[:6])+".sock")
}

func (v *vmRunner) sshArgs() []string {
	return []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "BatchMode=yes",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + v.socket,
		"-o", "ControlPersist=600",
	}
}

// sshCommand returns the argv for running one remote shell snippet.
func (v *vmRunner) sshCommand(remote string) []string {
	return append(append(v.sshArgs(), v.lease.Target), remote)
}

// remoteShell wraps a command so it runs with the guest's real PATH and in
// the mirrored project root, matching what runLaunchCommand does locally
// with `cd ... && export ... &&`.
func (v *vmRunner) remoteShell(command string) string {
	return vmRemoteEnv + " cd " + shellQuote(v.root) + " 2>/dev/null; " + command
}

func (v *vmRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(name))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return v.host.Run(ctx, "ssh", v.sshCommand(v.remoteShell("exec "+strings.Join(parts, " ")))...)
}

// LookPath answers from a single batched probe of everything mav might ask
// for. resolveConfigTools walks the whole known-tool list before every
// command, so answering one round trip at a time would put a dozen SSH
// handshakes in front of every tap.
func (v *vmRunner) LookPath(file string) (string, error) {
	v.mu.Lock()
	if path, ok := v.paths[file]; ok {
		v.mu.Unlock()
		if path == "" {
			return "", fmt.Errorf("%s: not found in %s", file, v.lease.Target)
		}
		return path, nil
	}
	v.mu.Unlock()

	wanted := append([]string{file}, knownTools()...)
	var probe strings.Builder
	for _, tool := range wanted {
		probe.WriteString("printf '%s\\t%s\\n' " + shellQuote(tool) + " \"$(command -v " + shellQuote(tool) + " 2>/dev/null)\"; ")
	}
	result := v.host.Run(context.Background(), "ssh", v.sshCommand(v.remoteShell(probe.String()))...)

	v.mu.Lock()
	for _, line := range strings.Split(result.Stdout, "\n") {
		tool, path, found := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		if !found {
			continue
		}
		v.paths[tool] = strings.TrimSpace(path)
	}
	path, known := v.paths[file]
	v.mu.Unlock()
	if !known || path == "" {
		return "", fmt.Errorf("%s: not found in %s", file, v.lease.Target)
	}
	return path, nil
}

// Start runs a long-lived process on the guest and returns the guest's PID.
// The log file is the guest's too, at the mirrored path, so the run's own
// evidence sync brings it back like every other artifact.
func (v *vmRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	if logPath == "" {
		logPath = "/dev/null"
	}
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(name))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	command := "mkdir -p " + shellQuote(filepath.Dir(logPath)) + "; " +
		"nohup " + strings.Join(parts, " ") + " >> " + shellQuote(logPath) + " 2>&1 & echo $!"
	result := v.host.Run(ctx, "ssh", v.sshCommand(v.remoteShell(command))...)
	if result.Err != nil {
		return 0, fmt.Errorf("vm_start_failed: %s", firstLine(strings.TrimSpace(result.Stderr)))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(firstLine(result.Stdout)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("vm_start_no_pid: %s", firstLine(strings.TrimSpace(result.Stdout)))
	}
	return pid, nil
}

// Stop implements processStopper. Without it, `mav stop` would take the
// PIDs this Runner produced -- guest PIDs -- and send signals to whatever
// happens to hold those numbers on the host. That is not a leak, it is
// killing an unrelated local process, which is why the seam exists at all.
func (v *vmRunner) Stop(pid int) error {
	if pid <= 0 {
		return nil
	}
	id := strconv.Itoa(pid)
	command := "kill -INT -" + id + " 2>/dev/null || kill -INT " + id
	result := v.host.Run(context.Background(), "ssh", v.sshCommand(v.remoteShell(command))...)
	if result.Err != nil {
		return fmt.Errorf("vm_stop_failed pid=%d: %s", pid, firstLine(strings.TrimSpace(result.Stderr)))
	}
	return nil
}

// closeControlMaster tears down the multiplexed connection. Leaving it
// behind would keep an SSH client alive for ControlPersist after the VM is
// already gone, and the next lease would find a stale socket at the same
// path only when the lease id happened to repeat -- rare enough to be a
// nightmare to debug, cheap enough to just prevent.
func (v *vmRunner) closeControlMaster() {
	args := append(v.sshArgs(), "-O", "exit", v.lease.Target)
	v.host.Run(context.Background(), "ssh", args...)
	_ = os.Remove(v.socket)
}

// processStopper is how a Runner that did not start a process on THIS
// machine gets to stop it anyway. Optional on purpose: making it part of
// Runner would force every fake in the test suite to grow a method none of
// them need.
type processStopper interface {
	Stop(pid int) error
}

// stopRunProcess kills a process the run started, wherever it started.
func stopRunProcess(runner Runner, pid int) error {
	if stopper, ok := runner.(processStopper); ok {
		return stopper.Stop(pid)
	}
	return stopProcess(pid)
}

// --- file transfer -------------------------------------------------------

// vmMirrorDir creates dir on the guest at the same absolute path it has
// here. The project root normally lives under another user's home, which
// does not exist in the guest, so the ancestors are created too -- with
// sudo, because /Users is not writable, and owned by the SSH user, because
// rsync then writes as that user.
func (v *vmRunner) vmMirrorDir(ctx context.Context, dir string) error {
	ancestors := []string{}
	for path := filepath.Clean(dir); path != "/" && path != "."; path = filepath.Dir(path) {
		ancestors = append([]string{path}, ancestors...)
	}
	var script strings.Builder
	script.WriteString(`u="$(id -un)"; `)
	for _, path := range ancestors {
		script.WriteString("[ -d " + shellQuote(path) + " ] || sudo install -d -o \"$u\" -m 755 " + shellQuote(path) + " || exit 1; ")
	}
	result := v.host.Run(ctx, "ssh", v.sshCommand(vmRemoteEnv+script.String()+"true")...)
	if result.Err != nil {
		return fmt.Errorf("vm_mirror_failed dir=%s detail=%s", dir, firstLine(strings.TrimSpace(result.Stderr)))
	}
	return nil
}

func (v *vmRunner) rsyncShell() string {
	return "ssh " + strings.Join(v.sshArgs(), " ")
}

// push copies a local directory to the same absolute path on the guest.
func (v *vmRunner) push(ctx context.Context, dir string, excludes []string) error {
	if err := v.vmMirrorDir(ctx, dir); err != nil {
		return err
	}
	args := []string{"-a", "--delete", "-e", v.rsyncShell()}
	for _, pattern := range excludes {
		args = append(args, "--exclude", pattern)
	}
	args = append(args, strings.TrimSuffix(dir, "/")+"/", v.lease.Target+":"+dir+"/")
	result := v.host.Run(ctx, "rsync", args...)
	if result.Err != nil {
		return fmt.Errorf("vm_push_failed dir=%s detail=%s", dir, firstLine(strings.TrimSpace(result.Stderr)))
	}
	return nil
}

// pull copies a guest directory back to the same absolute path here. This
// is the whole point of the feature: evidence that stays inside a machine
// that is then handed back is not evidence.
//
// No --delete: the local run dir also holds things mav wrote on this side
// (the current-run pointer's siblings, a report generated locally), and
// mirroring the guest's view over them would delete them.
func (v *vmRunner) pull(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	args := []string{"-a", "-e", v.rsyncShell(), v.lease.Target + ":" + strings.TrimSuffix(dir, "/") + "/", dir + "/"}
	result := v.host.Run(ctx, "rsync", args...)
	if result.Err != nil {
		return fmt.Errorf("vm_pull_failed dir=%s detail=%s", dir, firstLine(strings.TrimSpace(result.Stderr)))
	}
	return nil
}
