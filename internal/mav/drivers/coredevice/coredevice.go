// Package coredevice supplies physical-device app lifecycle through Apple's
// devicectl. UI input belongs to AXe; this driver needs no idb companion.
package coredevice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

const ID = "coredevice"

type Driver struct {
	exec     drivers.Executor
	tempRoot string
}

var _ drivers.LifecycleDriver = (*Driver)(nil)
var _ drivers.DeviceUtilityDriver = (*Driver)(nil)

// New keeps devicectl's result files inside the project, never in the host's
// shared temporary directory. Callers may instead supply a run's directory.
func New(exec drivers.Executor, tempRoot ...string) *Driver {
	root := filepath.Join(".mav", "tmp", ID)
	if len(tempRoot) > 0 && tempRoot[0] != "" {
		root = tempRoot[0]
	}
	return &Driver{exec: exec, tempRoot: root}
}

func (*Driver) ID() string { return ID }
func (*Driver) Provides(t drivers.Target) drivers.CapabilitySet {
	if !t.IsDevice() {
		return drivers.NewSet()
	}
	return drivers.NewSet(drivers.CapInstall, drivers.CapLaunch, drivers.CapUninstall,
		drivers.CapAppList, drivers.CapTerminate, drivers.CapOpenURL)
}
func (*Driver) Cost(drivers.Capability, drivers.Target) int { return 0 }
func (d *Driver) Probe(ctx context.Context, p drivers.Probe) drivers.HealthReport {
	path, err := p.LookPath("xcrun")
	if err != nil {
		return drivers.HealthReport{State: drivers.HealthMissing, Detail: "xcrun not on PATH", Next: "install Xcode and select it with xcode-select"}
	}
	res := d.exec.Run(ctx, "xcrun", "--find", "devicectl")
	if res.Err != nil {
		return drivers.HealthReport{State: drivers.HealthBroken, Detail: "the selected Xcode has no devicectl", Next: "select a full Xcode installation with xcode-select"}
	}
	return drivers.HealthReport{State: drivers.HealthOK, Tools: map[string]string{"xcrun": path}}
}
func (*Driver) Warm(context.Context, drivers.Target) <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}
func (*Driver) Boot(context.Context, drivers.Target) error {
	return errors.New("coredevice: a physical device cannot be booted by mav")
}

func (d *Driver) Install(ctx context.Context, t drivers.Target, spec drivers.InstallSpec) error {
	if strings.TrimSpace(spec.Path) == "" {
		return errors.New("coredevice: install path missing")
	}
	_, err := d.run(ctx, t, nil, "device", "install", "app", spec.Path)
	return err
}
func (d *Driver) Uninstall(ctx context.Context, t drivers.Target, bundle string) error {
	if strings.TrimSpace(bundle) == "" {
		return errors.New("coredevice: bundle identifier missing")
	}
	_, err := d.run(ctx, t, nil, "device", "uninstall", "app", bundle)
	return err
}
func (d *Driver) Launch(ctx context.Context, t drivers.Target, spec drivers.LaunchSpec) (drivers.LaunchResult, error) {
	return d.launch(ctx, t, spec, "")
}
func (d *Driver) launch(ctx context.Context, t drivers.Target, spec drivers.LaunchSpec, payloadURL string) (drivers.LaunchResult, error) {
	if strings.TrimSpace(spec.BundleID) == "" {
		return drivers.LaunchResult{}, errors.New("coredevice: bundle identifier missing")
	}
	args := []string{"device", "process", "launch"}
	if len(spec.Env) > 0 {
		for key, value := range spec.Env {
			if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
				return drivers.LaunchResult{}, errors.New("coredevice: invalid launch environment")
			}
		}
		env, _ := json.Marshal(spec.Env)
		args = append(args, "--environment-variables", string(env))
	}
	if payloadURL != "" {
		args = append(args, "--payload-url", payloadURL)
	}
	// devicectl consumes everything after the bundle as application arguments,
	// so even its device and output options must precede the bundle.
	appArgs := append([]string{spec.BundleID, "--"}, spec.Args...)
	result, err := d.runCommand(ctx, t, spec.Env, args, appArgs)
	if err != nil {
		return drivers.LaunchResult{}, err
	}
	var data struct {
		Process struct {
			PID int `json:"processIdentifier"`
		} `json:"process"`
	}
	if err := json.Unmarshal(result, &data); err != nil || data.Process.PID <= 0 {
		return drivers.LaunchResult{}, errors.New("coredevice: launch result has no process identifier")
	}
	return drivers.LaunchResult{PID: data.Process.PID, BundleID: spec.BundleID}, nil
}
func (d *Driver) OpenURL(ctx context.Context, t drivers.Target, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		return errors.New("coredevice: URL must have a scheme")
	}
	if t.BundleID == "" {
		return errors.New("coredevice: opening a URL requires the target app's bundle identifier")
	}
	_, err = d.launch(ctx, t, drivers.LaunchSpec{BundleID: t.BundleID}, rawURL)
	return err
}

func (d *Driver) ListApps(ctx context.Context, t drivers.Target) (string, error) {
	result, err := d.run(ctx, t, nil, "device", "info", "apps", "--include-all-apps")
	if err != nil {
		return "", err
	}
	var data struct {
		Apps json.RawMessage `json:"apps"`
	}
	if json.Unmarshal(result, &data) != nil || len(data.Apps) == 0 || data.Apps[0] != '[' {
		return "", errors.New("coredevice: app list result is unreadable")
	}
	return string(data.Apps), nil
}

