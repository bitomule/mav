package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// vmHostRunner stands in for this machine while a test drives VM mode: it
// answers which tooling exists and records every command mav sends out, so
// a test can assert on what reached the leasing tool and what reached the
// guest over ssh.
type vmHostRunner struct {
	tools    map[string]bool
	commands []string
	stdout   map[string]string
	failures map[string]bool
}

func (r *vmHostRunner) LookPath(file string) (string, error) {
	if r.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (r *vmHostRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	_ = ctx
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	for fragment, out := range r.stdout {
		if strings.Contains(command, fragment) {
			return CommandResult{Stdout: out}
		}
	}
	for fragment := range r.failures {
		if strings.Contains(command, fragment) {
			return CommandResult{Code: 1, Err: os.ErrPermission}
		}
	}
	return CommandResult{}
}

func (r *vmHostRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	_ = ctx
	_ = logPath
	r.commands = append(r.commands, "start "+name+" "+strings.Join(args, " "))
	return 0, nil
}

func (r *vmHostRunner) sent(fragment string) bool {
	for _, command := range r.commands {
		if strings.Contains(command, fragment) {
			return true
		}
	}
	return false
}

func vmConfigRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\ntarget_kind: macos\nvm: true\nbundle_id: com.example.app\n")
	return root
}

func writeTestVMLease(t *testing.T, root string) vmLease {
	t.Helper()
	lease := vmLease{ID: "lease-1", Target: "admin@10.0.0.9", Image: vmImage, Acquired: time.Now(), LastUsed: time.Now()}
	if err := writeVMLease(root, lease); err != nil {
		t.Fatal(err)
	}
	return lease
}

// TestVMWithoutToolingFailsBeforeTheRunStarts: without this, `vm: true` on a
// machine that has never installed the VM tooling gets as far as the launch
// recipe and then dies with a "command not found" naming a tool the user
// was never told they needed.
func TestVMWithoutToolingFailsBeforeTheRunStarts(t *testing.T) {
	root := vmConfigRoot(t)
	host := &vmHostRunner{tools: map[string]bool{}}
	var out bytes.Buffer
	cli := CLI{Runner: host, Stdout: &out, Stderr: &bytes.Buffer{}, Root: root}

	// A failure exits non-zero; what matters here is what it says.
	_ = cli.Run(context.Background(), []string{"open"})
	if !strings.Contains(out.String(), "vm_tooling_missing") {
		t.Fatalf("the failure must say the tooling is missing: %s", out.String())
	}
	if !strings.Contains(out.String(), vmInstallHint) {
		t.Fatalf("the failure must name the command that fixes it: %s", out.String())
	}
	if host.sent("ssh") {
		t.Fatal("mav dialled a machine it had no way to lease")
	}
}

// TestDoctorDoesNotLeaseAMachine: two concurrent macOS VMs is the whole
// budget, so a diagnostic that leases one to report that leasing works
// would take a slot from the run it is diagnosing.
func TestDoctorDoesNotLeaseAMachine(t *testing.T) {
	root := vmConfigRoot(t)
	host := &vmHostRunner{tools: map[string]bool{"crabbox": true, "tart": true}}
	var out bytes.Buffer
	cli := CLI{Runner: host, Stdout: &out, Stderr: &bytes.Buffer{}, Root: root}

	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	if host.sent("crabbox warmup") {
		t.Fatal("doctor leased a VM")
	}
	if !strings.Contains(out.String(), "vm=on") {
		t.Fatalf("doctor must report that this project runs in a VM: %s", out.String())
	}
	if _, held := readVMLease(root); held {
		t.Fatal("doctor left a lease behind")
	}
}

// TestDoctorNamesTheInstallCommandWhenToolingIsMissing: doctor exists so the
// tooling problem surfaces here and not deep inside a run.
func TestDoctorNamesTheInstallCommandWhenToolingIsMissing(t *testing.T) {
	root := vmConfigRoot(t)
	host := &vmHostRunner{tools: map[string]bool{}}
	var out bytes.Buffer
	cli := CLI{Runner: host, Stdout: &out, Stderr: &bytes.Buffer{}, Root: root}

	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "vm_tooling=missing") {
		t.Fatalf("doctor must report the missing tooling: %s", out.String())
	}
	if !strings.Contains(out.String(), "vm_next="+strings.ReplaceAll(vmInstallHint, " ", "")) &&
		!strings.Contains(out.String(), vmInstallHint) {
		t.Fatalf("doctor must name the command that fixes it: %s", out.String())
	}
}

