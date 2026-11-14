package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

type openURLRunner struct {
	coreDeviceRecipeRunner
	name string
	args []string
}

func (r *openURLRunner) LookPath(name string) (string, error) {
	if name == "idb" {
		return "", errors.New("not installed")
	}
	return "/usr/bin/" + name, nil
}

func (r *openURLRunner) Run(_ context.Context, name string, args ...string) CommandResult {
	if name == "xcrun" && len(args) > 0 && args[0] == "--find" {
		return CommandResult{Stdout: "/usr/bin/devicectl"}
	}
	r.name = name
	r.args = nil
	for i := 0; i < len(args); i++ {
		if args[i] == "--json-output" && i+1 < len(args) {
			body, _ := json.Marshal(map[string]any{"result": map[string]any{"process": map[string]int{"processIdentifier": 12}}})
			if err := os.WriteFile(args[i+1], body, 0600); err != nil {
				return CommandResult{Err: err}
			}
			i++
			continue
		}
		r.args = append(r.args, args[i])
	}
	return CommandResult{}
}

func TestOpenURLExplicitTargetPreservesURL(t *testing.T) {
	const rawURL = "demo://settings/a%2Fb?q=a%20b+c&next=x%26y%3Dz#part%201"
	for _, kind := range []string{"device", "simulator", "macos"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			cfg := DefaultConfig(root)
			cfg.TargetKind = "device"
			cfg.DeviceUDID = "CONFIG-DEVICE"
			cfg.SimulatorUDID = "CONFIG-SIM"
			cfg.BundleID = "com.example.app"
			if err := SaveConfig(root, cfg); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MAV_TARGET_KIND", kind)
			t.Setenv("MAV_TARGET_UDID", "SELECTED-TARGET")
			runner := &openURLRunner{}
			var out bytes.Buffer
			cli := CLI{Root: root, Runner: runner, Stdout: &out, Stderr: &bytes.Buffer{}, skipTargetResolution: true}
			if err := cli.Run(context.Background(), []string{"openURL", rawURL}); err != nil {
				t.Fatal(err)
			}
			name := "xcrun"
			want := []string{"devicectl", "device", "process", "launch", "--payload-url", rawURL, "--device", "SELECTED-TARGET", "com.example.app", "--"}
			if kind == "simulator" {
				want = []string{"simctl", "openurl", "SELECTED-TARGET", rawURL}
			}
			if kind == "macos" {
				name, want = "open", []string{rawURL}
			}
			if runner.name != name || !reflect.DeepEqual(runner.args, want) {
				t.Fatalf("got %s %#v; want %s %#v; output=%s", runner.name, runner.args, name, want, out.String())
			}
		})
	}
}
