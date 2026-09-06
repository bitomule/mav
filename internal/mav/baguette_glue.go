package mav

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// driverRegistry builds the registry this invocation routes against.
// Separate from router() because --prefer-driver validation needs to ask
// WHICH drivers exist before there is anything to route.
func (c CLI) driverRegistry() *drivers.Registry {
	reg := drivers.NewRegistry()
	RegisterDefaultDrivers(reg, NewExecutor(c.Runner))
	return reg
}

// router lazily builds the router for this CLI invocation. cli.go uses a value
// receiver everywhere, so the router is built once per call site that needs it
// rather than cached on CLI. That keeps the existing shape simple.
func (c CLI) router() *drivers.Router {
	disabled := strings.Split(os.Getenv("MAV_DRIVERS_DISABLE"), ",")
	return drivers.NewRouter(c.driverRegistry(), NewExecutor(c.Runner), disabled)
}

// routerWithout is router() with one more driver taken out of the running,
// for the case where a driver is installed and healthy but cannot serve this
// particular call. Preferring a different driver is not enough: a preference
// only breaks ties, so a driver that is canonical (cost 0) for the capability
// still wins.
func (c CLI) routerWithout(driverID string) *drivers.Router {
	disabled := append(strings.Split(os.Getenv("MAV_DRIVERS_DISABLE"), ","), driverID)
	return drivers.NewRouter(c.driverRegistry(), NewExecutor(c.Runner), disabled)
}

// targetFromConfig projects the cfg fields the drivers need onto Target.
func targetFromConfig(cfg Config) drivers.Target {
	target := drivers.Target{
		Kind:     targetKind(cfg),
		BundleID: cfg.BundleID,
		Locale:   cfg.Locale,
		Language: cfg.Language,
	}
	switch target.Kind {
	case drivers.KindDevice:
		target.UDID = cfg.DeviceUDID
		target.Name = cfg.DeviceName
	case drivers.KindMac:
		// No UDID: a macOS app's identity is its bundle and its path.
		target.Name = "localhost"
		target.AppPath = cfg.AppPath
	default:
		target.UDID = cfg.SimulatorUDID
		target.Name = cfg.SimulatorName
		target.Runtime = cfg.SimulatorRuntime
	}
	return target
}

// baguetteTree asks the router for the system/SpringBoard tree (sim only). The
// caller is responsible for surfacing tree_system_unsupported_on_device when
// target.IsDevice() — the router would reject for missing capability anyway,
// but cli.go wants the friendlier error early.
func baguetteTree(ctx context.Context, router *drivers.Router, target drivers.Target, includeSystem bool) (string, error) {
	driver, _, err := router.Route(ctx, drivers.CapTreeSystem, target, "")
	if err != nil {
		return "", err
	}
	td, ok := driver.(drivers.TreeDriver)
	if !ok {
		return "", fmt.Errorf("driver %q does not implement TreeDriver", driver.ID())
	}
	res, err := td.Tree(ctx, target, drivers.TreeSpec{IncludeSystem: includeSystem})
	if err != nil {
		return "", err
	}
	return string(res.JSON), nil
}

func baguetteTap(ctx context.Context, router *drivers.Router, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	cap := drivers.CapCoordTap
	if !spec.Selector.IsZero() {
		cap = drivers.CapSemanticTap
	}
	driver, _, err := router.Route(ctx, cap, target, "")
	if err != nil {
		return drivers.TapResult{}, err
	}
	td, ok := driver.(drivers.TapDriver)
	if !ok {
		return drivers.TapResult{}, fmt.Errorf("driver %q does not implement TapDriver", driver.ID())
	}
	return td.Tap(ctx, target, spec)
}

func baguettePinch(ctx context.Context, router *drivers.Router, target drivers.Target, spec drivers.PinchSpec) error {
	driver, _, err := router.Route(ctx, drivers.CapPinch, target, "")
	if err != nil {
		return err
	}
	gd, ok := driver.(drivers.GestureDriver)
	if !ok {
		return fmt.Errorf("driver %q does not implement GestureDriver", driver.ID())
	}
	return gd.Pinch(ctx, target, spec)
}

