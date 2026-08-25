package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
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
	TargetCommand     string
	Launch            LaunchConfig
	Tools             map[string]bool

	// DefaultProfile and Profiles are kept raw so they can be rewritten
	// without being lost, and so `mav doctor` can list them. ActiveProfile
	// is the one resolved for this invocation ("" if none).
	DefaultProfile string
	Profiles       map[string]profileYAML
	ActiveProfile  string
	ProfileRunner  string
	Fixtures       map[string][]string

	// AppPath is filled by the launch recipe at run time (app_path step);
	// it is neither read from nor written to the YAML. It is how the macOS
	// driver knows which bundle to run, its equivalent of the UDID.
	AppPath string
}

type LaunchConfig struct {
	Mode     string         `yaml:"mode"`
	Commands LaunchCommands `yaml:"commands"`
}

// LaunchCommands is the base config's launch recipe. The fields carry
// omitempty because in the base "empty" and "absent" mean the same thing:
// no command. The distinction does matter in a platform profile, which
// needs to be able to *override* an inherited command with nothing, which
// is why profiles use their own pointer-based type (see
// profileLaunchCommandsYAML) instead of reusing this one.
type LaunchCommands struct {
	Healthcheck string `yaml:"healthcheck,omitempty"`
	Build       string `yaml:"build,omitempty"`
	AppPath     string `yaml:"app_path,omitempty"`
	Install     string `yaml:"install,omitempty"`
	Launch      string `yaml:"launch,omitempty"`
	Cleanup     string `yaml:"cleanup,omitempty"`
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

// LoadConfig loads .mav/config.yaml applying whichever profile the
// documented precedence selects. Equivalent to
// LoadConfigWithProfile(root, "").
func LoadConfig(root string) (Config, error) {
	return LoadConfigWithProfile(root, "")
}

// LoadConfigRaw loads the config WITHOUT applying any profile. It is what
// the paths that later write (setup, sim select, device select) must use:
// only a config without an overlay can go back to disk without flattening
// the profile onto the base. See SaveConfig's guardrail.
func LoadConfigRaw(root string) (Config, error) {
	return loadConfig(root, "", true)
}

// LoadConfigWithProfile loads the config and overlays a platform profile.
//
// Selection precedence, strongest to weakest:
//
//  1. profileOverride, whatever an explicit --profile brings
//  2. MAV_PROFILE in the environment
//  3. default_profile in the config itself
//  4. none: the base fields are used as is
//
// A requested profile that does not exist is an error, never a no-op:
// accepting the flag and carrying on with the base would be dead
// configuration of the same kind target_command_ignored exists to make
// visible.
func LoadConfigWithProfile(root, profileOverride string) (Config, error) {
	return loadConfig(root, profileOverride, false)
}

// knownProfileKeys is a profile's contract, written once.
var knownProfileKeys = map[string]bool{
	"target_kind": true, "app_target": true, "process_name": true,
	"target_command": true, "log_subsystem": true, "log_category": true,
	"launch": true, "runner": true,
}

// rejectUnknownProfileKeys turns a key that does not exist inside a
// profile into an error.
//
// yaml.Unmarshal silently ignores what it does not know, and in a profile
// that is especially expensive: you write `fixture: x`, nothing happens,
// and there is no way to tell it apart from the fixture applying and having
// no effect. It is scoped to profiles on purpose: they are new, so no
// existing configuration can break because of this, while hardening the
// whole file could.
func rejectUnknownProfileKeys(data []byte) error {
	var doc struct {
		Profiles map[string]map[string]yaml.Node `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		// The main decode already gave the real error; it is not duplicated
		// here.
		return nil
	}
	for name, fields := range doc.Profiles {
		for key := range fields {
			if !knownProfileKeys[key] {
				return fmt.Errorf("profile_unknown_key profile=%s key=%s", name, key)
			}
		}
	}
	return nil
}

func loadConfig(root, profileOverride string, skipProfile bool) (Config, error) {
	path := filepath.Join(root, ConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config_not_found path=%s run=mav_setup", path)
	}
	cfg := DefaultConfig(root)
	var raw configYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Config{}, err
	}
	if err := rejectUnknownProfileKeys(data); err != nil {
		return Config{}, err
	}
	cfg.ProjectName = raw.ProjectName
	cfg.TargetKind = raw.TargetKind
	cfg.AppTarget = raw.AppTarget
	cfg.DeviceTarget = raw.DeviceTarget
	cfg.DeviceUDID = raw.DeviceUDID
	cfg.DeviceName = raw.DeviceName
	cfg.BundleID = firstNonEmpty(raw.App.BundleID, raw.BundleID)
	cfg.ProcessName = firstNonEmpty(raw.App.ProcessName, raw.ProcessName)
	cfg.SimulatorUDID = raw.SimulatorUDID
	cfg.SimulatorName = raw.SimulatorName
	cfg.SimulatorRuntime = raw.SimulatorRuntime
	cfg.Locale = raw.Locale
	cfg.Language = raw.Language
	cfg.LogSubsystem = raw.LogSubsystem
	cfg.LogCategory = raw.LogCategory
	cfg.PreferredUIDriver = raw.PreferredUIDriver
	cfg.AllowShell = raw.AllowShell
	cfg.TargetCommand = raw.TargetCommand
	cfg.Launch = raw.Launch
	cfg.DefaultProfile = raw.DefaultProfile
	cfg.Profiles = raw.Profiles
	cfg.Fixtures = raw.Fixtures
	if cfg.Launch.Mode == "" && hasLaunchCommands(cfg.Launch.Commands) {
		cfg.Launch.Mode = "custom"
	}
	if cfg.TargetKind == "" {
		cfg.TargetKind = "simulator"
	}
	// The profile applies BEFORE the MAV_TARGET_* variables: those are set
	// by `mav run --matrix` in its children to pin a specific device, and
	// pinning a device is a more specific decision than choosing a
	// platform.
	if !skipProfile {
		if err := applyProfile(&cfg, profileOverride); err != nil {
			return Config{}, err
		}
	}
	if kind := os.Getenv("MAV_TARGET_KIND"); kind != "" {
		cfg.TargetKind = kind
		cfg.SimulatorUDID = os.Getenv("MAV_TARGET_UDID")
		cfg.SimulatorName = os.Getenv("MAV_TARGET_NAME")
		cfg.SimulatorRuntime = os.Getenv("MAV_TARGET_RUNTIME")
		if kind == "device" {
			cfg.DeviceUDID = os.Getenv("MAV_TARGET_UDID")
			cfg.DeviceName = os.Getenv("MAV_TARGET_NAME")
		}
	}
	// At the end on purpose: the target_kind can come from the file, from a
	// profile or from MAV_TARGET_KIND, and all three sources have to pass
	// through the same filter. Validating it before the overlay would let
	// through exactly the case that matters most, a profile declaring a
	// platform that does not exist yet.
	if err := validateTargetKind(cfg.TargetKind); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyProfile resolves which profile applies and overlays its fields on
// cfg.
func applyProfile(cfg *Config, override string) error {
	name := strings.TrimSpace(override)
	if name == "" {
		name = strings.TrimSpace(os.Getenv("MAV_PROFILE"))
	}
	if name == "" {
		name = strings.TrimSpace(cfg.DefaultProfile)
	}
	if name == "" {
		return nil
	}
	profile, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile_not_found name=%s available=%s", name, strings.Join(profileNames(*cfg), ","))
	}
	cfg.ActiveProfile = name
	overlayString(&cfg.TargetKind, profile.TargetKind)
	overlayString(&cfg.AppTarget, profile.AppTarget)
	overlayString(&cfg.ProcessName, profile.ProcessName)
	overlayString(&cfg.TargetCommand, profile.TargetCommand)
	overlayString(&cfg.LogSubsystem, profile.LogSubsystem)
	overlayString(&cfg.LogCategory, profile.LogCategory)
	overlayString(&cfg.ProfileRunner, profile.Runner)
	if err := validateProfileRunner(cfg.ProfileRunner); err != nil {
		return err
	}
	if profile.Launch != nil {
		overlayString(&cfg.Launch.Mode, profile.Launch.Mode)
		if c := profile.Launch.Commands; c != nil {
			overlayString(&cfg.Launch.Commands.Healthcheck, c.Healthcheck)
			overlayString(&cfg.Launch.Commands.Build, c.Build)
			overlayString(&cfg.Launch.Commands.AppPath, c.AppPath)
			overlayString(&cfg.Launch.Commands.Install, c.Install)
			overlayString(&cfg.Launch.Commands.Launch, c.Launch)
			overlayString(&cfg.Launch.Commands.Cleanup, c.Cleanup)
		}
	}
	return nil
}

// overlayString applies a profile field over the base's. A nil pointer
// means "the profile says nothing, inherit"; a pointer to the empty string
// means "the profile explicitly says there is nothing here", which is NOT
// the same.
func overlayString(base *string, override *string) {
	if override == nil {
		return
	}
	*base = *override
}

func profileNames(cfg Config) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type configYAML struct {
	ProjectName       string        `yaml:"project_name"`
	TargetKind        string        `yaml:"target_kind"`
	AppTarget         string        `yaml:"app_target,omitempty"`
	DeviceTarget      string        `yaml:"device_target,omitempty"`
	DeviceUDID        string        `yaml:"device_udid,omitempty"`
	DeviceName        string        `yaml:"device_name,omitempty"`
	BundleID          string        `yaml:"bundle_id"`
	ProcessName       string        `yaml:"process_name"`
	App               configAppYAML `yaml:"app"`
	SimulatorUDID     string        `yaml:"simulator_udid"`
	SimulatorName     string        `yaml:"simulator_name"`
	SimulatorRuntime  string        `yaml:"simulator_runtime"`
	Locale            string        `yaml:"locale"`
	Language          string        `yaml:"language"`
	LogSubsystem      string        `yaml:"log_subsystem"`
	LogCategory       string        `yaml:"log_category"`
	PreferredUIDriver string        `yaml:"preferred_ui_driver"`
	AllowShell        bool          `yaml:"allow_shell,omitempty"`
	TargetCommand     string        `yaml:"target_command,omitempty"`
	Launch            LaunchConfig  `yaml:"launch,omitempty"`

	DefaultProfile string                 `yaml:"default_profile,omitempty"`
	Profiles       map[string]profileYAML `yaml:"profiles,omitempty"`

	// Fixtures are named states: lists of commands that leave the app in a
	// known situation before launching it. They are not a data format, mav
	// does not know what a fixture is inside, it only runs them, because
	// how to seed is specific to each app and formalizing it here would
	// force everyone into the same format.
	Fixtures map[string][]string `yaml:"fixtures,omitempty"`
}

// profileYAML is a platform profile's overlay layer. All fields are
// pointers on purpose: yaml.Unmarshal onto a plain string does not
// distinguish "absent" from `""`, and that distinction is exactly what a
// profile needs. A nil field inherits from the base; a present field wins,
// even when it holds the empty string, which is how the macOS profile
// cancels the inherited `simctl install`.
//
// The field list is deliberately closed, not open: if configYAML gains a
// new field that should be overridable, it must be added here by hand.
// TestProfileOverridableFieldsAreExhaustive exists so that omission breaks
// the build instead of being silently ignored.
type profileYAML struct {
	TargetKind    *string            `yaml:"target_kind,omitempty"`
	AppTarget     *string            `yaml:"app_target,omitempty"`
	ProcessName   *string            `yaml:"process_name,omitempty"`
	TargetCommand *string            `yaml:"target_command,omitempty"`
	LogSubsystem  *string            `yaml:"log_subsystem,omitempty"`
	LogCategory   *string            `yaml:"log_category,omitempty"`
	Launch        *profileLaunchYAML `yaml:"launch,omitempty"`

	// Runner says WHERE this profile runs: "local" (default) or "crabbox".
	// mav does not orchestrate machines, that is crabbox, which already
	// knows how to lease a macOS VM with tart, sync the dirty checkout and
	// return it when done. This field only declares the intent; the wrapper
	// executes it, not mav.
	Runner *string `yaml:"runner,omitempty"`
}

type profileLaunchYAML struct {
	Mode     *string                    `yaml:"mode,omitempty"`
	Commands *profileLaunchCommandsYAML `yaml:"commands,omitempty"`
}

type profileLaunchCommandsYAML struct {
	Healthcheck *string `yaml:"healthcheck,omitempty"`
	Build       *string `yaml:"build,omitempty"`
	AppPath     *string `yaml:"app_path,omitempty"`
	Install     *string `yaml:"install,omitempty"`
	Launch      *string `yaml:"launch,omitempty"`
	Cleanup     *string `yaml:"cleanup,omitempty"`
}

type configAppYAML struct {
	BundleID    string `yaml:"bundle_id"`
	ProcessName string `yaml:"process_name"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// SaveConfig serializa cfg a .mav/config.yaml.
//
// It uses yaml.Marshal deliberately instead of the hand-written writer
// that lived here before: that one omitted empty values (writeCommandKV),
// which makes it impossible to express "this field is present and holds
// the empty string". That distinction did not matter while the config was
// flat, but it is exactly the one platform profiles need so a profile can
// *cancel* a command inherited from the base instead of inheriting it.
//
// Note on what does NOT change: this function rebuilds the whole file, so
// comments the user wrote by hand are lost. That already happened with the
// previous writer; SaveConfig has never read the prior file to preserve
// anything.
func SaveConfig(root string, cfg Config) error {
	// A cfg with a profile applied is NO longer the base: its fields are
	// the overlay's result. Writing it would flatten the profile onto the
	// base; a `mav sim select` in a repo with default_profile: macos would
	// leave the macOS app_target as the base app_target and the profile
	// would stop making sense, silently and with no way back. It is
	// rejected by construction instead of trusting no caller ever slips.
	if cfg.ActiveProfile != "" {
		return fmt.Errorf("config_save_with_active_profile profile=%s (next: reload the config without a profile before saving)", cfg.ActiveProfile)
	}
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		return err
	}
	raw := configYAML{
		ProjectName:  cfg.ProjectName,
		TargetKind:   targetKindLabel(targetKind(cfg)),
		AppTarget:    cfg.AppTarget,
		DeviceTarget: cfg.DeviceTarget,
		DeviceUDID:   cfg.DeviceUDID,
		DeviceName:   cfg.DeviceName,
		// bundle_id and process_name are written in both places on purpose:
		// LoadConfig reads them with firstNonEmpty(raw.App.X, raw.X), so a
		// config written by mav has to remain readable through both paths.
		App: configAppYAML{
			BundleID:    cfg.BundleID,
			ProcessName: cfg.ProcessName,
		},
		BundleID:          cfg.BundleID,
		ProcessName:       cfg.ProcessName,
		SimulatorUDID:     cfg.SimulatorUDID,
		SimulatorName:     cfg.SimulatorName,
		SimulatorRuntime:  cfg.SimulatorRuntime,
		Locale:            cfg.Locale,
		Language:          cfg.Language,
		LogSubsystem:      probeLogSubsystem(cfg),
		LogCategory:       probeLogCategory(cfg),
		PreferredUIDriver: cfg.PreferredUIDriver,
		AllowShell:        cfg.AllowShell,
		TargetCommand:     strings.TrimSpace(cfg.TargetCommand),
		DefaultProfile:    cfg.DefaultProfile,
		Profiles:          cfg.Profiles,
		Fixtures:          cfg.Fixtures,
	}
	if cfg.Launch.Mode != "" || hasLaunchCommands(cfg.Launch.Commands) {
		mode := cfg.Launch.Mode
		if mode == "" {
			mode = "custom"
		}
		raw.Launch = LaunchConfig{Mode: mode, Commands: cfg.Launch.Commands}
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ConfigFile), data, 0o644)
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
		merged.TargetKind = targetKindLabel(targetKind(existing))
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
	if existing.TargetCommand != "" {
		merged.TargetCommand = existing.TargetCommand
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

// persistTargetSelection writes to disk ONLY the target selection (which
// simulator or device, plus locale/language) without dragging along the
// rest of the in-memory cfg, which may come with a profile already
// overlaid.
//
// It exists because `mav sim select`, `mav device select` and the explicit
// pin of `mav open --device/--ios/--udid` are the only three things that
// write config mid-session, and all three start from a resolved cfg.
// Saving it whole would flatten the profile onto the base (see SaveConfig's
// guardrail).
func persistTargetSelection(root string, cfg Config) error {
	base, err := LoadConfigRaw(root)
	if err != nil {
		return err
	}
	base.TargetKind = cfg.TargetKind
	base.SimulatorUDID = cfg.SimulatorUDID
	base.SimulatorName = cfg.SimulatorName
	base.SimulatorRuntime = cfg.SimulatorRuntime
	base.DeviceUDID = cfg.DeviceUDID
	base.DeviceName = cfg.DeviceName
	base.Locale = cfg.Locale
	base.Language = cfg.Language
	return SaveConfig(root, base)
}

// validateTargetKind rejects target_kind labels mav does not know.
//
// Without this the config fails OPEN, which in this specific case is
// dangerous: targetKind() returns KindDevice only for the literal "device"
// and sends everything else to KindSim, and targetKindLabel() normalizes
// it back to "simulator" on write. So a hand-written `target_kind: macos`
// gives no error; it behaves as a simulator from start to finish.
//
// The outcome is not theoretical: that run would resolve target_command
// (an iPhone `simpool lease`), lease and boot a simulator, and with
// --clear-state route CapUninstall against it using the bundle_id, which
// in a multiplatform app like Nokoru is the SAME on iOS and macOS. That
// is, it would uninstall the iOS app from the simulator during a "macOS"
// run. And the uninstall's result was not even checked until recently.
//
// When drivers.KindMac arrives, "macos" becomes a valid value and is added
// here. Until then, noise instead of silence.
func validateTargetKind(kind string) error {
	switch kind {
	case "simulator", "device", "macos":
		return nil
	default:
		return fmt.Errorf("target_kind_invalid value=%s valid=simulator,device,macos", kind)
	}
}

// validateProfileRunner rejects runners mav does not know, for the same
// reason as an unknown target_kind: a misspelled value that is silently
// ignored is dead configuration, and here it would also mean running
// locally something the user believed isolated in a VM.
func validateProfileRunner(runner string) error {
	switch runner {
	case "", "local", "crabbox":
		return nil
	default:
		return fmt.Errorf("profile_runner_invalid value=%s valid=local,crabbox", runner)
	}
}
