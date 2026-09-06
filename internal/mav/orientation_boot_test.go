package mav

import (
	"context"
	"os"
	"strings"
	"testing"
)

const bootedDevicesJSON = `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-3":[{"udid":"CCCCCCCC-0000-0000-0000-000000000003","state":"Booted"}]}}`
const noBootedDevicesJSON = `{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-3":[]}}`

func bootCLI(t *testing.T, udid, bootedJSON string) (CLI, *sequenceRecordingRunner, *strings.Builder, string) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true, "xcrun": true}
	cfg.SimulatorUDID = udid
	cfg.SimulatorName = "Test iPhone"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		out: map[string]string{
			"xcrun simctl list devices booted -j": bootedJSON,
		},
	}
	var out strings.Builder
	return CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &strings.Builder{}}, runner, &out, root
}

func declarationExists(root, udid string) bool {
	_, err := os.Stat(declaredOrientationPath(root, udid))
	return err == nil
}

// `simctl boot` on an already-booted device is a no-op -- Driver.Boot
// tolerates "Unable to boot device in current state" precisely because
// nothing happens. The device keeps whatever orientation it had, including
// one the caller just declared, so dropping the declaration there throws away
// the only thing that makes coordinate gestures correct on a headless
// simulator. Revert the wasBooted guard and this test fails.
func TestSimBootKeepsTheDeclarationWhenTheDeviceWasAlreadyBooted(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	cli, _, out, root := bootCLI(t, udid, bootedDevicesJSON)
	if err := writeDeclaredOrientation(root, udid, declaredOrientation{Value: orientationLandscapeRight, Rotation: 90}); err != nil {
		t.Fatal(err)
	}
	if err := cli.Run(context.Background(), []string{"sim", "boot"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	if !declarationExists(root, udid) {
		t.Fatal("a no-op boot discarded the caller's orientation declaration")
	}
}

// A device that really was not booted transitions to a fresh boot, which
// resets it to portrait. Anything MAV recorded before that describes an
// orientation the device no longer has.
func TestSimBootClearsTheDeclarationOnARealBoot(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	cli, _, out, root := bootCLI(t, udid, noBootedDevicesJSON)
	if err := writeDeclaredOrientation(root, udid, declaredOrientation{Value: orientationLandscapeRight, Rotation: 90}); err != nil {
		t.Fatal(err)
	}
	writeScreenCache(root, udid, screenCache{PortraitWidth: 402, PortraitHeight: 874, Angle: 90})
	if err := cli.Run(context.Background(), []string{"sim", "boot"}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	if declarationExists(root, udid) {
		t.Fatal("a real boot kept a declaration describing the pre-boot orientation")
	}
	if _, ok := readScreenCache(root, udid); ok {
		t.Fatal("a real boot kept the pre-boot screen cache")
	}
}

// The two booted-state helpers need OPPOSITE defaults when they cannot tell.
// isSimulatorBooted guards a retry, so unknown means "booted, do not retry".
// simulatorAlreadyBooted guards throwing away a rotation declaration, so
// unknown must mean "not booted, clear it": clearing costs one screen
// re-probe, while keeping a stale declaration silently transforms every later
// gesture into a space the device is not in. Make them share a default and
// this test fails.
func TestBootedHelpersDefaultOppositeWaysWhenTheStateIsUnknown(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003"
	runner := &sequenceRecordingRunner{
		tools: map[string]bool{"xcrun": true},
		err: map[string]CommandResult{
			"xcrun simctl list devices booted -j": {Stderr: "boom", Err: os.ErrInvalid},
		},
	}
	if !isSimulatorBooted(runner, udid) {
		t.Fatal("isSimulatorBooted must default an unknown state to booted")
	}
	if simulatorAlreadyBooted(runner, udid) {
		t.Fatal("simulatorAlreadyBooted must default an unknown state to NOT booted")
	}
	if simulatorAlreadyBooted(nil, udid) {
		t.Fatal("a nil runner must not read as booted")
	}
}

// hidResult only carries a screen size when it actually rotated, so any
// caller that copies PortraitWidth/PortraitHeight must gate on Rotation != 0
// or hand the gesture worker a 0x0 screen. Pinning the invariant here is what
// makes that gate readable at the call sites (uiDoubleTap, uiDragPath).
func TestHidResultCarriesNoScreenSizeWhenNothingRotated(t *testing.T) {
	const udid = "CCCCCCCC-0000-0000-0000-000000000003" // window angle 0
	portraitTree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}"}]`
	cli, _, _, _ := rotationCLI(t, udid, devicePreferencesDump, portraitTree)
	cfg, err := LoadConfig(cli.Root)
	if err != nil {
		t.Fatal(err)
	}
	hid := cli.hidPoint(context.Background(), cfg, 100, 200)
	if hid.Rotation != 0 {
		t.Fatalf("an unrotated simulator reported a rotation: %+v", hid)
	}
	if hid.PortraitWidth != 0 || hid.PortraitHeight != 0 {
		t.Fatalf("an unrotated hidResult carries a screen size callers would copy: %+v", hid)
	}

	// And when it did rotate, the size is there to be copied.
	rotatedCLI, _, _, _ := rotationCLI(t, "AAAAAAAA-0000-0000-0000-000000000001",
		devicePreferencesDump, `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {874, 402}}"}]`)
	rotatedCfg, err := LoadConfig(rotatedCLI.Root)
	if err != nil {
		t.Fatal(err)
	}
	rotated := rotatedCLI.hidPoint(context.Background(), rotatedCfg, 249, 202)
	if rotated.Rotation != 90 {
		t.Fatalf("expected a 90 rotation, got %+v", rotated)
	}
	if rotated.PortraitWidth != 402 || rotated.PortraitHeight != 874 {
		t.Fatalf("a rotated hidResult lost its screen size: %+v", rotated)
	}
}
