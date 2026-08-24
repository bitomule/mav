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

	// DefaultProfile y Profiles se conservan crudos para poder reescribirlos
	// sin perderlos, y para que `mav doctor` pueda listarlos. ActiveProfile es
	// el que se resolvio para esta invocacion ("" si ninguno).
	DefaultProfile string
	Profiles       map[string]profileYAML
	ActiveProfile  string
	ProfileRunner  string
	Fixtures       map[string][]string

	// AppPath lo rellena la receta de lanzamiento en tiempo de ejecucion (paso
	// app_path); no se lee ni se escribe en el YAML. Es como el driver de
	// macOS sabe que bundle ejecutar, que es su equivalente del UDID.
	AppPath string
}

type LaunchConfig struct {
	Mode     string         `yaml:"mode"`
	Commands LaunchCommands `yaml:"commands"`
}

// LaunchCommands es la receta de lanzamiento de la config base. Los campos
// llevan omitempty porque en la base "vacío" y "ausente" significan lo mismo:
// no hay comando. La distinción sí importa en un perfil de plataforma, que
// necesita poder *anular* un comando heredado -- por eso los perfiles usan su
// propio tipo con punteros (ver profileLaunchCommandsYAML), en vez de
// reutilizar este.
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

// LoadConfig carga .mav/config.yaml aplicando el perfil que corresponda segun
// la precedencia documentada. Equivale a LoadConfigWithProfile(root, "").
func LoadConfig(root string) (Config, error) {
	return LoadConfigWithProfile(root, "")
}

// LoadConfigRaw carga la config SIN aplicar ningun perfil. Es lo que deben
// usar los caminos que luego escriben (setup, sim select, device select): solo
// una config sin overlay puede volver a disco sin aplanar el perfil sobre la
// base. Ver el guardarrail de SaveConfig.
func LoadConfigRaw(root string) (Config, error) {
	return loadConfig(root, "", true)
}

// LoadConfigWithProfile carga la config y superpone un perfil de plataforma.
//
// Precedencia de seleccion, de mas fuerte a mas debil:
//
//  1. profileOverride -- lo que venga de un --profile explicito
//  2. MAV_PROFILE en el entorno
//  3. default_profile en la propia config
//  4. ninguno: los campos base se usan tal cual
//
// Un perfil pedido que no existe es un error, nunca un no-op: aceptar el flag
// y seguir con la base seria configuracion muerta del mismo tipo que
// target_command_ignored existe para hacer visible.
func LoadConfigWithProfile(root, profileOverride string) (Config, error) {
	return loadConfig(root, profileOverride, false)
}

// knownProfileKeys es el contrato de un perfil, escrito una sola vez.
var knownProfileKeys = map[string]bool{
	"target_kind": true, "app_target": true, "process_name": true,
	"target_command": true, "log_subsystem": true, "log_category": true,
	"launch": true, "runner": true,
}

