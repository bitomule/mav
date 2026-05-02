package mav

import (
	"bufio"
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
	BundleID          string
	ProcessName       string
	SimulatorUDID     string
	SimulatorName     string
	LogStrategy       string
	PreferredUIDriver string
	Tools             map[string]bool
}

func DefaultConfig(root string) Config {
	return Config{
		Root:              root,
		LogStrategy:       "simctl-log-stream",
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
		case "bundle_id":
			cfg.BundleID = value
		case "process_name":
			cfg.ProcessName = value
		case "simulator_udid":
			cfg.SimulatorUDID = value
		case "simulator_name":
			cfg.SimulatorName = value
		case "log_strategy":
			cfg.LogStrategy = value
		case "preferred_ui_driver":
			cfg.PreferredUIDriver = value
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
	writeKV("bundle_id", cfg.BundleID)
	writeKV("process_name", cfg.ProcessName)
	writeKV("simulator_udid", cfg.SimulatorUDID)
	writeKV("simulator_name", cfg.SimulatorName)
	writeKV("log_strategy", cfg.LogStrategy)
	writeKV("preferred_ui_driver", cfg.PreferredUIDriver)
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
	cfg.AppTarget = discoverAppTarget(root)
	cfg.DeviceTarget = cfg.AppTarget
	cfg.BundleID = discoverBundleID(root)
	cfg.ProcessName = processNameFromBundle(cfg.BundleID, cfg.ProjectName)
	for _, tool := range []string{"bazelisk", "xcrun", "axe", "idb", "maestro"} {
		_, err := runner.LookPath(tool)
		cfg.Tools[tool] = err == nil
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

func discoverAppTarget(root string) string {
	var candidates []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "BUILD.bazel" {
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
	return candidates[0]
}

func discoverBundleID(root string) string {
	re := regexp.MustCompile(`bundle_id\s*=\s*"([^"]+)"`)
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "BUILD.bazel" || found != "" {
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
