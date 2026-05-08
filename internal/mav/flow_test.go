package mav

import (
	"os"
	"path/filepath"
	"strings"
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
  - scrollUntil: { text: Privacy Policy, direction: up, maxSwipes: 3, prefer-driver: appium }
  - pinch: { x: 200, y: 450, scale: 0.5, panX: 80, panY: -40, duration: 800ms, hold: 2s }
  - rotate: { x: 200, y: 450, degrees: 30 }
  - twoFingerPan: { x: 200, y: 450, panX: 20, panY: 10 }
  - actions: { file: .mav/actions/map-zoom.json }
  - open: { clearState: true }
  - open: { clear-state: true }
  - erase: { value: "Email", focused: true, prefer-driver: appium }
  - hideKeyboard: {}
  - whileNotVisible:
      text: "You"
      timeout: 30s
      prefer-driver: appium
      do:
        - tap: { id: dismiss_button, optional: true }
        - delay: 500ms
`))
	if err != nil {
		t.Fatal(err)
	}
	if flow.Name != "verify_daily_reminder" || len(flow.Steps) != 19 {
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
	if flow.Steps[9].Action != "scrollUntil" || flow.Steps[9].Params["maxSwipes"] != "3" || flow.Steps[9].Params["prefer-driver"] != "appium" {
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
	if flow.Steps[14].Action != "open" || flow.Steps[14].Params["clearState"] != "true" {
		t.Fatalf("open clearState=%+v", flow.Steps[14])
	}
	if flow.Steps[15].Action != "open" || flow.Steps[15].Params["clearState"] != "true" {
		t.Fatalf("open clear-state=%+v", flow.Steps[15])
	}
	if flow.Steps[16].Action != "erase" || flow.Steps[16].Params["value"] != "Email" || flow.Steps[16].Params["focused"] != "true" {
		t.Fatalf("erase=%+v", flow.Steps[16])
	}
	if flow.Steps[17].Action != "hideKeyboard" {
		t.Fatalf("hideKeyboard=%+v", flow.Steps[17])
	}
	if flow.Steps[18].Action != "whileNotVisible" || flow.Steps[18].Params["text"] != "You" || flow.Steps[18].Params["timeout"] != "30s" || len(flow.Steps[18].Do) != 2 || flow.Steps[18].Do[0].Params["optional"] != "true" {
		t.Fatalf("while=%+v", flow.Steps[18])
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

func TestParseFlowWhenConditional(t *testing.T) {
	flow, err := ParseFlow([]byte(`
steps:
  - when: { visible: { id: ToggleX } }
    do:
      - tap: { id: ToggleX }
      - wait: { text: Enabled }
  - when: { text: Continue }
    do:
      - tap: { text: Continue }
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Steps) != 2 {
		t.Fatalf("steps=%+v", flow.Steps)
	}
	first := flow.Steps[0]
	if first.Action != "when" || first.Params["id"] != "ToggleX" || len(first.Do) != 2 {
		t.Fatalf("first=%+v", first)
	}
	if first.Do[0].Action != "tap" || first.Do[0].Params["id"] != "ToggleX" {
		t.Fatalf("first do=%+v", first.Do)
	}
	second := flow.Steps[1]
	if second.Action != "when" || second.Params["text"] != "Continue" || len(second.Do) != 1 {
		t.Fatalf("second=%+v", second)
	}
}

func TestParseFlowRejectsInvalidWhen(t *testing.T) {
	for _, data := range []string{
		"steps:\n  - when: { visible: { id: ToggleX } }\n",
		"steps:\n  - when: { visible: { id: ToggleX } }\n    do: []\n",
		"steps:\n  - when: {}\n    do:\n      - tap: { id: ToggleX }\n",
		"steps:\n  - when: { visible: { id: ToggleX } }\n    tap: { id: ToggleX }\n    do:\n      - tap: { id: ToggleX }\n",
		"steps:\n  - when: { visible: { id: ToggleX } }\n    do:\n      - open: {}\n",
		"steps:\n  - when: { visible: { id: ToggleX } }\n    do:\n      - exec: { cmd: echo hi }\n",
	} {
		if _, err := ParseFlow([]byte(data)); err == nil {
			t.Fatalf("expected error for %q", data)
		}
	}
}

