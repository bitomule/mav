package mav

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	MavDir        = ".mav"
	ConfigFile    = ".mav/config.yaml"
	AppMapFile    = ".mav/app-map.yaml"
	CurrentRunRef = ".mav/current-run"
)

type Config struct {
	ProjectName       string
	Root              string
	AppTarget         string
	DeviceTarget      string
	PreviewTarget     string
	PreviewBundleID   string
	BundleID          string
	ProcessName       string
	SimulatorUDID     string
	SimulatorName     string
	SimulatorRuntime  string
	Locale            string
	Language          string
	LogSubsystem      string
	LogCategory       string
	PreferredUIDriver string
	AllowShell        bool
	Tools             map[string]bool
}

func DefaultConfig(root string) Config {
	return Config{
		Root:              root,
		LogCategory:       "probe",
		PreferredUIDriver: "axe",
		Tools:             map[string]bool{},
	}
}

func LoadConfig(root string) (Config, error) {
	path := filepath.Join(root, ConfigFile)
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("config_not_found path=%s run=mav_discover", path)
	}
	defer file.Close()

	cfg := DefaultConfig(root)
	scanner := bufio.NewScanner(file)
	inTools := false
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "tools:" {
			inTools = true
			continue
		}
		if !strings.HasPrefix(raw, "  ") {
			inTools = false
		}
		if inTools {
			key, value, ok := splitYAMLKV(line)
			if ok {
				cfg.Tools[key] = value == "true"
			}
			continue
		}
		key, value, ok := splitYAMLKV(line)
		if !ok {
			continue
		}
		switch key {
		case "project_name":
			cfg.ProjectName = value
		case "app_target":
			cfg.AppTarget = value
		case "device_target":
			cfg.DeviceTarget = value
		case "preview_target":
			cfg.PreviewTarget = value
		case "preview_bundle_id":
			cfg.PreviewBundleID = value
		case "bundle_id":
			cfg.BundleID = value
		case "process_name":
			cfg.ProcessName = value
		case "simulator_udid":
			cfg.SimulatorUDID = value
		case "simulator_name":
			cfg.SimulatorName = value
		case "simulator_runtime":
			cfg.SimulatorRuntime = value
		case "locale":
			cfg.Locale = value
		case "language":
			cfg.Language = value
		case "log_subsystem":
			cfg.LogSubsystem = value
		case "log_category":
			cfg.LogCategory = value
		case "preferred_ui_driver":
			cfg.PreferredUIDriver = value
		case "allow_shell":
			cfg.AllowShell = value == "true"
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func SaveConfig(root string, cfg Config) error {
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	writeKV := func(key, value string) {
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(yamlQuote(value))
		b.WriteString("\n")
	}
	writeKV("project_name", cfg.ProjectName)
	writeKV("app_target", cfg.AppTarget)
	writeKV("device_target", cfg.DeviceTarget)
	writeKV("preview_target", cfg.PreviewTarget)
	writeKV("preview_bundle_id", cfg.PreviewBundleID)
	writeKV("bundle_id", cfg.BundleID)
	writeKV("process_name", cfg.ProcessName)
	writeKV("simulator_udid", cfg.SimulatorUDID)
	writeKV("simulator_name", cfg.SimulatorName)
	writeKV("simulator_runtime", cfg.SimulatorRuntime)
	writeKV("locale", cfg.Locale)
	writeKV("language", cfg.Language)
	writeKV("log_subsystem", probeLogSubsystem(cfg))
	writeKV("log_category", probeLogCategory(cfg))
	writeKV("preferred_ui_driver", cfg.PreferredUIDriver)
	if cfg.AllowShell {
		b.WriteString("allow_shell: true\n")
	}
	b.WriteString("tools:\n")
	keys := make([]string, 0, len(cfg.Tools))
	for key := range cfg.Tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b.WriteString("  ")
		b.WriteString(key)
		b.WriteString(": ")
		if cfg.Tools[key] {
			b.WriteString("true\n")
		} else {
			b.WriteString("false\n")
		}
	}
	return os.WriteFile(filepath.Join(root, ConfigFile), []byte(b.String()), 0o644)
}

func splitYAMLKV(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	value = strings.Trim(value, `"`)
	return key, value, true
}

func yamlQuote(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, ":#{}[]&,*?|-<>=!%@\\\"' \t") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