// TestStopHandsTheMachineBack: a lease that outlives the work is not
// untidiness. Virtualization.framework and the macOS EULA both cap the
// machine at two concurrent macOS VMs, so the leak is the next run failing
// to get a machine at all.
func TestStopHandsTheMachineBack(t *testing.T) {
	root := vmConfigRoot(t)
	writeTestVMLease(t, root)
	host := &vmHostRunner{
		tools:  map[string]bool{"crabbox": true, "tart": true},
		stdout: map[string]string{"crabbox inspect": `{"sshUser":"admin","sshHost":"10.0.0.9","sshPort":"22","sshKey":"/tmp/lease.key","ready":true}`},
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cli := CLI{Runner: host, Stdout: &out, Stderr: &bytes.Buffer{}, Root: root}

	if err := cli.Run(context.Background(), []string{"stop"}); err != nil {
		t.Fatal(err)
	}
	if !host.sent("crabbox stop --id lease-1") {
		t.Fatalf("stop never handed the machine back: %v", host.commands)
	}
	if _, held := readVMLease(root); held {
		t.Fatal("the lease record survived the stop, so the next run will dial a machine that is gone")
	}
}

// TestEvidenceComesHomeBeforeTheMachineGoesBack: the release takes the
// guest's disk with it. Handing the machine back first would leave the run
// directory with a report pointing at captures that no longer exist
// anywhere, and there is no second chance to fetch them.
func TestEvidenceComesHomeBeforeTheMachineGoesBack(t *testing.T) {
	root := vmConfigRoot(t)
	writeTestVMLease(t, root)
	host := &vmHostRunner{
		tools:  map[string]bool{"crabbox": true, "tart": true, "rsync": true},
		stdout: map[string]string{"crabbox inspect": `{"sshUser":"admin","sshHost":"10.0.0.9","sshPort":"22","sshKey":"/tmp/lease.key","ready":true}`},
	}
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: host, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Root: root}

	if err := cli.Run(context.Background(), []string{"stop"}); err != nil {
		t.Fatal(err)
	}
	pull, release := -1, -1
	for i, command := range host.commands {
		if pull < 0 && strings.HasPrefix(command, "rsync ") && strings.Contains(command, "admin@10.0.0.9:") {
			pull = i
		}
		if release < 0 && strings.Contains(command, "crabbox stop --id") {
			release = i
		}
	}
	if pull < 0 {
		t.Fatalf("the run directory was never pulled out of the guest: %v", host.commands)
	}
	if release < 0 || pull > release {
		t.Fatalf("the machine went back before the evidence came home: %v", host.commands)
	}
}

// TestOpenKeepsTheMachineWhileSupersedingTheOldRun: `open` stops the run it
// replaces, and that stop goes through the same code path that hands the
// machine back. Left alone, every single `open` would pay a fresh VM boot
// for a machine it was one line away from using.
func TestOpenKeepsTheMachineWhileSupersedingTheOldRun(t *testing.T) {
	root := vmConfigRoot(t)
	writeTestVMLease(t, root)
	previous, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, previous); err != nil {
		t.Fatal(err)
	}
	host := &vmHostRunner{
		tools:  map[string]bool{"crabbox": true, "tart": true, "rsync": true},
		stdout: map[string]string{"crabbox inspect": `{"sshUser":"admin","sshHost":"10.0.0.9","sshPort":"22","sshKey":"/tmp/lease.key","ready":true}`},
	}
	cli := CLI{Runner: host, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Root: root}

	superseding := cli.withStdout(&bytes.Buffer{})
	superseding.keepVM = true
	attached, detach, err := superseding.withVM(context.Background(), "stop")
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	if err := attached.stop(context.Background(), GlobalOptions{}, []string{"--run", previous.ID}); err != nil {
		t.Fatal(err)
	}
	if host.sent("crabbox stop --id") {
		t.Fatalf("superseding a run handed the machine back: %v", host.commands)
	}
	if _, held := readVMLease(root); !held {
		t.Fatal("the lease was dropped, so the open that follows will boot a second VM for nothing")
	}
}

