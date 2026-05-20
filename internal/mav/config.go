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
	"strconv"
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
	TargetKind        string
	AppTarget         string
	DeviceTarget      string
	DeviceUDID        string
	DeviceName        string
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
	Launch            LaunchConfig
	Tools             map[string]bool
}

type LaunchConfig struct {
	Mode     string
	Commands LaunchCommands
}

type LaunchCommands struct {
	Healthcheck string
	Build       string
	AppPath     string
	Install     string
	Launch      string
	Cleanup     string
}

func DefaultConfig(root string) Config {
	return Config{
		Root:              root,
		TargetKind:        "simulator",
		LogCategory:       "probe",
		PreferredUIDriver: "axe",
		Tools:             map[string]bool{},
	}
}

func LoadConfig(root string) (Config, error) {
	path := filepath.Join(root, ConfigFile)
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("config_not_found path=%s run=mav_setup", path)
	}
	defer file.Close()

	cfg := DefaultConfig(root)
	scanner := bufio.NewScanner(file)
	section := ""
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if strings.HasSuffix(line, ":") {
			key := strings.TrimSuffix(line, ":")
			switch {
			case indent == 0:
				section = key
			case indent == 2 && section == "launch" && key == "commands":
				section = "launch.commands"
			}
			continue
		}
		if section == "app" && indent >= 2 {
			key, value, ok := splitYAMLKV(line)
			if ok {
				switch key {
				case "bundle_id":
					cfg.BundleID = value
				case "process_name":
					cfg.ProcessName = value
				}
			}
			continue
		}
		if section == "launch.commands" && indent >= 4 {
			key, value, ok := splitYAMLKV(line)
			if ok {
				setLaunchCommand(&cfg.Launch.Commands, key, value)
			}
			continue
		}
		if section == "launch" && indent >= 2 {
			key, value, ok := splitYAMLKV(line)
			if ok && key == "mode" {
				cfg.Launch.Mode = value
			}
			continue
		}
		if indent == 0 {
			section = ""
		}
		key, value, ok := splitYAMLKV(line)
		if !ok {
			continue
		}
		switch key {
		case "project_name":
			cfg.ProjectName = value
		case "target_kind":
			cfg.TargetKind = value
		case "app_target":
			cfg.AppTarget = value
		case "device_target":
			cfg.DeviceTarget = value
		case "device_udid":
			cfg.DeviceUDID = value
		case "device_name":
			cfg.DeviceName = value
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
	if cfg.Launch.Mode == "" && hasLaunchCommands(cfg.Launch.Commands) {
		cfg.Launch.Mode = "custom"
	}
	if cfg.TargetKind == "" {
		cfg.TargetKind = "simulator"
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
	writeKV("target_kind", normalizedTargetKind(cfg))
	if cfg.AppTarget != "" {
		writeKV("app_target", cfg.AppTarget)
	}
	if cfg.DeviceTarget != "" {
		writeKV("device_target", cfg.DeviceTarget)
	}
	if cfg.DeviceUDID != "" {
		writeKV("device_udid", cfg.DeviceUDID)
	}
	if cfg.DeviceName != "" {
		writeKV("device_name", cfg.DeviceName)
	}
	b.WriteString("app:\n")
	b.WriteString("  bundle_id: ")
	b.WriteString(yamlQuote(cfg.BundleID))
	b.WriteString("\n")
	b.WriteString("  process_name: ")
	b.WriteString(yamlQuote(cfg.ProcessName))
	b.WriteString("\n")
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
	if cfg.Launch.Mode != "" || hasLaunchCommands(cfg.Launch.Commands) {
		mode := cfg.Launch.Mode
		if mode == "" {
			mode = "custom"
		}
		b.WriteString("launch:\n")
		b.WriteString("  mode: ")
		b.WriteString(yamlQuote(mode))
		b.WriteString("\n")
		b.WriteString("  commands:\n")
		writeCommandKV(&b, "healthcheck", cfg.Launch.Commands.Healthcheck)
		writeCommandKV(&b, "build", cfg.Launch.Commands.Build)
		writeCommandKV(&b, "app_path", cfg.Launch.Commands.AppPath)
		writeCommandKV(&b, "install", cfg.Launch.Commands.Install)
		writeCommandKV(&b, "launch", cfg.Launch.Commands.Launch)
		writeCommandKV(&b, "cleanup", cfg.Launch.Commands.Cleanup)
	}
	return os.WriteFile(filepath.Join(root, ConfigFile), []byte(b.String()), 0o644)
}

func writeCommandKV(b *strings.Builder, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString("    ")
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(yamlQuote(value))
	b.WriteString("\n")
}

func setLaunchCommand(commands *LaunchCommands, key, value string) {
	switch key {
	case "healthcheck":
		commands.Healthcheck = value
	case "build":
		commands.Build = value
	case "app_path":
		commands.AppPath = value
	case "install":
		commands.Install = value
	case "launch":
		commands.Launch = value
	case "cleanup":
		commands.Cleanup = value
	}
}

func hasLaunchCommands(commands LaunchCommands) bool {
	return strings.TrimSpace(commands.Healthcheck) != "" ||
		strings.TrimSpace(commands.Build) != "" ||
		strings.TrimSpace(commands.AppPath) != "" ||
		strings.TrimSpace(commands.Install) != "" ||
		strings.TrimSpace(commands.Launch) != "" ||
		strings.TrimSpace(commands.Cleanup) != ""
}

func splitYAMLKV(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if unquoted, err := strconv.Unquote(value); err == nil {
		value = unquoted
	} else if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			value = value[1 : len(value)-1]
		}
	}
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

