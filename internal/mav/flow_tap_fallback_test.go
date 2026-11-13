package mav

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowDoesNotReplayAnUncertainAXe2SelectorInput(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "required", true: "optional"}[optional], func(t *testing.T) {
			cli, runner, out := failingAXeSelectorCLI(t)
			path := filepath.Join(cli.Root, "flow.yaml")
			flow := "steps:\n  - tap: {text: Continue, onFailure: {strategy: retry, maxAttempts: 3}}\n"
			if optional {
				flow = "steps:\n  - tap: {text: Continue, optional: true, onFailure: {strategy: retry, maxAttempts: 3}}\n"
			}
			if err := os.WriteFile(path, []byte(flow), 0600); err != nil {
				t.Fatal(err)
			}
			if err := cli.Run(context.Background(), []string{"run", path}); err == nil {
				t.Fatal("uncertain input reported a passed flow")
			}
			assertNoCoordinateReplay(t, runner)
			run, err := LoadRun(cli.Root, "")
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(run.Dir, "run.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `"status": "failed"`) {
				t.Fatalf("missing failed-run evidence: %s output=%s", data, out.String())
			}
		})
	}
}
