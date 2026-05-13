package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type PhysicalDevice struct {
	UDID string
	Name string
}

func ListPhysicalDevices(ctx context.Context, runner Runner) ([]PhysicalDevice, error) {
	file, err := os.CreateTemp("", "mav-devices-*.json")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)

	result := runner.Run(ctx, "xcrun", "devicectl", "list", "devices", "--json-output", path)
	if result.Err != nil {
		if result.Stderr != "" {
			return nil, fmt.Errorf("%s", firstLine(result.Stderr))
		}
		return nil, result.Err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parsePhysicalDevicesJSON(data), nil
}

func parsePhysicalDevicesJSON(data []byte) []PhysicalDevice {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	devices := []PhysicalDevice{}
	seen := map[string]bool{}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if device := physicalDeviceFromMap(v); device.UDID != "" {
				if !seen[device.UDID] {
					seen[device.UDID] = true
					devices = append(devices, device)
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(root)
	return devices
}

func physicalDeviceFromMap(m map[string]any) PhysicalDevice {
	udid := firstStringField(m,
		"udid",
		"UDID",
		"identifier",
		"Identifier",
		"deviceIdentifier",
		"deviceIdentifier.identifier",
		"hardwareProperties.udid",
		"hardwareProperties.serialNumber",
	)
	name := firstStringField(m, "name", "Name", "deviceName", "properties.name")
	if udid == "" {
		return PhysicalDevice{}
	}
	return PhysicalDevice{UDID: udid, Name: name}
}

func firstStringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := nestedStringField(m, strings.Split(key, ".")...); value != "" {
			return value
		}
	}
	return ""
}

func nestedStringField(value any, path ...string) string {
	current := value
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	if str, ok := current.(string); ok {
		return str
	}
	return ""
}

func selectPhysicalDevice(devices []PhysicalDevice, udid, name string) (PhysicalDevice, bool) {
	for _, device := range devices {
		if udid != "" && device.UDID == udid {
			return device, true
		}
		if name != "" && device.Name == name {
			return device, true
		}
	}
	return PhysicalDevice{}, false
}
