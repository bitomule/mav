package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type nativeTestRunner struct {
	version  string
	response CommandResult
	commands [][]string
}

func (r *nativeTestRunner) LookPath(name string) (string, error) {
	if name == "axe" {
		return "axe", nil
	}
	return "", errors.New("missing")
}
func (r *nativeTestRunner) Run(_ context.Context, name string, args ...string) CommandResult {
	r.commands = append(r.commands, append([]string{name}, args...))
	if name == "axe" && len(args) == 1 && args[0] == "--version" {
		return CommandResult{Stdout: r.version}
	}
	return r.response
}
func (r *nativeTestRunner) Start(context.Context, string, string, ...string) (int, error) {
	return 0, errors.New("unexpected background process")
}
func nativeSuccess() CommandResult {
	return CommandResult{Stdout: `{"schema":"axe.output.command-result","schemaVersion":"1","didError":false,"data":{"result":{"ok":true}}}`}
}
func nativeCLI(t *testing.T, kind string) (CLI, *nativeTestRunner, Config, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.TargetKind = kind
	cfg.SimulatorUDID = "SIM-TEST"
	cfg.DeviceUDID = "DEVICE-TEST"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	r := &nativeTestRunner{version: "v2.0.4", response: nativeSuccess()}
	out := &bytes.Buffer{}
	return CLI{Root: root, Runner: r, Stdout: out, driverSession: &driverSession{}}, r, cfg, out
}
func TestNativeAPIDrivesPhysicalDeviceWithoutIDB(t *testing.T) {
	c, r, cfg, out := nativeCLI(t, "device")
	for _, req := range []Automation{
		{Command: "tap", Options: map[string]any{"x": 15, "y": 40}},
		{Command: "key", Options: map[string]any{"code": 42}},
		{Command: "press", Options: map[string]any{"button": "home"}},
		{Command: "settings", Options: map[string]any{"appearance": "dark"}},
	} {
		out.Reset()
		if err := c.automationOutput(context.Background(), cfg, GlobalOptions{}, req); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), "ok ") || !strings.Contains(out.String(), "driver=axe") {
			t.Fatalf("%s: %s", req.Command, out.String())
		}
	}
	versions := 0
	for _, cmd := range r.commands {
		if cmd[0] != "axe" {
			t.Fatalf("unexpected dependency: %v", cmd)
		}
		if strings.Join(cmd, " ") == "axe --version" {
			versions++
			continue
		}
		if !strings.Contains(strings.Join(cmd, " "), "--udid DEVICE-TEST") {
			t.Fatalf("unbound device: %v", cmd)
		}
	}
	if versions != 1 {
		t.Fatalf("version probes=%d, want once", versions)
	}
}
func TestNativeTapPreservesCurrentOrientationCoordinates(t *testing.T) {
	c, r, cfg, out := nativeCLI(t, "simulator")
	err := c.uiTap(context.Background(), GlobalOptions{}, cfg, []string{"--x", "302", "--y", "81"})
	if err != nil {
		t.Fatal(err)
	}
	last := strings.Join(r.commands[len(r.commands)-1], " ")
	if !strings.Contains(last, "-x 302 -y 81") || !strings.Contains(out.String(), "driver=axe") {
		t.Fatalf("command=%s output=%s", last, out)
	}
	if len(r.commands) != 2 {
		t.Fatalf("unexpected rotation/idb probes: %v", r.commands)
	}
}
func TestNativeAPIFailsAXe1WithUpgradeInstructions(t *testing.T) {
	c, r, cfg, out := nativeCLI(t, "device")
	r.version = "1.8.0"
	_ = c.automationOutput(context.Background(), cfg, GlobalOptions{}, Automation{Command: "key", Options: map[string]any{"code": 4}})
	if !strings.Contains(out.String(), "code=axe2_required") || !strings.Contains(out.String(), "brew upgrade") {
		t.Fatal(out.String())
	}
	if len(r.commands) != 1 {
		t.Fatalf("input sent with AXe1: %v", r.commands)
	}
}
func TestNativeAPIExposesDeliveryWithoutReplaying(t *testing.T) {
	c, r, cfg, out := nativeCLI(t, "simulator")
	r.response = CommandResult{Stdout: "not json", Err: errors.New("transport lost")}
	_ = c.uiTap(context.Background(), GlobalOptions{}, cfg, []string{"--x", "10", "--y", "20"})
	if !strings.Contains(out.String(), "delivery=uncertain") || !strings.Contains(out.String(), "retry=unsafe") {
		t.Fatal(out.String())
	}
	if len(r.commands) != 2 {
		t.Fatalf("input replayed or fallback: %v", r.commands)
	}
}
func TestNativeSpecRejectsUnknownFieldsBeforeSendingInput(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	_, _, err := c.executeAutomation(context.Background(), cfg, "", Automation{Command: "key", Options: map[string]any{"code": 4, "shell": "no"}})
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("error=%v", err)
	}
	if len(r.commands) != 0 {
		t.Fatalf("sent invalid input: %v", r.commands)
	}
}
func TestAutomationFlowLintValidatesTypedFields(t *testing.T) {
	good := []byte("name: native\nsteps:\n  - automation:\n      command: key\n      options: {code: 4}\n")
	f, err := ParseFlow(good)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Steps) != 1 || f.Steps[0].Automation == nil {
		t.Fatalf("flow=%+v", f)
	}
	for _, bad := range []string{strings.Replace(string(good), "code: 4", "shell: hi", 1), strings.Replace(string(good), "command: key", "command: agent", 1), strings.Replace(string(good), "code: 4", "code: nope", 1)} {
		if _, err := ParseFlow([]byte(bad)); err == nil {
			t.Fatalf("accepted invalid flow: %s", bad)
		}
	}
}
func TestNativeContextAndTextParsing(t *testing.T) {
	req, err := automationCLI("type", []string{"--text", "--looks-like-a-flag", "--selected", "--screen-id", "1", "--screen-uuid", "DISPLAY-TEST", "--expected-pid", "42"})
	if err != nil {
		t.Fatal(err)
	}
	if req.Options["text"] != "--looks-like-a-flag" || req.Context == nil || req.Context.ScreenID == nil || *req.Context.ScreenID != 1 || req.Context.ExpectedPID != 42 {
		t.Fatalf("request=%+v", req)
	}
}
func TestRetrySafetyPreservesUncertainDelivery(t *testing.T) {
	for _, delivery := range []drivers.InputDelivery{drivers.DeliverySent, drivers.DeliveryUncertain} {
		if inputRetrySafe(&drivers.DriverError{Delivery: delivery}) {
			t.Fatal(delivery)
		}
	}
	if !inputRetrySafe(&drivers.DriverError{Delivery: drivers.DeliveryNotSent}) {
		t.Fatal("rejected-before-input can be retried")
	}
}

