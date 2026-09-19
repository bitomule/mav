package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.19.1 made an unrecognised key in .mav/config.yaml fail the load, and
// not one line of output ever said so. Every caller flattened any load
// error into `fail code=config_not_found next="mav setup"`, and `mav
// doctor` discarded it outright and answered `ok`.
//
// Measured on the released 0.19.1 with a config carrying a legacy `tools:`
// section: `mav doctor` → `ok ... launch_recipe=missing` and no udid;
// `mav ui tree` → `fail code=config_not_found next="mav setup"`. Both of
// those are worse than the silence they replaced: the first is a green
// that drives nothing, and the second sends you to a command that rewrites
// the file you were one line from fixing.

const configWithUnknownKey = `project_name: Demo
target_kind: simulator
bundle_id: com.example.demo
simulator_udid: SIM-1
launch:
  mode: custom
  commands:
    launch: "echo launch"
tools:
  axe: true
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ConfigFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runMav(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	runner := fakeRunner{out: map[string]string{
		"xcrun simctl list devices booted -j": `{"devices":{}}`,
	}}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	err := cli.Run(context.Background(), args)
	return out.String(), err
}

// The load error's own code has to reach the output of every command that
// refuses to run on it -- not `config_not_found`, which is a different
// fact with a remediation that rewrites the file.
func TestUnknownKeyReachesTheFailLine(t *testing.T) {
	root := writeConfig(t, configWithUnknownKey)
	for _, args := range [][]string{
		{"ui", "tree"},
		{"open"},
		{"crashes"},
		{"network", "start"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			got, err := runMav(t, root, args...)
			if err == nil {
				t.Fatalf("an unloadable config must fail the command, got %q", got)
			}
			if !strings.Contains(got, "code=config_unknown_key") {
				t.Fatalf("the load error's own code must reach the output, got %q", got)
			}
			if !strings.Contains(got, "key=tools") {
				t.Fatalf("the failure must name the key, got %q", got)
			}
			if strings.Contains(got, "config_not_found") {
				t.Fatalf("a config that exists must not be reported as missing, got %q", got)
			}
		})
	}
}

// The contradiction to eliminate: with this config there must be no way to
// get an `ok` out of mav. doctor kept printing its diagnosis -- correctly,
// that is its job -- but printed it under `ok` beside `launch_recipe=missing`.
func TestDoctorCannotReportOKOnAnUnloadableConfig(t *testing.T) {
	root := writeConfig(t, configWithUnknownKey)
	got, err := runMav(t, root, "doctor")
	if err == nil {
		t.Fatalf("doctor must not succeed on a config it could not load, got %q", got)
	}
	if !strings.HasPrefix(got, "fail code=config_unknown_key ") {
		t.Fatalf("doctor must fail with the load error's code, got %q", got)
	}
	if !strings.Contains(got, "key=tools") {
		t.Fatalf("doctor must name the key, got %q", got)
	}
	// The diagnosis survives the failure: doctor is the command you reach
	// for because something is broken.
	if !strings.Contains(got, "mav_version=") {
		t.Fatalf("doctor must still report the diagnosis, got %q", got)
	}
}

// The control that keeps `mav doctor` usable before `mav setup`: a
// directory with no config at all is not a broken config, and still
// reports ok.
func TestDoctorStillReportsOKWithNoConfigAtAll(t *testing.T) {
	got, err := runMav(t, t.TempDir(), "doctor")
	if err != nil {
		t.Fatalf("doctor before `mav setup` must still work: %v (%q)", err, got)
	}
	if !strings.HasPrefix(got, "ok cmd=doctor ") {
		t.Fatalf("got %q, want an ok doctor line", got)
	}
}

// A file that is genuinely missing keeps its own code and its own
// remediation, for the commands that do need a config.
func TestMissingConfigStillReportsConfigNotFound(t *testing.T) {
	got, err := runMav(t, t.TempDir(), "ui", "tree")
	if err == nil {
		t.Fatalf("got %q, want a failure", got)
	}
	if !strings.Contains(got, "code=config_not_found") {
		t.Fatalf("got %q, want code=config_not_found", got)
	}
}

// Every other reason a load can fail travels the same way. These were all
// reported as `config_not_found` before, which is how a typo'd profile
// name sent people to `mav setup`.
func TestEveryLoadFailureCarriesItsOwnCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{"unknown key in a profile", "project_name: Demo\nprofiles:\n  mac:\n    fixture: x\n", "profile_unknown_key"},
		{"default_profile that does not exist", "project_name: Demo\ndefault_profile: nope\nprofiles:\n  mac:\n    target_kind: macos\n", "profile_not_found"},
		{"target_kind that does not exist", "project_name: Demo\ntarget_kind: watch\n", "target_kind_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeConfig(t, tc.body)
			got, err := runMav(t, root, "ui", "tree")
			if err == nil {
				t.Fatalf("got %q, want a failure", got)
			}
			if !strings.Contains(got, "code="+tc.code) {
				t.Fatalf("got %q, want code=%s", got, tc.code)
			}
		})
	}
}
