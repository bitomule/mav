package mav

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// Simulator.app rotates the window, not the touch surface. idb and axe
// dispatch HID events in the device's native portrait point space whatever
// the window is doing, while the accessibility tree — the only place a caller
// gets coordinates from — reports the rotated space. So on a simulator that
// has been rotated, a point read off `mav ui tree` and handed straight to
// `mav ui tap --x --y` lands somewhere else entirely, and every flow had to
// carry the rotation by hand.
//
// The rotation Simulator.app applied is a user default it keeps per device.
// Reading it costs ~13ms, and when it is 0 — every headless run, every
// simulator nobody rotated — nothing else is read and no coordinate changes.
const simulatorPrefsDomain = "com.apple.iphonesimulator"

// screenCache is the device's native portrait size in points. It never
// changes for a given UDID, so it is probed once (from the accessibility
// tree, the only source that speaks points) and kept next to the project's
// other state. Angle is the rotation the probe ran under: the size is
// trustworthy only while that is still the rotation in effect, because the
// probe's tree-shape check is what proved the tree really was reporting the
// rotated space.
type screenCache struct {
	PortraitWidth  int `json:"portrait_width"`
	PortraitHeight int `json:"portrait_height"`
	Angle          int `json:"angle"`
}

func screenCachePath(root, udid string) string {
	return filepath.Join(root, MavDir, "screens", udid+".json")
}

func readScreenCache(root, udid string) (screenCache, bool) {
	data, err := os.ReadFile(screenCachePath(root, udid))
	if err != nil {
		return screenCache{}, false
	}
	var cached screenCache
	if json.Unmarshal(data, &cached) != nil || cached.PortraitWidth <= 0 || cached.PortraitHeight <= 0 {
		return screenCache{}, false
	}
	return cached, true
}

