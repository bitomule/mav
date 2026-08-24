package mav

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type fakeRunner struct {
	tools   map[string]bool
	runs    []string
	out     map[string]string
	seq     map[string][]string
	calls   map[string]int
	results map[string]CommandResult
}

func (f fakeRunner) LookPath(file string) (string, error) {
	if f.tools[file] {
		return "/usr/bin/" + file, nil
	}
	return "", os.ErrNotExist
}

func (f fakeRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	if name == "/usr/bin/avconvert" {
		var source, out string
		for i := 0; i < len(args)-1; i++ {
			switch args[i] {
			case "-s":
				source = args[i+1]
			case "-o":
				out = args[i+1]
			}
		}
		if out != "" {
			_ = os.MkdirAll(filepath.Dir(out), 0o755)
			data := testMovieWithDuration(600, 1200)
			if source != "" {
				if sourceData, err := os.ReadFile(source); err == nil {
					data = sourceData
				}
			}
			_ = os.WriteFile(out, data, 0o644)
		}
		return CommandResult{}
	}
	key := name
	for _, arg := range args {
		key += " " + arg
	}
	if result, ok := f.results[key]; ok {
		if f.calls != nil {
			f.calls[key]++
		}
		return result
	}
	if values := f.seq[key]; len(values) > 0 {
		index := 0
		if f.calls != nil {
			index = f.calls[key]
			if index >= len(values) {
				index = len(values) - 1
			}
			f.calls[key]++
		}
		return CommandResult{Stdout: values[index]}
	}
	return CommandResult{Stdout: f.out[key]}
}

func (f fakeRunner) Start(ctx context.Context, logPath string, name string, args ...string) (int, error) {
	for i, arg := range args {
		if arg == "recordVideo" && i+1 < len(args) {
			path := args[len(args)-1]
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			_ = os.WriteFile(path, testMovieWithDuration(600, 1200), 0o644)
		}
	}
	return 123, nil
}

func TestSaveConfigRoundTripsIncludingExplicitEmptyOverride(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.ProjectName = "nokoru"
	cfg.AppTarget = "//App:NokoruiOS"
	cfg.BundleID = "com.davidcollado.nokoru.debug"
	cfg.ProcessName = "nNokoru"
	cfg.SimulatorName = "iPhone 17 Pro"
	cfg.TargetCommand = `simpool lease --device "iPhone 17 Pro" --os 26.3`
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Build:   "bazelisk build '//App:NokoruiOS'",
		AppPath: "./scripts/mav-app-path.sh",
		Install: `xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"`,
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	back, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, want, got string }{
		{"project_name", cfg.ProjectName, back.ProjectName},
		{"app_target", cfg.AppTarget, back.AppTarget},
		{"bundle_id", cfg.BundleID, back.BundleID},
		{"process_name", cfg.ProcessName, back.ProcessName},
		{"target_command", cfg.TargetCommand, back.TargetCommand},
		{"launch.build", cfg.Launch.Commands.Build, back.Launch.Commands.Build},
		{"launch.app_path", cfg.Launch.Commands.AppPath, back.Launch.Commands.AppPath},
		{"launch.install", cfg.Launch.Commands.Install, back.Launch.Commands.Install},
	} {
		if c.want != c.got {
			t.Fatalf("%s: round-trip perdio el valor: want %q got %q", c.name, c.want, c.got)
		}
	}
	// El escritor anterior omitia los valores vacios, asi que un comando que
	// nunca se configuro no debe reaparecer como cadena vacia en el fichero:
	// la config de un repo real se lee a mano y el ruido cuesta.
	data, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "cleanup:") || strings.Contains(string(data), "healthcheck:") {
		t.Fatalf("comandos no configurados no deben escribirse:\n%s", data)
	}
}

