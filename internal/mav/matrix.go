package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type matrixTarget struct {
	Kind    string `json:"kind"`
	UDID    string `json:"udid"`
	Name    string `json:"name"`
	Runtime string `json:"runtime,omitempty"`
}

type matrixResult struct {
	Target   matrixTarget `json:"target"`
	Status   string       `json:"status"`
	ExitCode int          `json:"exitCode"`
	Dir      string       `json:"dir"`
	Output   string       `json:"output,omitempty"`
	Elapsed  string       `json:"elapsed"`
}

type matrixManifest struct {
	ID      string         `json:"id"`
	Flow    string         `json:"flow"`
	Started time.Time      `json:"started"`
	Results []matrixResult `json:"results"`
}

// stripMatrixFlags removes matrix-only flags before re-exec'ing each target
// as its own `mav run` child. --run is stripped too: each child already gets
// a unique, unambiguous run directory via MAV_EXACT_RUN_DIR (set per target
// below), and forwarding --run as-is would make every child adopt the same
// run.ID while writing to different directories -- distinct runs on disk
// that look identical by id to anything comparing runs by ID.
func stripMatrixFlags(args []string) []string {
	out := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--target" || args[i] == "--jobs" || args[i] == "--run" {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--target=") || strings.HasPrefix(args[i], "--jobs=") || strings.HasPrefix(args[i], "--run=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func (c CLI) resolveMatrixTargets(ctx context.Context, selectors []string) ([]matrixTarget, error) {
	sims, simErr := ListSimulators(c.Runner)
	devices, deviceErr := ListPhysicalDevices(ctx, c.Runner)
	if simErr != nil && deviceErr != nil {
		return nil, fmt.Errorf("target_discovery_failed")
	}
	var resolved []matrixTarget
	seen := map[string]bool{}
	for _, selector := range selectors {
		var matches []matrixTarget
		for _, sim := range sims {
			if sim.UDID == selector {
				matches = []matrixTarget{{Kind: "simulator", UDID: sim.UDID, Name: sim.Name, Runtime: sim.Runtime}}
				break
			}
			if sim.Name == selector {
				matches = append(matches, matrixTarget{Kind: "simulator", UDID: sim.UDID, Name: sim.Name, Runtime: sim.Runtime})
			}
		}
		if len(matches) == 0 || matches[0].UDID != selector {
			for _, device := range devices {
				if device.UDID == selector {
					matches = []matrixTarget{{Kind: "device", UDID: device.UDID, Name: device.Name}}
					break
				}
				if device.Name == selector {
					duplicate := false
					for _, match := range matches {
						duplicate = duplicate || match.UDID == device.UDID
					}
					if !duplicate {
						matches = append(matches, matrixTarget{Kind: "device", UDID: device.UDID, Name: device.Name})
					}
				}
			}
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("target_not_found target=%s", selector)
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("target_ambiguous target=%s matches=%d", selector, len(matches))
		}
		if !seen[matches[0].UDID] {
			seen[matches[0].UDID] = true
			resolved = append(resolved, matches[0])
		}
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].UDID < resolved[j].UDID })
	for _, target := range resolved {
		if target.Kind == "simulator" {
			if lock, locked := simulatorLockedByOther(target.UDID, c.Root); locked {
				return nil, fmt.Errorf("sim_locked udid=%s run=%s", target.UDID, lock.RunID)
			}
		}
	}
	return resolved, nil
}

