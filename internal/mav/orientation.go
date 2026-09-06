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

func clearScreenCache(root, udid string) {
	_ = os.Remove(screenCachePath(root, udid))
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
	w, h, sized := screenExtent(elements)
	if !sized || w <= h {
		return screenCache{}, false
	}
	cached := screenCache{PortraitWidth: int(h), PortraitHeight: int(w), Angle: angle}
	writeScreenCache(c.Root, udid, cached)
	return cached, true
}

// screenExtent is the largest zero-origin frame in a snapshot, which is the
// screen the elements were laid out on. Returns false when the snapshot
// carries no frame that could be one, because "no evidence" must never read
// as "portrait".
func screenExtent(elements []Element) (w, h float64, ok bool) {
	for _, el := range elements {
		ex, ey, ew, eh, parsed := parseElementFrame(el.Frame)
		if !parsed || ex != 0 || ey != 0 {
			continue
		}
		if ew*eh > w*h {
			w, h = ew, eh
		}
	}
	return w, h, w > 0 && h > 0
}

// treeIsLandscape reports that a snapshot was laid out on a landscape screen.
func treeIsLandscape(elements []Element) bool {
	w, h, ok := screenExtent(elements)
	return ok && w > h
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
	w, h, ok := screenExtent(elements)
	return ok && w <= h
}

// hidResult is what hidPoint concluded: the point to dispatch, the rotation
// it applied (0 when none was), the rotation the source claimed regardless of
// whether it could be applied, and where that claim came from.
type hidResult struct {
	X, Y     int
	Rotation int
	Detected int
	Source   string
}

// addRotationFields records that a gesture's coordinates were rotated, and
// where they actually went. Nothing is added when nothing moved, so the
// common portrait result line is unchanged. When a rotation was claimed but
// could not be applied (the screen probe failed or disagreed with the tree
// shape), the caller knows the dispatched point may be in the wrong space and
// that is surfaced instead of a silent plain ok.
func addRotationFields(fields map[string]string, hid hidResult) {
	if hid.Rotation == 0 {
		if hid.Detected != 0 {
			fields["rotation_unavailable"] = strconv.Itoa(hid.Detected)
			fields["next"] = "coordinates were dispatched unrotated; re-run once the app's accessibility tree is available"
		}
		return
	}
	fields["rotation"] = strconv.Itoa(hid.Rotation)
	fields["hid_x"] = strconv.Itoa(hid.X)
	fields["hid_y"] = strconv.Itoa(hid.Y)
	if hid.Source != "" {
		fields["rotation_source"] = hid.Source
	}
}

// addUnknownLandscapeFields says the last tree MAV read was landscape while
// nothing on the machine claims a rotation — the signature of a simulator
// rotated by something that is not Simulator.app (baguette, a raw GSEvent, an
// app that is landscape-only). MAV cannot tell the two landscapes apart from
// the tree alone and they differ by 180 degrees, so it dispatches raw and
// says so rather than guessing and landing every tap in the opposite corner.
func addUnknownLandscapeFields(fields map[string]string) {
	fields["rotation_unavailable"] = "unknown_landscape"
	fields["next"] = "the tree is landscape but no rotation is declared; run `mav ui orientation landscape-left|landscape-right` so mav knows which one"
}

// hidPoint maps a point in the accessibility tree's coordinate space to the
// space idb, baguette and axe dispatch touches in. On an unrotated simulator
// — and on every non-simulator target — it is the identity and costs one
// cheap source read. The applied rotation is 0 whenever nothing had to move,
// so a caller can put `rotation=` on its result line and have it mean
// something.
func (c CLI) hidPoint(ctx context.Context, cfg Config, x, y int) hidResult {
	if targetKind(cfg) != drivers.KindSim {
		return hidResult{X: x, Y: y}
	}
	reading := resolveRotation(c.Runner, c.Root, cfg.SimulatorUDID)
	if reading.Angle == 0 {
		return hidResult{X: x, Y: y}
	}
	// At 180 the tree root is portrait-shaped (h > w) whether the app really
	// flipped upside-down or is portrait-locked and never moved, so the
	// shape check that makes 90/270 safe proves nothing here — and most apps
	// (and SpringBoard on every home-button-less iPhone) never rotate to
	// upside-down at all, in which case mirroring the point sends the tap to
	// the diagonally opposite corner. Short-circuited before the probe
	// because there is no answer the tree could give that would change it.
	if reading.Angle == 180 {
		return hidResult{X: x, Y: y, Detected: reading.Angle, Source: reading.Source}
	}
	screen, ok := c.portraitScreenSize(ctx, cfg, reading.Angle)
	if !ok {
		// Without the screen size there is no correct rotation to apply.
		// Passing the point through unchanged is the old behaviour, which
		// is wrong here but is at least the wrongness callers already
		// compensate for; inventing a size would be a new one.
		return hidResult{X: x, Y: y, Detected: reading.Angle, Source: reading.Source}
	}
	var hx, hy int
	switch reading.Angle {
	case 90:
		hx, hy = screen.PortraitWidth-y, x
	case 270:
		hx, hy = y, screen.PortraitHeight-x
	default:
		return hidResult{X: x, Y: y, Detected: reading.Angle, Source: reading.Source}
	}
	// A point read off the rotated tree always lands inside the portrait
	// touch surface, so one that does not is proof the input was not in the
	// space the angle says it was — a portrait-locked app, a stale angle, a
	// caller passing HID-space constants. Reporting the transform as applied
	// there would stamp rotation= and a negative, off-screen hid_x onto an ok
	// line, which is worse than not rotating: the caller is told the
	// coordinate it can trust is the one that cannot be tapped.
	if hx < 0 || hy < 0 || hx >= screen.PortraitWidth || hy >= screen.PortraitHeight {
		return hidResult{X: x, Y: y, Detected: reading.Angle, Source: reading.Source}
	}
	return hidResult{X: hx, Y: hy, Rotation: reading.Angle, Detected: reading.Angle, Source: reading.Source}
}

