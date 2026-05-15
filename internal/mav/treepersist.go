package mav

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TreesDir is the subdirectory under a run dir where snapshot/delta JSON
// files live. Created on demand by PersistTree.
const TreesDir = "trees"

// PersistedTree is the metadata PersistTree returns to its caller so the
// evidence step record can reference it.
type PersistedTree struct {
	CompactPath string // <runDir>/trees/step-NN_<name>.json (capped, agent-facing)
	FullPath    string // <runDir>/trees/step-NN_<name>.full.json (uncapped, debug)
	DeltaPath   string // <runDir>/trees/step-NN_<name>.delta.json — empty when previous == nil
	Hash        string // sha256 hex of the compact JSON; populates EvidenceStep.TreeHash
}

// PersistTree writes the compact and full snapshots of a tree under
// <runDir>/trees/, optionally also writing a delta vs `previous`.
//
// Naming uses the step index zero-padded to 2 digits plus a slug derived
// from name (alphanumerics + dashes). The leading index keeps the directory
// listing chronological — important for the HTML report.
//
// When previous == nil no delta is written and DeltaPath is empty. The hash
// is the sha256 of the compact JSON so EvidenceStep.TreeHash can identify
// identical screen states cheaply.
func PersistTree(runDir string, stepIdx int, name string, raw []Element, previous []Element) (PersistedTree, error) {
	dir := filepath.Join(runDir, TreesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return PersistedTree{}, fmt.Errorf("mkdir trees: %w", err)
	}
	compact := Compact(raw)

	compactJSON, err := json.MarshalIndent(compact, "", "  ")
	if err != nil {
		return PersistedTree{}, fmt.Errorf("marshal compact: %w", err)
	}
	fullJSON, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return PersistedTree{}, fmt.Errorf("marshal full: %w", err)
	}

	slug := stepSlug(name)
	prefix := fmt.Sprintf("step-%02d_%s", stepIdx, slug)
	compactPath := filepath.Join(dir, prefix+".json")
	fullPath := filepath.Join(dir, prefix+".full.json")

	if err := os.WriteFile(compactPath, compactJSON, 0o644); err != nil {
		return PersistedTree{}, fmt.Errorf("write compact: %w", err)
	}
	if err := os.WriteFile(fullPath, fullJSON, 0o644); err != nil {
		return PersistedTree{}, fmt.Errorf("write full: %w", err)
	}

	sum := sha256.Sum256(compactJSON)
	out := PersistedTree{
		CompactPath: compactPath,
		FullPath:    fullPath,
		Hash:        hex.EncodeToString(sum[:]),
	}

	if previous != nil {
		delta := TreeDiff(previous, raw)
		deltaJSON, err := json.MarshalIndent(delta, "", "  ")
		if err != nil {
			return out, fmt.Errorf("marshal delta: %w", err)
		}
		deltaPath := filepath.Join(dir, prefix+".delta.json")
		if err := os.WriteFile(deltaPath, deltaJSON, 0o644); err != nil {
			return out, fmt.Errorf("write delta: %w", err)
		}
		out.DeltaPath = deltaPath
	}

	return out, nil
}

// stepSlug normalises name into a kebab-case slug for filenames.
// Used by PersistTree so step-3_my-name lands instead of step-3_my name.
func stepSlug(name string) string {
	out := make([]byte, 0, len(name))
	lastDash := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
			lastDash = false
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32) // to lower
			lastDash = false
		default:
			if !lastDash && len(out) > 0 {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "step"
	}
	return string(out)
}

// LoadPersistedTree reads back a compact tree snapshot. Used by `mav ui
// tree --since step-N` to compute a fresh delta against an older step
// without needing the caller to keep history in memory.
func LoadPersistedTree(path string) ([]Element, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var elements []Element
	if err := json.Unmarshal(body, &elements); err != nil {
		return nil, err
	}
	return elements, nil
}