func TestSetupConfigFindsBazelApp(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "MODULE.bazel"), "module(name = \"Demo\")\n")
	mustWrite(t, filepath.Join(root, "Demo", "BUILD.bazel"), `load("@build_bazel_rules_apple//apple:ios.bzl", "ios_application")
ios_application(
    name = "DemoApp",
    bundle_id = "com.example.demo",
)
`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true, "xcrun": true, "axe": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppTarget != "//Demo:DemoApp" {
		t.Fatalf("target=%q", cfg.AppTarget)
	}
	if cfg.BundleID != "com.example.demo" {
		t.Fatalf("bundle=%q", cfg.BundleID)
	}
	if cfg.PreferredUIDriver != "axe" {
		t.Fatalf("driver=%q", cfg.PreferredUIDriver)
	}
}

func TestSetupConfigSkipsHiddenWorktrees(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".claude", "worktrees", "bad", "Undolly", "BUILD.bazel"), `ios_application(name = "Clean similar photos with Undolly", bundle_id = "bad")`)
	mustWrite(t, filepath.Join(root, "Undolly", "BUILD.bazel"), `ios_application(name = "UndollyApp", bundle_id = "com.example.release")`)
	mustWrite(t, filepath.Join(root, "tools", "shared.bzl"), `app_info = struct(bundle_id_debug = "com.example.debug", bundle_id = "com.example.release", executable_name = "Undolly")`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppTarget != "//Undolly:UndollyApp" {
		t.Fatalf("target=%q", cfg.AppTarget)
	}
	if cfg.BundleID != "com.example.debug" {
		t.Fatalf("bundle=%q", cfg.BundleID)
	}
	if cfg.ProcessName != "Undolly" {
		t.Fatalf("process=%q", cfg.ProcessName)
	}
}

func TestSetupConfigOnlyUsesIOSApplicationRuleNames(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `load("@build_bazel_rules_apple//apple:ios.bzl", "ios_application")
swift_macro(
    name = "DIMacros",
)
ios_application(
    name = "DemoApp",
    bundle_id = "com.example.demo",
)
`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppTarget != "//:DemoApp" {
		t.Fatalf("target=%q", cfg.AppTarget)
	}
}

