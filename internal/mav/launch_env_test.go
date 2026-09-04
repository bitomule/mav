package mav

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitEnvPrefixReadsTheAssignmentsAShellWould(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []envAssignment
		rest    string
	}{
		{
			name:    "the reported case",
			command: `FOO=bar xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
			want:    []envAssignment{{Name: "FOO", Value: "bar"}},
			rest:    `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
		},
		{
			name:    "several, in order",
			command: `A=1 B=2 xcrun simctl launch x y`,
			want:    []envAssignment{{Name: "A", Value: "1"}, {Name: "B", Value: "2"}},
			rest:    `xcrun simctl launch x y`,
		},
		{
			name:    "a quoted value with spaces",
			command: `MSG="hello world" xcrun simctl launch x y`,
			want:    []envAssignment{{Name: "MSG", Value: "hello world"}},
			rest:    `xcrun simctl launch x y`,
		},
		{
			name:    "an empty value",
			command: `FLAG= xcrun simctl launch x y`,
			want:    []envAssignment{{Name: "FLAG", Value: ""}},
			rest:    `xcrun simctl launch x y`,
		},
		{
			name:    "no prefix at all",
			command: `xcrun simctl launch x y`,
			want:    nil,
			rest:    `xcrun simctl launch x y`,
		},
		{
			// A later `--arg=value` is part of the command, not a prefix:
			// the scan stops at the first token that is not an assignment.
			name:    "an assignment-looking argument after the command",
			command: `xcrun simctl launch x y --arg=value`,
			want:    nil,
			rest:    `xcrun simctl launch x y --arg=value`,
		},
		{
			name:    "a name that is not a shell identifier",
			command: `not-a-name=1 xcrun simctl launch x y`,
			want:    nil,
			rest:    `not-a-name=1 xcrun simctl launch x y`,
		},
		{
			name:    "the prefix alone",
			command: `FOO=bar`,
			want:    []envAssignment{{Name: "FOO", Value: "bar"}},
			rest:    ``,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, rest, ok := splitEnvPrefix(tc.command)
			if !ok {
				t.Fatal("a well-formed command must tokenize")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("assignments=%v want=%v", got, tc.want)
			}
			if rest != tc.rest {
				t.Fatalf("rest=%q want=%q", rest, tc.rest)
			}
		})
	}
}

// An unbalanced quote is not mav's to interpret: it keeps the command whole
// and the shell path reports the syntax error the author actually made.
func TestSplitEnvPrefixLeavesAnUnparseableCommandAlone(t *testing.T) {
	command := `FOO="bar xcrun simctl launch x y`
	assignments, rest, ok := splitEnvPrefix(command)
	if ok {
		t.Fatal("an unbalanced quote must not be parsed as a prefix")
	}
	if assignments != nil || rest != command {
		t.Fatalf("assignments=%v rest=%q", assignments, rest)
	}
}

// A value referring to a MAV variable would otherwise arrive at the app as
// the literal string, which is the same silent lie in a smaller costume.
func TestExpandEnvAssignmentsResolvesMAVVariables(t *testing.T) {
	got := expandEnvAssignments(
		[]envAssignment{
			{Name: "DIR", Value: "$MAV_RUN_DIR/out"},
			{Name: "ID", Value: "${MAV_BUNDLE_ID}"},
			{Name: "PLAIN", Value: "bar"},
		},
		map[string]string{"MAV_RUN_DIR": "/runs/1", "MAV_BUNDLE_ID": "com.example.app"},
	)
	want := map[string]string{"DIR": "/runs/1/out", "ID": "com.example.app", "PLAIN": "bar"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestJoinEnvPrefixSurvivesARewrite(t *testing.T) {
	got := joinEnvPrefix(
		[]envAssignment{{Name: "MSG", Value: "hello world"}},
		`idb launch --udid "$MAV_UDID" -f "$MAV_BUNDLE_ID"`,
	)
	want := `MSG='hello world' idb launch --udid "$MAV_UDID" -f "$MAV_BUNDLE_ID"`
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

// The prefix must not change which route the launch takes: it is carried
// into the app by the driver, not a reason to hand the line to a shell that
// would set the variable on simctl instead of on the app.
func TestDriverLaunchRoutingIgnoresTheEnvPrefix(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.BundleID = "com.example.app"
	commands := LaunchCommands{Launch: `FOO=bar xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`}
	if !shouldUseDriverLaunch(cfg, commands) {
		t.Fatal("a prefixed simctl launch must still route to the driver")
	}
}

// The device install rewrite must keep the author's own prefix bytes, not
// shellQuote's re-encoding of them: the rewritten line runs in a shell
// (shouldUseDriverInstall sends any prefixed install there), and a value
// referring to a MAV variable needs to still expand there.
func TestEffectiveLaunchCommandsPreservesExpansionInDeviceInstallPrefix(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.Launch.Commands = LaunchCommands{
		Install: `FOO=$MAV_APP_PATH xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
	}
	commands := effectiveLaunchCommands(cfg)
	if !strings.HasPrefix(commands.Install, `FOO=$MAV_APP_PATH `) {
		t.Fatalf("install=%q: prefix must stay unquoted to expand", commands.Install)
	}
	if strings.Contains(commands.Install, `'$MAV_APP_PATH'`) {
		t.Fatalf("install=%q: prefix must not be single-quoted", commands.Install)
	}
}

// Install is the other way round: its variables are for the install tool,
// not for the app, so the shell is where they mean what they say.
// A line that cannot be tokenized (an unbalanced quote) must never be routed
// to the driver with the prefix silently dropped: it goes to the shell, where
// the author sees the syntax error they actually made.
func TestUnparseableEnvPrefixGoesToTheShell(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.BundleID = "com.example.app"
	commands := LaunchCommands{Launch: `FOO="bar xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`}
	if shouldUseDriverLaunch(cfg, commands) {
		t.Fatal("an unparseable launch line must not route to the driver")
	}
}

func TestPrefixedInstallGoesToTheShell(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	commands := LaunchCommands{Install: `FOO=bar xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`}
	if shouldUseDriverInstall(cfg, commands) {
		t.Fatal("a prefixed install must run in the shell that honours the prefix")
	}
}
