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
	Identifier string
	UDID       string
	Name       string
	Model      string
	OS         string
	State      string
	Platform   string
}

func ListPhysicalDevices(runner Runner) ([]PhysicalDevice, error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("mav-devices-%d.json", os.Getpid()))
	_ = os.Remove(path)
	result := runner.Run(context.Background(), "xcrun", "devicectl", "list", "devices", "--json-output", path)
	data := []byte(strings.TrimSpace(result.Stdout))
	if len(data) == 0 || data[0] != '{' {
		if fileData, err := os.ReadFile(path); err == nil {
			data = fileData
		}
	}
	_ = os.Remove(path)
	if result.Err != nil && len(data) == 0 {
		return nil, fmt.Errorf("%s", firstLine(result.Stderr))
	}
	return ParsePhysicalDevices(data)
}

func ParsePhysicalDevices(data []byte) ([]PhysicalDevice, error) {
	var parsed struct {
		Result struct {
			Devices []struct {
				Identifier   string `json:"identifier"`
				Capabilities []struct {
					FeatureIdentifier string `json:"featureIdentifier"`
				} `json:"capabilities"`
				ConnectionProperties struct {
					PairingState       string `json:"pairingState"`
					TunnelState        string `json:"tunnelState"`
					TransportType      string `json:"transportType"`
					IsMobileDeviceOnly bool   `json:"isMobileDeviceOnly"`
				} `json:"connectionProperties"`
				DeviceProperties struct {
					Name                 string `json:"name"`
					OSVersionNumber      string `json:"osVersionNumber"`
					DeveloperModeStatus  string `json:"developerModeStatus"`
					DDIServicesAvailable bool   `json:"ddiServicesAvailable"`
				} `json:"deviceProperties"`
				HardwareProperties struct {
					DeviceType    string `json:"deviceType"`
					MarketingName string `json:"marketingName"`
					Platform      string `json:"platform"`
					ProductType   string `json:"productType"`
					Reality       string `json:"reality"`
					UDID          string `json:"udid"`
				} `json:"hardwareProperties"`
			} `json:"devices"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	devices := []PhysicalDevice{}
	for _, device := range parsed.Result.Devices {
		hw := device.HardwareProperties
		props := device.DeviceProperties
		conn := device.ConnectionProperties
		platform := strings.ToLower(hw.Platform)
		if hw.Reality != "physical" {
			continue
		}
		if platform != "ios" && platform != "ipados" {
			continue
		}
		if conn.PairingState != "paired" {
			continue
		}
		state := "unavailable"
		if hasAvailableDeviceFeature(device.Capabilities) || conn.TunnelState == "connected" || conn.TransportType != "" {
			state = "available"
		}
		if state != "available" {
			continue
		}
		model := hw.MarketingName
		if model == "" {
			model = hw.ProductType
		}
		devices = append(devices, PhysicalDevice{
			Identifier: device.Identifier,
			UDID:       hw.UDID,
			Name:       props.Name,
			Model:      model,
			OS:         props.OSVersionNumber,
			State:      state,
			Platform:   hw.Platform,
		})
	}
	return devices, nil
}

func hasAvailableDeviceFeature(capabilities []struct {
	FeatureIdentifier string `json:"featureIdentifier"`
}) bool {
	for _, capability := range capabilities {
		switch capability.FeatureIdentifier {
		case "com.apple.coredevice.feature.acquireusageassertion", "com.apple.coredevice.feature.connectdevice":
			return true
		}
	}
	return false
}

func SelectPhysicalDevice(devices []PhysicalDevice, id, name string) (PhysicalDevice, bool) {
	if id != "" {
		for _, device := range devices {
			if device.Identifier == id || device.UDID == id {
				return device, true
			}
		}
		return PhysicalDevice{}, false
	}
	if name != "" {
		lower := strings.ToLower(name)
		for _, device := range devices {
			if strings.Contains(strings.ToLower(device.Name), lower) || strings.Contains(strings.ToLower(device.Model), lower) {
				return device, true
			}
		}
	}
	if len(devices) == 1 {
		return devices[0], true
	}
	return PhysicalDevice{}, false
}

func detectAvailablePhysicalDevice(runner Runner) (PhysicalDevice, bool) {
	devices, err := ListPhysicalDevices(runner)
	if err != nil || len(devices) == 0 {
		return PhysicalDevice{}, false
	}
	return devices[0], true
}