func TestSetupConfigPrefersProjectLaunchScripts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "Makefile"), "mav-build:\n\ttrue\nmav-app-path:\n\tprintf /tmp/App.app\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Launch.Commands.Build != "make mav-build" || cfg.Launch.Commands.AppPath != "make mav-app-path" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigDoesNotUsePartialMakefileRecipe(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "Makefile"), "build-ios:\n\ttrue\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Launch.Commands.Build, "make") || cfg.Launch.Commands.AppPath == "" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigUsesExplicitMAVScripts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "scripts", "mav-build"), "#!/bin/sh\ntrue\n")
	mustWrite(t, filepath.Join(root, "scripts", "mav-app-path"), "#!/bin/sh\nprintf /tmp/App.app\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Launch.Commands.Build != "./scripts/mav-build" || cfg.Launch.Commands.AppPath != "./scripts/mav-app-path" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigIgnoresDotMAVLaunchScripts(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "Makefile"), "mav-build:\n\ttrue\nmav-app-path:\n\tprintf /tmp/App.app\n")
	mustWrite(t, filepath.Join(root, ".mav", "mav-build.sh"), "#!/bin/sh\ntrue\n")
	mustWrite(t, filepath.Join(root, ".mav", "mav-app-path.sh"), "#!/bin/sh\nprintf /tmp/App.app\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Launch.Commands.Build != "make mav-build" || cfg.Launch.Commands.AppPath != "make mav-app-path" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigUsesExplicitJustMAVRecipes(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "justfile"), "mav-build:\n  true\nmav-app-path:\n  printf /tmp/App.app\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Launch.Commands.Build != "just mav-build" || cfg.Launch.Commands.AppPath != "just mav-app-path" {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigDoesNotGuessUnrelatedJustfile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "justfile"), "build:\n  true\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Launch.Commands.Build, "just") {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigDoesNotDetectFastlane(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "fastlane", "Fastfile"), `
lane :mav_build do
end
private_lane :mav_app_path do
end
`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Launch.Commands.Build, "fastlane") {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigDoesNotGuessTenteWrapper(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	mustWrite(t, filepath.Join(root, "tente.toml"), "[build]\n")
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"bazelisk": true}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Launch.Commands.Build, "tente") {
		t.Fatalf("launch=%+v", cfg.Launch.Commands)
	}
}

func TestSetupConfigStoresBootedSimulator(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Demo", "BUILD.bazel"), `ios_application(name = "DemoApp", bundle_id = "com.example.demo")`)
	out := `{"devices":{"runtime":[{"udid":"SIM-1","name":"iPhone 17 Pro Max","state":"Booted"}]}}`
	cfg, err := SetupConfig(root, fakeRunner{
		tools: map[string]bool{"xcrun": true},
		out:   map[string]string{"xcrun simctl list devices booted -j": out},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SimulatorUDID != "SIM-1" || cfg.SimulatorName != "iPhone 17 Pro Max" || cfg.SimulatorRuntime != "runtime" {
		t.Fatalf("sim=%q %q %q", cfg.SimulatorUDID, cfg.SimulatorName, cfg.SimulatorRuntime)
	}
}

func TestSetupConfigDoesNotRequireBazelAppTarget(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Info.plist"), `<plist><dict><key>CFBundleIdentifier</key><string>com.example.demo</string></dict></plist>`)
	cfg, err := SetupConfig(root, fakeRunner{tools: map[string]bool{"xcrun": true}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppTarget != "" {
		t.Fatalf("target=%q", cfg.AppTarget)
	}
	if cfg.BundleID != "com.example.demo" {
		t.Fatalf("bundle=%q", cfg.BundleID)
	}
}

func TestSaveLoadConfig(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.ProjectName = "Demo"
	cfg.AppTarget = "//Demo:DemoApp"
	cfg.BundleID = "com.example.demo"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{Build: "make build-ios", AppPath: "make app-path", Launch: `xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tools:") {
		t.Fatalf("config should not persist runtime tool detection:\n%s", data)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AppTarget != cfg.AppTarget || loaded.Launch.Commands.AppPath != "make app-path" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestLoadConfigIgnoresLegacyToolsSection(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ConfigFile), `project_name: Demo
target_kind: simulator
app:
  bundle_id: com.example.demo
  process_name: Demo
bundle_id: com.example.demo
process_name: Demo
simulator_udid: SIM
tools:
  axe: true
  idb: true
`)
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Tools) != 0 {
		t.Fatalf("legacy tools should not be loaded into config state: %+v", loaded.Tools)
	}
	if err := SaveConfig(root, loaded); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "tools:") || strings.Contains(string(data), "axe: true") {
		t.Fatalf("legacy tools should be dropped on save:\n%s", data)
	}
}

func TestLoadLegacyConfigDefaultsToSimulatorTarget(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ConfigFile), `project_name: Demo
bundle_id: com.example.demo
simulator_udid: SIM-1
`)
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TargetKind != "simulator" || targetKind(loaded) != drivers.KindSim {
		t.Fatalf("target=%q loaded=%+v", loaded.TargetKind, loaded)
	}
}

