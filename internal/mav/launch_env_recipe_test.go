package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The whole point, end to end: a recipe that puts FOO=bar in front of the
// simctl launch must reach the app as SIMCTL_CHILD_FOO, and the run's
// commands trail must say so. Before this, mav routed the line to the driver
// and dropped the variable without a word, so the app started, the agent
// believed its variable had arrived, and read the wrong behaviour as an app
// bug.
func TestOpenCarriesTheRecipeEnvPrefixIntoTheApp(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		AppPath: "make ios-app-path",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
		Launch:  `FOO=bar RUN=$MAV_BUNDLE_ID xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"make ios-app-path": {Stdout: "/tmp/App.app\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"open"}); err != nil {
		t.Fatal(err)
	}
	launch := ""
	for _, command := range runner.commands {
		if strings.Contains(command, "simctl launch") {
			launch = command
		}
	}
	if launch == "" {
		t.Fatalf("no launch ran: %v", runner.commands)
	}
	for _, want := range []string{"/usr/bin/env", "SIMCTL_CHILD_FOO=bar", "SIMCTL_CHILD_RUN=com.example.app"} {
		if !strings.Contains(launch, want) {
			t.Fatalf("launch=%q missing %q", launch, want)
		}
	}
	// A variable that reached the app but left no trace in the evidence is
	// half the bug: the trail is where anyone checks what the run actually
	// did. Values stay out of it, a recipe can carry a token.
	trail := readCommandsTrail(t, root)
	if !strings.Contains(trail, "launch.launch driver=simctl env=FOO,RUN") {
		t.Fatalf("the trail must name the variables that were passed:\n%s", trail)
	}
	if strings.Contains(trail, "bar") {
		t.Fatalf("values must not be written to the evidence:\n%s", trail)
	}
}

func readCommandsTrail(t *testing.T, root string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, ".mav", "runs", "*", "commands.jsonl"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no commands trail was written: %v", err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
