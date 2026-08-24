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

// resolveMacPermissions pregunta a peekaboo por el estado de TCC. Es la unica
// de las herramientas que sabe contestarlo sin efectos secundarios: axcli lo
// comprueba al arrancar un comando de verdad, y provocar eso solo para sondear
// tocaria la app bajo prueba.
func (c CLI) resolveMacPermissions(ctx context.Context, caps *Capabilities) {
	if !caps.Tools["peekaboo"] {
		caps.MacPermissions = "unknown"
		caps.MacPermissionsNext = "mav setup --install peekaboo to report Accessibility and Screen Recording state"
		return
	}
	res := c.Runner.Run(ctx, "peekaboo", "permissions", "--json")
	missing := macMissingPermissions(res.Stdout)
	if len(missing) == 0 {
		caps.MacPermissions = "ok"
		return
	}
	caps.MacPermissions = strings.Join(missing, "+") + "_missing"
	caps.MacPermissionsNext = "grant it to the terminal or agent harness that runs mav, not to mav itself: System Settings > Privacy & Security"
}

// macMissingPermissions lee la respuesta de `peekaboo permissions --json`.
// Deliberadamente tolerante: si la salida no se entiende, no se inventa un
// estado -- devolver "todo bien" ante un formato desconocido seria peor que
// admitir que no se sabe.
func macMissingPermissions(stdout string) []string {
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			Permissions []struct {
				Name      string `json:"name"`
				IsGranted bool   `json:"isGranted"`
			} `json:"permissions"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil || !envelope.Success {
		return []string{"unreadable"}
	}
	var missing []string
	for _, perm := range envelope.Data.Permissions {
		if !perm.IsGranted {
			missing = append(missing, strings.ReplaceAll(strings.ToLower(perm.Name), " ", "_"))
		}
	}
	return missing
}

func knownTools() []string {
	return []string{"go", "bazelisk", "xcrun", "axe", "idb", "baguette", "simtime", "lldb-dap", "mitmdump", "pipx", "python3.12", "python3.13", "python3.14", "peekaboo", "axcli", "screencapture"}
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