func TestSaveLoadConfigPreservesDeviceTarget(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.ProjectName = "Demo"
	cfg.TargetKind = "device"
	cfg.DeviceUDID = "REAL-1"
	cfg.DeviceName = "David iPhone"
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TargetKind != "device" || loaded.DeviceUDID != "REAL-1" || loaded.DeviceName != "David iPhone" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestSplitYAMLKVPreservesInnerShellQuotes(t *testing.T) {
	key, value, ok := splitYAMLKV(`app_path: echo "$MAV_ROOT/bazel-bin/Demo.app"`)
	if !ok {
		t.Fatal("expected key/value")
	}
	if key != "app_path" {
		t.Fatalf("key=%q", key)
	}
	if value != `echo "$MAV_ROOT/bazel-bin/Demo.app"` {
		t.Fatalf("value=%q", value)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRawConfig(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ConfigFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const profileFixture = `project_name: nokoru
app_target: "//App:NokoruiOS"
bundle_id: com.davidcollado.nokoru.debug
process_name: nNokoru
target_command: simpool lease --device "iPhone 17 Pro"
launch:
  mode: custom
  commands:
    build: "bazelisk build '//App:NokoruiOS'"
    app_path: "./scripts/mav-app-path.sh"
    install: "xcrun simctl install $MAV_UDID $MAV_APP_PATH"
    launch: "xcrun simctl launch $MAV_UDID $MAV_BUNDLE_ID"
profiles:
  mac:
    app_target: "//App:NokoruMac"
    process_name: Nokoru
    target_command: ""
    launch:
      commands:
        build: "bazelisk build '//App:NokoruMac'"
        install: ""
`

func TestProfileOverlayReplacesOnlyDeclaredFields(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture)
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfile != "mac" {
		t.Fatalf("perfil activo=%q", cfg.ActiveProfile)
	}
	if cfg.AppTarget != "//App:NokoruMac" || cfg.ProcessName != "Nokoru" {
		t.Fatalf("el perfil debe ganar: app_target=%q process_name=%q", cfg.AppTarget, cfg.ProcessName)
	}
	if cfg.Launch.Commands.Build != "bazelisk build '//App:NokoruMac'" {
		t.Fatalf("build=%q", cfg.Launch.Commands.Build)
	}
	// Heredado: el perfil no lo menciona.
	if cfg.Launch.Commands.Launch != "xcrun simctl launch $MAV_UDID $MAV_BUNDLE_ID" {
		t.Fatalf("launch deberia heredarse de la base: %q", cfg.Launch.Commands.Launch)
	}
	if cfg.BundleID != "com.davidcollado.nokoru.debug" {
		t.Fatalf("bundle_id deberia heredarse: %q", cfg.BundleID)
	}
}

func TestProfileEmptyStringAnnulsInheritedValue(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture)
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatal(err)
	}
	// Esta es la distincion que justifica los punteros: presente-y-vacio NO es
	// ausente. Un simctl install heredado en un run de macOS instalaria la app
	// de Mac en un simulador.
	if cfg.Launch.Commands.Install != "" {
		t.Fatalf(`install: "" en el perfil debe anular el de la base, got %q`, cfg.Launch.Commands.Install)
	}
	if cfg.TargetCommand != "" {
		t.Fatalf(`target_command: "" debe anular el lease de simpool, got %q`, cfg.TargetCommand)
	}
}

func TestProfileNotFoundFailsListingAvailable(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture)
	_, err := LoadConfigWithProfile(root, "windows")
	if err == nil {
		t.Fatal("un perfil inexistente debe fallar, no caer en la base en silencio")
	}
	if !strings.Contains(err.Error(), "profile_not_found") || !strings.Contains(err.Error(), "mac") {
		t.Fatalf("el error debe nombrar los perfiles validos: %v", err)
	}
}

func TestProfilePrecedenceFlagOverEnvOverDefault(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture+"default_profile: mac\n")

	// 3) default_profile cuando no hay nada mas
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfile != "mac" {
		t.Fatalf("default_profile deberia aplicarse: %q", cfg.ActiveProfile)
	}

	// 2) MAV_PROFILE gana al default
	t.Setenv("MAV_PROFILE", "nope")
	if _, err := LoadConfig(root); err == nil {
		t.Fatal("MAV_PROFILE deberia ganar al default_profile y fallar al no existir")
	}

	// 1) el override explicito gana a MAV_PROFILE
	cfg, err = LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatalf("el override explicito debe ganar a MAV_PROFILE: %v", err)
	}
	if cfg.ActiveProfile != "mac" {
		t.Fatalf("perfil activo=%q", cfg.ActiveProfile)
	}
}

func TestConfigWithoutProfilesIsUnchanged(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: plain\nbundle_id: com.example.app\n")
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfile != "" || len(cfg.Profiles) != 0 {
		t.Fatalf("una config sin perfiles no debe activar ninguno: %q %v", cfg.ActiveProfile, cfg.Profiles)
	}
	if cfg.BundleID != "com.example.app" {
		t.Fatalf("bundle_id=%q", cfg.BundleID)
	}
}

