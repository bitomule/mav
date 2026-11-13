package mav

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type goalRetirementRunner struct{ calls int }

func (r *goalRetirementRunner) LookPath(string) (string, error) {
	r.calls++
	return "", errors.New("must not inspect tools")
}
func (r *goalRetirementRunner) Run(context.Context, string, ...string) CommandResult {
	r.calls++
	return CommandResult{Err: errors.New("must not invoke tools")}
}
func (r *goalRetirementRunner) Start(context.Context, string, string, ...string) (int, error) {
	r.calls++
	return 0, errors.New("must not start input")
}

func TestRetiredGotoFailsBeforeConfigTargetModelOrInput(t *testing.T) {
	for _, args := range [][]string{{"goto", "Settings", "--arrived-when", `title:"Settings"`}, {"goto", "--help"}, {"goto"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".mav"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".mav/config.yaml"), []byte("invalid: ["), 0600); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			runner := &goalRetirementRunner{}
			c := CLI{Root: root, Runner: runner, Stdout: &out, Stderr: &out}
			err := c.Run(t.Context(), args)
			if err == nil || !strings.Contains(out.String(), "code=goto_removed") || !strings.Contains(out.String(), "mav goal") || runner.calls != 0 {
				t.Fatalf("err=%v calls=%d output=%s", err, runner.calls, out.String())
			}
		})
	}
}

func TestGotoMigrationCommandPreservesArgumentsAsShellLiterals(t *testing.T) {
	objective := "Open user's $(printf bad) settings"
	args := []string{objective, "--arrived-when", `title:"User's settings"`, "--max-steps", "6", "--timeout=45s", "--dismiss-permission", "Don't Allow"}
	command := gotoMigrationCommand(GlobalOptions{PreferDriver: "axe"}, args)
	if !strings.HasPrefix(command, "mav goal ") || strings.Contains(command, "--arrived-when") {
		t.Fatal(command)
	}
	// Parse through a real shell, using printf rather than invoking MAV.
	parsed, err := exec.CommandContext(t.Context(), "sh", "-c", "set -- "+strings.TrimPrefix(command, "mav goal ")+`; printf '%s\n' "$@"`).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--mode", "navigation", objective, "--succeeded-when", `title:"User's settings"`, "--max-steps", "6", "--timeout=45s", "--dismiss-permission", "Don't Allow", "--prefer-driver", "axe"}
	if got := strings.Split(strings.TrimSuffix(string(parsed), "\n"), "\n"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%q want=%q command=%s", got, want, command)
	}
	inline := gotoMigrationCommand(GlobalOptions{}, []string{"Settings", `--arrived-when=title:"Settings"`})
	if !strings.Contains(inline, "--succeeded-when=title:") || strings.Contains(inline, "--arrived-when") {
		t.Fatal(inline)
	}
}

func TestFlowRetirementRejectsOldStepsAndPropertiesBeforeExecution(t *testing.T) {
	for _, body := range []string{
		`- goto: {goal: Settings, arrivedWhen: 'title:"Settings"'}`,
		`- goto: Settings`,
		`- goal: {goal: Settings, arrivedWhen: '', succeededWhen: 'title:"Settings"'}`,
		`- tap: {text: Next, arrivedWhen: ''}`,
		"- when: {visible: {id: next}}\n  do:\n    - goto: {goal: Settings, arrivedWhen: 'title:\"Settings\"'}",
	} {
		root := t.TempDir()
		path := filepath.Join(root, "flow.yaml")
		if err := os.WriteFile(path, []byte("name: migration\nsteps:\n"+body+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadFlow(path)
		if err == nil || !strings.Contains(err.Error(), "removed") || !strings.Contains(err.Error(), "succeededWhen") {
			t.Fatalf("old YAML accepted or unhelpful diagnostic: %s err=%v", body, err)
		}
	}
}

func TestFlowRetirementDiagnosesIncludedOldSteps(t *testing.T) {
	root := t.TempDir()
	writeTestFlow(t, filepath.Join(root, "main.yaml"), "steps:\n  - include: {file: old.yaml}\n")
	writeTestFlow(t, filepath.Join(root, "old.yaml"), "steps:\n  - goto: {goal: Settings, arrivedWhen: 'title:\"Settings\"'}\n")
	_, err := LoadFlow(filepath.Join(root, "main.yaml"))
	if err == nil || !strings.Contains(err.Error(), "goto_removed") || !strings.Contains(err.Error(), "succeededWhen") {
		t.Fatalf("included old command accepted or diagnostic lost: %v", err)
	}
}

func TestRetiredInternalFlowStepCannotReachConfigOrInput(t *testing.T) {
	runner := &goalRetirementRunner{}
	c := CLI{Root: t.TempDir(), Runner: runner}
	_, err := c.executeFlowStepWithOptions(t.Context(), GlobalOptions{}, RunState{}, 0, FlowStep{Action: "goto", Params: map[string]string{"goal": "Settings", "arrivedWhen": `title:"Settings"`}})
	if err == nil || !strings.Contains(err.Error(), "goto_removed") || runner.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, runner.calls)
	}
}