// TestGuestProcessesAreStoppedOnTheGuest: the PIDs a run records in VM mode
// belong to the guest. Signalling them here does not fail loudly, it kills
// whatever local process happens to hold that number.
func TestGuestProcessesAreStoppedOnTheGuest(t *testing.T) {
	root := vmConfigRoot(t)
	lease := writeTestVMLease(t, root)
	host := &vmHostRunner{tools: map[string]bool{"crabbox": true, "tart": true}}
	runner := newVMRunner(host, lease, root)

	if err := stopRunProcess(runner, 4321); err != nil {
		t.Fatal(err)
	}
	if !host.sent("ssh") || !host.sent("kill -INT") {
		t.Fatalf("the stop never left this machine: %v", host.commands)
	}
	if !host.sent("4321") {
		t.Fatalf("the guest was never told which process to stop: %v", host.commands)
	}
}

// TestGuestArtifactsKeepTheirLocalPaths: mav computes artifact paths all
// over the place (run dirs, screenshots, HAR files). VM mode mirrors the
// project root at the same absolute path precisely so none of them needs
// translating; if that ever stops holding, the pull silently fetches
// nothing and the run reports evidence it does not have.
func TestGuestArtifactsKeepTheirLocalPaths(t *testing.T) {
	root := vmConfigRoot(t)
	lease := writeTestVMLease(t, root)
	host := &vmHostRunner{tools: map[string]bool{"crabbox": true, "tart": true, "rsync": true}}
	runner := newVMRunner(host, lease, root)

	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.pull(context.Background(), run.Dir); err != nil {
		t.Fatal(err)
	}
	if !host.sent(lease.Target + ":" + run.Dir + "/") {
		t.Fatalf("the guest path does not mirror the local one: %v", host.commands)
	}
	if !host.sent(" " + run.Dir + "/") {
		t.Fatalf("the evidence did not land in the local run directory: %v", host.commands)
	}
}

// TestRunDirIsNeverPushedToTheGuest: the run directory is the evidence
// channel and it only travels one way. Pushing it would overwrite captures
// the guest just took with the host's older copy of the same filenames.
func TestRunDirIsNeverPushedToTheGuest(t *testing.T) {
	for _, pattern := range vmProjectExcludes {
		if pattern == filepath.Join(MavDir, "runs") {
			return
		}
	}
	t.Fatalf("the run directory is not excluded from the push: %v", vmProjectExcludes)
}

// TestLeaseIDIsTheOneJustCreated: the provisioner prints progress before the
// slug, and a line mentioning an earlier lease would otherwise win. Driving
// the wrong lease means every command lands on somebody else's machine
// while reporting success.
func TestLeaseIDIsTheOneJustCreated(t *testing.T) {
	output := "found stale lease --id cbx_old\n" +
		"provisioning provider=tart lease=cbx_new slug=blue-lobster image=mav-macos cpus=4\n" +
		"leased cbx_new slug=blue-lobster provider=tart ip=192.168.64.10\n"
	if got := vmParseLeaseID(output); got != "cbx_new" {
		t.Fatalf("lease=%q", got)
	}
	if got := vmParseLeaseID("nothing useful here\n"); got != "" {
		t.Fatalf("an output with no slug must not invent one: %q", got)
	}
}

// TestStaleLeaseIsNotReused: the record outlives the machine whenever the
// host reboots or somebody kills the VM by hand. Trusting it means every
// command dialling an address that no longer answers, forever, with no
// path back to a working run.
func TestStaleLeaseIsNotReused(t *testing.T) {
	root := vmConfigRoot(t)
	writeTestVMLease(t, root)
	host := &vmHostRunner{
		tools:    map[string]bool{"crabbox": true, "tart": true},
		failures: map[string]bool{"crabbox inspect": true},
		stdout:   map[string]string{"tart list": vmImage + "\n"},
	}
	cli := CLI{Runner: host, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Root: root}

	if _, err := cli.acquireVMLease(context.Background(), host); err == nil {
		t.Fatal("with no new lease available this must fail, not reuse the dead one")
	}
	if _, held := readVMLease(root); held {
		t.Fatal("the dead lease was kept, so every later command will dial a machine that is gone")
	}
}

// TestMissingImageIsCaughtBeforeBooting: a VM booted from some other image
// has no mav, no driver and none of the TCC grants, and the failure that
// causes surfaces minutes later as an unrelated tree error.
func TestMissingImageIsCaughtBeforeBooting(t *testing.T) {
	root := vmConfigRoot(t)
	host := &vmHostRunner{
		tools:  map[string]bool{"crabbox": true, "tart": true},
		stdout: map[string]string{"tart list": "some-other-image\n"},
	}
	cli := CLI{Runner: host, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Root: root}

	_, err := cli.acquireVMLease(context.Background(), host)
	if err == nil || !strings.Contains(err.Error(), "vm_image_missing") {
		t.Fatalf("err=%v", err)
	}
	if host.sent("crabbox warmup") {
		t.Fatal("mav booted a machine it already knew was unusable")
	}
}

