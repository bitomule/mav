package mav

import (
	"strings"
	"testing"
)

const sampleDeviceJSON = `{
  "result": {
    "devices": [
      {
        "capabilities": [{"featureIdentifier": "com.apple.coredevice.feature.connectdevice"}],
        "connectionProperties": {"pairingState": "paired", "transportType": "localNetwork"},
        "deviceProperties": {"name": "David iPhone", "osVersionNumber": "26.3.1"},
        "hardwareProperties": {"marketingName": "iPhone Air", "platform": "iOS", "productType": "iPhone18,4", "reality": "physical", "udid": "REAL-UDID"},
        "identifier": "COREDEVICE-ID"
      },
      {
        "capabilities": [],
        "connectionProperties": {"pairingState": "paired"},
        "deviceProperties": {"name": "Offline iPhone", "osVersionNumber": "26.0"},
        "hardwareProperties": {"marketingName": "iPhone 16 Pro", "platform": "iOS", "reality": "physical", "udid": "OFFLINE-UDID"},
        "identifier": "OFFLINE-ID"
      },
      {
        "capabilities": [{"featureIdentifier": "com.apple.coredevice.feature.connectdevice"}],
        "connectionProperties": {"pairingState": "paired", "transportType": "localNetwork"},
        "deviceProperties": {"name": "Apple Watch", "osVersionNumber": "11.5"},
        "hardwareProperties": {"marketingName": "Apple Watch", "platform": "watchOS", "reality": "physical", "udid": "WATCH-UDID"},
        "identifier": "WATCH-ID"
      }
    ]
  }
}`

func TestParsePhysicalDevicesFiltersAvailablePairedIOSDevices(t *testing.T) {
	devices, err := ParsePhysicalDevices([]byte(sampleDeviceJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices=%+v", devices)
	}
	got := devices[0]
	if got.Identifier != "COREDEVICE-ID" || got.UDID != "REAL-UDID" || got.Name != "David iPhone" || got.Model != "iPhone Air" || got.OS != "26.3.1" {
		t.Fatalf("device=%+v", got)
	}
}

func TestSelectPhysicalDeviceByIDOrName(t *testing.T) {
	devices, err := ParsePhysicalDevices([]byte(sampleDeviceJSON))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := SelectPhysicalDevice(devices, "COREDEVICE-ID", ""); !ok || got.UDID != "REAL-UDID" {
		t.Fatalf("by id got=%+v ok=%v", got, ok)
	}
	if got, ok := SelectPhysicalDevice(devices, "", "air"); !ok || !strings.Contains(got.Model, "Air") {
		t.Fatalf("by name got=%+v ok=%v", got, ok)
	}
}
