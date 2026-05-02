package mav

import "testing"

func TestSelectSimulator(t *testing.T) {
	sims := []Simulator{
		{UDID: "1", Name: "iPhone 15", Runtime: "iOS-26-0", State: "Shutdown"},
		{UDID: "2", Name: "iPhone 17 Pro Max", Runtime: "iOS-26-2", State: "Booted"},
	}
	got, ok := SelectSimulator(sims, "17", "26-2", "")
	if !ok || got.UDID != "2" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	got, ok = SelectSimulator(sims, "", "", "1")
	if !ok || got.UDID != "1" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}