// TestBuildStaysOnThisMachine: a VM image carrying every project's build
// dependencies is not an image anybody can share, so the recipe's build
// half has to run where the toolchain is. Sending it to the guest fails
// with a missing compiler, far from the decision that caused it.
func TestBuildStaysOnThisMachine(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\ntarget_kind: macos\nvm: true\nbundle_id: com.example.app\n"+
		"launch:\n  mode: custom\n  commands:\n    build: \"echo building\"\n    launch: \"echo launching\"\n")
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	lease := vmLease{ID: "lease-1", Target: "admin@10.0.0.9", LastUsed: time.Now()}
	host := &vmHostRunner{tools: map[string]bool{"crabbox": true, "tart": true, "rsync": true}}
	guest := &vmHostRunner{tools: map[string]bool{}}
	runner := newVMRunner(guest, lease, root)

	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: runner, host: host, vmRun: runner, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Root: root}
	cli.runLaunchRecipe(context.Background(), cfg, run, false, "")

	if !host.sent("echo building") {
		t.Fatalf("the build never ran on this machine: %v", host.commands)
	}
	if guest.sent("echo building") {
		t.Fatalf("the build was sent to the guest: %v", guest.commands)
	}
	if !guest.sent("echo launching") {
		t.Fatalf("the launch never reached the guest: %v", guest.commands)
	}
}

// TestIdleLeaseIsHandedBack: an agent that crashes never calls stop, and a
// machine nobody is using still holds one of the two slots the next run
// needs.
func TestIdleLeaseIsHandedBack(t *testing.T) {
	root := vmConfigRoot(t)
	stale := vmLease{ID: "lease-idle", Target: "admin@10.0.0.9", LastUsed: time.Now().Add(-2 * vmIdleTimeout)}
	if err := writeVMLease(root, stale); err != nil {
		t.Fatal(err)
	}
	host := &vmHostRunner{
		tools:  map[string]bool{"crabbox": true, "tart": true},
		stdout: map[string]string{"crabbox inspect": `{"sshUser":"admin","sshHost":"10.0.0.9","sshPort":"22","sshKey":"/tmp/lease.key","ready":true}`, "tart list": "nothing\n"},
	}
	cli := CLI{Runner: host, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Root: root}

	_, _ = cli.acquireVMLease(context.Background(), host)
	if !host.sent("crabbox stop --id lease-idle") {
		t.Fatalf("the idle machine was never handed back: %v", host.commands)
	}
}

// TestOutdatedHypervisorIsCaughtUpFront: below the floor, the hypervisor
// dies while injecting the SSH key with a message about terminal sizes.
// Measured on a real machine, and nothing in that message points at the
// cause, so the version is checked before anything is leased.
func TestOutdatedHypervisorIsCaughtUpFront(t *testing.T) {
	root := vmConfigRoot(t)
	host := &vmHostRunner{
		tools:  map[string]bool{"crabbox": true, "tart": true},
		stdout: map[string]string{"tart --version": "2.28.1\n"},
	}
	cli := CLI{Runner: host, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Root: root}

	_, err := cli.acquireVMLease(context.Background(), host)
	if err == nil || !strings.Contains(err.Error(), "vm_tooling_outdated") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), vmInstallHint) {
		t.Fatalf("the failure must name the command that fixes it: %v", err)
	}
	if host.sent("crabbox warmup") {
		t.Fatal("a machine was leased with a hypervisor that cannot be driven from a script")
	}
}

// TestAVersionAboveTheFloorIsNotRefused: a floor that also blocks working
// versions is worse than no floor, because the workaround people reach for
// is to stop trusting the check.
func TestAVersionAboveTheFloorIsNotRefused(t *testing.T) {
	host := &vmHostRunner{
		tools:  map[string]bool{"crabbox": true, "tart": true},
		stdout: map[string]string{"tart --version": "2.32.1\n"},
	}
	if old := vmHostTooOld(context.Background(), host); old != "" {
		t.Fatalf("2.32.1 is above the floor and was refused: %q", old)
	}
	// An unreadable version must not block either: failing to parse a
	// string is a worse reason to refuse than the failure being guarded.
	quiet := &vmHostRunner{tools: map[string]bool{"tart": true}}
	if old := vmHostTooOld(context.Background(), quiet); old != "" {
		t.Fatalf("an unreadable version must not count as too old: %q", old)
	}
}

