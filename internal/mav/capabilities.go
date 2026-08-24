package mav

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type Capabilities struct {
	Tools                map[string]bool
	LaunchRecipe         bool
	Accessibility        bool
	AccessibilityDriver  string
	SemanticActions      bool
	CoordinateTap        bool
	CoordinateTapDriver  string
	DeviceFallback       bool
	DeviceFallbackDriver string
	Multitouch           bool
	MultitouchDriver     string
	NetworkCapture       bool
	NetworkCaptureDriver string
	WallClock            bool
	Debug                bool
	IDBIssue             string
	IDBNext              string

	// macOS: TCC es el factor que decide, no la API. Se reporta aparte porque
	// el titular del permiso NO es mav sino el proceso que lo ejecuta, y eso
	// hay que decirlo o el usuario lo busca donde no esta.
	MacPermissions     string
	MacPermissionsNext string
}

func (c CLI) resolveCapabilities(ctx context.Context, cfg Config) Capabilities {
	tools := map[string]bool{}
	for _, tool := range knownTools() {
		_, err := c.Runner.LookPath(tool)
		tools[tool] = err == nil
	}
	caps := Capabilities{Tools: tools}
	caps.LaunchRecipe = hasLaunchCommands(cfg.Launch.Commands) || cfg.BundleID != ""
	if tools["axe"] {
		caps.Accessibility = true
		caps.AccessibilityDriver = "axe"
		caps.SemanticActions = true
	} else if tools["idb"] {
		caps.Accessibility = true
		caps.AccessibilityDriver = "idb"
	}
	if tools["idb"] {
		caps.CoordinateTap = true
		caps.CoordinateTapDriver = "idb"
		caps.DeviceFallback = true
		caps.DeviceFallbackDriver = "idb"
		status := c.Runner.Run(ctx, "idb", "--version")
		if status.Err != nil && idbPythonUnsupported(status.Stdout+"\n"+status.Stderr) {
			caps.IDBIssue = "fb-idb does not support the active Python version"
			caps.IDBNext = "pipx install --python python3.12 fb-idb"
		}
	}
	if tools["baguette"] {
		caps.Multitouch = true
		caps.MultitouchDriver = "baguette"
	}
	if tools["mitmdump"] {
		caps.NetworkCapture = true
		caps.NetworkCaptureDriver = "mitmproxy"
	}
	if targetKind(cfg) == drivers.KindMac {
		c.resolveMacPermissions(ctx, &caps)
	}
	caps.WallClock = tools["simtime"]
	if tools["xcrun"] {
		caps.Debug = c.Runner.Run(ctx, "xcrun", "--find", "lldb-dap").Err == nil
	}
	return caps
}

// resolveMacPermissions pregunta a cua-driver por el estado de TCC.
//
// Y pregunta al DEMONIO, que es lo que hace util la respuesta: contesta con la
// identidad de CuaDriver.app, que es quien tiene los permisos de verdad. Un
// sondeo que mirara los permisos del proceso que corre mav no diria nada,
// porque en macOS un CLI nunca los tiene: solo los procesos GUI interactivos
// pueden tenerlos, y de ahi toda la arquitectura de broker.
func (c CLI) resolveMacPermissions(ctx context.Context, caps *Capabilities) {
	if !caps.Tools["cua-driver"] {
		caps.MacPermissions = "unknown"
		caps.MacPermissionsNext = "mav setup --install cua-driver to report Accessibility and Screen Recording state"
		return
	}
	res := c.Runner.Run(ctx, "cua-driver", "permissions", "status", "--json")
	missing := macMissingPermissions(res.Stdout)
	if len(missing) == 0 {
		caps.MacPermissions = "ok"
		return
	}
	caps.MacPermissions = strings.Join(missing, "+") + "_missing"
	// Su propio flujo de concesion lanza la app por LaunchServices para que los
	// dialogos se atribuyan a ella y la registra en los paneles. Conceder a la
	// terminal no sirve: el que captura es el demonio.
	caps.MacPermissionsNext = "cua-driver permissions grant"
}

