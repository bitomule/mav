package mav

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// foldableDeviceTypes are the simulators with more than one integrated panel.
// Only these pay for a per-panel capture.
var foldableDeviceTypes = map[string]bool{"com.apple.CoreSimulator.SimDeviceType.iPhone-Duo": true}

func isFoldableSimulator(runner Runner, udid string) bool {
	sims, err := ListSimulators(runner)
	if err != nil {
		return false
	}
	for _, sim := range sims {
		if sim.UDID == udid {
			return foldableDeviceTypes[sim.DeviceType]
		}
	}
	return false
}

// integratedScreenIDs reads the Connected Screens of `simctl io enumerate`:
// iPhone Duo lists its cover as 1 and its inner panel as 3 (2 is TV out).
func integratedScreenIDs(ctx context.Context, runner Runner, udid string) []string {
	res := runner.Run(ctx, "xcrun", "simctl", "io", udid, "enumerate")
	if res.Err != nil {
		return nil
	}
	var ids []string
	pending := ""
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if id, ok := strings.CutPrefix(line, "Screen ID:"); ok {
			pending = strings.TrimSpace(id)
			continue
		}
		if kind, ok := strings.CutPrefix(line, "Screen Type:"); ok {
			if strings.TrimSpace(kind) == "Integrated" && pending != "" {
				ids = append(ids, pending)
			}
			pending = ""
		}
	}
	return ids
}

// The hinge angle at which SpringBoard swaps panels, as baguette reads it.
const hingePanelSwapDegrees = 90.0

func declaredHingePath(root, udid string) string {
	return filepath.Join(root, MavDir, "hinge", udid)
}

// writeDeclaredHinge records the angle mav itself moved the hinge to. It is
// the only reliable account of which panel is lit: measured on 2026-10-01 with
// the Duo flat, `devicectl device info displays` still marked the cover
// active and `baguette hinge` read the angle as null.
func writeDeclaredHinge(root, udid string, angle float64) {
	path := declaredHingePath(root, udid)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strconv.FormatFloat(angle, 'f', -1, 64)), 0o644)
}

func readDeclaredHinge(root, udid string) (float64, bool) {
	data, err := os.ReadFile(declaredHingePath(root, udid))
	if err != nil {
		return 0, false
	}
	angle, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	return angle, err == nil
}

func clearDeclaredHinge(root, udid string) {
	_ = os.Remove(declaredHingePath(root, udid))
}

func poseAngle(pose string) float64 {
	switch pose {
	case "open":
		return 130
	case "flat":
		return 180
	}
	return 0
}

// captureLitPanel captures the panel that is lit. With a declared hinge it
// captures that panel directly: the cover is the lowest integrated screen id.
// Without one it captures every panel and keeps the least black, which is
// only a fallback, because a panel that went dark keeps its last frame.
func (c CLI) captureLitPanel(ctx context.Context, udid, path string) (string, error) {
	ids := integratedScreenIDs(ctx, c.Runner, udid)
	if len(ids) < 2 {
		return "", errors.New("fewer than two integrated panels")
	}
	sort.Slice(ids, func(i, j int) bool {
		a, _ := strconv.Atoi(ids[i])
		b, _ := strconv.Atoi(ids[j])
		return a < b
	})
	if angle, ok := readDeclaredHinge(c.Root, udid); ok {
		id := ids[0]
		if angle >= hingePanelSwapDegrees {
			id = ids[1]
		}
		res := c.Runner.Run(ctx, "xcrun", "simctl", "io", udid, "screenshot", "--display="+id, path)
		if res.Err != nil {
			return "", res.Err
		}
		return id, nil
	}
	best, bestID, bestLit := "", "", -1.0
	for _, id := range ids {
		candidate := path + ".display-" + id + ".png"
		res := c.Runner.Run(ctx, "xcrun", "simctl", "io", udid, "screenshot", "--display="+id, candidate)
		if res.Err != nil {
			continue
		}
		lit := litFraction(candidate)
		if lit > bestLit {
			if best != "" {
				_ = os.Remove(best)
			}
			best, bestID, bestLit = candidate, id, lit
			continue
		}
		_ = os.Remove(candidate)
	}
	if best == "" {
		return "", errors.New("no panel could be captured")
	}
	return bestID, os.Rename(best, path)
}

// foldableOpen is true when mav itself unfolded this simulator past the angle
// where SpringBoard lights the inner panel. A fold made in Device Hub is not
// seen here; mav sim hinge records the pose it applies.
func (c CLI) foldableOpen(cfg Config) bool {
	if targetKind(cfg) != drivers.KindSim || cfg.SimulatorUDID == "" {
		return false
	}
	angle, ok := readDeclaredHinge(c.Root, cfg.SimulatorUDID)
	return ok && angle >= hingePanelSwapDegrees
}

// resolveOnOpenFoldable resolves a simple selector the way `axe tap --label`
// would have, for a tap that has to go out as a point. Settings' sidebar row
// "Accesibilidad" matches twice -- the button and its own label inside it --
// which is one control, not an ambiguity; measured on an open Duo, the strict
// resolver refused it with selector_ambiguous. Matches that all sit inside the
// first one's frame resolve to it; anything else is still ambiguous.
func (c CLI) resolveOnOpenFoldable(ctx context.Context, cfg Config, selector Selector, prefer string) (Element, error) {
	matched, err := c.resolveSelector(ctx, cfg, selector, prefer)
	var ambiguous *ambiguousSelectorError
	if !errors.As(err, &ambiguous) {
		return matched, err
	}
	described, treeErr := c.describeUITree(ctx, cfg, prefer, false)
	if treeErr != nil || described.Result.Err != nil {
		return matched, err
	}
	matches, matchErr := MatchElements(ExtractElements(described.Result.Stdout), selector)
	if matchErr != nil || len(matches) == 0 {
		return matched, err
	}
	fx, fy, fw, fh, ok := parseElementFrame(matches[0].Frame)
	if !ok {
		return matched, err
	}
	for _, m := range matches[1:] {
		x, y, ok := TapPoint(m)
		if !ok || float64(x) < fx || float64(x) > fx+fw || float64(y) < fy || float64(y) > fy+fh {
			return matched, err
		}
	}
	return matches[0], nil
}

// litFraction samples how much of a capture is not black. A panel that is off
// is a framebuffer of zeros.
func litFraction(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return 0
	}
	b := img.Bounds()
	lit, total := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y += 16 {
		for x := b.Min.X; x < b.Max.X; x += 16 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r+g+bl > 0x3000 {
				lit++
			}
			total++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(lit) / float64(total)
}
