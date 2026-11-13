package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PhysicalDevice struct {
	UDID string
	Name string
}

func ListPhysicalDevices(ctx context.Context, runner Runner) ([]PhysicalDevice, error) {
	// Keep the result in the project rather than the host's shared temp dir.
	tempRoot := filepath.Join(".mav", "tmp", "devices")
	if err := os.MkdirAll(tempRoot, 0700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(tempRoot, "mav-devices-*.json")
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
	devices := parsePhysicalDevicesJSON(data)
	if devices == nil {
		return nil, fmt.Errorf("devicectl device list result is unreadable")
	}
	return devices, nil
}

func parsePhysicalDevicesJSON(data []byte) []PhysicalDevice {
	var root struct {
		Result struct {
			Devices []map[string]any `json:"devices"`
		} `json:"result"`
		Devices []map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	devices := []PhysicalDevice{}
	seen := map[string]bool{}
	rows := root.Result.Devices
	if rows == nil {
		rows = root.Devices
	}
	if rows == nil {
		return nil
	}
	for _, row := range rows {
		device := physicalDeviceFromMap(row)
		if device.UDID != "" && !seen[device.UDID] {
			seen[device.UDID] = true
			devices = append(devices, device)
		}
	}
	return devices
}

func physicalDeviceFromMap(m map[string]any) PhysicalDevice {
	// CoreDevice also lists same-machine simulators and companion platforms.
	// Offline iPhones/iPads remain visible so readiness can explain their state.
	transport := firstStringField(m, "properties.connection.transportType", "connectionProperties.transportType")
	if strings.EqualFold(transport, "sameMachine") {
		return PhysicalDevice{}
	}
	deviceType := firstStringField(m, "properties.hardware.deviceType", "hardwareProperties.deviceType")
	if deviceType != "" && !strings.EqualFold(deviceType, "iPhone") && !strings.EqualFold(deviceType, "iPad") {
		return PhysicalDevice{}
	}
	udid := firstStringField(m,
		"properties.hardware.udid",
		"hardwareProperties.udid",
		"udid",
		"UDID",
	)
	// CoreDevice's identifier is its own UUID, not the hardware UDID AXe
	// takes. Serial numbers and identifiers of nested objects are not devices.
	name := firstStringField(m, "properties.device.name", "deviceProperties.name", "name", "Name", "deviceName", "properties.name")
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
	var match PhysicalDevice
	matches := 0
	for _, device := range devices {
		if udid != "" {
			if device.UDID == udid {
				return device, true
			}
		} else if name != "" && device.Name == name {
			match = device
			matches++
		}
	}
	return match, matches == 1
}