func (d *Driver) Terminate(ctx context.Context, t drivers.Target, bundle string) error {
	if strings.TrimSpace(bundle) == "" {
		return errors.New("coredevice: bundle identifier missing")
	}
	// Process listings have executable URLs rather than bundle IDs on older
	// Xcodes. Resolve the installed app's exact bundle URL, never its display
	// name: a same-named process may belong to a different app.
	apps, err := d.run(ctx, t, nil, "device", "info", "apps", "--include-all-apps", "--bundle-id", bundle)
	if err != nil {
		return err
	}
	var installed struct {
		Apps []struct {
			BundleID  string `json:"bundleIdentifier"`
			URL       string `json:"url"`
			Path      string `json:"path"`
			BundleURL string `json:"bundleURL"`
		} `json:"apps"`
	}
	if json.Unmarshal(apps, &installed) != nil || installed.Apps == nil {
		return errors.New("coredevice: app list result is unreadable")
	}
	var paths []string
	installedTarget := false
	for _, app := range installed.Apps {
		if app.BundleID != bundle {
			continue
		}
		installedTarget = true
		for _, raw := range []string{app.URL, app.Path, app.BundleURL} {
			if path := remotePath(raw); path != "" {
				paths = append(paths, path)
			}
		}
	}
	result, err := d.run(ctx, t, nil, "device", "info", "processes")
	if err != nil {
		return err
	}
	var processes struct {
		Processes []struct {
			PID        int    `json:"processIdentifier"`
			Executable string `json:"executable"`
			BundleID   string `json:"bundleIdentifier"`
		} `json:"runningProcesses"`
	}
	if json.Unmarshal(result, &processes) != nil || processes.Processes == nil {
		return errors.New("coredevice: process list result is unreadable")
	}
	var pids []int
	for _, process := range processes.Processes {
		matches := process.BundleID == bundle
		for _, path := range paths {
			matches = matches || process.BundleID == "" && strings.HasPrefix(remotePath(process.Executable), strings.TrimRight(path, "/")+"/")
		}
		if !matches {
			continue
		}
		if process.PID <= 0 {
			return errors.New("coredevice: matching process has no process identifier")
		}
		pids = append(pids, process.PID)
	}
	if installedTarget && len(paths) == 0 && len(pids) == 0 {
		for _, process := range processes.Processes {
			if process.BundleID == "" {
				return errors.New("coredevice: cannot identify the app's process from its bundle URL")
			}
		}
	}
	for _, pid := range pids {
		if _, err := d.run(ctx, t, nil, "device", "process", "terminate", "--pid", strconv.Itoa(pid)); err != nil {
			return err
		}
	}
	return nil
}

func remotePath(raw string) string {
	if strings.HasPrefix(raw, "/") {
		return strings.TrimRight(raw, "/")
	}
	u, err := url.Parse(raw)
	if err == nil && u.Scheme == "file" && u.Path != "" {
		return strings.TrimRight(u.Path, "/")
	}
	return ""
}

func (*Driver) SetLocation(context.Context, drivers.Target, float64, float64) error {
	return errors.New("coredevice: location is provided by AXe")
}
func (*Driver) ResetLocation(context.Context, drivers.Target) error {
	return errors.New("coredevice: location is provided by AXe")
}
func (*Driver) ClipboardWrite(context.Context, drivers.Target, string) error {
	return errors.New("coredevice: physical-device clipboard is unsupported")
}
func (*Driver) ClipboardRead(context.Context, drivers.Target) (string, error) {
	return "", errors.New("coredevice: physical-device clipboard is unsupported")
}

// run reads only the documented JSON result. Human output is not a data API.
// The private result directory is discarded even on a failed invocation.
func (d *Driver) run(ctx context.Context, t drivers.Target, secrets map[string]string, args ...string) (json.RawMessage, error) {
	return d.runCommand(ctx, t, secrets, args, nil)
}

func (d *Driver) runCommand(ctx context.Context, t drivers.Target, secrets map[string]string, args, appArgs []string) (json.RawMessage, error) {
	if !t.IsDevice() || strings.TrimSpace(t.UDID) == "" {
		return nil, errors.New("coredevice: an explicit physical-device UDID is required")
	}
	if err := os.MkdirAll(d.tempRoot, 0700); err != nil {
		return nil, fmt.Errorf("coredevice: create result directory: %w", err)
	}
	dir, err := os.MkdirTemp(d.tempRoot, "result-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "result.json")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		return nil, err
	}
	// Keep tool options separate from the launch's bundle and app arguments.
	options := []string{"--device", t.UDID, "--json-output", path}
	command := append([]string{"devicectl"}, args...)
	command = append(command, options...)
	command = append(command, appArgs...)
	res := d.exec.Run(ctx, "xcrun", command...)
	if res.Err != nil {
		message := strings.TrimSpace(res.Stderr)
		if message == "" {
			message = res.Err.Error()
		}
		// devicectl can echo its argv (including the environment dictionary).
		if len(secrets) > 0 {
			message = "app launch failed; environment values withheld"
		}
		if line, _, ok := strings.Cut(message, "\n"); ok {
			message = line
		}
		return nil, fmt.Errorf("coredevice: %s", message)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("coredevice: command did not write its JSON result")
	}
	var envelope struct {
		Info struct {
			Outcome string `json:"outcome"`
		} `json:"info"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Result) == 0 || envelope.Result[0] != '{' {
		return nil, errors.New("coredevice: command result is unreadable")
	}
	if envelope.Info.Outcome != "" && envelope.Info.Outcome != "success" {
		return nil, errors.New("coredevice: command reported failure")
	}
	return envelope.Result, nil
}
