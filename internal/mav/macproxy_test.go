package mav

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const routeOutput = "   route to: default\ndestination: default\n  interface: en0\n"
const serviceOrder = "An asterisk (*) denotes that a network service is disabled.\n(1) Tunel\n(Hardware Port: tun, Device: utun0)\n\n(4) Ethernet\n(Hardware Port: Ethernet, Device: en0)\n"

// TestMacNetworkServiceFollowsTheDefaultRoute: taking the first service of
// the list looks equivalent and is not, that is where the virtual
// interfaces left by VMs and tunnels show up. Setting the proxy on the
// wrong one gives NO error: it simply captures nothing, which is the most
// expensive failure to diagnose.
func TestMacNetworkServiceFollowsTheDefaultRoute(t *testing.T) {
	runner := &scriptedRunner{outputs: map[string]string{
		"route -n get default":                  routeOutput,
		"networksetup -listnetworkserviceorder": serviceOrder,
	}}
	cli := CLI{Runner: runner, Root: t.TempDir()}
	if got := cli.macNetworkService(context.Background()); got != "Ethernet" {
		t.Fatalf("it must follow the default route, not the list order: %q", got)
	}
}

// TestMacProxyRestoresWhatItFound pins the property that makes this safe:
// stopping the capture returns the machine to how it was. Without it, a run
// that dies leaves the system pointing at a proxy that no longer exists,
// that is, without network and without a single clue why.
func TestMacProxyRestoresWhatItFound(t *testing.T) {
	root := t.TempDir()
	run := RunState{ID: "r1", Dir: root}
	runner := &scriptedRunner{outputs: map[string]string{
		"route -n get default":                  routeOutput,
		"networksetup -listnetworkserviceorder": serviceOrder,
		"networksetup -getwebproxy Ethernet":    "Enabled: Yes\nServer: 10.0.0.1\nPort: 3128\n",
	}}
	cli := CLI{Runner: runner, Root: root}
	if _, ok := cli.pointMacAtProxy(context.Background(), run, 8080); !ok {
		t.Fatal("it should have set the proxy")
	}
	if !strings.Contains(strings.Join(runner.commands, "|"), "-setsecurewebproxy Ethernet 127.0.0.1 8080") {
		t.Fatalf("https too: an app that only speaks https would ignore the other one: %v", runner.commands)
	}
	if _, err := os.Stat(filepath.Join(root, macProxyStateFile)); err != nil {
		t.Fatal("the previous state has to go to disk: start and stop are separate processes")
	}
	runner.commands = nil
	if !cli.restoreMacProxy(context.Background(), run) {
		t.Fatal("it should restore")
	}
	joined := strings.Join(runner.commands, "|")
	if !strings.Contains(joined, "-setwebproxy Ethernet 10.0.0.1 3128") {
		t.Fatalf("there was a proxy before and it must be put back, not turned off: %v", runner.commands)
	}
	if cli.restoreMacProxy(context.Background(), run) {
		t.Fatal("restoring twice has to be harmless")
	}
}

// TestMacProxyTurnsOffWhenThereWasNoProxy: if there was no proxy, restoring
// means turning it off. Leaving it set with the port of a dead capture
// would be worse than not having captured.
func TestMacProxyTurnsOffWhenThereWasNoProxy(t *testing.T) {
	root := t.TempDir()
	run := RunState{ID: "r1", Dir: root}
	runner := &scriptedRunner{outputs: map[string]string{
		"route -n get default":                  routeOutput,
		"networksetup -listnetworkserviceorder": serviceOrder,
		"networksetup -getwebproxy Ethernet":    "Enabled: No\nServer: \nPort: 0\n",
	}}
	cli := CLI{Runner: runner, Root: root}
	if _, ok := cli.pointMacAtProxy(context.Background(), run, 8080); !ok {
		t.Fatal("it should have set the proxy")
	}
	runner.commands = nil
	cli.restoreMacProxy(context.Background(), run)
	if !strings.Contains(strings.Join(runner.commands, "|"), "-setwebproxystate Ethernet off") {
		t.Fatalf("%v", runner.commands)
	}
}

// scriptedRunner answers by substring and records what ran, which is what
// testing a sequence of system commands needs.
type scriptedRunner struct {
	outputs  map[string]string
	commands []string
}

func (r *scriptedRunner) Run(_ context.Context, name string, args ...string) CommandResult {
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	for needle, out := range r.outputs {
		if strings.Contains(command, needle) {
			return CommandResult{Stdout: out}
		}
	}
	return CommandResult{}
}

func (r *scriptedRunner) Start(_ context.Context, _ string, name string, args ...string) (int, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	return 1, nil
}

func (r *scriptedRunner) LookPath(name string) (string, error) { return "/usr/sbin/" + name, nil }

// TestMacClockIsClosedOutsideAVM: on macOS moving the clock moves the whole
// machine's, it expires certificates, breaks sessions, shuffles files by
// date. Inside a dedicated VM that is cheap; on somebody's Mac it is not.
// That is why the gate is closed unless said explicitly.
func TestMacClockIsClosedOutsideAVM(t *testing.T) {
	host := &scriptedRunner{outputs: map[string]string{"sysctl -n kern.hv_vmm_present": "0\n"}}
	cli := CLI{Runner: host, Root: t.TempDir()}
	if cli.macClockAllowed(context.Background(), []string{"travel"}) {
		t.Fatal("outside a VM the clock is not touched without saying so")
	}
	if !cli.macClockAllowed(context.Background(), []string{"travel", "--system-clock"}) {
		t.Fatal("whoever says it explicitly knows what they are doing")
	}

	vm := &scriptedRunner{outputs: map[string]string{"sysctl -n kern.hv_vmm_present": "1\n"}}
	inVM := CLI{Runner: vm, Root: t.TempDir()}
	if !inVM.macClockAllowed(context.Background(), []string{"travel"}) {
		t.Fatal("inside a VM is the case it exists for")
	}
}

// TestMacTimeTravelDisablesNetworkTimeFirst: otherwise the system undoes
// the change whenever it feels like checking the time, and the symptom
// would be a test that passes or fails depending on when it runs.
func TestMacTimeTravelDisablesNetworkTimeFirst(t *testing.T) {
	runner := &scriptedRunner{}
	cli := CLI{Runner: runner, Root: t.TempDir()}
	at, err := time.Parse(time.RFC3339, "2030-06-15T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.macTimeTravel(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) < 3 || !strings.Contains(runner.commands[0], "-setusingnetworktime off") {
		t.Fatalf("turning off sync goes FIRST: %v", runner.commands)
	}
	joined := strings.Join(runner.commands, "|")
	if !strings.Contains(joined, "-setdate") || !strings.Contains(joined, "-settime") {
		t.Fatalf("%v", runner.commands)
	}
}