// hidVector rotates a displacement rather than a position: a pan or a drag
// delta has no origin to translate, so only the axis swap applies. Rotating
// it as if it were a point would add the screen's width to it and send the
// gesture off the surface.
//
// The rotations are the derivatives of hidPoint's:
//
//	90:  (x, y) -> (W - y, x)   so   (dx, dy) -> (-dy,  dx)
//	270: (x, y) -> (y, H - x)   so   (dx, dy) -> ( dy, -dx)
func hidVector(dx, dy, rotation int) (int, int) {
	switch rotation {
	case 90:
		return -dy, dx
	case 270:
		return dy, -dx
	}
	return dx, dy
}

// directionSwipeRotationNext is the hint a direction-default swipe carries
// when MAV cannot compensate its axis. The coordinates swipeCoordinates
// returns are fixed portrait-space constants, so with no screen size to
// re-derive them from they are dispatched exactly as written — which means
// the drag runs along the wrong axis of the rotated screen.
const directionSwipeRotationNext = "the simulator is rotated and this direction swipe could not be re-derived for it; pass --start-x/--start-y/--end-x/--end-y read from `mav ui tree`"

// directionSwipeRotationAngle reports the rotation a direction-default
// gesture is about to ignore, so the caller can say so instead of returning a
// plain ok for a drag that went sideways.
func (c CLI) directionSwipeRotationAngle(cfg Config) int {
	if targetKind(cfg) != drivers.KindSim {
		return 0
	}
	return resolveRotation(c.Runner, c.Root, cfg.SimulatorUDID).Angle
}

// rotatedDirectionSwipe re-derives a direction swipe's endpoints for a
// rotated screen instead of dispatching the portrait constants sideways. The
// endpoints are expressed as fractions of the CURRENT (rotated) screen, read
// from the same cache the point transform uses, and then pushed through
// hidPoint like any caller-supplied pair — so "up" is up on the screen the
// user is looking at, whichever way it is turned.
//
// The fractions match what swipeCoordinates encodes for a portrait iPhone:
// the long axis runs from 0.87 to 0.30 of the screen for up/down, and the
// short axis from 0.10 to 0.90 for left/right, both centred on the other.
func (c CLI) rotatedDirectionSwipe(ctx context.Context, cfg Config, direction string, angle int) (sx, sy, ex, ey int, ok bool) {
	screen, sized := c.portraitScreenSize(ctx, cfg, angle)
	if !sized {
		return 0, 0, 0, 0, false
	}
	// The tree-space screen is the portrait one turned on its side.
	w, h := screen.PortraitHeight, screen.PortraitWidth
	near, far := 0.87, 0.30
	lo, hi := 0.10, 0.90
	switch direction {
	case "up":
		sx, sy, ex, ey = w/2, int(float64(h)*near), w/2, int(float64(h)*far)
	case "down":
		sx, sy, ex, ey = w/2, int(float64(h)*far), w/2, int(float64(h)*near)
	case "left":
		sx, sy, ex, ey = int(float64(w)*hi), h/2, int(float64(w)*lo), h/2
	case "right":
		sx, sy, ex, ey = int(float64(w)*lo), h/2, int(float64(w)*hi), h/2
	default:
		return 0, 0, 0, 0, false
	}
	start := c.hidPoint(ctx, cfg, sx, sy)
	end := c.hidPoint(ctx, cfg, ex, ey)
	if start.Rotation == 0 || end.Rotation == 0 || start.Rotation != end.Rotation {
		return 0, 0, 0, 0, false
	}
	return start.X, start.Y, end.X, end.Y, true
}
