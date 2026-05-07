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
  - video.start: {}
  - evidence.step: { name: before-toggle, note: Daily Reminder before tap }
  - delay: { duration: 2s }
  - video.stop: { note: Done }
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
	if flow.Name != "verify_daily_reminder" || len(flow.Steps) != 14 {
		t.Fatalf("flow=%+v", flow)
	}
	if flow.Steps[1].Action != "go" || flow.Steps[1].Params["screen"] != "settings" {
		t.Fatalf("go step=%+v", flow.Steps[1])
	}
	if len(flow.Steps[3].Any) != 3 || flow.Steps[3].Any[2].ChangedFrom != "before-toggle" {
		t.Fatalf("waitUntil=%+v", flow.Steps[3])
	}
	if flow.Steps[4].Action != "video.start" {
		t.Fatalf("video.start=%+v", flow.Steps[4])
	}
	if flow.Steps[6].Action != "delay" || flow.Steps[6].Params["duration"] != "2s" {
		t.Fatalf("delay=%+v", flow.Steps[6])
	}
	if flow.Steps[7].Action != "video.stop" || flow.Steps[7].Params["note"] != "Done" {
		t.Fatalf("video.stop=%+v", flow.Steps[7])
	}
	if flow.Steps[8].Action != "logs" || flow.Steps[8].Params["key"] != "SettingsReached" {
		t.Fatalf("logs=%+v", flow.Steps[8])
	}
	if flow.Steps[9].Action != "scrollUntil" || flow.Steps[9].Params["maxSwipes"] != "3" {
		t.Fatalf("scrollUntil=%+v", flow.Steps[9])
	}
	if flow.Steps[10].Action != "pinch" || flow.Steps[10].Params["scale"] != "0.5" || flow.Steps[10].Params["panX"] != "80" || flow.Steps[10].Params["hold"] != "2s" {
		t.Fatalf("pinch=%+v", flow.Steps[10])
	}
	if flow.Steps[11].Action != "rotate" || flow.Steps[11].Params["degrees"] != "30" {
		t.Fatalf("rotate=%+v", flow.Steps[11])
	}
	if flow.Steps[12].Action != "twoFingerPan" || flow.Steps[12].Params["panY"] != "10" {
		t.Fatalf("twoFingerPan=%+v", flow.Steps[12])
	}
	if flow.Steps[13].Action != "actions" || flow.Steps[13].Params["file"] != ".mav/actions/map-zoom.json" {
		t.Fatalf("actions=%+v", flow.Steps[13])
	}
}

func TestParseFlowRejectsUnknownVersion(t *testing.T) {
	_, err := ParseFlow([]byte("version: 2\nsteps:\n  - open: {}\n"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFlowAcceptsScalarTypeAndDelay(t *testing.T) {
	flow, err := ParseFlow([]byte(`
steps:
  - type: "Basketball shoes"
  - type: "  padded text  "
  - delay: 2s
  - sleep: 500ms
  - delay: "  750ms  "
  - sleep: "  250ms  "
  - type: { text: "Legacy shape still works" }
  - delay: { duration: 1s }
`))
	if err != nil {
		t.Fatal(err)
	}
	if flow.Steps[0].Action != "type" || flow.Steps[0].Params["text"] != "Basketball shoes" {
		t.Fatalf("type=%+v", flow.Steps[0])
	}
	if flow.Steps[1].Action != "type" || flow.Steps[1].Params["text"] != "  padded text  " {
		t.Fatalf("padded type=%+v", flow.Steps[1])
	}
	if flow.Steps[2].Action != "delay" || flow.Steps[2].Params["duration"] != "2s" {
		t.Fatalf("delay=%+v", flow.Steps[2])
	}
	if flow.Steps[3].Action != "sleep" || flow.Steps[3].Params["duration"] != "500ms" {
		t.Fatalf("sleep=%+v", flow.Steps[3])
	}
	if flow.Steps[4].Action != "delay" || flow.Steps[4].Params["duration"] != "750ms" {
		t.Fatalf("padded delay=%+v", flow.Steps[4])
	}
	if flow.Steps[5].Action != "sleep" || flow.Steps[5].Params["duration"] != "250ms" {
		t.Fatalf("padded sleep=%+v", flow.Steps[5])
	}
	if flow.Steps[6].Params["text"] != "Legacy shape still works" || flow.Steps[7].Params["duration"] != "1s" {
		t.Fatalf("legacy steps=%+v %+v", flow.Steps[6], flow.Steps[7])
	}
}

func TestParseFlowRejectsEmptyScalarTypeAndDelay(t *testing.T) {
	for _, data := range []string{
		"steps:\n  - type: \"   \"\n",
		"steps:\n  - delay: \"   \"\n",
		"steps:\n  - sleep: \"   \"\n",
	} {
		if _, err := ParseFlow([]byte(data)); err == nil {
			t.Fatalf("expected error for %q", data)
		}
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
