package mav

import (
	"github.com/bitomule/mav/internal/mav/drivers"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func diagnosticCrashRoots(cfg Config) []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	roots := []string{filepath.Join(home, "Library", "Logs", "DiagnosticReports")}
	if cfg.SimulatorUDID != "" {
		roots = append(roots, filepath.Join(home, "Library", "Developer", "CoreSimulator", "Devices", cfg.SimulatorUDID))
	}
	if targetKind(cfg) == drivers.KindMac {
		// Un crash de una app de macOS puede caer en la carpeta del usuario o
		// en la del sistema segun quien lo genere; hay que mirar en las dos.
		roots = append(roots, filepath.Join("/", "Library", "Logs", "DiagnosticReports"))
	}
	return roots
}

func crashNameNeedles(cfg Config) []string {
	seen := map[string]bool{}
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == '.' || r == '-' || r == '_' || r == ' '
		}) {
			part = strings.TrimSpace(part)
			if len(part) < 3 {
				continue
			}
			lower := strings.ToLower(part)
			switch lower {
			case "com", "app", "debug", "release", "ios":
				continue
			}
			if !seen[lower] {
				seen[lower] = true
				out = append(out, lower)
			}
		}
	}
	add(cfg.ProcessName)
	add(cfg.ProjectName)
	add(cfg.BundleID)
	return out
}

func findRecentIPSFiles(roots []string, needles []string, since time.Time) []string {
	if len(needles) == 0 {
		return nil
	}
	var matches []string
	seen := map[string]bool{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.ToLower(filepath.Ext(path)) != ".ips" {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if !since.IsZero() && info.ModTime().Before(since) {
				return nil
			}
			name := strings.ToLower(filepath.Base(path))
			for _, needle := range needles {
				if strings.Contains(name, strings.ToLower(needle)) {
					if !seen[path] {
						seen[path] = true
						matches = append(matches, path)
					}
					break
				}
			}
			return nil
		})
	}
	sort.Strings(matches)
	return matches
}

func copyDiagnosticCrashes(paths []string, outDir string) (int, int) {
	if len(paths) == 0 || outDir == "" {
		return 0, 0
	}
	_ = os.MkdirAll(outDir, 0o755)
	copied := 0
	summarised := 0
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		dst := filepath.Join(outDir, filepath.Base(path))
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			continue
		}
		copied++
		if summary, err := ParseIPS(body); err == nil {
			txtPath := filepath.Join(outDir, strings.TrimSuffix(filepath.Base(dst), filepath.Ext(dst))+".txt")
			_ = os.WriteFile(txtPath, []byte(summary.OneLiner()+"\n"), 0o644)
			summarised++
		}
	}
	return copied, summarised
}
