// Package network provides the network-capture drivers. On simulator,
// mitmproxy is the canonical path: mav arranges CA trust and proxy
// configuration through `xcrun simctl`, then routes traffic through a
// background `mitmdump` instance that writes a HAR file. On physical
// device, MAV does not bundle a capture solution -- users point the
// device manually at a host-running proxy (documented in setup).
package network

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// ID is the registry key for the mitmproxy driver.
const ID = "mitmproxy"

// MitmproxyDriver wraps `mitmdump` for sim network capture.
type MitmproxyDriver struct {
	exec drivers.Executor
	path string
}

// New constructs a Driver.
func New(exec drivers.Executor) *MitmproxyDriver { return &MitmproxyDriver{exec: exec} }

func (d *MitmproxyDriver) ID() string { return ID }

// Provides advertises CapNetworkCapture only on simulator targets. Device
// users are expected to point traffic at an externally-running proxy --
// MAV does not bundle that flow.
func (d *MitmproxyDriver) Provides(target drivers.Target) drivers.CapabilitySet {
	if target.IsDevice() {
		return drivers.NewSet()
	}
	return drivers.NewSet(drivers.CapNetworkCapture)
}

// Cost is 0: mitmproxy is the only network-capture provider on sim.
func (d *MitmproxyDriver) Cost(_ drivers.Capability, _ drivers.Target) int { return 0 }

// Probe checks mitmdump is on PATH.
func (d *MitmproxyDriver) Probe(_ context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("mitmdump")
	if err != nil {
		return drivers.HealthReport{
			State:  drivers.HealthMissing,
			Detail: "mitmdump not on PATH",
			Next:   "mav setup (installs mitmproxy via pipx)",
		}
	}
	d.path = path
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"mitmdump": path}}
}

// Warm has no async work.
func (d *MitmproxyDriver) Warm(_ context.Context, _ drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}

// NetworkStart spawns mitmdump as a background process writing a HAR file.
//
// The HAR output relies on mitmproxy's bundled `har_dump` addon (shipped
// since mitmproxy 10). Listen port defaults to 8080 when spec.ListenPort
// is zero; we additionally pick a free port on the loopback interface so
// parallel MAV runs don't clash.
//
// CA trust and the simulator's proxy plist are NOT touched here -- the
// `mav setup` flow handles those once per machine. NetworkStart assumes
// trust is already established; otherwise mitmdump still records but
// HTTPS traffic shows as CONNECT-tunnel only.
func (d *MitmproxyDriver) NetworkStart(ctx context.Context, target drivers.Target, spec drivers.NetworkCaptureSpec) (drivers.NetworkCaptureResult, error) {
	if spec.OutPath == "" {
		return drivers.NetworkCaptureResult{}, fmt.Errorf("mitmproxy: NetworkCaptureSpec.OutPath required")
	}
	port := spec.ListenPort
	if port == 0 {
		free, err := freePort()
		if err != nil {
			return drivers.NetworkCaptureResult{}, fmt.Errorf("mitmproxy: pick port: %w", err)
		}
		port = free
	}

	args := []string{
		"--listen-port", strconv.Itoa(port),
		"--quiet",
		"--set", "hardump=" + spec.OutPath,
	}
	// Start with no log file: mitmdump writes its own status to stdout and
	// the HAR addon writes the capture file. The Executor's Start helper
	// already streams its output to a log path -- pass empty so it routes
	// through the standard run-state log.
	pid, err := d.exec.Start(ctx, "", "mitmdump", args...)
	if err != nil {
		return drivers.NetworkCaptureResult{}, fmt.Errorf("mitmproxy: start: %w", err)
	}
	return drivers.NetworkCaptureResult{
		PID:        pid,
		OutPath:    spec.OutPath,
		ListenPort: port,
		ProxyURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
	}, nil
}

// NetworkStop sends a clean shutdown to mitmdump (SIGTERM via the
// platform's process model). The caller is responsible for waiting on
// the HAR file to flush -- mitmdump writes it incrementally so the file
// is valid even if interrupted.
func (d *MitmproxyDriver) NetworkStop(ctx context.Context, pid int) error {
	if pid <= 0 {
		return fmt.Errorf("mitmproxy: invalid pid %d", pid)
	}
	// We can't address the process directly through the Executor abstraction
	// (Start returns a PID but there is no Stop). Use `kill -TERM <pid>`
	// via the same executor so test fakes can intercept.
	res := d.exec.Run(ctx, "kill", "-TERM", strconv.Itoa(pid))
	if res.Err != nil {
		return fmt.Errorf("mitmproxy: kill %d: %w (%s)", pid, res.Err, firstLine(res.Stderr))
	}
	return nil
}

// freePort asks the kernel for an unused TCP port on the loopback interface.
// Used when NetworkCaptureSpec.ListenPort is zero so parallel mav runs do
// not collide.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
