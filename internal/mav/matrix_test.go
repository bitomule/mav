package mav

import (
	"reflect"
	"testing"
)

func TestStripMatrixFlagsPreservesFlowParams(t *testing.T) {
	got := stripMatrixFlags([]string{"flow.yaml", "--target", "SIM-1", "--param", "category=Travel", "--target=SIM-2", "--jobs", "2"})
	want := []string{"flow.yaml", "--param", "category=Travel"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

// TestStripMatrixFlagsDropsRun guards against every matrix child adopting
// the same run.ID: each child already gets a unique run directory through
// MAV_EXACT_RUN_DIR, so a forwarded --run would make N distinct runs on
// disk report the same id in matrix.json / simulator locks / report.html.
func TestStripMatrixFlagsDropsRun(t *testing.T) {
	got := stripMatrixFlags([]string{"flow.yaml", "--run", "abc123", "--param", "category=Travel", "--run=def456"})
	want := []string{"flow.yaml", "--param", "category=Travel"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

// The matrix builds once for every target before fanning out, so a child
// that rebuilt would pay for that build again per target -- the exact cost
// --skip-build exists to remove.
func TestMatrixChildArgsSkipTheBuildTheParentAlreadyRan(t *testing.T) {
	got := matrixChildArgs([]string{"flow.yaml", "--target", "SIM-1", "--param", "category=Travel"})
	want := []string{"run", "flow.yaml", "--param", "category=Travel", "--skip-build"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestMatrixChildArgsDoesNotRepeatSkipBuild(t *testing.T) {
	got := matrixChildArgs([]string{"flow.yaml", "--skip-build", "--target", "SIM-1"})
	want := []string{"run", "flow.yaml", "--skip-build"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestApplyMatrixTarget(t *testing.T) {
	cfg := Config{}
	applyMatrixTarget(&cfg, matrixTarget{Kind: "simulator", UDID: "SIM-1", Name: "iPhone", Runtime: "iOS-26-3"})
	if cfg.SimulatorUDID != "SIM-1" || cfg.SimulatorName != "iPhone" || cfg.SimulatorRuntime != "iOS-26-3" {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestMatrixTreatsStructuredFailureAsFailedExit(t *testing.T) {
	if !matrixOutputFailed("fail code=wait_timeout step=2\n") {
		t.Fatal("structured child failure must fail matrix even when child process exits zero")
	}
	if matrixOutputFailed("ok cmd=run steps=2\n") {
		t.Fatal("successful child output marked failed")
	}
}
