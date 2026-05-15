package drivers

import "testing"

func TestCapabilitySetHasAndAdd(t *testing.T) {
	set := NewSet(CapTap, CapScreenshot)
	if !set.Has(CapTap) {
		t.Fatalf("expected CapTap in set")
	}
	if set.Has(CapPinch) {
		t.Fatalf("did not expect CapPinch in set")
	}
	set.Add(CapPinch)
	if !set.Has(CapPinch) {
		t.Fatalf("Add did not insert CapPinch")
	}
}
