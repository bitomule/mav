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