func DiscoverConfig(root string, runner Runner) (Config, error) {
	cfg := DefaultConfig(root)
	cfg.ProjectName = filepath.Base(root)
	cfg.AppTarget = discoverAppTarget(root, cfg.ProjectName)
	cfg.DeviceTarget = cfg.AppTarget
	cfg.BundleID = discoverBundleID(root)
	cfg.LogSubsystem = probeLogSubsystem(cfg)
	cfg.LogCategory = probeLogCategory(cfg)
	cfg.ProcessName = discoverScalar(root, "executable_name")
	if cfg.ProcessName == "" {
		cfg.ProcessName = processNameFromBundle(cfg.BundleID, cfg.ProjectName)
	}
	for _, tool := range []string{"bazelisk", "xcrun", "axe", "idb"} {
		_, err := runner.LookPath(tool)
		cfg.Tools[tool] = err == nil
	}
	if cfg.Tools["xcrun"] {
		udid, name, runtime := discoverBootedSimulator(runner)
		cfg.SimulatorUDID = udid
		cfg.SimulatorName = name
		cfg.SimulatorRuntime = runtime
	}
	if cfg.Tools["axe"] {
		cfg.PreferredUIDriver = "axe"
	} else if cfg.Tools["idb"] {
		cfg.PreferredUIDriver = "idb"
	}
	if cfg.AppTarget == "" {
		return cfg, fmt.Errorf("app_target_not_found")
	}
	return cfg, nil
}

func probeLogSubsystem(cfg Config) string {
	if cfg.LogSubsystem != "" {
		return cfg.LogSubsystem
	}
	if cfg.BundleID != "" {
		return "mav." + cfg.BundleID
	}
	if cfg.ProjectName != "" {
		return "mav." + strings.ToLower(regexp.MustCompile(`[^a-zA-Z0-9_.-]+`).ReplaceAllString(cfg.ProjectName, "-"))
	}
	return "mav.probe"
}

func probeLogCategory(cfg Config) string {
	if cfg.LogCategory != "" {
		return cfg.LogCategory
	}
	return "probe"
}

func discoverBootedSimulator(runner Runner) (string, string, string) {
	result := runner.Run(context.Background(), "xcrun", "simctl", "list", "devices", "booted", "-j")
	if result.Err != nil {
		return "", "", ""
	}
	var parsed struct {
		Devices map[string][]struct {
			UDID  string `json:"udid"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return "", "", ""
	}
	for runtime, devices := range parsed.Devices {
		for _, device := range devices {
			if device.State == "Booted" && device.UDID != "" {
				return device.UDID, device.Name, runtime
			}
		}
	}
	return "", "", ""
}

func discoverAppTarget(root, projectName string) string {
	var candidates []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(path) != "BUILD.bazel" {
			return nil
		}
		if strings.Contains(path, "bazel-") || strings.Contains(path, "/.git/") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || !strings.Contains(string(data), "ios_application(") {
			return nil
		}
		re := regexp.MustCompile(`(?m)name\s*=\s*"([^"]+)"`)
		matches := re.FindAllStringSubmatch(string(data), -1)
		pkg := strings.TrimPrefix(filepath.Dir(path), root)
		pkg = strings.Trim(pkg, string(filepath.Separator))
		for _, match := range matches {
			name := match[1]
			if strings.Contains(strings.ToLower(name), "release") {
				continue
			}
			if strings.Contains(strings.ToLower(name), "extension") {
				continue
			}
			if pkg == "" {
				candidates = append(candidates, "//:"+name)
			} else {
				candidates = append(candidates, "//"+filepath.ToSlash(pkg)+":"+name)
			}
		}
		return nil
	})
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return targetScore(candidates[i], projectName) > targetScore(candidates[j], projectName)
	})
	return candidates[0]
}

func discoverBundleID(root string) string {
	if value := discoverBundleFromBzl(root, "bundle_id_debug"); value != "" {
		return value
	}
	if value := discoverBundleFromBzl(root, "bundle_id"); value != "" {
		return value
	}
	re := regexp.MustCompile(`bundle_id\s*=\s*"([^"]+)"`)
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(path) != "BUILD.bazel" || found != "" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if match := re.FindStringSubmatch(string(data)); len(match) == 2 {
			found = match[1]
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func discoverBundleFromBzl(root, key string) string {
	return discoverScalar(root, key)
}

func discoverScalar(root, key string) string {
	re := regexp.MustCompile(key + `\s*=\s*"([^"]+)"`)
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if (filepath.Ext(path) != ".bzl" && filepath.Base(path) != "BUILD.bazel") || found != "" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if match := re.FindStringSubmatch(string(data)); len(match) == 2 {
			found = match[1]
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func shouldSkipDir(root, path string) bool {
	if path == root {
		return false
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return true
	}
	if strings.HasPrefix(base, "bazel-") || base == "bazel-bin" || base == "bazel-out" || base == "bazel-testlogs" {
		return true
	}
	return false
}

func targetScore(target, projectName string) int {
	score := 0
	lower := strings.ToLower(target)
	project := strings.ToLower(projectName)
	if strings.Contains(lower, "//"+project+"/") || strings.Contains(lower, "//"+project+":") {
		score += 100
	}
	if strings.HasSuffix(lower, project+"app") || strings.HasSuffix(lower, ":app") {
		score += 50
	}
	if strings.Contains(lower, "test") {
		score -= 25
	}
	return score
}

func processNameFromBundle(bundleID, fallback string) string {
	if bundleID == "" {
		return fallback
	}
	parts := strings.Split(bundleID, ".")
	if len(parts) == 0 {
		return fallback
	}
	return parts[len(parts)-1]
}
