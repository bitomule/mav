package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const leaseCommand = `simpool lease --device "$MAV_DEVICE" --os "$MAV_IOS"`

func deviceRequestRoot(t *testing.T, kind, command string) string {
	t.Helper()
	t.Setenv("MAV_DEVICE", "")
	t.Setenv("MAV_IOS", "")
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.TargetKind = kind
	cfg.TargetCommand = command
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

func leaseKey(root string, request deviceRequest) string {
	env := request.env()
	env["MAV_ROOT"] = root
	return "/bin/bash -lc " + shellEnvPrefix(env) + " " + leaseCommand
}

func runDeviceCLI(t *testing.T, root string, runner Runner, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	allowFail(t, cli.Run(context.Background(), args))
	return out.String()
}

func countCommand(commands []string, command string) int {
	n := 0
	for _, c := range commands {
		if c == command {
			n++
		}
	}
	return n
}

// The model and OS reach target_command, stay out of .mav/config.yaml, and
// outlive the target cache: a later command in the same run, with nothing in
// its environment, asks target_command for the same simulator again. A
// project whose base target is a physical device gets a simulator for the run.
func TestOpenHandsDeviceAndIOSToTargetCommandForTheWholeRun(t *testing.T) {
	for _, kind := range []string{"simulator", "device"} {
		root := deviceRequestRoot(t, kind, leaseCommand)
		duo := deviceRequest{Device: "iPhone Duo", IOS: "27.1"}
		runner := &launchRecipeRunner{
			tools:   map[string]bool{"xcrun": true},
			results: map[string]CommandResult{leaseKey(root, duo): {Stdout: "DUO-UDID\n"}},
		}
		out := runDeviceCLI(t, root, runner, "open", "--device", "iPhone Duo", "--ios", "27.1", "--no-relaunch")
		if !strings.HasPrefix(out, "ok cmd=open") || !strings.Contains(out, "ios=27.1") || !strings.Contains(out, `device="iPhone Duo"`) {
			t.Fatalf("%s: open output=%q", kind, out)
		}
		if countCommand(runner.commands, leaseKey(root, duo)) == 0 {
			t.Fatalf("%s: target_command never saw the device: %v", kind, runner.commands)
		}
		config, err := os.ReadFile(filepath.Join(root, MavDir, "config.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(config), "DUO-UDID") || strings.Contains(string(config), "iPhone Duo") {
			t.Fatalf("%s: the device leaked into .mav/config.yaml:\n%s", kind, config)
		}

		run, err := LoadRun(root, "")
		if err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(targetCommandCachePath(run))
		if err := os.WriteFile(run.LogsPath, []byte("hello\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		before := countCommand(runner.commands, leaseKey(root, duo))
		t.Setenv("MAV_DEVICE", "")
		t.Setenv("MAV_IOS", "")
		out = runDeviceCLI(t, root, runner, "logs", "--run", run.ID)
		if countCommand(runner.commands, leaseKey(root, duo)) != before+1 {
			t.Fatalf("%s: with the cache gone, a later command did not ask target_command for the run's device: %v", kind, runner.commands)
		}
		if countCommand(runner.commands, leaseKey(root, deviceRequest{})) != 0 {
			t.Fatalf("%s: target_command ran without the run's device: %v", kind, runner.commands)
		}
		if !strings.Contains(out, "udid=DUO-UDID") || !strings.Contains(out, "target_kind=simulator") {
			t.Fatalf("%s: later command output=%q", kind, out)
		}
	}
}

func TestOpenRefusesADeviceTargetCommandWouldIgnore(t *testing.T) {
	cases := []struct {
		name    string
		command string
		args    []string
		reason  string
	}{
		{"never reads it", "simpool lease --device 'iPhone 17 Pro'", []string{"--device", "iPhone Duo"}, "does not read $MAV_DEVICE"},
		{"reads the device but not the ios", `simpool lease --device "$MAV_DEVICE"`, []string{"--device", "iPhone Duo", "--ios", "27.1"}, "does not read $MAV_IOS"},
	}
	for _, tc := range cases {
		root := deviceRequestRoot(t, "simulator", tc.command)
		runner := &launchRecipeRunner{tools: map[string]bool{"xcrun": true}}
		out := runDeviceCLI(t, root, runner, append([]string{"open"}, append(tc.args, "--no-relaunch")...)...)
		if !strings.Contains(out, "code=open_device_unusable") || !strings.Contains(out, tc.reason) {
			t.Fatalf("%s: output=%q", tc.name, out)
		}
		for _, command := range runner.commands {
			if strings.Contains(command, "simpool") {
				t.Fatalf("%s: target_command ran although the request was refused: %v", tc.name, runner.commands)
			}
		}
	}
}

func TestOpenRefusesADeviceWhenASimulatorIsPinned(t *testing.T) {
	root := deviceRequestRoot(t, "simulator", leaseCommand)
	t.Setenv("MAV_TARGET_UDID", "PINNED")
	runner := &launchRecipeRunner{tools: map[string]bool{"xcrun": true}}
	out := runDeviceCLI(t, root, runner, "open", "--device", "iPhone Duo", "--no-relaunch")
	if !strings.Contains(out, "code=open_device_unusable") || !strings.Contains(out, "pinned simulator") {
		t.Fatalf("output=%q", out)
	}
}

func TestWithoutFlagValuesDropsBothSpellings(t *testing.T) {
	got := withoutFlagValues([]string{"--device", "iPhone Duo", "--ios=27.1", "--no-relaunch"}, "--device", "--ios")
	if strings.Join(got, " ") != "--no-relaunch" {
		t.Fatalf("got %v", got)
	}
}