// TestInstallNeverNamesTheUnderlyingTool: the whole point of a one-key
// config surface is that nobody has to learn which project ships the
// hypervisor. Leaking it in the success output undoes that on the very
// first command a new user runs. It also pins that the VM tooling installs
// through `mav setup --install`, like everything else mav can install, and
// not through a second command nobody would think to look for.
func TestInstallNeverNamesTheUnderlyingTool(t *testing.T) {
	root := vmConfigRoot(t)
	host := &vmHostRunner{
		tools:  map[string]bool{"brew": true, "crabbox": true, "tart": true},
		stdout: map[string]string{"tart --version": "2.32.1\n", "tart list": vmImage + "\n"},
	}
	var out bytes.Buffer
	cli := CLI{Runner: host, Stdout: &out, Stderr: &bytes.Buffer{}, Root: root}

	if err := cli.Run(context.Background(), []string{"setup", "--install", "vm"}); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{vmLeaseTool, vmHostTool, "openclaw", "cirruslabs"} {
		if strings.Contains(out.String(), leak) {
			t.Fatalf("the output names %q: %s", leak, out.String())
		}
	}
	if !strings.Contains(out.String(), "vm_tooling=ok") {
		t.Fatalf("install must report that the tooling is ready: %s", out.String())
	}
}

// TestGuestAppIsResignedSoItCanLaunch: a development-signed bundle carries
// entitlements tied to a team and a device list, and in a clean VM the
// kernel kills it on launch with no message. Measured against a real app:
// the symptom is "the app is not running" three commands later, which
// points at everything except the signature.
func TestGuestAppIsResignedSoItCanLaunch(t *testing.T) {
	root := vmConfigRoot(t)
	lease := writeTestVMLease(t, root)
	guest := &vmHostRunner{tools: map[string]bool{}}
	runner := newVMRunner(guest, lease, root)
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: runner, host: guest, vmRun: runner, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Root: root}

	if err := cli.vmAdoptApp(context.Background(), run, "/tmp/Example.app"); err != nil {
		t.Fatal(err)
	}
	if !guest.sent("codesign") {
		t.Fatalf("the guest's copy was never re-signed: %v", guest.commands)
	}
	if !guest.sent("embedded.provisionprofile") {
		t.Fatalf("the provisioning profile was left in place: %v", guest.commands)
	}
	if !runner.resigned {
		t.Fatal("the bundle was changed and the run has no way to say so")
	}
}

// TestAnAlreadyLaunchableAppIsLeftAlone: re-signing is a real trade, iCloud
// and push go with it, so it only happens when the bundle would otherwise
// not launch at all.
func TestAnAlreadyLaunchableAppIsLeftAlone(t *testing.T) {
	root := vmConfigRoot(t)
	lease := writeTestVMLease(t, root)
	guest := &vmHostRunner{
		tools:    map[string]bool{},
		failures: map[string]bool{"embedded.provisionprofile": true},
	}
	runner := newVMRunner(guest, lease, root)
	run, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: runner, host: guest, vmRun: runner, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Root: root}

	if err := cli.vmAdoptApp(context.Background(), run, "/tmp/Example.app"); err != nil {
		t.Fatal(err)
	}
	if guest.sent("codesign") {
		t.Fatalf("a bundle that did not need it was re-signed anyway: %v", guest.commands)
	}
	if runner.resigned {
		t.Fatal("nothing was changed and the run says otherwise")
	}
}

// TestAnAlreadyDeadGuestProcessIsNotAFailure: a log stream that ended on
// its own is the state stop wants. Reporting it as a failure makes every
// tidy run look broken, which is how people learn to ignore stop's output.
func TestAnAlreadyDeadGuestProcessIsNotAFailure(t *testing.T) {
	root := vmConfigRoot(t)
	lease := writeTestVMLease(t, root)
	guest := &vmHostRunner{tools: map[string]bool{}}
	runner := newVMRunner(guest, lease, root)

	if err := stopRunProcess(runner, 999); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, command := range guest.commands {
		if strings.Contains(command, "kill -0 999") {
			found = true
		}
	}
	if !found {
		t.Fatalf("nothing checked whether the process was already gone: %v", guest.commands)
	}
}