func TestParseFlowIncludeStep(t *testing.T) {
	flow, err := ParseFlow([]byte(`
steps:
  - include:
      file: components/login.mav.yaml
      env:
        USER: sellersXp
        FRESH_INSTALL: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Steps) != 1 {
		t.Fatalf("steps=%+v", flow.Steps)
	}
	step := flow.Steps[0]
	if step.Action != "include" || step.Params["file"] != "components/login.mav.yaml" {
		t.Fatalf("step=%+v", step)
	}
	if step.Env["USER"] != "sellersXp" || step.Env["FRESH_INSTALL"] != "true" {
		t.Fatalf("env=%+v", step.Env)
	}
}

func TestLoadFlowExpandsIncludeWithEnv(t *testing.T) {
	root := t.TempDir()
	writeTestFlow(t, filepath.Join(root, "main.yaml"), `
steps:
  - tap: { id: before_login }
  - include:
      file: components/login.yaml
      env:
        USER: sellersXp
        FRESH_INSTALL: true
  - tap: { id: after_login }
`)
	writeTestFlow(t, filepath.Join(root, "components", "login.yaml"), `
steps:
  - tap: { id: "${env.USER}_email" }
  - type: "fresh=${env.FRESH_INSTALL}"
`)
	flow, err := LoadFlow(filepath.Join(root, "main.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Steps) != 4 {
		t.Fatalf("steps=%+v", flow.Steps)
	}
	if flow.Steps[1].Action != "tap" || flow.Steps[1].Params["id"] != "sellersXp_email" {
		t.Fatalf("included tap=%+v", flow.Steps[1])
	}
	if flow.Steps[2].Action != "type" || flow.Steps[2].Params["text"] != "fresh=true" {
		t.Fatalf("included type=%+v", flow.Steps[2])
	}
}

func TestLoadFlowIncludeEnvCanReferenceParentEnv(t *testing.T) {
	root := t.TempDir()
	writeTestFlow(t, filepath.Join(root, "main.yaml"), `
steps:
  - include:
      file: wrapper.yaml
      env:
        USER: sellersXp
`)
	writeTestFlow(t, filepath.Join(root, "wrapper.yaml"), `
steps:
  - include:
      file: login.yaml
      env:
        ACCOUNT: "${env.USER}-account"
`)
	writeTestFlow(t, filepath.Join(root, "login.yaml"), `
steps:
  - type: "${env.ACCOUNT}"
`)
	flow, err := LoadFlow(filepath.Join(root, "main.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Steps) != 1 || flow.Steps[0].Params["text"] != "sellersXp-account" {
		t.Fatalf("steps=%+v", flow.Steps)
	}
}

func TestLoadFlowIncludeFileCanReferenceIncludeEnv(t *testing.T) {
	root := t.TempDir()
	writeTestFlow(t, filepath.Join(root, "main.yaml"), `
steps:
  - include:
      file: "components/${env.COMPONENT}.yaml"
      env:
        COMPONENT: login
        USER: sellersXp
`)
	writeTestFlow(t, filepath.Join(root, "components", "login.yaml"), `
steps:
  - tap: { id: "${env.USER}_email" }
`)
	flow, err := LoadFlow(filepath.Join(root, "main.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Steps) != 1 || flow.Steps[0].Params["id"] != "sellersXp_email" {
		t.Fatalf("steps=%+v", flow.Steps)
	}
}

func TestLoadFlowDetectsIncludeCycle(t *testing.T) {
	root := t.TempDir()
	writeTestFlow(t, filepath.Join(root, "a.yaml"), "steps:\n  - include: { file: b.yaml }\n")
	writeTestFlow(t, filepath.Join(root, "b.yaml"), "steps:\n  - include: { file: a.yaml }\n")
	_, err := LoadFlow(filepath.Join(root, "a.yaml"))
	if err == nil || !strings.Contains(err.Error(), "include_cycle") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFlowRejectsUnknownIncludedAction(t *testing.T) {
	root := t.TempDir()
	writeTestFlow(t, filepath.Join(root, "main.yaml"), "steps:\n  - include: { file: bad.yaml }\n")
	writeTestFlow(t, filepath.Join(root, "bad.yaml"), "steps:\n  - typo: {}\n")
	_, err := LoadFlow(filepath.Join(root, "main.yaml"))
	if err == nil || !strings.Contains(err.Error(), "unknown_step action=typo") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFlowRejectsMissingEnvBinding(t *testing.T) {
	root := t.TempDir()
	writeTestFlow(t, filepath.Join(root, "main.yaml"), `
steps:
  - include: { file: login.yaml }
`)
	writeTestFlow(t, filepath.Join(root, "login.yaml"), `
steps:
  - type: "${env.USER}"
`)
	_, err := LoadFlow(filepath.Join(root, "main.yaml"))
	if err == nil || !strings.Contains(err.Error(), "env_missing name=USER") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFlowRejectsUnsupportedIncludeInsideWhen(t *testing.T) {
	root := t.TempDir()
	writeTestFlow(t, filepath.Join(root, "main.yaml"), `
steps:
  - when: { visible: { id: Gate } }
    do:
      - include: { file: launch.yaml }
`)
	writeTestFlow(t, filepath.Join(root, "launch.yaml"), `
steps:
  - open: {}
`)
	_, err := LoadFlow(filepath.Join(root, "main.yaml"))
	if err == nil || !strings.Contains(err.Error(), "when_child_unsupported action=open") {
		t.Fatalf("err=%v", err)
	}
}

func writeTestFlow(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
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