func TestSaveConfigRefusesToFlattenAnActiveProfile(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, profileFixture)
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatal(err)
	}
	// Guardar esto escribiria //App:NokoruMac como app_target BASE y dejaria el
	// perfil sin sentido, en silencio y sin vuelta atras.
	if err := SaveConfig(root, cfg); err == nil {
		t.Fatal("SaveConfig debe rechazar una config con perfil aplicado")
	} else if !strings.Contains(err.Error(), "config_save_with_active_profile") {
		t.Fatalf("error inesperado: %v", err)
	}
}

// TestProfileOverridableFieldsAreExhaustive rompe cuando configYAML gana un
// campo y nadie decide si un perfil de plataforma deberia poder
// sobreescribirlo. Sin este test ese olvido no se nota: el campo simplemente
// se ignora dentro del perfil, en silencio, que es la clase de configuracion
// muerta que este proyecto persigue.
func TestProfileOverridableFieldsAreExhaustive(t *testing.T) {
	overridable := map[string]bool{
		"target_kind":    true, // Fase 2: el eje de plataforma
		"app_target":     true, // //App:NokoruiOS vs //App:NokoruMac
		"process_name":   true, // el binario de Mac no se llama igual
		"target_command": true, // un lease de simpool no pinta nada en macOS
		"log_subsystem":  true,
		"log_category":   true,
		"launch":         true, // recetas distintas por plataforma
	}
	notOverridable := map[string]string{
		"project_name":        "el proyecto es el mismo en todas las plataformas",
		"bundle_id":           "compartido; si divergiera seria otra app",
		"app":                 "espejo de bundle_id/process_name",
		"device_target":       "eje de dispositivo iOS fisico, ortogonal a la plataforma",
		"device_udid":         "seleccion de dispositivo, se persiste aparte",
		"device_name":         "idem",
		"simulator_udid":      "seleccion de simulador, se persiste aparte",
		"simulator_name":      "idem",
		"simulator_runtime":   "idem",
		"locale":              "se fija por invocacion, no por plataforma",
		"language":            "idem",
		"preferred_ui_driver": "lo decide el router por capacidad y coste",
		"allow_shell":         "politica del repo, no de la plataforma",
		"default_profile":     "es el selector, no puede estar dentro de lo seleccionado",
		"profiles":            "idem",
		"fixtures":            "los estados con nombre son del repo, no de la plataforma; un fixture que solo valga para una la nombra",
	}
	typ := reflect.TypeOf(configYAML{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("yaml")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if overridable[name] {
			continue
		}
		if _, ok := notOverridable[name]; ok {
			continue
		}
		t.Fatalf("configYAML tiene el campo %q y nadie ha decidido si un perfil puede sobreescribirlo.\n"+
			"Anadelo a overridable (y a profileYAML + applyProfile) o a notOverridable con el motivo.", name)
	}
}

func TestUnknownTargetKindFailsFromEverySource(t *testing.T) {
	// Fichero
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\ntarget_kind: windows\n")
	if _, err := LoadConfig(root); err == nil {
		t.Fatal("un target_kind desconocido en el fichero debe fallar")
	} else if !strings.Contains(err.Error(), "target_kind_invalid") {
		t.Fatalf("error inesperado: %v", err)
	}

	// Perfil
	root2 := t.TempDir()
	writeRawConfig(t, root2, "project_name: x\nprofiles:\n  weird:\n    target_kind: windows\n")
	if _, err := LoadConfigWithProfile(root2, "weird"); err == nil {
		t.Fatal("un target_kind desconocido en un perfil debe fallar")
	}
	if _, err := LoadConfig(root2); err != nil {
		t.Fatalf("sin perfil activo no deberia fallar: %v", err)
	}

	// Entorno
	root3 := t.TempDir()
	writeRawConfig(t, root3, "project_name: x\n")
	t.Setenv("MAV_TARGET_KIND", "windows")
	if _, err := LoadConfig(root3); err == nil {
		t.Fatal("un MAV_TARGET_KIND desconocido debe fallar")
	}
}

func TestMacosIsAValidTargetKind(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\nprofiles:\n  mac:\n    target_kind: macos\n")
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatalf("macos debe ser un target_kind valido: %v", err)
	}
	if targetKind(cfg) != drivers.KindMac {
		t.Fatalf("kind=%v", targetKind(cfg))
	}
	// La grafia publica se conserva: es contrato con los agentes y con los
	// config.yaml ya escritos.
	if got := targetKindLabel(targetKind(cfg)); got != "macos" {
		t.Fatalf("label=%q", got)
	}
	// Una app de macOS no tiene UDID; reportar uno seria mentir.
	if targetUDID(cfg) != "" {
		t.Fatalf("udid=%q", targetUDID(cfg))
	}
}

