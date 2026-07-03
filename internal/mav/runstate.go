package mav

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type RunState struct {
	ID        string
	Dir       string
	LogsPath  string
	Commands  string
	Processes string
	Started   time.Time
}

func NewRunState() (RunState, error) {
	return newRunStateIn(filepath.Join(os.TempDir(), "mav"))
}

func NewProjectRunState(root string) (RunState, error) {
	if dir := os.Getenv("MAV_EXACT_RUN_DIR"); dir != "" {
		return newExactRunState(dir)
	}
	return newRunStateIn(filepath.Join(root, MavDir, "runs"))
}

func newExactRunState(dir string) (RunState, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return RunState{}, err
	}
	id := filepath.Base(dir)
	return RunState{ID: id, Dir: dir, LogsPath: filepath.Join(dir, "logs.txt"), Commands: filepath.Join(dir, "commands.jsonl"), Processes: filepath.Join(dir, "processes.jsonl"), Started: time.Now()}, nil
}

func newRunStateIn(baseDir string) (RunState, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return RunState{}, err
	}
	id := hex.EncodeToString(buf[:])
	dir := filepath.Join(baseDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return RunState{}, err
	}
	return RunState{
		ID:        id,
		Dir:       dir,
		LogsPath:  filepath.Join(dir, "logs.txt"),
		Commands:  filepath.Join(dir, "commands.jsonl"),
		Processes: filepath.Join(dir, "processes.jsonl"),
		Started:   time.Now(),
	}, nil
}

func SaveCurrentRun(root string, run RunState) error {
	if dir := os.Getenv("MAV_EXACT_RUN_DIR"); dir != "" {
		return os.WriteFile(filepath.Join(dir, "current-run"), []byte(run.ID+"\n"), 0o644)
	}
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, CurrentRunRef), []byte(run.ID+"\n"), 0o644)
}

func LoadRun(root, id string) (RunState, error) {
	if dir := os.Getenv("MAV_EXACT_RUN_DIR"); dir != "" {
		if id == "" {
			data, err := os.ReadFile(filepath.Join(dir, "current-run"))
			if err != nil {
				return RunState{}, fmt.Errorf("run_not_found")
			}
			id = trimSpace(string(data))
		}
		return RunState{ID: id, Dir: dir, LogsPath: filepath.Join(dir, "logs.txt"), Commands: filepath.Join(dir, "commands.jsonl"), Processes: filepath.Join(dir, "processes.jsonl")}, nil
	}
	if id == "" {
		data, err := os.ReadFile(filepath.Join(root, CurrentRunRef))
		if err != nil {
			return RunState{}, fmt.Errorf("run_not_found")
		}
		id = string(data)
		id = trimSpace(id)
	}
	if id == "" {
		return RunState{}, fmt.Errorf("run_not_found")
	}
	dir := filepath.Join(root, MavDir, "runs", id)
	if _, err := os.Stat(dir); err != nil {
		dir = filepath.Join(os.TempDir(), "mav", id)
	}
	return RunState{
		ID:        id,
		Dir:       dir,
		LogsPath:  filepath.Join(dir, "logs.txt"),
		Commands:  filepath.Join(dir, "commands.jsonl"),
		Processes: filepath.Join(dir, "processes.jsonl"),
	}, nil
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == '\n' || s[0] == '\r' || s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 {
		last := s[len(s)-1]
		if last != '\n' && last != '\r' && last != ' ' && last != '\t' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}