func SetupConfig(root string, runner Runner) (Config, error) {
	cfg := DefaultConfig(root)
	cfg.TargetKind = "simulator"
	cfg.ProjectName = filepath.Base(root)
	cfg.AppTarget = detectAppTarget(root, cfg.ProjectName)
	cfg.DeviceTarget = cfg.AppTarget
	cfg.BundleID = detectBundleID(root)
	cfg.LogSubsystem = probeLogSubsystem(cfg)
	cfg.LogCategory = probeLogCategory(cfg)
	cfg.ProcessName = detectScalar(root, "executable_name")
	if cfg.ProcessName == "" {
		cfg.ProcessName = processNameFromBundle(cfg.BundleID, cfg.ProjectName)
	}
	for _, tool := range knownTools() {
		_, err := runner.LookPath(tool)
		cfg.Tools[tool] = err == nil
	}
	if cfg.Tools["xcrun"] {
		udid, name, runtime := detectBootedSimulator(runner)
		cfg.SimulatorUDID = udid
		cfg.SimulatorName = name
		cfg.SimulatorRuntime = runtime
	}
	if cfg.Tools["axe"] {
		cfg.PreferredUIDriver = "axe"
	} else if cfg.Tools["idb"] {
		cfg.PreferredUIDriver = "idb"
	}
	if candidate, ok := selectLaunchCandidate(root, cfg); ok {
		if candidate.AppTarget != "" {
			cfg.AppTarget = candidate.AppTarget
			cfg.DeviceTarget = candidate.AppTarget
		}
		if candidate.BundleID != "" {
			cfg.BundleID = candidate.BundleID
		}
		cfg.Launch = candidate.Launch
	} else {
		cfg.Launch = defaultLaunchConfig(cfg)
	}
	return cfg, nil
}

func defaultLaunchConfig(cfg Config) LaunchConfig {
	if cfg.BundleID != "" {
		return LaunchConfig{
			Mode: "already_installed",
			Commands: LaunchCommands{
				Launch: `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`,
			},
		}
	}
	return LaunchConfig{}
}

