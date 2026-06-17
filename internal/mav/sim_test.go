package mav

import "testing"

func TestSelectSimulator(t *testing.T) {
	sims := []Simulator{
		{UDID: "1", Name: "iPhone 15", Runtime: "iOS-26-0", State: "Shutdown"},
		{UDID: "2", Name: "iPhone 17 Pro Max", Runtime: "iOS-26-2", State: "Booted"},
		{UDID: "3", Name: "iPhone 17 Pro", Runtime: "com.apple.CoreSimulator.SimRuntime.iOS-26-3", State: "Shutdown"},
	}
	got, ok := SelectSimulator(sims, "17", "26-2", "")
	if !ok || got.UDID != "2" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	got, ok = SelectSimulator(sims, "17 Pro", "26.3", "")
	if !ok || got.UDID != "3" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	got, ok = SelectSimulator(sims, "", "", "1")
	if !ok || got.UDID != "1" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}
