package mav

import "testing"

// A gesture that reports success and does nothing is the failure these tests
// exist for. Measured on 2026-09-19, iPhone 17 Pro / iOS 26.3 on a simpool
// slot: `axe tap -x 364 -y 84` printed "✓ Tap at (364.0, 84.0) completed
// successfully" with the tree byte-identical either side, on two different
// targets, and `mav ui swipe` did the same. The verification is what turns
// that into `verified=unchanged`, so the verification must not itself be
// fooled — which is what the first two tests are about.

func stillScreen() []Element {
	return []Element{
		{ID: "WIFI", Label: "Wi-Fi", Role: "cell", Frame: "{{16, 260}, {370, 44}}"},
		{ID: "CLOCK", Label: "Hora", Role: "text", Value: "21:41", Frame: "{{16, 320}, {100, 20}}"},
		{Label: "Guardar", Role: "button", Frame: "{{16, 400}, {80, 44}}"},
	}
}

func TestAStillScreenKeepsItsFingerprintWhenCoordinatesDrift(t *testing.T) {
	// Two reads of one unchanged screen disagree on frame by fractions of a
	// point. A verification that noticed would report `changed` after a
	// gesture that did nothing, which is the exact lie it is meant to catch.
	before := stillScreen()
	after := stillScreen()
	after[0].Frame = "{{16.000000000000004, 260.33333333333331}, {370, 44}}"
	after[2].Frame = "{{16, 400.00000000000006}, {80, 44}}"

	if screenFingerprint(before) != screenFingerprint(after) {
		t.Fatal("frame drift changed the fingerprint; a no-op gesture would report changed")
	}
}

func TestAClockTickingIsNotAScreenChange(t *testing.T) {
	before := stillScreen()
	after := stillScreen()
	after[1].Value = "21:42"

	if screenFingerprint(before) != screenFingerprint(after) {
		t.Fatal("a value that moves on its own changed the fingerprint")
	}
}

func TestANewScreenChangesTheFingerprint(t *testing.T) {
	// The control: without this, a fingerprint that ignored everything would
	// pass the two tests above and detect nothing at all.
	before := stillScreen()
	after := []Element{
		{ID: "CAMERA_TITLE", Label: "Cámara", Role: "heading"},
		{ID: "BackButton", Label: "Atrás", Role: "back button"},
	}
	if screenFingerprint(before) == screenFingerprint(after) {
		t.Fatal("a different screen kept the same fingerprint")
	}
}

func TestReorderingTheSameElementsIsNotAChange(t *testing.T) {
	// The tree's order is deterministic today, but sorting costs nothing and
	// removes the dependency.
	before := stillScreen()
	after := []Element{before[2], before[0], before[1]}
	if screenFingerprint(before) != screenFingerprint(after) {
		t.Fatal("the same elements in another order changed the fingerprint")
	}
}

func TestNodeCountIsNotEnoughToSeeAScreenChange(t *testing.T) {
	// Measured: 80 nodes before a tap, 80 after, and the screen had changed
	// entirely. Counting is the instrument this one replaces.
	before := []Element{
		{ID: "A", Label: "Ajustes", Role: "heading"},
		{ID: "B", Label: "Wi-Fi", Role: "cell"},
	}
	after := []Element{
		{ID: "C", Label: "Cámara", Role: "heading"},
		{ID: "D", Label: "Formatos", Role: "cell"},
	}
	if len(before) != len(after) {
		t.Fatal("this test needs both screens to carry the same number of elements")
	}
	if screenFingerprint(before) == screenFingerprint(after) {
		t.Fatal("two different screens of equal size share a fingerprint")
	}
}
