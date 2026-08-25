package macos

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// AxcliID is the driver's registry key.
const AxcliID = "axcli"

// Axcli wraps axcli, which is in the mix for one reason only: it delivers
// events to the target process with CGEventPostToPid, without activating the
// app, without moving the cursor and without jumping Spaces. If an agent
// validates while you work, that is not a comfort detail: it is the
// difference between being able to use the Mac and not.
//
// It stays as an escape hatch next to cua-driver, which is the canonical
// one. The reason is concrete: cua-driver resolves the window through
// `list_windows`, which only enumerates layer 0, so a floating UI, a panel,
// HUD, popover, an onboarding, is invisible to it. axcli targets by `--app`
// and needs no window id, so it reaches exactly those windows.
//
// Its capture is NOT declared, and that is not an omission: it returned the
// desktop cropped to the window's dimensions, with no error, when the
// process has no graphical session, and it also activated the app. A
// plausible but false PNG is worse than having no capture.
type Axcli struct {
	exec drivers.Executor
}

var (
	_ drivers.TapDriver  = (*Axcli)(nil)
	_ drivers.TypeDriver = (*Axcli)(nil)
)

// NewAxcli builds the driver.
func NewAxcli(exec drivers.Executor) *Axcli { return &Axcli{exec: exec} }

func (d *Axcli) ID() string { return AxcliID }

func (d *Axcli) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindMac {
		return drivers.NewSet()
	}
	return drivers.NewSet(
		drivers.CapCoordTap,
		drivers.CapSemanticTap,
		drivers.CapType,
	)
}

// Cost is where the split with Peekaboo is expressed, with no new traits on
// the Driver interface.
//
// Taps are cost 0: they go through CGEventPostToPid, axcli's default for
// click and scroll, and they do not steal focus.
//
// Typing is NOT cost 0, and this is easy to get wrong: `input` and `fill`
// activate the app before typing, they do it in the code, with no flag to
// avoid it, so there axcli is no better than Peekaboo. It is declared
// equally expensive so the router does not prefer it believing it gains
// something.
func (d *Axcli) Cost(c drivers.Capability, _ drivers.Target) int {
	switch c {
	case drivers.CapCoordTap, drivers.CapSemanticTap:
		// Behind cua-driver (0), and not for being worse at delivery but for
		// how it presses: cg-pid synthesizes a mouse event, and there are
		// buttons, SwiftUI, measured, that accept it without reacting.
		// cua-driver's click goes through AXPress when the element exposes
		// it, which does take effect on those same buttons. axcli remains
		// the only path when cua cannot resolve the window.
		return 10
	case drivers.CapType:
		return 60
	default:
		return 100
	}
}

// Probe checks the binary. axcli has no diagnostic command: it verifies
// AXIsProcessTrusted when starting any targeted command and dies with
// `error: accessibility not granted`. It is not invoked here on purpose,
// asking a real app for permission just to probe would have side effects,
// so the TCC state is reported by Peekaboo, which does know how to ask
// without touching anything.
func (d *Axcli) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("axcli")
	if err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "axcli not on PATH",
			Next:   "mav setup --install axcli",
		}
	}
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"axcli": path}}
}

func (d *Axcli) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// axcliTargetArgs points at the app. axcli demands --app or --pid on every
// targeted command.
func axcliTargetArgs(target drivers.Target) ([]string, error) {
	if target.PID > 0 {
		return []string{"--pid", strconv.Itoa(target.PID)}, nil
	}
	if target.BundleID != "" {
		return []string{"--app", target.BundleID}, nil
	}
	return nil, errors.New("axcli: no app to target; set bundle_id or resolve the pid")
}

// axcliError translates the failure. axcli returns exit 1 for everything and
// writes `error: <message>` to stderr, so the reason lives only in the text.
// No attempt is made to classify it by substring beyond the essential: those
// messages are theirs and they can change them whenever they want.
func axcliError(res drivers.ExecResult) error {
	detail := strings.TrimSpace(res.Stderr)
	for _, line := range strings.Split(detail, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "error:") {
			return errors.New(strings.TrimSpace(strings.TrimPrefix(line, "error:")))
		}
	}
	if detail == "" {
		return errors.New("axcli: command failed")
	}
	return errors.New(firstLine(detail))
}

// Tap clicks without stealing focus.
func (d *Axcli) Tap(ctx context.Context, target drivers.Target, spec drivers.TapSpec) (drivers.TapResult, error) {
	base, err := axcliTargetArgs(target)
	if err != nil {
		return drivers.TapResult{}, err
	}
	// The software cursor axcli draws by default is useful for a watching
	// human and noise for an agent: it contradicts exactly what is asked of
	// it, going unnoticed.
	// --strategy cg-pid is EXPLICIT even though it is the default. Two
	// reasons: it leaves written in the command which property mav depends
	// on, and if a future version changes the default, this fails instead
	// of silently starting to steal focus.
	//
	// And it is not theoretical: the 0.1.0 published on crates.io does not
	// even have this option, its click calls activate() and clicks by
	// coordinates moving the real cursor. That is why the formula points at
	// a commit of main and not at the published version.
	//
	// --app/--pid are subcommand flags, not global ones.
	args := []string{"click", "--strategy", "cg-pid"}
	args = append(args, base...)
	switch {
	case spec.Selector.ID != "":
		args = append(args, `[identifier="`+spec.Selector.ID+`"]`)
	case spec.Selector.Text != "":
		args = append(args, `text="`+spec.Selector.Text+`"`)
	case spec.X != 0 || spec.Y != 0:
		// `mouse click` is global and ignores --app: it moves the real
		// cursor and fires on whatever window is on top. It stops being
		// background-safe, so this says so instead of pretending it is.
		return drivers.TapResult{}, errors.New("axcli: coordinate taps are not background-safe; use a selector")
	default:
		return drivers.TapResult{}, errors.New("axcli: tap requires an id or text selector")
	}
	if res := d.exec.Run(ctx, "axcli", args...); res.Err != nil {
		return drivers.TapResult{}, axcliError(res)
	}
	return drivers.TapResult{MatchedID: spec.Selector.ID, MatchedText: spec.Selector.Text}, nil
}

// Type writes into a specific element. Beware: this DOES activate the app,
// `fill` calls activate() before typing and there is no way around it, hence
// Cost declares it expensive.
func (d *Axcli) Type(ctx context.Context, target drivers.Target, spec drivers.TextSpec) error {
	if spec.Text == "" {
		return errors.New("axcli: type text missing")
	}
	base, err := axcliTargetArgs(target)
	if err != nil {
		return err
	}
	args := []string{"fill"}
	args = append(args, base...)
	switch {
	case spec.Selector.ID != "":
		args = append(args, `[identifier="`+spec.Selector.ID+`"]`, spec.Text)
	case spec.Selector.Text != "":
		args = append(args, `text="`+spec.Selector.Text+`"`, spec.Text)
	default:
		return errors.New("axcli: typing requires a selector; use cua-driver to type into the focused element")
	}
	if res := d.exec.Run(ctx, "axcli", args...); res.Err != nil {
		return axcliError(res)
	}
	return nil
}
