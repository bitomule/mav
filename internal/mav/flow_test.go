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
  - logs: { key: SettingsReached }
  - scrollUntil: { text: Privacy Policy, direction: up, maxSwipes: 3 }
  - pinch: { x: 200, y: 450, scale: 0.5, panX: 80, panY: -40, duration: 800ms, hold: 2s }
  - rotate: { x: 200, y: 450, degrees: 30 }
  - twoFingerPan: { x: 200, y: 450, panX: 20, panY: 10 }
  - actions: { file: .mav/actions/map-zoom.json }
`))
	if err != nil {
		t.Fatal(err)
	}
	if flow.Name != "verify_daily_reminder" || len(flow.Steps) != 12 {
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
	if flow.Steps[6].Action != "logs" || flow.Steps[6].Params["key"] != "SettingsReached" {
		t.Fatalf("logs=%+v", flow.Steps[6])
	}
	if flow.Steps[7].Action != "scrollUntil" || flow.Steps[7].Params["maxSwipes"] != "3" {
		t.Fatalf("scrollUntil=%+v", flow.Steps[7])
	}
	if flow.Steps[8].Action != "pinch" || flow.Steps[8].Params["scale"] != "0.5" || flow.Steps[8].Params["panX"] != "80" || flow.Steps[8].Params["hold"] != "2s" {
		t.Fatalf("pinch=%+v", flow.Steps[8])
	}
	if flow.Steps[9].Action != "rotate" || flow.Steps[9].Params["degrees"] != "30" {
		t.Fatalf("rotate=%+v", flow.Steps[9])
	}
	if flow.Steps[10].Action != "twoFingerPan" || flow.Steps[10].Params["panY"] != "10" {
		t.Fatalf("twoFingerPan=%+v", flow.Steps[10])
	}
	if flow.Steps[11].Action != "actions" || flow.Steps[11].Params["file"] != ".mav/actions/map-zoom.json" {
		t.Fatalf("actions=%+v", flow.Steps[11])
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