func writeScreenCache(root, udid string, cached screenCache) {
	path := screenCachePath(root, udid)
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// simulatorRotationAngle reports the rotation Simulator.app is showing for
// this device, in degrees clockwise from portrait: 0, 90, 180 or 270. A
// device Simulator.app has never rotated has no entry at all, which is the
// same thing as 0. Anything unreadable is also 0, because the fallback has to
// be "leave the coordinates alone" — that is what MAV did before this
// existed, and a wrong non-zero angle would break portrait runs, which are
// almost all of them.
func simulatorRotationAngle(runner Runner, udid string) int {
	if udid == "" {
		return 0
	}
	// `defaults read` goes through cfprefsd, so it sees a rotation the
	// moment it happens. Reading the plist file directly does not: cfprefsd
	// flushes it lazily, and MAV would act on a stale angle.
	res := runner.Run(context.Background(), "defaults", "read", simulatorPrefsDomain, "DevicePreferences")
	if res.Err != nil {
		return 0
	}
	return parseRotationAngle(res.Stdout, udid)
}

// parseRotationAngle picks one device's SimulatorWindowRotationAngle out of
// the old-style plist `defaults read` prints. The blocks nest (window
// geometry is a dictionary of its own), so the end of a device's block is
// found by the next device key rather than by the next closing brace.
func parseRotationAngle(dump, udid string) int {
	start := strings.Index(dump, `"`+udid+`"`)
	if start < 0 {
		return 0
	}
	block := dump[start:]
	// A device key is quoted at the same indentation for every entry; the
	// first one after this device's own key ends its block.
	if next := strings.Index(block[1:], "\n    \""); next >= 0 {
		block = block[:next+1]
	}
	const key = "SimulatorWindowRotationAngle"
	at := strings.Index(block, key)
	if at < 0 {
		return 0
	}
	rest := block[at+len(key):]
	rest = strings.TrimLeft(rest, " =")
	end := strings.IndexAny(rest, ";\n")
	if end < 0 {
		return 0
	}
	// The value's shape varies by rotation: Simulator.app writes 90 bare
	// but LandscapeRight as the quoted string "-90", and PlistBuddy renders
	// the same key as 90.000000. All three have to parse, and a quoted -90
	// falling through to 0 would leave exactly one of the two landscapes
	// broken -- the one the original report came from.
	value := strings.TrimSpace(rest[:end])
	value = strings.Trim(value, `"`)
	if dot := strings.Index(value, "."); dot >= 0 {
		value = value[:dot]
	}
	angle, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	switch ((angle % 360) + 360) % 360 {
	case 90:
		return 90
	case 180:
		return 180
	case 270:
		return 270
	}
	return 0
}

// portraitScreenSize returns the device's native portrait size in points,
// probing the accessibility tree once per UDID and caching the result. It is
// only ever called with a 90 or 270 angle (hidPoint short-circuits 0 and
// 180), but that report can be stale or the app under test can be
// portrait-locked, so a fresh probe always checks the observed tree-root
// shape against the angle before it is trusted or cached: a 90/270 angle
// requires a landscape (w > h) tree root. A mismatch means the tree is not
// actually reporting the rotated space, so nothing is cached and the caller
// must not rotate anything either.
//
// A cache entry is reused without re-probing only while the angle it was
// probed under is still the angle in effect, which keeps the hot path to one
// probe per UDID per rotation. Trusting it across a rotation change would
// re-open the hole the shape check exists to close: the cached size stays
// right, but nothing would have re-checked whether the tree is still
// reporting the rotated space, and a portrait tree read under a 90 angle
// rotates to an off-screen point.
func (c CLI) portraitScreenSize(ctx context.Context, cfg Config, angle int) (screenCache, bool) {
	udid := cfg.SimulatorUDID
	if udid == "" {
		return screenCache{}, false
	}
	if cached, ok := readScreenCache(c.Root, udid); ok && cached.Angle == angle {
		return cached, true
	}
	described, err := c.describeUITree(ctx, cfg, "auto", false)
	if err != nil || described.Result.Err != nil {
		return screenCache{}, false
	}
	elements := ExtractElementsRaw(described.Result.Stdout)
	if len(elements) == 0 {
		return screenCache{}, false
	}
	// elements[0] is the first pre-order node that survives dedup, which is
	// usually the screen root but is not guaranteed to be (a node with no
	// AXFrame, or one that does not cover the whole screen, can sort first).
	// The largest zero-origin frame is the screen extent regardless of
	// where it falls in the list.
	var w, h float64
	for _, el := range elements {
		ex, ey, ew, eh, ok := parseElementFrame(el.Frame)
		if !ok || ex != 0 || ey != 0 {
			continue
		}
		if ew*eh > w*h {
			w, h = ew, eh
		}
	}
	if w <= 0 || h <= 0 {
		return screenCache{}, false
	}
	if w <= h {
		return screenCache{}, false
	}
	cached := screenCache{PortraitWidth: int(h), PortraitHeight: int(w), Angle: angle}
	writeScreenCache(c.Root, udid, cached)
	return cached, true
}

// treeShapeContradictsRotation reports that a snapshot's screen extent is
// portrait-shaped even though a 90/270 rotation was applied to the gesture.
// The screen-size cache only re-probes on an angle change, so a foreground
// app that went portrait-locked under the same angle sails through on a
// cache hit; a caller that already paid for a tree read (the --verify
// snapshot) can use it to catch exactly that case for free. Only a positive
// proof counts: an empty or frameless snapshot proves nothing and must not
// downgrade a rotation the probe validated.
func treeShapeContradictsRotation(elements []Element, rotation int) bool {
	if rotation != 90 && rotation != 270 {
		return false
	}
	var w, h float64
	for _, el := range elements {
		ex, ey, ew, eh, ok := parseElementFrame(el.Frame)
		if !ok || ex != 0 || ey != 0 {
			continue
		}
		if ew*eh > w*h {
			w, h = ew, eh
		}
	}
	if w <= 0 || h <= 0 {
		return false
	}
	return w <= h
}

// addRotationFields records that a gesture's coordinates were rotated, and
// where they actually went. Nothing is added when nothing moved, so the
// common portrait result line is unchanged. When Simulator.app reports a
// rotation but it could not be applied (the screen probe failed or
// disagreed with the tree shape), the caller knows the dispatched point may
// be in the wrong space and that is surfaced instead of a silent plain ok.
func addRotationFields(fields map[string]string, rotation, hidX, hidY, detectedAngle int) {
	if rotation == 0 {
		if detectedAngle != 0 {
			fields["rotation_unavailable"] = strconv.Itoa(detectedAngle)
			fields["next"] = "coordinates were dispatched unrotated; re-run once the app's accessibility tree is available"
		}
		return
	}
	fields["rotation"] = strconv.Itoa(rotation)
	fields["hid_x"] = strconv.Itoa(hidX)
	fields["hid_y"] = strconv.Itoa(hidY)
}

// hidPoint maps a point in the accessibility tree's coordinate space to the
// space idb and axe dispatch touches in. On an unrotated simulator — and on
// every non-simulator target — it is the identity and costs one `defaults
// read`. The reported angle is 0 whenever nothing had to move, so a caller
// can put `rotation=` on its result line and have it mean something. The
// fourth return value is the angle Simulator.app reported regardless of
// whether it could be applied, so a caller can tell "nothing to rotate"
// (0, 0) apart from "rotation detected but not applied" (0, detected != 0).
func (c CLI) hidPoint(ctx context.Context, cfg Config, x, y int) (int, int, int, int) {
	if targetKind(cfg) != drivers.KindSim {
		return x, y, 0, 0
	}
	angle := simulatorRotationAngle(c.Runner, cfg.SimulatorUDID)
	if angle == 0 {
		return x, y, 0, 0
	}
	// At 180 the tree root is portrait-shaped (h > w) whether the app really
	// flipped upside-down or is portrait-locked and never moved, so the
	// shape check that makes 90/270 safe proves nothing here — and most
	// apps (and SpringBoard on every home-button-less iPhone) never rotate
	// to upside-down at all, in which case mirroring the point sends the
	// tap to the diagonally opposite corner. No transform is applied until
	// a proof stronger than the tree shape exists.
	if angle == 180 {
		return x, y, 0, angle
	}
	screen, ok := c.portraitScreenSize(ctx, cfg, angle)
	if !ok {
		// Without the screen size there is no correct rotation to apply.
		// Passing the point through unchanged is the old behaviour, which
		// is wrong here but is at least the wrongness callers already
		// compensate for; inventing a size would be a new one.
		return x, y, 0, angle
	}
	hx, hy := x, y
	switch angle {
	case 90:
		hx, hy = screen.PortraitWidth-y, x
	case 270:
		hx, hy = y, screen.PortraitHeight-x
	default:
		return x, y, 0, angle
	}
	// A point read off the rotated tree always lands inside the portrait
	// touch surface, so one that does not is proof the input was not in the
	// space the angle says it was — a portrait-locked app, a stale angle, a
	// caller passing HID-space constants. Reporting the transform as applied
	// there would stamp rotation= and a negative, off-screen hid_x onto an ok
	// line, which is worse than not rotating: the caller is told the
	// coordinate it can trust is the one that cannot be tapped.
	if hx < 0 || hy < 0 || hx >= screen.PortraitWidth || hy >= screen.PortraitHeight {
		return x, y, 0, angle
	}
	return hx, hy, angle, angle
}

// directionSwipeRotationNext is the hint a direction-default swipe carries on
// a rotated simulator. The coordinates swipeCoordinates returns are fixed
// portrait-HID-space constants, so they are dispatched exactly as written —
// which means the drag runs along the wrong axis of the rotated screen, and
// no rotation can fix that because there is no tree-space input to rotate.
const directionSwipeRotationNext = "direction swipes use fixed portrait-space coordinates and are not axis-compensated on a rotated simulator; pass --start-x/--start-y/--end-x/--end-y read from `mav ui tree`"

// directionSwipeRotationAngle reports the rotation a direction-default
// gesture is about to ignore, so the caller can say so instead of returning a
// plain ok for a drag that went sideways.
func (c CLI) directionSwipeRotationAngle(cfg Config) int {
	if targetKind(cfg) != drivers.KindSim {
		return 0
	}
	return simulatorRotationAngle(c.Runner, cfg.SimulatorUDID)
}
