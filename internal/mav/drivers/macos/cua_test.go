package macos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func macTarget() drivers.Target {
	return drivers.Target{Kind: drivers.KindMac, BundleID: "com.example.app", PID: 4242}
}

const cuaWindowList = `{"windows":[
 {"window_id":10,"pid":4242,"app_name":"App","title":"","is_on_screen":true,"bounds":{"width":1024,"height":30}},
 {"window_id":11,"pid":4242,"app_name":"App","title":"Main","is_on_screen":true,"bounds":{"width":800,"height":600}},
 {"window_id":12,"pid":999,"app_name":"Otra","title":"","is_on_screen":true,"bounds":{"width":1600,"height":1200}}
]}`

func cuaState1x1() string {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgo=")
	_ = png
	return `{"snapshot_id":"s1","screenshot_png_b64":"aGkK","elements":[
 {"element_index":0,"element_token":"s1:0","role":"AXWindow","frame":{"x":0,"y":0,"w":800,"h":600}},
 {"element_index":1,"element_token":"s1:1","role":"AXButton","label":"Get started","enabled":true,"frame":{"x":10,"y":20,"w":100,"h":30}}
]}`
}

func cuaExec() *fakeExec {
	return &fakeExec{
		tools: map[string]bool{"cua-driver": true},
		results: map[string]drivers.ExecResult{
			"cua-driver call list_windows":     {Stdout: cuaWindowList},
			"cua-driver call get_window_state": {Stdout: cuaState1x1()},
			"cua-driver call click":            {Stdout: `{"effect":"unverifiable","delivery":{"mode":"background"}}`},
			"cua-driver call type_text":        {Stdout: `{"ok":true}`},
			"cua-driver permissions status":    {Stdout: `{"accessibility":true,"screen_recording":true}`},
		},
	}
}

// TestCuaPicksTheLargestOnScreenWindowOfThePid: list_windows also returns
// the menu bar strip every app publishes, and windows of other processes.
// Choosing by visible area of the right pid is what avoids capturing the
// menu bar believing it is the app.
func TestCuaPicksTheLargestOnScreenWindowOfThePid(t *testing.T) {
	f := cuaExec()
	if _, err := NewCua(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{}); err != nil {
		t.Fatal(err)
	}
	var got string
	for _, c := range f.commands {
		if strings.Contains(c, "get_window_state") {
			got = c
		}
	}
	if !strings.Contains(got, `"window_id":11`) {
		t.Fatalf("must pick the 800x600 window of pid 4242: %q", got)
	}
}

// TestCuaAsksForWindowsByPID: with no pid in the request, list_windows
// enumerates only layer 0, to avoid flooding the caller with tooltips,
// popovers and the Dock, and an app whose entire UI is a floating window
// looks closed. Naming the process admits every layer. Filtering afterwards
// in Go does not help: what does not arrive cannot be filtered.
func TestCuaAsksForWindowsByPID(t *testing.T) {
	f := cuaExec()
	if _, err := NewCua(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{}); err != nil {
		t.Fatal(err)
	}
	var listing string
	for _, c := range f.commands {
		if strings.Contains(c, "list_windows") {
			listing = c
		}
	}
	if !strings.Contains(listing, `"pid":4242`) {
		t.Fatalf("the pid must go in the request, not be applied afterwards: %q", listing)
	}
}

func TestCuaNoWindowFailsLoudly(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call list_windows"] = drivers.ExecResult{Stdout: `{"windows":[]}`}
	_, err := NewCua(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{})
	if err == nil {
		t.Fatal("with no window there can be no tree")
	}
	if !strings.Contains(err.Error(), "no on-screen window") {
		t.Fatalf("the error must name the real cause: %v", err)
	}
}

// TestCuaRefusalIsAnErrorDespiteExitZero: a refusal arrives with exit 0 and
// a `refusal` object on stdout, so checking the exit code is not enough.
func TestCuaRefusalIsAnErrorDespiteExitZero(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call click"] = drivers.ExecResult{
		Stdout: `{"status":"refused","refusal":{"code":"snapshot_id_required","message":"bare element_index is not accepted"}}`,
	}
	_, err := NewCua(f).Tap(context.Background(), macTarget(), drivers.TapSpec{Selector: drivers.ElementSelector{Text: "Get started"}})
	if err == nil {
		t.Fatal("a refusal is a failure even with exit code 0")
	}
	if !strings.Contains(err.Error(), "snapshot_id_required") {
		t.Fatalf("the refusal code must reach the error: %v", err)
	}
}

