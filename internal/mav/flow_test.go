package mav

import (
	"testing"
	"time"
)

func TestParseFlowYAML(t *testing.T) {
	flow, err := ParseFlow([]byte(`
version: 1
name: verify_daily_reminder
steps:
  - open: {}
  - go: { screen: settings }
  - wait: { text: Daily Reminder, timeout: 5s }
  - waitUntil:
      any:
        - text: "Don’t Allow"
        - text: "Allow"
        - changedFrom: before-toggle
      timeout: 5s
  - evidence.step: { name: before-toggle, note: Daily Reminder before tap }
  - delay: { duration: 2s }
  - exec: { cmd: "rg UniqueMarker $MAV_LOGS", contains: UniqueMarker, timeout: 5s }
  - scrollUntil: { text: Privacy Policy, direction: up, maxSwipes: 3 }
`))
	if err != nil {
		t.Fatal(err)
	}
	if flow.Name != "verify_daily_reminder" || len(flow.Steps) != 8 {
		t.Fatalf("flow=%+v", flow)
	}
	if flow.Steps[1].Action != "go" || flow.Steps[1].Params["screen"] != "settings" {
		t.Fatalf("go step=%+v", flow.Steps[1])
	}
	if len(flow.Steps[3].Any) != 3 || flow.Steps[3].Any[2].ChangedFrom != "before-toggle" {
		t.Fatalf("waitUntil=%+v", flow.Steps[3])
	}
	if flow.Steps[5].Action != "delay" || flow.Steps[5].Params["duration"] != "2s" {
		t.Fatalf("delay=%+v", flow.Steps[5])
	}
	if flow.Steps[6].Action != "exec" || flow.Steps[6].Params["cmd"] == "" || flow.Steps[6].Params["contains"] != "UniqueMarker" {
		t.Fatalf("exec=%+v", flow.Steps[6])
	}
	if flow.Steps[7].Action != "scrollUntil" || flow.Steps[7].Params["maxSwipes"] != "3" {
		t.Fatalf("scrollUntil=%+v", flow.Steps[7])
	}
}

func TestParseFlowRejectsUnknownVersion(t *testing.T) {
	_, err := ParseFlow([]byte("version: 2\nsteps:\n  - open: {}\n"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFlowDuration(t *testing.T) {
	if got := parseFlowDuration("5s", 0); got != 5*time.Second {
		t.Fatalf("got %s", got)
	}
	if got := parseFlowDuration("1000", 0); got != time.Second {
		t.Fatalf("got %s", got)
	}
}