func (c CLI) runFlowMatrix(ctx context.Context, opts GlobalOptions, args []string) error {
	targets, err := c.resolveMatrixTargets(ctx, repeatedFlagValues(args[1:], "--target"))
	if err != nil {
		return Fail("matrix_targets_invalid", map[string]string{"error": err.Error()}).Write(c.Stdout)
	}
	jobs := len(targets)
	if raw := flagValue(args[1:], "--jobs"); raw != "" {
		jobs, err = strconv.Atoi(raw)
		if err != nil || jobs < 1 {
			return Fail("matrix_jobs_invalid", map[string]string{"jobs": raw}).Write(c.Stdout)
		}
	}
	if jobs > len(targets) {
		jobs = len(targets)
	}
	base, err := newRunStateIn(filepath.Join(c.Root, MavDir, "runs"))
	if err != nil {
		return err
	}
	matrixDir := base.Dir
	_ = os.RemoveAll(filepath.Join(matrixDir, "commands.jsonl"))
	if err := os.MkdirAll(filepath.Join(matrixDir, "targets"), 0o755); err != nil {
		return err
	}
	var reserved []string
	for _, target := range targets {
		if target.Kind != "simulator" {
			continue
		}
		if err := writeSimulatorLock(target.UDID, base, c.Root, os.Getpid()); err != nil {
			for _, udid := range reserved {
				removeSimulatorLock(udid, c.Root)
			}
			return Fail("matrix_lock_failed", map[string]string{"udid": target.UDID, "error": err.Error()}).Write(c.Stdout)
		}
		reserved = append(reserved, target.UDID)
	}
	defer func() {
		for _, udid := range reserved {
			removeSimulatorLock(udid, c.Root)
		}
	}()
	cfg, configErr := LoadConfig(c.Root)
	if configErr != nil {
		return configErr
	}
	if len(targets) > 0 {
		applyMatrixTarget(&cfg, targets[0])
		commands := effectiveLaunchCommands(cfg)
		for _, step := range []launchStep{{Name: "healthcheck", Command: commands.Healthcheck}, {Name: "build", Command: commands.Build}} {
			if strings.TrimSpace(step.Command) == "" {
				continue
			}
			result := c.runLaunchCommand(ctx, cfg, base, step, launchEnv(cfg, base, ""))
			if result.Err != nil {
				return Fail("matrix_build_failed", map[string]string{"step": step.Name, "stderr": firstLine(result.Stderr), "dir": matrixDir}).Write(c.Stdout)
			}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	childArgs := append([]string{"run"}, stripMatrixFlags(args)...)
	sem := make(chan struct{}, jobs)
	results := make([]matrixResult, len(targets))
	var wg sync.WaitGroup
	for index, target := range targets {
		index, target := index, target
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[index] = matrixResult{Target: target, Status: "failed", ExitCode: 1, Output: ctx.Err().Error()}
				return
			}
			defer func() { <-sem }()
			started := time.Now()
			targetDir := filepath.Join(matrixDir, "targets", safeFileName(target.Name+"-"+target.UDID[:min(8, len(target.UDID))]))
			cmd := exec.CommandContext(ctx, executable, childArgs...)
			cmd.Dir = c.Root
			cmd.Env = append(os.Environ(),
				"MAV_MATRIX_CHILD=1",
				"MAV_EXACT_RUN_DIR="+targetDir,
				"MAV_TARGET_KIND="+target.Kind,
				"MAV_TARGET_UDID="+target.UDID,
				"MAV_TARGET_NAME="+target.Name,
				"MAV_TARGET_RUNTIME="+target.Runtime,
				"MAV_SKIP_BUILD=1",
			)
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			runErr := cmd.Run()
			code, status := 0, "passed"
			if runErr != nil {
				code, status = 1, "failed"
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					code = exitErr.ExitCode()
				}
			}
			if matrixOutputFailed(output.String()) {
				status = "failed"
				if code == 0 {
					code = 1
				}
			}
			result := matrixResult{Target: target, Status: status, ExitCode: code, Dir: targetDir, Output: strings.TrimSpace(output.String()), Elapsed: time.Since(started).String()}
			results[index] = result
			_ = os.WriteFile(filepath.Join(targetDir, "report.html"), []byte(renderMatrixTargetReport(result)), 0o644)
		}()
	}
	wg.Wait()
	manifest := matrixManifest{ID: base.ID, Flow: args[0], Started: base.Started, Results: results}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(filepath.Join(matrixDir, "matrix.json"), data, 0o644)
	_ = os.WriteFile(filepath.Join(matrixDir, "report.html"), []byte(renderMatrixReport(manifest)), 0o644)
	failed := 0
	for _, result := range results {
		if result.Status != "passed" {
			failed++
		}
	}
	fields := map[string]string{"matrix": base.ID, "dir": matrixDir, "targets": strconv.Itoa(len(targets)), "failed": strconv.Itoa(failed), "report": filepath.Join(matrixDir, "report.html")}
	if failed > 0 {
		if err := Fail("matrix_failed", fields).Write(c.Stdout); err != nil {
			return err
		}
		return CommandFailed{}
	}
	return c.OK("run.matrix", fields).Write(c.Stdout)
}

func matrixOutputFailed(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "fail ") {
			return true
		}
	}
	return false
}

func applyMatrixTarget(cfg *Config, target matrixTarget) {
	cfg.TargetKind = target.Kind
	if target.Kind == "device" {
		cfg.DeviceUDID, cfg.DeviceName = target.UDID, target.Name
		return
	}
	cfg.SimulatorUDID, cfg.SimulatorName, cfg.SimulatorRuntime = target.UDID, target.Name, target.Runtime
}

func renderMatrixReport(manifest matrixManifest) string {
	var rows strings.Builder
	for _, result := range manifest.Results {
		fmt.Fprintf(&rows, "<tr><td>%s</td><td>%s</td><td>%s</td><td><a href=\"%s/report.html\">report</a></td><td>%s</td></tr>",
			html.EscapeString(result.Target.Name), html.EscapeString(result.Target.UDID), result.Status,
			html.EscapeString(filepath.ToSlash(strings.TrimPrefix(result.Dir, filepath.Dir(filepath.Dir(result.Dir))+string(filepath.Separator)))), html.EscapeString(result.Elapsed))
	}
	return "<!doctype html><meta charset=utf-8><title>MAV matrix " + html.EscapeString(manifest.ID) + "</title><h1>MAV matrix</h1><table><thead><tr><th>Target</th><th>UDID</th><th>Status</th><th>Artifacts</th><th>Elapsed</th></tr></thead><tbody>" + rows.String() + "</tbody></table>"
}

func renderMatrixTargetReport(result matrixResult) string {
	return "<!doctype html><meta charset=utf-8><title>MAV " + html.EscapeString(result.Target.Name) +
		"</title><h1>" + html.EscapeString(result.Target.Name) + "</h1><dl><dt>UDID</dt><dd>" +
		html.EscapeString(result.Target.UDID) + "</dd><dt>Status</dt><dd>" + html.EscapeString(result.Status) +
		"</dd><dt>Elapsed</dt><dd>" + html.EscapeString(result.Elapsed) +
		"</dd></dl><h2>Output</h2><pre>" + html.EscapeString(result.Output) + "</pre>"
}
