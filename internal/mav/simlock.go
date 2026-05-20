package mav

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const simLockFreshWindow = 90 * time.Second

type simulatorLock struct {
	RunID     string `json:"run_id"`
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
	Project   string `json:"project"`
	AgentHint string `json:"agent_hint,omitempty"`
}

func simLockPath(udid string) string {
	return filepath.Join(os.TempDir(), "mav", "sim-locks", safeFileName(udid)+".json")
}

func writeSimulatorLock(udid string, run RunState, root string, pid int) error {
	if strings.TrimSpace(udid) == "" {
		return nil
	}
	if pid <= 0 {
		pid = os.Getpid()
	}
	lock := simulatorLock{
		RunID:     run.ID,
		PID:       pid,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Project:   root,
		AgentHint: "codex",
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(simLockPath(udid)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(simLockPath(udid), data, 0o644)
}

func readSimulatorLock(udid string) (simulatorLock, bool) {
	data, err := os.ReadFile(simLockPath(udid))
	if err != nil {
		return simulatorLock{}, false
	}
	var lock simulatorLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return simulatorLock{}, false
	}
	return lock, true
}

func removeSimulatorLock(udid, root string) {
	lock, ok := readSimulatorLock(udid)
	if ok && root != "" && lock.Project != "" && lock.Project != root {
		return
	}
	_ = os.Remove(simLockPath(udid))
}

func simulatorLockFresh(lock simulatorLock) bool {
	if lock.PID > 0 {
		return processAlive(lock.PID)
	}
	started, err := time.Parse(time.RFC3339, lock.StartedAt)
	if err != nil {
		return false
	}
	return time.Since(started) < simLockFreshWindow
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func simulatorOwner(sim Simulator) string {
	if lock, ok := readSimulatorLock(sim.UDID); ok && simulatorLockFresh(lock) {
		project := filepath.Base(lock.Project)
		if project == "." || project == string(filepath.Separator) || project == "" {
			project = lock.Project
		}
		parts := []string{"mav"}
		if lock.RunID != "" {
			parts = append(parts, "run="+lock.RunID)
		}
		if project != "" {
			parts = append(parts, "project="+project)
		}
		if lock.PID > 0 {
			parts = append(parts, "pid="+strconv.Itoa(lock.PID))
		}
		return strings.Join(parts, " ")
	}
	lower := strings.ToLower(sim.Name)
	switch {
	case strings.HasPrefix(sim.Name, "BAZEL_TEST_"):
		return "bazel-test"
	case strings.HasPrefix(lower, "maestro_") || strings.HasPrefix(lower, "maestro-"):
		return "maestro"
	}
	return ""
}

func simulatorLockedByOther(udid, root string) (simulatorLock, bool) {
	lock, ok := readSimulatorLock(udid)
	if !ok || !simulatorLockFresh(lock) {
		return simulatorLock{}, false
	}
	return lock, lock.Project != "" && root != "" && lock.Project != root
}
