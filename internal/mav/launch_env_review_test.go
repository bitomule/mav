package mav

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// One missing quote turns the whole launch line into a variable's value. The
// prefix scan then sees nothing left to run, and the driver clause for "no
// launch command" would launch the bundle and report success while the text
// the author wrote never ran — the same silence, one layer down.
func TestLaunchThatIsOnlyAssignmentsFailsLoudly(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Launch: `FOO="bar xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{tools: map[string]bool{"xcrun": true}}
	var out, errOut bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &errOut}
	if err := cli.Run(context.Background(), []string{"open"}); err == nil {
		t.Fatal("a launch line that reduces to assignments must fail")
	}
	if !strings.Contains(out.String()+errOut.String(), "launch_command_only_env") {
		t.Fatalf("output=%q", out.String()+errOut.String())
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "simctl launch") {
		t.Fatalf("nothing must be launched: %v", runner.commands)
	}
	trail := readCommandsTrail(t, root)
	if !strings.Contains(trail, "env_only") || !strings.Contains(trail, `"code":1`) {
		t.Fatalf("the failure must be in the evidence, with a non-zero code:\n%s", trail)
	}
	if strings.Contains(trail, "bar xcrun") {
		t.Fatalf("the assignment's value must not be written to the evidence:\n%s", trail)
	}
}

// mav has no shell on the driver path, so a command substitution would arrive
// at the app as its own literal text and the trail (names only) would show
// nothing. Refusing is the honest answer.
func TestLaunchEnvWithCommandSubstitutionIsRefused(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Launch: "STAMP=$(date) xcrun simctl launch \"$MAV_UDID\" \"$MAV_BUNDLE_ID\"",
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{tools: map[string]bool{"xcrun": true}}
	var out, errOut bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &errOut}
	if err := cli.Run(context.Background(), []string{"open"}); err == nil {
		t.Fatal("command substitution must be refused, not shipped literally")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "launch_env_command_substitution") || !strings.Contains(combined, "STAMP") {
		t.Fatalf("output=%q", combined)
	}
	trail := readCommandsTrail(t, root)
	if !strings.Contains(trail, "env_rejected") {
		t.Fatalf("the refusal must leave evidence:\n%s", trail)
	}
	if !strings.Contains(trail, `"code":1`) {
		t.Fatalf("a failure recorded with code 0 reads as a success:\n%s", trail)
	}
}

// A single-quoted value is delivered as written, the way the shell whose
// syntax this imitates would deliver it.
func TestSingleQuotedEnvValueIsNotExpanded(t *testing.T) {
	assignments, _, ok := splitEnvPrefix(`LIT='$MAV_RUN_DIR' xcrun simctl launch x y`)
	if !ok || len(assignments) != 1 {
		t.Fatalf("assignments=%v ok=%v", assignments, ok)
	}
	got := expandEnvAssignments(assignments, map[string]string{"MAV_RUN_DIR": "/runs/1"})
	if got["LIT"] != "$MAV_RUN_DIR" {
		t.Fatalf("LIT=%q: a single-quoted value must not expand", got["LIT"])
	}
}

// The evidence guarantee is the same on both routes: names yes, values never.
// A prefixed install deliberately goes to the shell, and its command is what
// the trail records verbatim.
func TestShellPathRedactsThePrefixValuesInTheTrail(t *testing.T) {
	got := redactEnvPrefix(`TOKEN=s3cret xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`)
	want := `TOKEN=<redacted> xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
	if plain := redactEnvPrefix(`xcrun simctl install a b`); plain != `xcrun simctl install a b` {
		t.Fatalf("a command with no prefix must be untouched: %q", plain)
	}
}