// TestCuaTapUsesAFreshSnapshotToken: the tool invalidates the index map as
// soon as you take another snapshot, and rejects bare indexes. Targeting by
// element_token of the freshly taken snapshot is what prevents clicking
// what is no longer there.
func TestCuaTapUsesAFreshSnapshotToken(t *testing.T) {
	f := cuaExec()
	if _, err := NewCua(f).Tap(context.Background(), macTarget(), drivers.TapSpec{Selector: drivers.ElementSelector{Text: "Get started"}}); err != nil {
		t.Fatal(err)
	}
	var click string
	for _, c := range f.commands {
		if strings.Contains(c, "call click") {
			click = c
		}
	}
	if !strings.Contains(click, `"element_token":"s1:1"`) {
		t.Fatalf("the tap must go by the snapshot token: %q", click)
	}
	if strings.Contains(click, "element_index") {
		t.Fatalf("bare indexes are rejected by the tool: %q", click)
	}
}

// TestCuaTreeCarriesGeometry: the tree has to carry frame, which is what
// axcli does not give and what the diff between snapshots needs to match
// elements without an identifier.
func TestCuaTreeCarriesGeometry(t *testing.T) {
	res, err := NewCua(cuaExec()).Tree(context.Background(), macTarget(), drivers.TreeSpec{})
	if err != nil {
		t.Fatal(err)
	}
	var nodes []map[string]any
	if err := json.Unmarshal(res.JSON, &nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes=%v", nodes)
	}
	if nodes[1]["frame"] != "{{10, 20}, {100, 30}}" {
		t.Fatalf("geometry is missing: %v", nodes[1])
	}
	if nodes[1]["role"] != "AXButton" || nodes[1]["label"] != "Get started" {
		t.Fatalf("role and label must pass through as is: %v", nodes[1])
	}
}

// TestCuaScreenshotComesFromTheSameCallAsTheTree: image and tree come from
// the same get_window_state, so they describe the same instant. If the
// capture needed its own invocation, the visual evidence could be of a
// screen that already changed.
func TestCuaScreenshotComesFromTheSameCallAsTheTree(t *testing.T) {
	f := cuaExec()
	out := filepath.Join(t.TempDir(), "s.png")
	if err := NewCua(f).Screenshot(context.Background(), macTarget(), drivers.ScreenshotSpec{OutPath: out}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi\n" {
		t.Fatalf("the capture must come from the response's base64: %q", data)
	}
	for _, c := range f.commands {
		if strings.Contains(c, "screenshot") {
			t.Fatalf("there must be no second capture call: %v", f.commands)
		}
	}
}

// TestCuaUnprovableScreenshotIsAnError: the tool omits the image on purpose
// when it cannot prove it matches the requested dimensions. That is
// propagated: a capture that cannot be proven is not evidence.
func TestCuaUnprovableScreenshotIsAnError(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call get_window_state"] = drivers.ExecResult{Stdout: `{"snapshot_id":"s1","elements":[{"element_index":0,"element_token":"s1:0","role":"AXWindow"}]}`}
	err := NewCua(f).Screenshot(context.Background(), macTarget(), drivers.ScreenshotSpec{OutPath: filepath.Join(t.TempDir(), "s.png")})
	if err == nil || !strings.Contains(err.Error(), "provable") {
		t.Fatalf("with no provable image it must fail: %v", err)
	}
}

// TestCuaProbeAsksTheDaemonNotTheCaller: `permissions status` answers with
// CuaDriver's identity because it is its own responsible process. With no
// daemon it answers something that is not permission JSON, and that is
// degraded, not healthy, because the caller's permissions say nothing about
// the capturer's.
func TestCuaProbeAsksTheDaemonNotTheCaller(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver permissions status"] = drivers.ExecResult{Stdout: `{"accessibility":true,"screen_recording":false}`}
	report := NewCua(f).Probe(context.Background(), f)
	if report.State != drivers.HealthDegraded {
		t.Fatalf("a missing permission is degraded: %v", report.State)
	}
	if !strings.Contains(report.Detail, "Screen Recording") {
		t.Fatalf("it has to say which one is missing: %q", report.Detail)
	}
	if report.Next != "cua-driver permissions grant" {
		t.Fatalf("the next step is its grant flow, which does automate it: %q", report.Next)
	}
}

