package mav

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// These tests pin the macOS-target behaviours that were observed broken while
// validating a real Mac app from a VM: iOS-flavoured guidance on a macos
// target, an opaque TCC capture failure, a doctor that talks about simulators,
// and a double click that did not exist.

func macGapsConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\ntarget_kind: macos\nbundle_id: com.example.app\n"+
		"launch:\n  mode: custom\n  commands:\n    app_path: \"echo /tmp/App.app\"\n")
	return root
}

// TestUITreeToolMissingOnMacNamesTheMacDriver: with no tree driver installed,
// `mav ui tree` on a macos target used to answer `mav setup --install axe
// idb`, both iOS tools that provide nothing on a Mac. The agent that follows
// that hint installs them and is exactly where it started.
func TestUITreeToolMissingOnMacNamesTheMacDriver(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{}}, Root: macGapsConfig(t), Stdout: &out, Stderr: &bytes.Buffer{}}
	// A structured fail line exits non-zero via CommandFailed; the output
	// is the assertion, not the exit.
	_ = cli.Run(context.Background(), []string{"ui", "tree"})
	got := out.String()
	if !strings.Contains(got, "tool=cua-driver") {
		t.Fatalf("must name the macOS tree driver: %q", got)
	}
	if !strings.Contains(got, "mav setup --install cua-driver") {
		t.Fatalf("must say how to install it: %q", got)
	}
	if strings.Contains(got, "axe idb") {
		t.Fatalf("must not send a mac target to install iOS tools: %q", got)
	}
}

// TestUITreeToolMissingOnSimStillNamesAxeAndIdb: the fix is a split, not a
// replacement; the simulator answer must not change.
func TestUITreeToolMissingOnSimStillNamesAxeAndIdb(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\ntarget_kind: simulator\nbundle_id: com.example.app\nsimulator_udid: SIM\n")
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	// A structured fail line exits non-zero via CommandFailed; the output
	// is the assertion, not the exit.
	_ = cli.Run(context.Background(), []string{"ui", "tree"})
	if got := out.String(); !strings.Contains(got, "mav setup --install axe idb") {
		t.Fatalf("simulator guidance must stay: %q", got)
	}
}

// TestDoctorReflectsTheSelectedMacProfile: doctor read the base config,
// reported simulator state and prescribed iOS installs even when the run was
// `--profile mac`. Reflecting the resolved target is the whole point of a
// diagnostic.
func TestDoctorReflectsTheSelectedMacProfile(t *testing.T) {
	t.Setenv("MAV_PROFILE", "")
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\ntarget_kind: simulator\nbundle_id: com.example.app\n"+
		"profiles:\n  mac:\n    target_kind: macos\n")
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{"go": true, "xcrun": true}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor", "--profile", "mac"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "target_kind=macos") {
		t.Fatalf("doctor must report the profile's target kind: %q", got)
	}
	if !strings.Contains(got, `next="mav setup --install cua-driver"`) {
		t.Fatalf("a mac target with no driver must be sent to the mac driver: %q", got)
	}
	if strings.Contains(got, "install axe") || strings.Contains(got, "baguette") {
		t.Fatalf("iOS tool guidance has no place on a mac target: %q", got)
	}
}

// TestDoctorReportsSimulatorTargetKind: the field must exist on iOS targets
// too, or nobody can tell which target doctor examined.
func TestDoctorReportsSimulatorTargetKind(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{"axe": true, "idb": true, "baguette": true}}, Root: t.TempDir(), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "target_kind=simulator") {
		t.Fatalf("doctor must say which target it examined: %q", got)
	}
}

// TestDoctorAcceptsAnEmptyMacLaunchCommand: on a macos target an empty
// launch command IS the recipe — mav launches the bundle's binary directly
// because `open` does not propagate environment. Doctor flagged exactly that
// correct configuration as incomplete.
func TestDoctorAcceptsAnEmptyMacLaunchCommand(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{"cua-driver": true}}, Root: macGapsConfig(t), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "launch_recipe=ok") || strings.Contains(got, "launch_missing") {
		t.Fatalf("an empty mac launch command is driver launch, not a hole: %q", got)
	}
}

// TestDoctorStillFlagsAnEmptySimulatorLaunchCommand: the same emptiness on a
// simulator target has no driver fallback without a bundle id resolved at
// launch, and silence would hide a real hole.
func TestDoctorStillFlagsAnEmptySimulatorLaunchCommand(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\ntarget_kind: simulator\n"+
		"launch:\n  mode: custom\n  commands:\n    app_path: \"echo /tmp/App.app\"\n")
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{}}, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "launch_recipe=incomplete") {
		t.Fatalf("a simulator recipe with no launch and no bundle id is incomplete: %q", got)
	}
}

