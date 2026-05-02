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
	ID       string
	Dir      string
	LogsPath string
	Commands string
	Started  time.Time
}

func NewRunState() (RunState, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return RunState{}, err
	}
	id := hex.EncodeToString(buf[:])
	dir := filepath.Join(os.TempDir(), "mav", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return RunState{}, err
	}
	return RunState{
		ID:       id,
		Dir:      dir,
		LogsPath: filepath.Join(dir, "logs.txt"),
		Commands: filepath.Join(dir, "commands.jsonl"),
		Started:  time.Now(),
	}, nil
}

func SaveCurrentRun(root string, run RunState) error {
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, CurrentRunRef), []byte(run.ID+"\n"), 0o644)
}

func LoadRun(root, id string) (RunState, error) {
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
	dir := filepath.Join(os.TempDir(), "mav", id)
	return RunState{
		ID:       id,
		Dir:      dir,
		LogsPath: filepath.Join(dir, "logs.txt"),
		Commands: filepath.Join(dir, "commands.jsonl"),
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