// rejectUnknownProfileKeys convierte en error una clave que no existe dentro de
// un perfil.
//
// yaml.Unmarshal ignora en silencio lo que no conoce, y en un perfil eso es
// especialmente caro: escribes `fixture: x`, no pasa nada, y no hay forma de
// distinguirlo de que el fixture se aplicara y no hiciera efecto. Se acota a
// los perfiles a proposito: son nuevos, asi que ninguna configuracion existente
// puede romperse por esto, mientras que endurecer el fichero entero si podria.
func rejectUnknownProfileKeys(data []byte) error {
	var doc struct {
		Profiles map[string]map[string]yaml.Node `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		// El error real ya lo dio el decodificado principal; aqui no se
		// duplica.
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
	// El perfil se aplica ANTES que los MAV_TARGET_*: esas variables las fija
	// `mav run --matrix` en sus hijos para clavar un dispositivo concreto, y
	// clavar dispositivo es una decision mas especifica que elegir plataforma.
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
	// Al final a proposito: el target_kind puede venir del fichero, de un
	// perfil o de MAV_TARGET_KIND, y las tres fuentes tienen que pasar por el
	// mismo filtro. Validarlo antes del overlay dejaria pasar justo el caso
	// que mas importa, que es un perfil declarando una plataforma que aun no
	// existe.
	if err := validateTargetKind(cfg.TargetKind); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyProfile resuelve que perfil toca y superpone sus campos sobre cfg.
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

// overlayString aplica un campo de perfil sobre el de la base. Un puntero nil
// significa "el perfil no dice nada, hereda"; un puntero a cadena vacia
// significa "el perfil dice explicitamente que aqui no hay nada", que NO es lo
// mismo.
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

	// Fixtures son estados con nombre: listas de comandos que dejan la app en
	// una situacion conocida antes de lanzarla. No son un formato de datos --
	// mav no sabe que es un fixture por dentro, solo los ejecuta -- porque el
	// como sembrar es especifico de cada app y formalizarlo aqui obligaria a
	// todo el mundo al mismo formato.
	Fixtures map[string][]string `yaml:"fixtures,omitempty"`
}

// profileYAML es la capa de overlay de un perfil de plataforma. Todos los
// campos son punteros a proposito: yaml.Unmarshal sobre un string plano no
// distingue "ausente" de `""`, y esa distincion es justo lo que un perfil
// necesita. Un campo nil se hereda de la base; un campo presente gana, incluso
// cuando vale cadena vacia -- que es como el perfil de macOS anula el
// `simctl install` heredado.
//
// La lista de campos es deliberadamente cerrada, no abierta: si configYAML
// gana un campo nuevo que deberia poder sobreescribirse, hay que anadirlo aqui
// a mano. TestProfileOverridableFieldsAreExhaustive existe para que ese olvido
// rompa el build en vez de ignorarse en silencio.
type profileYAML struct {
	TargetKind    *string            `yaml:"target_kind,omitempty"`
	AppTarget     *string            `yaml:"app_target,omitempty"`
	ProcessName   *string            `yaml:"process_name,omitempty"`
	TargetCommand *string            `yaml:"target_command,omitempty"`
	LogSubsystem  *string            `yaml:"log_subsystem,omitempty"`
	LogCategory   *string            `yaml:"log_category,omitempty"`
	Launch        *profileLaunchYAML `yaml:"launch,omitempty"`

	// Runner dice DONDE corre este perfil: "local" (por defecto) o "crabbox".
	// mav no orquesta maquinas -- eso es crabbox, que ya sabe alquilar una VM
	// de macOS con tart, sincronizar el checkout sucio y devolverla al acabar.
	// Este campo solo declara la intencion; quien la ejecuta es el envoltorio,
	// no mav.
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
// Usa yaml.Marshal deliberadamente en vez del escritor a mano que había aquí
// antes: aquel omitía los valores vacíos (writeCommandKV), lo que hace
// imposible expresar "este campo está presente y vale cadena vacía". Esa
// distinción no importaba mientras la config era plana, pero es justo la que
// los perfiles de plataforma necesitan para que un perfil pueda *anular* un
// comando heredado de la base en vez de heredarlo.
//
// Nota sobre lo que NO cambia: esta función reconstruye el fichero entero, así
// que los comentarios que el usuario haya escrito a mano se pierden. Ya pasaba
// con el escritor anterior -- SaveConfig nunca ha leído el fichero previo para
// preservar nada.
func SaveConfig(root string, cfg Config) error {
	// Un cfg con perfil aplicado ya NO es la base: sus campos son el resultado
	// del overlay. Escribirlo aplanaria el perfil sobre la base -- un
	// `mav sim select` en un repo con default_profile: macos dejaria el
	// app_target de macOS como app_target base y el perfil dejaria de tener
	// sentido, en silencio y sin vuelta atras. Se rechaza por construccion en
	// vez de confiar en que ningun llamante se equivoque.
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
		// bundle_id y process_name se escriben en los dos sitios a propósito:
		// LoadConfig los lee con firstNonEmpty(raw.App.X, raw.X), así que una
		// config escrita por mav tiene que seguir siendo legible por ambos
		// caminos.
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

// persistTargetSelection guarda en disco SOLO la seleccion de target (que
// simulador o dispositivo, y locale/idioma) sin arrastrar el resto del cfg en
// memoria, que puede venir con un perfil ya superpuesto.
//
// Existe porque `mav sim select`, `mav device select` y el pin explicito de
// `mav open --device/--ios/--udid` son las tres unicas cosas que escriben
// config a media sesion, y las tres parten de un cfg resuelto. Guardarlo
// entero aplanaria el perfil sobre la base (ver el guardarrail de SaveConfig).
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

// validateTargetKind rechaza los labels de target_kind que mav no conoce.
//
// Sin esto la config falla ABIERTA, que en este caso concreto es peligroso:
// targetKind() devuelve KindDevice solo para el literal "device" y manda todo
// lo demas a KindSim, y targetKindLabel() lo normaliza de vuelta a "simulator"
// al escribir. Asi que un `target_kind: macos` escrito a mano no da error --
// se comporta como simulador de principio a fin.
//
// El desenlace no es teorico: ese run resolveria target_command (un
// `simpool lease` de iPhone), alquilaria y arrancaria un simulador, y con
// --clear-state encaminaria CapUninstall contra el usando el bundle_id, que en
// una app multiplataforma como Nokoru es el MISMO en iOS y macOS. Es decir,
// desinstalaria la app de iOS del simulador durante un run "de macOS". Y el
// resultado del uninstall ni siquiera se miraba hasta hace poco.
//
// Cuando llegue drivers.KindMac, "macos" pasa a ser un valor valido y se anade
// aqui. Hasta entonces, ruido en vez de silencio.
func validateTargetKind(kind string) error {
	switch kind {
	case "simulator", "device", "macos":
		return nil
	default:
		return fmt.Errorf("target_kind_invalid value=%s valid=simulator,device,macos", kind)
	}
}

// validateProfileRunner rechaza runners que mav no conoce, por el mismo motivo
// que un target_kind desconocido: un valor mal escrito que se ignora en
// silencio es configuracion muerta, y aqui ademas significaria correr en local
// algo que el usuario creia aislado en una VM.
func validateProfileRunner(runner string) error {
	switch runner {
	case "", "local", "crabbox":
		return nil
	default:
		return fmt.Errorf("profile_runner_invalid value=%s valid=local,crabbox", runner)
	}
}