func TestNativeSpecAndStringOperandsAreStrict(t *testing.T) {
	for _, args := range [][]string{{"--spec", "null", "--code", "4"}, {"--spec", "{} {}"}, {"--spec", "[]"}} {
		if _, err := automationCLI("key", args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	req, err := automationCLI("slider", []string{"--id", "[volume]", "--percent", "50"})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateAutomation(req); err != nil {
		t.Fatal(err)
	}
	req, err = automationCLI("key", []string{"--code", "4", "--focus-id", "123"})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateAutomation(req); err != nil {
		t.Fatalf("numeric focus identifier became a number: %v", err)
	}
	for _, req := range []Automation{{Command: "touch", Options: map[string]any{"Down": true}}, {Command: "drag", Options: map[string]any{"EndX": 10, "EndY": 20}}, {Command: "tap", Options: map[string]any{"Selector": map[string]any{}}}} {
		if err := validateAutomation(req); err == nil {
			t.Fatalf("accepted missing coordinates: %+v", req)
		}
	}
}

func TestNativeInputSourcesPreserveLiteralText(t *testing.T) {
	c, _, _, _ := nativeCLI(t, "simulator")
	c.Stdin = strings.NewReader("--literal [input]\n")
	req, err := c.prepareAutomation("type", []string{"--stdin"})
	if err != nil {
		t.Fatal(err)
	}
	if req.Options["Text"] != "--literal [input]\n" {
		t.Fatalf("text=%q", req.Options["Text"])
	}
	if _, err = c.prepareAutomation("type", []string{"--stdin", "--text", "other"}); err == nil {
		t.Fatal("accepted two text sources")
	}
}

func TestAutomationFlowPreservesAfterAndRetryPolicy(t *testing.T) {
	f, err := ParseFlow([]byte("name: native\nsteps:\n  - automation:\n      command: key\n      options: {code: 4}\n      onFailure: {strategy: retry, maxAttempts: 2}\n      after: {observe: tree}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Steps[0].OnFailure.MaxAttempts != 2 || f.Steps[0].After == nil {
		t.Fatalf("step=%+v", f.Steps[0])
	}
}

func TestFlowNeverReplaysActionAfterVerificationFails(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	run, err := NewProjectRunState(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err = SaveConfig(c.Root, cfg); err != nil {
		t.Fatal(err)
	}
	req := Automation{Command: "tap", Options: map[string]any{"x": 10, "y": 20}}
	step := FlowStep{Action: "automation", Params: map[string]string{}, Automation: &req, OnFailure: FailurePolicy{Strategy: "retry", MaxAttempts: 3, Delay: "1ms"}, After: &FlowAfter{Observe: "invalid"}}
	fields, err := c.executeFlowStepBoundWithOptions(context.Background(), GlobalOptions{}, run, 1, step, nil)
	if err == nil || fields["retry"] != "unsafe" {
		t.Fatalf("fields=%v err=%v", fields, err)
	}
	taps := 0
	for _, cmd := range r.commands {
		if strings.Contains(strings.Join(cmd, " "), "json tap ") {
			taps++
		}
	}
	if taps != 1 {
		t.Fatalf("action replayed %d times: %v", taps, r.commands)
	}
}

type stopFailureRunner struct {
	*nativeTestRunner
	stopped int
}

func (r *stopFailureRunner) StopProcess(context.Context, int) error {
	r.stopped++
	return errors.New("recorder still alive")
}

func TestFailedRecorderStopRetainsProcessLedger(t *testing.T) {
	c, base, cfg, out := nativeCLI(t, "simulator")
	runner := &stopFailureRunner{nativeTestRunner: base}
	c.Runner = runner
	run, err := NewProjectRunState(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err = SaveCurrentRun(c.Root, run); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(run.Dir, "video.pid"), []byte("987654\n"), 0600); err != nil {
		t.Fatal(err)
	}
	appendProcess(run, "video", 987654, "video.axe")
	identity, _ := json.Marshal(recordingIdentity{Driver: "axe", Target: targetFromConfig(cfg), Path: filepath.Join(run.Dir, "video.mp4")})
	if err = os.WriteFile(filepath.Join(run.Dir, "video-driver.json"), identity, 0600); err != nil {
		t.Fatal(err)
	}
	if err = c.evidenceStop(context.Background(), GlobalOptions{}, nil); err == nil {
		t.Fatal("recorder stop incorrectly succeeded")
	}
	if !fileExists(filepath.Join(run.Dir, "video.pid")) || len(loadProcessRecords(run)) != 1 || runner.stopped != 1 {
		t.Fatalf("tracking lost: %v", out.String())
	}
	if !strings.Contains(out.String(), "video_stop_failed") {
		t.Fatal(out.String())
	}
	c.stopAbandonedVideo(context.Background(), run)
	if !fileExists(filepath.Join(run.Dir, "video.pid")) || len(loadProcessRecords(run)) != 1 || runner.stopped != 2 {
		t.Fatal("abandonment cleanup lost a recorder that refused to stop")
	}

}

func TestStreamPreflightErrorsNeverContaminateMediaStdout(t *testing.T) {
	c, _, _, out := nativeCLI(t, "simulator")
	var diagnostic bytes.Buffer
	c.Stderr = &diagnostic
	_ = c.videoCommand(context.Background(), GlobalOptions{}, []string{"stream", "--not-an-option", "1"})
	if out.Len() != 0 || !strings.Contains(diagnostic.String(), "fail ") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), diagnostic.String())
	}
}

func TestSelectedInputNeverDegradesToAnUnboundDriver(t *testing.T) {
	c, r, cfg, out := nativeCLI(t, "simulator")
	_ = c.dispatchUICommand(context.Background(), GlobalOptions{PreferDriver: "baguette"}, cfg, []string{"tap", "--x", "10", "--y", "20", "--selected"})
	if !strings.Contains(out.String(), "unsupportedInputContext") || len(r.commands) != 1 {
		t.Fatalf("ignored binding: %s commands=%v", out.String(), r.commands)
	}
	for _, command := range []string{"settings", "location", "turn", "readiness"} {
		if err := validateOperationContext(command, &drivers.InputContext{Selected: true}); err == nil {
			t.Fatalf("%s ignored input context", command)
		}
	}
}

func TestCaptureRefusesPIDBindingBeforeCapture(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	cfg.InputContext = &drivers.InputContext{ExpectedPID: 123}
	_, err := c.captureScreenshotWith(context.Background(), cfg, filepath.Join(c.Root, "screen.png"), "")
	if err == nil || !strings.Contains(err.Error(), "expected PID") {
		t.Fatalf("error=%v", err)
	}
	for _, command := range r.commands {
		if strings.Contains(strings.Join(command, " "), "screenshot") {
			t.Fatalf("captured without PID guard: %v", r.commands)
		}
	}
}

func TestBatchRejectsMissingNestedCoordinatesBeforeAnyInput(t *testing.T) {
	req, err := automationCLI("batch", []string{"--spec", `{"steps":[{"tap":{}}]}`})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateAutomation(req); err == nil {
		t.Fatal("empty tap silently became (0,0)")
	}
	req, err = automationCLI("batch", []string{"--spec", `{"steps":[{"tap":{"x":0,"y":0}}]}`})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateAutomation(req); err != nil {
		t.Fatalf("explicit zero is valid: %v", err)
	}
}

type portableTapDriver struct{ taps int }

func (*portableTapDriver) ID() string { return "portable" }
func (*portableTapDriver) Provides(drivers.Target) drivers.CapabilitySet {
	return drivers.NewSet(drivers.CapCoordTap)
}
func (*portableTapDriver) Cost(drivers.Capability, drivers.Target) int { return 0 }
func (*portableTapDriver) Probe(context.Context, drivers.Probe) drivers.HealthReport {
	return drivers.HealthReport{State: drivers.HealthOK}
}
func (*portableTapDriver) Warm(context.Context, drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}
func (d *portableTapDriver) Tap(context.Context, drivers.Target, drivers.TapSpec) (drivers.TapResult, error) {
	d.taps++
	return drivers.TapResult{}, nil
}

func TestTypedTapUsesFunctionalDriverInterface(t *testing.T) {
	c, _, cfg, _ := nativeCLI(t, "simulator")
	driver := &portableTapDriver{}
	c.driverRegistry().Register(driver)
	req := Automation{Command: "tap", Options: map[string]any{"x": 0, "y": 0}}
	_, id, err := c.executeAutomation(context.Background(), cfg, "portable", req)
	if err != nil || id != "portable" || driver.taps != 1 {
		t.Fatalf("id=%s taps=%d err=%v", id, driver.taps, err)
	}
	req.Context = &drivers.InputContext{Selected: true}
	_, _, err = c.executeAutomation(context.Background(), cfg, "portable", req)
	if err == nil || driver.taps != 1 {
		t.Fatalf("ignored selected binding: taps=%d err=%v", driver.taps, err)
	}
}