// TestCaptureFailureOnMacExplainsScreenRecording: `could not create image
// from display` is CGDisplay's way of saying the Screen Recording permission
// is missing, and forwarding it raw leaves the reader debugging displays.
func TestCaptureFailureOnMacExplainsScreenRecording(t *testing.T) {
	fields := map[string]string{"stderr": "could not create image from display"}
	cfg := Config{TargetKind: "macos"}
	addMacScreenRecordingNext(fields, cfg, "could not create image from display")
	if fields["cause"] != "screen_recording_permission_missing" {
		t.Fatalf("the TCC cause must be named: %v", fields)
	}
	if next := fields["next"]; !strings.Contains(next, "Screen Recording") {
		t.Fatalf("the fix must be actionable: %v", fields)
	}
}

// TestCaptureFailureNextIsScopedToTheMacSymptom: other capture failures, and
// every other platform, must keep their own diagnosis.
func TestCaptureFailureNextIsScopedToTheMacSymptom(t *testing.T) {
	fields := map[string]string{}
	addMacScreenRecordingNext(fields, Config{TargetKind: "macos"}, "disk full")
	if _, ok := fields["next"]; ok {
		t.Fatalf("an unrelated error must not be labelled as TCC: %v", fields)
	}
	fields = map[string]string{}
	addMacScreenRecordingNext(fields, Config{TargetKind: "simulator"}, "could not create image from display")
	if _, ok := fields["next"]; ok {
		t.Fatalf("the simulator has no Screen Recording TCC: %v", fields)
	}
}

// TestCaptureToolMissingOnMacNamesTheMacDrivers: the capture tool_missing
// answer named axe|idb|xcrun, all of them iOS.
func TestCaptureToolMissingOnMacNamesTheMacDrivers(t *testing.T) {
	fields := captureToolMissingFields(Config{TargetKind: "macos"})
	if !strings.Contains(fields["tool"], "cua-driver") {
		t.Fatalf("must name the mac capture path: %v", fields)
	}
	if !strings.Contains(fields["next"], "cua-driver") {
		t.Fatalf("must say how to get it: %v", fields)
	}
	fields = captureToolMissingFields(Config{TargetKind: "simulator"})
	if fields["tool"] != "axe|idb|xcrun" {
		t.Fatalf("iOS guidance must stay: %v", fields)
	}
}

// TestUIDoubleTapOnMacDoubleClicksTheElement: Nokoru's inline rename triggers
// on a double click over the title; without this the only path was cliclick
// from outside mav, with no evidence trail.
func TestUIDoubleTapOnMacDoubleClicksTheElement(t *testing.T) {
	var out bytes.Buffer
	runner := fakeRunner{
		tools: map[string]bool{"cua-driver": true},
		out: map[string]string{
			`cua-driver call list_apps {}`:                                     `{"apps":[{"bundle_id":"com.example.app","name":"App","pid":4242,"running":true}]}`,
			`cua-driver call list_windows {"pid":4242}`:                        `{"windows":[{"window_id":11,"pid":4242,"app_name":"App","title":"Main","is_on_screen":true,"bounds":{"width":800,"height":600}}]}`,
			`cua-driver call get_window_state {"pid":4242,"window_id":11}`:     `{"snapshot_id":"s1","elements":[{"element_index":1,"element_token":"s1:1","role":"AXStaticText","label":"Team sync","frame":{"x":10,"y":20,"w":100,"h":30}}]}`,
			`cua-driver call double_click {"element_token":"s1:1","pid":4242}`: `{"effect":"unverifiable"}`,
		},
	}
	cli := CLI{Runner: runner, Root: macGapsConfig(t), Stdout: &out, Stderr: &bytes.Buffer{}}
	// A structured fail line exits non-zero via CommandFailed; the output
	// is the assertion, not the exit.
	_ = cli.Run(context.Background(), []string{"ui", "doubleTap", "--text", "Team sync"})
	got := out.String()
	if !strings.Contains(got, "ok cmd=ui.doubleTap") {
		t.Fatalf("the double tap must succeed through cua: %q", got)
	}
	if !strings.Contains(got, "driver=cua") {
		t.Fatalf("the mac driver must serve it: %q", got)
	}
}

// TestUIDoubleTapOnMacWithoutTheDriverPointsAtIt: tool_missing must carry mac
// guidance, not baguette.
func TestUIDoubleTapOnMacWithoutTheDriverPointsAtIt(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{}}, Root: macGapsConfig(t), Stdout: &out, Stderr: &bytes.Buffer{}}
	// A structured fail line exits non-zero via CommandFailed; the output
	// is the assertion, not the exit.
	_ = cli.Run(context.Background(), []string{"ui", "doubleTap", "--text", "Team sync"})
	got := out.String()
	if !strings.Contains(got, "tool=cua-driver") || strings.Contains(got, "baguette") {
		t.Fatalf("a mac double tap must be sent to the mac driver: %q", got)
	}
}

