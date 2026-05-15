package mav

import (
	"context"
	"strings"
)

type Capabilities struct {
	Tools                  map[string]bool
	LaunchRecipe           bool
	Accessibility          bool
	AccessibilityDriver    string
	SemanticActions        bool
	CoordinateTap          bool
	CoordinateTapDriver    string
	DeviceFallback         bool
	DeviceFallbackDriver   string
	Multitouch             bool
	MultitouchDriver       string
	NetworkCapture         bool
	NetworkCaptureDriver   string
	IDBIssue               string
	IDBNext                string
}

func (c CLI) resolveCapabilities(ctx context.Context, cfg Config) Capabilities {
	tools := map[string]bool{}
	for _, tool := range []string{"go", "bazelisk", "xcrun", "axe", "idb", "baguette", "mitmdump", "pipx", "python3.12", "python3.13", "python3.14"} {
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
	return caps
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
