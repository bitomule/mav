package coredevice

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type recordedCommand struct {
	name string
	args []string
}
type fakeExecutor struct {
	commands []recordedCommand
	results  []string
	result   drivers.ExecResult
	missing  bool
	noFile   bool
	paths    []string
}

func (f *fakeExecutor) LookPath(name string) (string, error) {
	if f.missing {
		return "", errors.New("missing")
	}
	return "/usr/bin/" + name, nil
}
func (f *fakeExecutor) Start(context.Context, string, string, ...string) (int, error) {
	panic("not used")
}
func (f *fakeExecutor) Run(_ context.Context, name string, args ...string) drivers.ExecResult {
	f.commands = append(f.commands, recordedCommand{name: name, args: append([]string{}, args...)})
	for i, arg := range args {
		if arg != "--json-output" || i+1 >= len(args) {
			continue
		}
		path := args[i+1]
		f.paths = append(f.paths, path)
		if f.noFile {
			break
		}
		body := `{"info":{"outcome":"success"},"result":{}}`
		if len(f.results) > 0 {
			body = f.results[0]
			f.results = f.results[1:]
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			panic(err)
		}
		break
	}
	return f.result
}
func device() drivers.Target {
	return drivers.Target{Kind: drivers.KindDevice, UDID: "00008101-000A0C123456001E", BundleID: "com.example.app"}
}
func newTestDriver(t *testing.T, exec *fakeExecutor) *Driver {
	t.Helper()
	return New(exec, t.TempDir())
}
func withoutResultPath(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--json-output" {
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func TestLifecycleCapabilitiesArePhysicalOnly(t *testing.T) {
	d := New(&fakeExecutor{})
	for _, kind := range []drivers.TargetKind{drivers.KindSim, drivers.KindMac} {
		if len(d.Provides(drivers.Target{Kind: kind})) != 0 {
			t.Fatalf("capabilities leaked to %s", kind)
		}
	}
	caps := d.Provides(device())
	for _, cap := range []drivers.Capability{drivers.CapInstall, drivers.CapLaunch, drivers.CapUninstall, drivers.CapAppList, drivers.CapTerminate, drivers.CapOpenURL} {
		if !caps.Has(cap) || d.Cost(cap, device()) != 0 {
			t.Fatalf("missing canonical %s", cap)
		}
	}
	for _, cap := range []drivers.Capability{drivers.CapBoot, drivers.CapClipboard, drivers.CapLocation, drivers.CapTreeAX} {
		if caps.Has(cap) {
			t.Fatalf("unsupported %s advertised", cap)
		}
	}
	if err := d.Boot(context.Background(), device()); err == nil {
		t.Fatal("boot silently succeeded")
	}
	if err := d.SetLocation(context.Background(), device(), 1, 2); err == nil {
		t.Fatal("location silently succeeded")
	}
	if _, err := d.ClipboardRead(context.Background(), device()); err == nil {
		t.Fatal("clipboard silently succeeded")
	}
}

func TestProbeRequiresSelectedXcodeDevicectl(t *testing.T) {
	ctx := context.Background()
	missing := &fakeExecutor{missing: true}
	if got := New(missing).Probe(ctx, missing); got.State != drivers.HealthMissing {
		t.Fatalf("health=%#v", got)
	}
	broken := &fakeExecutor{result: drivers.ExecResult{Err: errors.New("not found")}}
	if got := New(broken).Probe(ctx, broken); got.State != drivers.HealthBroken {
		t.Fatalf("health=%#v", got)
	}
	healthy := &fakeExecutor{}
	if got := New(healthy).Probe(ctx, healthy); !got.IsHealthy() {
		t.Fatalf("health=%#v", got)
	}
	if got := healthy.commands[0]; got.name != "xcrun" || !reflect.DeepEqual(got.args, []string{"--find", "devicectl"}) {
		t.Fatalf("probe=%#v", got)
	}
}

func TestInstallAndUninstallUseExplicitDeviceAndPrivateResultFiles(t *testing.T) {
	exec := &fakeExecutor{}
	d := newTestDriver(t, exec)
	ctx := context.Background()
	if err := d.Install(ctx, device(), drivers.InstallSpec{Path: "/workspace/Test App.app"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Uninstall(ctx, device(), "com.example.app"); err != nil {
		t.Fatal(err)
	}
	wants := [][]string{
		{"devicectl", "device", "install", "app", "/workspace/Test App.app", "--device", device().UDID},
		{"devicectl", "device", "uninstall", "app", "com.example.app", "--device", device().UDID},
	}
	for i, command := range exec.commands {
		if command.name != "xcrun" || !reflect.DeepEqual(withoutResultPath(command.args), wants[i]) {
			t.Fatalf("command=%#v", command)
		}
	}
	for _, path := range exec.paths {
		rel, err := filepath.Rel(d.tempRoot, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("result escaped root: %s", path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("result left behind: %v", err)
		}
	}
}

func TestLaunchReadsJSONPIDAndPreservesAppArgumentsAndEnvironment(t *testing.T) {
	exec := &fakeExecutor{results: []string{`{"info":{"outcome":"success"},"result":{"process":{"processIdentifier":4821}}}`}, result: drivers.ExecResult{Stdout: "launched with unrelated human PID 999"}}
	d := newTestDriver(t, exec)
	env := map[string]string{"FIRST": "one", "SPACES": "value with spaces", "QUOTE": "a\"b", "NEWLINE": "a\nb", "DEVICECTL_CHILD_NAME": "literal"}
	args := []string{"--json-output", "app-data", "--", "words with spaces"}
	got, err := d.Launch(context.Background(), device(), drivers.LaunchSpec{BundleID: "com.example.app", Args: args, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 4821 || got.BundleID != "com.example.app" {
		t.Fatalf("launch=%#v", got)
	}
	command := exec.commands[0]
	var received map[string]string
	for i, arg := range command.args {
		if arg == "--environment-variables" {
			if err := json.Unmarshal([]byte(command.args[i+1]), &received); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !reflect.DeepEqual(received, env) {
		t.Fatalf("environment changed")
	}
	boundary := -1
	for i, arg := range command.args {
		if arg == "--" {
			boundary = i
			break
		}
	}
	if boundary < 0 || !reflect.DeepEqual(command.args[boundary+1:], args) {
		t.Fatalf("arguments=%#v", command.args)
	}
	if !reflect.DeepEqual(command.args[boundary-4:boundary], []string{"--device", device().UDID, "--json-output", exec.paths[0]}) {
		t.Fatalf("device options crossed app boundary: %#v", command.args)
	}
	if env["FIRST"] != "one" {
		t.Fatal("caller environment mutated")
	}
}

func TestOpenURLPassesPayloadToConfiguredApp(t *testing.T) {
	exec := &fakeExecutor{results: []string{`{"result":{"process":{"processIdentifier":12}}}`}}
	d := newTestDriver(t, exec)
	if err := d.OpenURL(context.Background(), device(), "demo://settings?tab=one"); err != nil {
		t.Fatal(err)
	}
	got := withoutResultPath(exec.commands[0].args)
	want := []string{"devicectl", "device", "process", "launch", "--payload-url", "demo://settings?tab=one", "com.example.app", "--device", device().UDID, "--"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command=%#v", got)
	}
	target := device()
	target.BundleID = ""
	if err := d.OpenURL(context.Background(), target, "demo://settings"); err == nil {
		t.Fatal("missing app silently opened URL")
	}
	if len(exec.commands) != 1 {
		t.Fatal("invalid operation invoked device")
	}
}

func TestTerminateMatchesExactAppBundleAndDoesNotKillPrefixNeighbour(t *testing.T) {
	exec := &fakeExecutor{results: []string{
		`{"result":{"apps":[{"bundleIdentifier":"com.example.app","url":"file:///private/Applications/Example.app/"}]}}`,
		`{"result":{"runningProcesses":[{"processIdentifier":10,"executable":"file:///private/Applications/Example.app/Example"},{"processIdentifier":11,"executable":"file:///private/Applications/Example.app.evil/Example"},{"processIdentifier":12,"executable":"file:///private/Applications/Other.app/Example"}]}}`,
	}}
	d := newTestDriver(t, exec)
	if err := d.Terminate(context.Background(), device(), "com.example.app"); err != nil {
		t.Fatal(err)
	}
	if len(exec.commands) != 3 {
		t.Fatalf("commands=%#v", exec.commands)
	}
	got := withoutResultPath(exec.commands[2].args)
	want := []string{"devicectl", "device", "process", "terminate", "--pid", "10", "--device", device().UDID}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command=%#v", got)
	}
}

func TestTerminateNotRunningSucceedsWithoutLaunchingAnything(t *testing.T) {
	exec := &fakeExecutor{results: []string{`{"result":{"apps":[]}}`, `{"result":{"runningProcesses":[]}}`}}
	if err := newTestDriver(t, exec).Terminate(context.Background(), device(), "com.example.absent"); err != nil {
		t.Fatal(err)
	}
	if len(exec.commands) != 2 {
		t.Fatalf("commands=%#v", exec.commands)
	}
}

func TestTerminateUnreadableIdentityDoesNotGuessAProcessByName(t *testing.T) {
	exec := &fakeExecutor{results: []string{
		`{"result":{"apps":[{"bundleIdentifier":"com.example.app","name":"Example"}]}}`,
		`{"result":{"runningProcesses":[{"processIdentifier":10,"name":"Example","executable":"file:///private/Applications/Other.app/Example"}]}}`,
	}}
	if err := newTestDriver(t, exec).Terminate(context.Background(), device(), "com.example.app"); err == nil {
		t.Fatal("unidentified target reported terminated")
	}
	if len(exec.commands) != 2 {
		t.Fatalf("a guessed process was terminated: %#v", exec.commands)
	}
}

func TestLifecycleFailureUsesJSONOutcomeAndRequiresAResultFile(t *testing.T) {
	for _, exec := range []*fakeExecutor{
		{results: []string{`{"info":{"outcome":"failure"},"result":{}}`}, result: drivers.ExecResult{Stdout: "Installed successfully"}},
		{noFile: true, result: drivers.ExecResult{Stdout: "Installed successfully"}},
	} {
		if err := newTestDriver(t, exec).Install(context.Background(), device(), drivers.InstallSpec{Path: "/workspace/Example.app"}); err == nil {
			t.Fatal("human stdout overrode a missing or failed result")
		}
	}
}

func TestListAppsReturnsStructuredArrayIncludingSystemApps(t *testing.T) {
	exec := &fakeExecutor{results: []string{`{"result":{"apps":[{"bundleIdentifier":"com.example.app","name":"Example"}]}}`}}
	got, err := newTestDriver(t, exec).ListApps(context.Background(), device())
	if err != nil || got != `[{"bundleIdentifier":"com.example.app","name":"Example"}]` {
		t.Fatalf("apps=%q err=%v", got, err)
	}
	if !strings.Contains(strings.Join(exec.commands[0].args, " "), "--include-all-apps") {
		t.Fatal("list omitted system apps")
	}
}

func TestMalformedOrFailedResultsCannotReportLaunchSuccess(t *testing.T) {
	for _, body := range []string{`not JSON`, `{"result":{}}`, `{"result":{"process":{"processIdentifier":0}}}`, `{"info":{"outcome":"failure"},"result":{"process":{"processIdentifier":12}}}`} {
		t.Run(body, func(t *testing.T) {
			exec := &fakeExecutor{results: []string{body}}
			if _, err := newTestDriver(t, exec).Launch(context.Background(), device(), drivers.LaunchSpec{BundleID: "com.example.app"}); err == nil {
				t.Fatal("unproven launch reported success")
			}
		})
	}
}

func TestLaunchFailureDoesNotExposeEnvironmentValues(t *testing.T) {
	exec := &fakeExecutor{result: drivers.ExecResult{Stderr: `error with arguments {"TOKEN":"synthetic-secret"}`, Err: errors.New("failed synthetic-secret")}}
	d := newTestDriver(t, exec)
	_, err := d.Launch(context.Background(), device(), drivers.LaunchSpec{BundleID: "com.example.app", Env: map[string]string{"TOKEN": "synthetic-secret"}})
	if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatalf("error leaked environment: %v", err)
	}
	for _, path := range exec.paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("failed result not cleaned up")
		}
	}
}

func TestInvalidTargetOrInputsNeverInvokeDevice(t *testing.T) {
	exec := &fakeExecutor{}
	d := newTestDriver(t, exec)
	ctx := context.Background()
	for _, target := range []drivers.Target{{Kind: drivers.KindSim, UDID: "SIM-1"}, {Kind: drivers.KindDevice}} {
		if err := d.Install(ctx, target, drivers.InstallSpec{Path: "/workspace/Example.app"}); err == nil {
			t.Fatal("invalid target accepted")
		}
	}
	if _, err := d.Launch(ctx, device(), drivers.LaunchSpec{BundleID: "com.example.app", Env: map[string]string{"BAD=KEY": "value"}}); err == nil {
		t.Fatal("invalid environment accepted")
	}
	if err := d.Install(ctx, device(), drivers.InstallSpec{}); err == nil {
		t.Fatal("empty app path accepted")
	}
	if err := d.Terminate(ctx, device(), ""); err == nil {
		t.Fatal("empty bundle accepted")
	}
	if len(exec.commands) != 0 {
		t.Fatalf("invalid inputs executed: %#v", exec.commands)
	}
}
