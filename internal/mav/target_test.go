package mav

import (
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func TestTargetKindLabel(t *testing.T) {
	if got := targetKindLabel(drivers.KindSim); got != "simulator" {
		t.Fatalf("targetKindLabel(KindSim) = %q, want %q", got, "simulator")
	}
	if got := targetKindLabel(drivers.KindDevice); got != "device" {
		t.Fatalf("targetKindLabel(KindDevice) = %q, want %q", got, "device")
	}
}