// TestCuaIsSilentOnNonMacTargets pins that it does not compete on iOS.
func TestCuaIsSilentOnNonMacTargets(t *testing.T) {
	d := NewCua(cuaExec())
	for _, kind := range []drivers.TargetKind{drivers.KindSim, drivers.KindDevice} {
		if d.Provides(drivers.Target{Kind: kind}).Has(drivers.CapTreeAX) {
			t.Fatalf("kind=%v", kind)
		}
	}
}

// TestCuaResolvesPIDFromBundleID: mav identifies an app by bundle, which is
// what stays stable across runs; cua-driver works by pid, which changes on
// every launch. If that translation did not happen, no macOS command would
// find its target.
func TestCuaResolvesPIDFromBundleID(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call list_apps"] = drivers.ExecResult{Stdout: `{"apps":[
	 {"bundle_id":"com.otra","name":"Otra","pid":7,"running":true},
	 {"bundle_id":"com.example.app","name":"App","pid":4242,"running":true},
	 {"bundle_id":"com.example.app.old","name":"Vieja","pid":9,"running":false}
	]}`}
	target := macTarget()
	target.PID = 0
	if _, err := NewCua(f).Tree(context.Background(), target, drivers.TreeSpec{}); err != nil {
		t.Fatal(err)
	}
	var state string
	for _, c := range f.commands {
		if strings.Contains(c, "get_window_state") {
			state = c
		}
	}
	if !strings.Contains(state, `"pid":4242`) {
		t.Fatalf("must translate bundle -> pid: %q", state)
	}
}

// TestCuaNotRunningSaysSo: with the app not running there is no pid, and
// the error has to say that and not a window failure, which sends you
// looking in the wrong place.
func TestCuaNotRunningSaysSo(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call list_apps"] = drivers.ExecResult{Stdout: `{"apps":[{"bundle_id":"com.example.app","name":"App","pid":0,"running":false}]}`}
	target := macTarget()
	target.PID = 0
	_, err := NewCua(f).Tree(context.Background(), target, drivers.TreeSpec{})
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("got %v", err)
	}
}

// TestCuaStartsTheDaemonItselfWhenItIsDown: a down daemon is the only
// failure mav can fix on its own, and doing so keeps every agent from
// learning, or inventing, the incantation. What matters in the command is
// `open -g`: starting the driver cannot steal focus from anyone.
func TestCuaStartsTheDaemonItselfWhenItIsDown(t *testing.T) {
	f := cuaExec()
	down := drivers.ExecResult{Stdout: "Cua Driver daemon is not running on /tmp/x.sock.\nStart it first with: cua-driver serve"}
	f.results["cua-driver call list_windows"] = down
	f.onCommand = func(cmd string) {
		if strings.HasPrefix(cmd, "open ") {
			f.results["cua-driver call list_windows"] = drivers.ExecResult{Stdout: cuaWindowList}
		}
	}
	if _, err := NewCua(f).Tree(context.Background(), macTarget(), drivers.TreeSpec{}); err != nil {
		t.Fatal(err)
	}
	var launch string
	for _, c := range f.commands {
		if strings.HasPrefix(c, "open ") {
			launch = c
		}
	}
	if launch == "" {
		t.Fatalf("mav must bring it up, not give up: %v", f.commands)
	}
	if !strings.Contains(launch, "-g") {
		t.Fatalf("without -g the start steals focus, the one thing this driver promises not to do: %q", launch)
	}
	if !strings.Contains(launch, "-a CuaDriver") {
		t.Fatalf("it has to go through the app, which is who holds the permissions: %q", launch)
	}
}

// TestCuaGivesUpAfterOneStartAttempt: if the daemon does not come up,
// retrying on every command turns a clear failure into a string of waits.
func TestCuaGivesUpAfterOneStartAttempt(t *testing.T) {
	f := cuaExec()
	f.results["cua-driver call list_windows"] = drivers.ExecResult{Stdout: "Cua Driver daemon is not running on /tmp/x.sock."}
	f.results["cua-driver permissions status"] = drivers.ExecResult{Stdout: "{}"}
	d := NewCua(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 3; i++ {
		if _, err := d.Tree(ctx, macTarget(), drivers.TreeSpec{}); err == nil {
			t.Fatal("with no daemon there is no tree")
		}
	}
	var starts int
	for _, c := range f.commands {
		if strings.HasPrefix(c, "open ") {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("one attempt per process, not one per command: %d", starts)
	}
}
