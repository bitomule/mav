package mav

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type pathRunner map[string]string

func (r pathRunner) LookPath(file string) (string, error) {
	if path := r[file]; path != "" {
		return path, nil
	}
	return "", os.ErrNotExist
}

func (r pathRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	return CommandResult{}
}

func (r pathRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	return 0, nil
}

func TestBuildPinchPanActions(t *testing.T) {
	actions, fields, err := buildGestureActions(gestureParams{
		Kind:     "pinch",
		X:        "200",
		Y:        "450",
		Scale:    "0.5",
		PanX:     "80",
		PanY:     "-40",
		Duration: "800ms",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fields["scale"] != "0.5" || fields["pan_x"] != "80" || fields["pan_y"] != "-40" {
		t.Fatalf("fields=%v", fields)
	}
	first := actions[0]["actions"].([]map[string]any)
	second := actions[1]["actions"].([]map[string]any)
	if first[0]["x"] != 130 || first[0]["y"] != 450 || second[0]["x"] != 270 || second[0]["y"] != 450 {
		t.Fatalf("start first=%v second=%v", first[0], second[0])
	}
	if first[2]["x"] != 245 || first[2]["y"] != 410 || second[2]["x"] != 315 || second[2]["y"] != 410 {
		t.Fatalf("end first=%v second=%v", first[2], second[2])
	}
}

func TestBuildRotateActions(t *testing.T) {
	actions, fields, err := buildGestureActions(gestureParams{
		Kind:    "rotate",
		X:       "200",
		Y:       "450",
		Degrees: "90",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fields["degrees"] != "90" {
		t.Fatalf("fields=%v", fields)
	}
	first := actions[0]["actions"].([]map[string]any)
	second := actions[1]["actions"].([]map[string]any)
	if first[2]["x"] != 200 || first[2]["y"] != 380 || second[2]["x"] != 200 || second[2]["y"] != 520 {
		t.Fatalf("rotated first=%v second=%v", first[2], second[2])
	}
}

func TestBuildTwoFingerPanRequiresDelta(t *testing.T) {
	_, _, err := buildGestureActions(gestureParams{Kind: "twoFingerPan", X: "200", Y: "450"})
	if err == nil || err.Error() != "pan_delta_missing" {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildGestureRejectsInvalidNumbers(t *testing.T) {
	_, _, err := buildGestureActions(gestureParams{Kind: "pinch", X: "200", Y: "450", Scale: "abc"})
	if err == nil || err.Error() != "scale_invalid" {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadW3CActionsFileAcceptsObjectOrArray(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "actions.json")
	if err := os.WriteFile(path, []byte(`{"actions":[{"type":"pointer","id":"finger1","actions":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := loadW3CActionsFile(root, "actions.json")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(payload)
	if string(data) != `[{"actions":[],"id":"finger1","type":"pointer"}]` {
		t.Fatalf("payload=%s", data)
	}
}

func TestAppiumHasXCUITestDriver(t *testing.T) {
	runner := fakeRunner{out: map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"}}
	if !appiumHasXCUITestDriver(context.Background(), runner) {
		t.Fatal("expected xcuitest installed")
	}
}

func TestCheckAppiumXCUITestDriverDetectsNodeRuntimeFailure(t *testing.T) {
	runner := fakeRunner{out: map[string]string{"appium driver list --installed": "Error [ERR_REQUIRE_ESM]\nNode.js v20.16.0\n"}}
	status := checkAppiumXCUITestDriver(context.Background(), runner)
	if status.OK || !status.NodeMismatch || status.Message != "appium_node_runtime_failed" {
		t.Fatalf("status=%+v", status)
	}
}

func TestCheckAppiumNodePathDetectsMismatch(t *testing.T) {
	root := t.TempDir()
	appiumNode := filepath.Join(root, "nvm", "bin", "node")
	activeNode := filepath.Join(root, "usr", "bin", "node")
	appiumPath := filepath.Join(root, "nvm", "bin", "appium")
	if err := os.MkdirAll(filepath.Dir(appiumNode), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(activeNode), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appiumNode, []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(activeNode, []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appiumPath, []byte("#!"+appiumNode+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	check := checkAppiumNodePath(pathRunner{"appium": appiumPath, "node": activeNode})
	if check.OK || check.Message != "appium_node_mismatch" || !strings.Contains(check.Next, filepath.Dir(appiumNode)) {
		t.Fatalf("check=%+v", check)
	}
}

func TestCheckAppiumNodePathAcceptsEnvShebang(t *testing.T) {
	root := t.TempDir()
	appiumPath := filepath.Join(root, "bin", "appium")
	nodePath := filepath.Join(root, "bin", "node")
	if err := os.MkdirAll(filepath.Dir(appiumPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodePath, []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appiumPath, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	check := checkAppiumNodePath(pathRunner{"appium": appiumPath, "node": nodePath})
	if !check.OK {
		t.Fatalf("check=%+v", check)
	}
}

func TestPerformAppiumActionsReusesStoredSession(t *testing.T) {
	root := t.TempDir()
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCurrentRun(root, run); err != nil {
		t.Fatal(err)
	}
	posted := ""
	released := false
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session/s1/actions":
			data, _ := io.ReadAll(r.Body)
			posted = string(data)
		case r.Method == http.MethodDelete && r.URL.Path == "/session/s1/actions":
			released = true
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":null}`)), Header: http.Header{}}, nil
	})}
	defer func() { http.DefaultClient = oldClient }()
	state := appiumSessionState{PID: 123, BaseURL: "http://appium.local", SessionID: "s1", UDID: "SIM", BundleID: "com.example.app"}
	if err := writeAppiumSession(run, state); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Runner: fakeRunner{
		tools: map[string]bool{"appium": true, "node": true},
		out:   map[string]string{"appium driver list --installed": "xcuitest@7.0.0\n"},
	}, Root: root, Stdout: os.Stdout, Stderr: os.Stderr}
	err = cli.performAppiumActions(context.Background(), Config{SimulatorUDID: "SIM", BundleID: "com.example.app"}, []map[string]any{{"type": "pointer", "id": "finger1", "actions": []any{}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(posted, `"actions"`) || !released {
		t.Fatalf("posted=%q released=%v", posted, released)
	}
}

func TestCreateAppiumSessionUsesUDIDAndBundleID(t *testing.T) {
	oldClient := http.DefaultClient
	posted := ""
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/session" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		posted = string(data)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"value":{"sessionId":"session-1"}}`)), Header: http.Header{}}, nil
	})}
	defer func() { http.DefaultClient = oldClient }()
	id, err := createAppiumSession(context.Background(), "http://appium.local", Config{SimulatorUDID: "SIM", BundleID: "com.example.app"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "session-1" || !strings.Contains(posted, `"appium:udid":"SIM"`) || !strings.Contains(posted, `"appium:bundleId":"com.example.app"`) {
		t.Fatalf("id=%q posted=%s", id, posted)
	}
}