// TestUITapByCoordinatesOnMacRoutesToTheMacDriver: doctor now reports
// coordinate_tap_driver=cua, and the coordinate branch hard-preferred idb —
// which provides nothing on a Mac — so every `ui tap --x --y` died
// tool_missing tool=idb while the healthy mac driver sat there.
func TestUITapByCoordinatesOnMacRoutesToTheMacDriver(t *testing.T) {
	var out bytes.Buffer
	runner := fakeRunner{
		tools: map[string]bool{"cua-driver": true},
		out: map[string]string{
			`cua-driver call list_apps {}`:                                 `{"apps":[{"bundle_id":"com.example.app","name":"App","pid":4242,"running":true}]}`,
			`cua-driver call list_windows {"pid":4242}`:                    `{"windows":[{"window_id":11,"pid":4242,"app_name":"App","title":"Main","is_on_screen":true,"bounds":{"width":800,"height":600}}]}`,
			`cua-driver call get_window_state {"pid":4242,"window_id":11}`: `{"snapshot_id":"s1","elements":[{"element_index":1,"element_token":"s1:1","role":"AXStaticText","label":"Team sync","frame":{"x":10,"y":20,"w":100,"h":30}}]}`,
			`cua-driver call click {"pid":4242,"x":60,"y":35}`:             `{"effect":"unverifiable"}`,
		},
	}
	cli := CLI{Runner: runner, Root: macGapsConfig(t), Stdout: &out, Stderr: &bytes.Buffer{}}
	_ = cli.Run(context.Background(), []string{"ui", "tap", "--x", "60", "--y", "35"})
	got := out.String()
	if !strings.Contains(got, "ok cmd=ui.tap") || !strings.Contains(got, "driver=cua") {
		t.Fatalf("a mac coordinate tap must route to cua: %q", got)
	}
	if strings.Contains(got, "tool=idb") {
		t.Fatalf("idb has no business on a mac target: %q", got)
	}
}

// TestUIDoubleTapOnMacResolvesRichSelectors: forwarding only --id/--text to
// the driver silently dropped every other predicate — a --text-contains
// selector reached the driver empty and was rejected as if none was given.
func TestUIDoubleTapOnMacResolvesRichSelectors(t *testing.T) {
	var out bytes.Buffer
	runner := fakeRunner{
		tools: map[string]bool{"cua-driver": true},
		out: map[string]string{
			`cua-driver call list_apps {}`:                                 `{"apps":[{"bundle_id":"com.example.app","name":"App","pid":4242,"running":true}]}`,
			`cua-driver call list_windows {"pid":4242}`:                    `{"windows":[{"window_id":11,"pid":4242,"app_name":"App","title":"Main","is_on_screen":true,"bounds":{"width":800,"height":600}}]}`,
			`cua-driver call get_window_state {"pid":4242,"window_id":11}`: `{"snapshot_id":"s1","elements":[{"element_index":1,"element_token":"s1:1","role":"AXStaticText","label":"Team sync","frame":{"x":10,"y":20,"w":100,"h":30}}]}`,
			`cua-driver call double_click {"pid":4242,"x":60,"y":35}`:      `{"effect":"unverifiable"}`,
		},
	}
	cli := CLI{Runner: runner, Root: macGapsConfig(t), Stdout: &out, Stderr: &bytes.Buffer{}}
	_ = cli.Run(context.Background(), []string{"ui", "doubleTap", "--text-contains", "Team"})
	got := out.String()
	if !strings.Contains(got, "ok cmd=ui.doubleTap") {
		t.Fatalf("a rich selector must resolve and double-click: %q", got)
	}
	if !strings.Contains(got, "x=60") || !strings.Contains(got, "y=35") {
		t.Fatalf("the matched element's center must be the click point: %q", got)
	}
}

// TestUIDoubleTapOnMacRejectsHalfCoordinates: --x without --y used to click
// at (X, 0) — the menu bar — and report ok; a typo in one axis did the same.
func TestUIDoubleTapOnMacRejectsHalfCoordinates(t *testing.T) {
	for _, args := range [][]string{
		{"ui", "doubleTap", "--x", "100"},
		{"ui", "doubleTap", "--x", "100", "--y", "3O"},
	} {
		var out bytes.Buffer
		cli := CLI{Runner: fakeRunner{tools: map[string]bool{"cua-driver": true}}, Root: macGapsConfig(t), Stdout: &out, Stderr: &bytes.Buffer{}}
		_ = cli.Run(context.Background(), args)
		if got := out.String(); !strings.Contains(got, "fail code=gesture_invalid") {
			t.Fatalf("%v must be rejected, not clicked at a guessed point: %q", args, got)
		}
	}
}

// TestDoctorCountsAxcliForSemanticTaps: tapToolPresent accepts axcli, so a
// Mac with only axcli CAN tap by id/text — doctor calling that setup broken
// contradicted the commands that then worked.
func TestDoctorCountsAxcliForSemanticTaps(t *testing.T) {
	var out bytes.Buffer
	cli := CLI{Runner: fakeRunner{tools: map[string]bool{"axcli": true}}, Root: macGapsConfig(t), Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "semantic_actions=ok") || !strings.Contains(got, "semantic_actions_driver=axcli") {
		t.Fatalf("axcli semantic taps must be reported: %q", got)
	}
	if !strings.Contains(got, "accessibility=missing") || strings.Contains(got, "mac_tree_driver=axcli") {
		t.Fatalf("axcli has no tree and must not be reported as having one: %q", got)
	}
}
