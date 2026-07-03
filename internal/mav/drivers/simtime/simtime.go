package simtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

const ID = "simtime"

type Driver struct{ exec drivers.Executor }

var _ drivers.WallClockDriver = (*Driver)(nil)

func New(exec drivers.Executor) *Driver { return &Driver{exec: exec} }
func (d *Driver) ID() string            { return ID }

func (d *Driver) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.Kind != drivers.KindSim {
		return drivers.NewSet()
	}
	return drivers.NewSet(drivers.CapWallClock)
}

func (d *Driver) Cost(drivers.Capability, drivers.Target) int { return 0 }

func (d *Driver) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("simtime")
	if err != nil {
		return drivers.HealthReport{State: drivers.HealthMissing, Detail: "simtime not on PATH", Next: "mav setup --install simtime"}
	}
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"simtime": path}}
}

func (d *Driver) Warm(context.Context, drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

func (d *Driver) args(target drivers.Target, command string, value ...string) []string {
	args := []string{command, "--udid", target.UDID, "--bundle", target.BundleID}
	return append(args, value...)
}

func (d *Driver) run(ctx context.Context, target drivers.Target, command string, value ...string) (string, error) {
	if target.Kind != drivers.KindSim {
		return "", errors.New("time_control_unsupported_on_device")
	}
	result := d.exec.Run(ctx, "simtime", d.args(target, command, value...)...)
	if result.Err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		return "", fmt.Errorf("%s: %s", command, detail)
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (d *Driver) InjectTimeControl(ctx context.Context, target drivers.Target) error {
	_, err := d.run(ctx, target, "inject")
	return err
}

func (d *Driver) FreezeTime(ctx context.Context, target drivers.Target, at string) (string, error) {
	return d.run(ctx, target, "freeze", at)
}

func (d *Driver) TravelTime(ctx context.Context, target drivers.Target, by string) (string, error) {
	return d.run(ctx, target, "travel", by)
}

func (d *Driver) ScaleTime(ctx context.Context, target drivers.Target, factor float64) (string, error) {
	return d.run(ctx, target, "scale", strconv.FormatFloat(factor, 'g', -1, 64))
}

func (d *Driver) TimeStatus(ctx context.Context, target drivers.Target) (string, error) {
	if target.Kind != drivers.KindSim {
		return "", errors.New("time_control_unsupported_on_device")
	}
	result := d.exec.Run(ctx, "simtime", "--udid", target.UDID, "--bundle", target.BundleID)
	if result.Err != nil {
		return "", fmt.Errorf("status: %s", strings.TrimSpace(result.Stderr))
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (d *Driver) ResetTime(ctx context.Context, target drivers.Target) error {
	_, err := d.run(ctx, target, "reset")
	return err
}