// TestMacTargetSkipsSimulatorResolution: un target de macOS no debe resolver
// target_command ni caer al simulador arrancado. Es la guarda que evita el
// escenario mas caro que encontro la revision -- un run "de macOS" alquilando
// un iPhone simulado y, con --clear-state, desinstalando de el la app de iOS
// porque el bundle_id es compartido.
func TestMacTargetSkipsSimulatorResolution(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\nbundle_id: com.example.app\ntarget_command: echo SHOULD-NOT-RUN\nprofiles:\n  mac:\n    target_kind: macos\n")
	cfg, err := LoadConfigWithProfile(root, "mac")
	if err != nil {
		t.Fatal(err)
	}
	cli := CLI{Root: root}
	if warn := cli.resolveConfigTarget(&cfg); warn != "" {
		t.Fatalf("un target de macOS no deberia avisar de target_command: %q", warn)
	}
	if cfg.SimulatorUDID != "" {
		t.Fatalf("un target de macOS no debe resolver simulador, got %q", cfg.SimulatorUDID)
	}
}

func TestMacMissingPermissionsRefusesToGuess(t *testing.T) {
	// Devolver "todo bien" ante un formato que no se entiende seria peor que
	// admitir que no se sabe: el usuario se enteraria del permiso que falta
	// cuando fallase un comando, no cuando pregunta.
	for name, stdout := range map[string]string{
		"vacio":      "",
		"no-json":    "error: something",
		"otra-forma": `{"success":false}`,
		// Sin demonio al que preguntar la respuesta trae null, y eso no es
		// "faltan los dos": es que nadie ha contestado.
		"sin-demonio": `{"accessibility":null,"screen_recording":null}`,
	} {
		if got := macMissingPermissions(stdout); len(got) != 1 || got[0] != "unreadable" {
			t.Fatalf("%s: no debe darse por bueno lo que no se entiende, got %v", name, got)
		}
	}
	granted := `{"accessibility":true,"screen_recording":true}`
	if got := macMissingPermissions(granted); len(got) != 0 {
		t.Fatalf("con todo concedido no falta nada: %v", got)
	}
	partial := `{"accessibility":true,"screen_recording":false}`
	got := macMissingPermissions(partial)
	if len(got) != 1 || got[0] != "screen_recording" {
		t.Fatalf("got %v", got)
	}
}

func TestProfileRunnerRejectsUnknownValues(t *testing.T) {
	root := t.TempDir()
	writeRawConfig(t, root, "project_name: x\nprofiles:\n  mac:\n    runner: podman\n")
	// Un runner mal escrito que se ignore en silencio significaria correr en
	// local algo que el usuario creia aislado en una VM.
	if _, err := LoadConfigWithProfile(root, "mac"); err == nil {
		t.Fatal("un runner desconocido debe fallar")
	} else if !strings.Contains(err.Error(), "profile_runner_invalid") {
		t.Fatalf("error inesperado: %v", err)
	}

	root2 := t.TempDir()
	writeRawConfig(t, root2, "project_name: x\nprofiles:\n  mac:\n    runner: crabbox\n")
	cfg, err := LoadConfigWithProfile(root2, "mac")
	if err != nil {
		t.Fatalf("crabbox es valido: %v", err)
	}
	if cfg.ProfileRunner != "crabbox" {
		t.Fatalf("runner=%q", cfg.ProfileRunner)
	}
}
