package mav

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

type Capabilities struct {
	// Kind is the target these capabilities were resolved FOR. fields()
	// needs it because the right guidance differs by platform: prescribing
	// baguette or simtime to a macos target sends the reader to install
	// tools that provide nothing there.
	Kind                 drivers.TargetKind
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

	// macOS: TCC is the deciding factor, not the API. It is reported
	// separately because the permission holder is NOT mav but the process
	// running it, and that must be said or the user looks where it is not.
	MacPermissions     string
	MacPermissionsNext string
}

func (c CLI) resolveCapabilities(ctx context.Context, cfg Config) Capabilities {
	tools := map[string]bool{}
	for _, tool := range knownTools() {
		_, err := c.Runner.LookPath(tool)
		tools[tool] = err == nil
	}
	caps := Capabilities{Tools: tools, Kind: targetKind(cfg)}
	caps.LaunchRecipe = hasLaunchCommands(cfg.Launch.Commands) || cfg.BundleID != ""
	if caps.Kind == drivers.KindMac {
		// A mac target's tree, taps and typing all come from cua-driver;
		// reading axe/idb presence here would report iOS state about a
		// platform those tools cannot touch.
		if tools["cua-driver"] {
			caps.Accessibility = true
			caps.AccessibilityDriver = "cua"
			caps.SemanticActions = true
			caps.CoordinateTap = true
			caps.CoordinateTapDriver = "cua"
		}
		if tools["mitmdump"] {
			caps.NetworkCapture = true
			caps.NetworkCaptureDriver = "mitmproxy"
		}
		c.resolveMacPermissions(ctx, &caps)
		if tools["xcrun"] {
			caps.Debug = c.Runner.Run(ctx, "xcrun", "--find", "lldb-dap").Err == nil
		}
		return caps
	}
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
	caps.WallClock = tools["simtime"]
	if tools["xcrun"] {
		caps.Debug = c.Runner.Run(ctx, "xcrun", "--find", "lldb-dap").Err == nil
	}
	return caps
}

// resolveMacPermissions asks cua-driver for the TCC state.
//
// And it asks the DAEMON, which is what makes the answer useful: it
// answers with CuaDriver.app's identity, which is who really holds the
// permissions. A probe looking at the permissions of the process running
// mav would say nothing, because on macOS a CLI never has them: only
// interactive GUI processes can, hence the whole broker architecture.
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
	// Its own grant flow launches the app through LaunchServices so the
	// dialogs are attributed to it, and registers it in the panels.
	// Granting to the terminal does not help: the daemon is who captures.
	caps.MacPermissionsNext = "cua-driver permissions grant"
}

// macMissingPermissions reads the `cua-driver permissions status` answer.
// Deliberately tolerant: if the output is not understood, no state is
// invented; returning "all good" for an unknown format would be worse than
// admitting it is not known.
func macMissingPermissions(stdout string) []string {
	// Pointers and not bool: the answer carries `null` when there is no
	// daemon to ask, and an implicit `false` there would lie in the
	// opposite direction, it would say "permission missing" when what
	// happened is that nobody answered.
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
	if caps.Kind != drivers.KindMac {
		if caps.DeviceFallback {
			fields["device_fallback"] = "ok"
			fields["device_fallback_driver"] = caps.DeviceFallbackDriver
		} else {
			fields["device_fallback"] = "missing"
		}
	}
	if caps.Multitouch {
		fields["multitouch"] = "ok"
		fields["multitouch_driver"] = caps.MultitouchDriver
	} else if caps.Kind == drivers.KindMac {
		// Not "missing": a trackpad delivers gestures to the system and the
		// focused app, not to a pid, so no driver can provide this in the
		// background. Prescribing baguette here would send the reader to
		// install a simulator tool for a capability that cannot exist.
		fields["multitouch"] = "unsupported"
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
	} else if caps.Kind == drivers.KindMac {
		// The mac clock is the machine's, handled by `mav time` with its own
		// gates; simtime is a simulator tool and would be dead weight here.
		fields["wall_clock"] = "system"
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