// macMissingPermissions lee la respuesta de `cua-driver permissions status`.
// Deliberadamente tolerante: si la salida no se entiende, no se inventa un
// estado -- devolver "todo bien" ante un formato desconocido seria peor que
// admitir que no se sabe.
func macMissingPermissions(stdout string) []string {
	// Punteros y no bool: la respuesta trae `null` cuando no hay demonio al
	// que preguntar, y un `false` implicito ahi seria mentir en la direccion
	// contraria -- diria "falta permiso" cuando lo que pasa es que nadie ha
	// contestado.
	var status struct {
		Accessibility   *bool `json:"accessibility"`
		ScreenRecording *bool `json:"screen_recording"`
	}
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		return []string{"unreadable"}
	}
	if status.Accessibility == nil && status.ScreenRecording == nil {
		return []string{"unreadable"}
	}
	var missing []string
	if status.Accessibility == nil || !*status.Accessibility {
		missing = append(missing, "accessibility")
	}
	if status.ScreenRecording == nil || !*status.ScreenRecording {
		missing = append(missing, "screen_recording")
	}
	return missing
}

func knownTools() []string {
	return []string{"go", "bazelisk", "xcrun", "axe", "idb", "baguette", "simtime", "lldb-dap", "mitmdump", "pipx", "python3.12", "python3.13", "python3.14", "cua-driver", "axcli", "screencapture"}
}

func (c CLI) resolveConfigTools(cfg *Config) {
	if cfg.Tools == nil {
		cfg.Tools = map[string]bool{}
	}
	for _, tool := range knownTools() {
		if _, err := c.Runner.LookPath(tool); err == nil {
			cfg.Tools[tool] = true
		} else {
			cfg.Tools[tool] = false
		}
	}
}

func (caps Capabilities) fields() map[string]string {
	fields := map[string]string{}
	if caps.LaunchRecipe {
		fields["launch_recipe"] = "ok"
	} else {
		fields["launch_recipe"] = "missing"
	}
	if caps.Accessibility {
		fields["accessibility"] = "ok"
		fields["accessibility_driver"] = caps.AccessibilityDriver
	} else {
		fields["accessibility"] = "missing"
	}
	if caps.SemanticActions {
		fields["semantic_actions"] = "ok"
		fields["semantic_actions_driver"] = caps.AccessibilityDriver
	} else {
		fields["semantic_actions"] = "missing"
	}
	if caps.CoordinateTap {
		fields["coordinate_tap"] = "ok"
		fields["coordinate_tap_driver"] = caps.CoordinateTapDriver
	} else {
		fields["coordinate_tap"] = "missing"
	}
	if caps.DeviceFallback {
		fields["device_fallback"] = "ok"
		fields["device_fallback_driver"] = caps.DeviceFallbackDriver
	} else {
		fields["device_fallback"] = "missing"
	}
	if caps.Multitouch {
		fields["multitouch"] = "ok"
		fields["multitouch_driver"] = caps.MultitouchDriver
	} else {
		fields["multitouch"] = "missing"
		fields["multitouch_next"] = "mav setup --install baguette"
	}
	if caps.NetworkCapture {
		fields["network_capture"] = "ok"
		fields["network_capture_driver"] = caps.NetworkCaptureDriver
	} else {
		fields["network_capture"] = "missing"
		fields["network_capture_next"] = "mav setup --install mitmproxy"
	}
	if caps.WallClock {
		fields["wall_clock"] = "ok"
		fields["wall_clock_driver"] = "simtime"
	} else {
		fields["wall_clock"] = "missing"
		fields["wall_clock_next"] = "mav setup --install simtime"
	}
	if caps.Debug {
		fields["debug"] = "ok"
		fields["debug_driver"] = "lldb-dap"
	} else {
		fields["debug"] = "missing"
		fields["debug_next"] = "mav setup --install lldb-dap"
	}
	if caps.MacPermissions != "" {
		fields["mac_permissions"] = caps.MacPermissions
		if caps.MacPermissionsNext != "" {
			fields["mac_permissions_next"] = caps.MacPermissionsNext
		}
	}
	if caps.IDBIssue != "" {
		fields["idb_issue"] = caps.IDBIssue
		fields["idb_next"] = caps.IDBNext
	}
	return fields
}

func idbPythonUnsupported(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "get_event_loop") ||
		(strings.Contains(lower, "python 3.14") && strings.Contains(lower, "asyncio"))
}
