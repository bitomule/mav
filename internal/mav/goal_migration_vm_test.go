package mav

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoalRetiredFlowDiagnosedBeforeVMAttachmentOrMatrix(t *testing.T) {
	t.Setenv("MAV_PROFILE", "")
	for _, matrix := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "matrix"}[matrix], func(t *testing.T) {
			root := t.TempDir()
			cfg := DefaultConfig(root)
			cfg.VM = true
			cfg.TargetKind = "macos"
			if err := SaveConfig(root, cfg); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "old.yaml")
			if err := os.WriteFile(path, []byte("steps:\n  - goto: {goal: Settings, arrivedWhen: 'title:Settings'}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			runner := &goalRetirementRunner{}
			var out bytes.Buffer
			c := CLI{Root: root, Runner: runner, Stdout: &out, Stderr: &out}
			args := []string{"run", path}
			if matrix {
				args = append(args, "--target", "kind=sim,udid=synthetic-device")
			}
			err := c.Run(t.Context(), args)
			if err == nil || !strings.Contains(out.String(), "code=flow_invalid") || !strings.Contains(out.String(), "goto_removed") || runner.calls != 0 {
				t.Fatalf("retired flow reached setup before its migration diagnostic: err=%v calls=%d output=%s", err, runner.calls, out.String())
			}
		})
	}
}