func baguetteRotate(ctx context.Context, router *drivers.Router, target drivers.Target, spec drivers.RotateSpec) error {
	driver, _, err := router.Route(ctx, drivers.CapRotate, target, "")
	if err != nil {
		return err
	}
	gd, ok := driver.(drivers.GestureDriver)
	if !ok {
		return fmt.Errorf("driver %q does not implement GestureDriver", driver.ID())
	}
	return gd.Rotate(ctx, target, spec)
}

func baguetteTwoFingerPan(ctx context.Context, router *drivers.Router, target drivers.Target, spec drivers.TwoFingerPanSpec) error {
	driver, _, err := router.Route(ctx, drivers.CapTwoFingerPan, target, "")
	if err != nil {
		return err
	}
	gd, ok := driver.(drivers.GestureDriver)
	if !ok {
		return fmt.Errorf("driver %q does not implement GestureDriver", driver.ID())
	}
	return gd.TwoFingerPan(ctx, target, spec)
}

func baguetteW3CActions(ctx context.Context, router *drivers.Router, target drivers.Target, body []byte) error {
	driver, _, err := router.Route(ctx, drivers.CapW3CActions, target, "")
	if err != nil {
		return err
	}
	gd, ok := driver.(drivers.GestureDriver)
	if !ok {
		return fmt.Errorf("driver %q does not implement GestureDriver", driver.ID())
	}
	return gd.W3CActions(ctx, target, body)
}

func baguetteErase(ctx context.Context, router *drivers.Router, target drivers.Target, spec drivers.TextSpec) error {
	driver, _, err := router.Route(ctx, drivers.CapErase, target, "")
	if err != nil {
		return err
	}
	td, ok := driver.(drivers.TextDriver)
	if !ok {
		return fmt.Errorf("driver %q does not implement TextDriver", driver.ID())
	}
	return td.Erase(ctx, target, spec)
}

func baguetteHideKeyboard(ctx context.Context, router *drivers.Router, target drivers.Target) error {
	driver, _, err := router.Route(ctx, drivers.CapHideKeyboard, target, "")
	if err != nil {
		return err
	}
	td, ok := driver.(drivers.TextDriver)
	if !ok {
		return fmt.Errorf("driver %q does not implement TextDriver", driver.ID())
	}
	return td.HideKeyboard(ctx, target)
}

// loadW3CActionsBody reads a W3C Actions JSON file and returns the raw bytes
// passed straight to the baguette translator. Path is resolved relative to
// the project root.
func loadW3CActionsBody(root, path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("actions_file_read_failed")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("actions_payload_invalid")
	}
	return data, nil
}

// --- gesture parsing helpers --------------------------------------------

type gestureParams struct {
	Kind     string
	X        string
	Y        string
	Scale    string
	PanX     string
	PanY     string
	Distance string
	Angle    string
	Rotate   string
	Degrees  string
	Duration string
	Hold     string
}

func gestureParamsFromArgs(args []string) gestureParams {
	return gestureParams{
		X:        flagValue(args, "--x"),
		Y:        flagValue(args, "--y"),
		Scale:    flagValue(args, "--scale"),
		PanX:     flagValue(args, "--pan-x"),
		PanY:     flagValue(args, "--pan-y"),
		Distance: flagValue(args, "--distance"),
		Angle:    flagValue(args, "--angle"),
		Rotate:   flagValue(args, "--rotate"),
		Degrees:  flagValue(args, "--degrees"),
		Duration: flagValue(args, "--duration"),
		Hold:     flagValue(args, "--hold"),
	}
}

func parseRequiredFloat(value, name string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s_missing", name)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s_invalid", name)
	}
	return parsed, nil
}

func parseOptionalFloat(value string, fallback float64, name string) (float64, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s_invalid", name)
	}
	return parsed, nil
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// _ keeps the time import used when we revisit gesture defaults; remove if
// the file ever stops depending on time elsewhere.
var _ = time.Millisecond
