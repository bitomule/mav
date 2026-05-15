package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Golden tests pin the user-visible output of mav subcommands so that the driver
// layer refactor (and every future change) cannot silently alter agent-facing
// shapes. Each test runs a CLI invocation against a deterministic fakeRunner,
// captures stdout, normalises volatile fields (timestamps, run IDs, temp paths),
// and diffs against a checked-in fixture under testdata/golden/.
//
// Run with UPDATE_GOLDEN=1 to regenerate fixtures. Always inspect the diff
// before committing regenerated fixtures.

// goldenCase is a single golden invocation.
type goldenCase struct {
	// name identifies the fixture file: testdata/golden/<name>.golden.txt.
	name string
	// args is the argv passed to cli.Run (without the leading "mav").
	args []string
	// tools lists the tool names that the fakeRunner should pretend are on PATH.
	tools []string
	// out is the canned stdout per "<name> <arg1> <arg2>" key, like fakeRunner.
	out map[string]string
	// seq is the canned sequence per key (one entry returned per call), like fakeRunner.
	seq map[string][]string
	// stdin is the stdin the CLI sees (for interactive prompts).
	stdin string
}

// runGolden executes the case and asserts stdout (normalised) matches the fixture.
func runGolden(t *testing.T, tc goldenCase) {
	t.Helper()
	root := t.TempDir()
	runner := fakeRunner{
		tools: toolSet(tc.tools),
		out:   tc.out,
		seq:   tc.seq,
		calls: map[string]int{},
	}
	var stdout, stderr bytes.Buffer
	cli := CLI{
		Runner: runner,
		Stdin:  strings.NewReader(tc.stdin),
		Stdout: &stdout,
		Stderr: &stderr,
		Root:   root,
	}
	_ = cli.Run(context.Background(), tc.args)

	got := normaliseGolden(stdout.String(), root)
	fixture := filepath.Join("testdata", "golden", tc.name+".golden.txt")

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err != nil {
			t.Fatalf("mkdir fixture dir: %v", err)
		}
		if err := os.WriteFile(fixture, []byte(got), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return
	}

	want, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture %s: %v (run UPDATE_GOLDEN=1 to create)", fixture, err)
	}
	if string(want) != got {
		t.Errorf("golden mismatch for %s\nwant:\n%s\n\ngot:\n%s", tc.name, string(want), got)
	}
}

func toolSet(tools []string) map[string]bool {
	if len(tools) == 0 {
		return map[string]bool{}
	}
	set := make(map[string]bool, len(tools))
	for _, name := range tools {
		set[name] = true
	}
	return set
}

// normaliseGolden masks volatile fields so the fixture stays stable across runs.
// The transformations are intentionally conservative: only well-known volatile
// patterns are replaced so that real behavioural drift is still caught.
var (
	rxRFC3339 = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:[.+-]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\b`)
	rxRunID   = regexp.MustCompile(`/mav/[0-9a-f]{8}\b`)
	rxTempDir = regexp.MustCompile(`/var/folders/[^ "/\n]+`)
)

func normaliseGolden(s, root string) string {
	if root != "" {
		s = strings.ReplaceAll(s, root, "<ROOT>")
	}
	s = rxTempDir.ReplaceAllString(s, "<TMP>")
	s = rxRunID.ReplaceAllString(s, "/mav/<RUNID>")
	s = rxRFC3339.ReplaceAllString(s, "<TS>")
	return s
}

// --- cases ---------------------------------------------------------------

func TestGolden_UnknownCommand(t *testing.T) {
	runGolden(t, goldenCase{
		name: "unknown_command",
		args: []string{"definitely-not-a-command"},
	})
}

func TestGolden_HelpRoot(t *testing.T) {
	runGolden(t, goldenCase{
		name: "help_root",
		args: []string{"help"},
	})
}

func TestGolden_HelpUi(t *testing.T) {
	runGolden(t, goldenCase{
		name: "help_ui",
		args: []string{"help", "ui"},
	})
}

func TestGolden_DoctorNoTools(t *testing.T) {
	runGolden(t, goldenCase{
		name: "doctor_no_tools",
		args: []string{"doctor"},
	})
}

func TestGolden_DoctorAxeAndIdb(t *testing.T) {
	runGolden(t, goldenCase{
		name:  "doctor_axe_idb",
		args:  []string{"doctor"},
		tools: []string{"xcrun", "axe", "idb"},
	})
}