func mergeSetupConfig(existing, detected Config) Config {
	merged := detected
	if existing.ProjectName != "" {
		merged.ProjectName = existing.ProjectName
	}
	if existing.BundleID != "" {
		merged.BundleID = existing.BundleID
	}
	if existing.ProcessName != "" {
		merged.ProcessName = existing.ProcessName
	}
	if existing.SimulatorUDID != "" {
		merged.SimulatorUDID = existing.SimulatorUDID
		merged.SimulatorName = existing.SimulatorName
		merged.SimulatorRuntime = existing.SimulatorRuntime
	}
	if existing.TargetKind != "" {
		merged.TargetKind = normalizedTargetKind(existing)
	}
	if existing.DeviceUDID != "" {
		merged.DeviceUDID = existing.DeviceUDID
	}
	if existing.DeviceName != "" {
		merged.DeviceName = existing.DeviceName
	}
	if existing.Locale != "" {
		merged.Locale = existing.Locale
	}
	if existing.Language != "" {
		merged.Language = existing.Language
	}
	if existing.LogSubsystem != "" {
		merged.LogSubsystem = existing.LogSubsystem
	}
	if existing.LogCategory != "" {
		merged.LogCategory = existing.LogCategory
	}
	if existing.PreferredUIDriver != "" {
		merged.PreferredUIDriver = existing.PreferredUIDriver
	}
	if existing.AllowShell {
		merged.AllowShell = true
	}
	if existing.AppTarget != "" {
		merged.AppTarget = existing.AppTarget
	}
	if existing.DeviceTarget != "" {
		merged.DeviceTarget = existing.DeviceTarget
	}
	if existing.Launch.Mode != "" || hasLaunchCommands(existing.Launch.Commands) {
		merged.Launch = existing.Launch
	}
	return merged
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

func detectBootedSimulator(runner Runner) (string, string, string) {
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

func detectAppTarget(root, projectName string) string {
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
		pkg := strings.TrimPrefix(filepath.Dir(path), root)
		pkg = strings.Trim(pkg, string(filepath.Separator))
		for _, name := range iosApplicationNames(string(data)) {
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

func iosApplicationNames(data string) []string {
	var names []string
	nameRe := regexp.MustCompile(`(?m)\bname\s*=\s*"([^"]+)"`)
	for _, block := range ruleBlocks(data, "ios_application") {
		if match := nameRe.FindStringSubmatch(block); len(match) == 2 {
			names = append(names, match[1])
		}
	}
	return names
}

func ruleBlocks(data, rule string) []string {
	var blocks []string
	needle := rule + "("
	for offset := 0; ; {
		index := strings.Index(data[offset:], needle)
		if index < 0 {
			return blocks
		}
		start := offset + index
		pos := start + len(needle)
		depth := 1
		inString := false
		escaped := false
		for pos < len(data) {
			ch := data[pos]
			if inString {
				if escaped {
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					inString = false
				}
				pos++
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					blocks = append(blocks, data[start:pos+1])
					pos++
					goto next
				}
			}
			pos++
		}
	next:
		offset = pos
	}
}

func detectBundleID(root string) string {
	if value := detectBundleFromBzl(root, "bundle_id_debug"); value != "" {
		return value
	}
	if value := detectBundleFromBzl(root, "bundle_id"); value != "" {
		return value
	}
	if value := detectBundleFromPBXProj(root); value != "" {
		return value
	}
	if value := detectBundleFromInfoPlist(root); value != "" {
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

func detectBundleFromPBXProj(root string) string {
	re := regexp.MustCompile(`PRODUCT_BUNDLE_IDENTIFIER\s*=\s*([^;\n]+)`)
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(path) != "project.pbxproj" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, match := range re.FindAllStringSubmatch(string(data), -1) {
			value := strings.Trim(strings.TrimSpace(match[1]), `"`)
			if value != "" && !strings.Contains(value, "$(") {
				found = value
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}

func detectBundleFromInfoPlist(root string) string {
	re := regexp.MustCompile(`(?s)<key>CFBundleIdentifier</key>\s*<string>([^<]+)</string>`)
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(root, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(path) != "Info.plist" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if match := re.FindStringSubmatch(string(data)); len(match) == 2 {
			value := strings.TrimSpace(match[1])
			if value != "" && !strings.Contains(value, "$(") {
				found = value
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}

func detectBundleFromBzl(root, key string) string {
	return detectScalar(root, key)
}

func detectScalar(root, key string) string {
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
