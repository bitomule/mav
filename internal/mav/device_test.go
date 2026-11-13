package mav

import (
	"reflect"
	"testing"
)

func TestPhysicalDevicesUseHardwareUDIDAcrossCoreDeviceSchemas(t *testing.T) {
	raw := `{"result":{"devices":[
	 {"identifier":"core-device-uuid","properties":{"hardware":{"udid":"00008101-000A0C123456001E"},"device":{"name":"Test iPhone"}}},
	 {"identifier":"older-core-uuid","hardwareProperties":{"udid":"00008101-000A0C123456001F","serialNumber":"SERIAL"},"deviceProperties":{"name":"Test iPad"}},
	 {"hardwareProperties":{"udid":"00008101-000A0C123456001E"},"deviceProperties":{"name":"Duplicate"}},
	 {"identifier":"not-a-hardware-udid","hardwareProperties":{"serialNumber":"SERIAL-ONLY"},"deviceProperties":{"name":"Unavailable"}},
	 {"identifier":"nested-object","properties":{"nested":{"udid":"not-a-device","name":"Nested"}}}
	 ]}}`
	want := []PhysicalDevice{{UDID: "00008101-000A0C123456001E", Name: "Test iPhone"}, {UDID: "00008101-000A0C123456001F", Name: "Test iPad"}}
	if got := parsePhysicalDevicesJSON([]byte(raw)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestPhysicalDevicesAcceptExplicitLegacyUDID(t *testing.T) {
	got := parsePhysicalDevicesJSON([]byte(`{"devices":[{"udid":"REAL-1","name":"Test iPhone"}]}`))
	if len(got) != 1 || got[0].UDID != "REAL-1" {
		t.Fatalf("devices=%#v", got)
	}
}

func TestPhysicalDeviceListDistinguishesEmptyFromUnreadable(t *testing.T) {
	for _, raw := range []string{`not JSON`, `{}`, `{"result":{"devices":null}}`, `{"result":{"devices":"unknown"}}`} {
		if got := parsePhysicalDevicesJSON([]byte(raw)); got != nil {
			t.Fatalf("unreadable list treated as devices: %#v", got)
		}
	}
	if got := parsePhysicalDevicesJSON([]byte(`{"result":{"devices":[]}}`)); got == nil || len(got) != 0 {
		t.Fatalf("valid empty list=%#v", got)
	}
}

func TestPhysicalDeviceListExcludesSimulatorsAndCompanionPlatforms(t *testing.T) {
	raw := `{"result":{"devices":[
	 {"properties":{"hardware":{"udid":"SIM-1","deviceType":"iPhone"},"connection":{"transportType":"sameMachine"},"device":{"name":"Simulator"}}},
	 {"hardwareProperties":{"udid":"SIM-2","deviceType":"iPad"},"connectionProperties":{"transportType":"sameMachine"}},
	 {"hardwareProperties":{"udid":"WATCH-1","deviceType":"appleWatch"},"connectionProperties":{"transportType":"wired"}},
	 {"properties":{"hardware":{"udid":"MAC-1","deviceType":"Mac"},"connection":{"transportType":"network"}}},
	 {"properties":{"hardware":{"udid":"REAL-1","deviceType":"iPhone"},"connection":{"transportType":"network"},"device":{"name":"Offline iPhone"},"state":{"visibilityClass":"unavailable"}}},
	 {"hardwareProperties":{"udid":"REAL-2","deviceType":"iPad"},"connectionProperties":{"transportType":"wired"},"deviceProperties":{"name":"Test iPad"}}
	 ]}}`
	want := []PhysicalDevice{{UDID: "REAL-1", Name: "Offline iPhone"}, {UDID: "REAL-2", Name: "Test iPad"}}
	if got := parsePhysicalDevicesJSON([]byte(raw)); !reflect.DeepEqual(got, want) {
		t.Fatalf("devices=%#v want=%#v", got, want)
	}
}

func TestPhysicalDeviceSelectionRejectsAmbiguousNameAndDoesNotOverrideUDID(t *testing.T) {
	devices := []PhysicalDevice{{UDID: "REAL-1", Name: "Test iPhone"}, {UDID: "REAL-2", Name: "Test iPhone"}}
	if _, ok := selectPhysicalDevice(devices, "", "Test iPhone"); ok {
		t.Fatal("ambiguous name selected a device")
	}
	if _, ok := selectPhysicalDevice(devices, "MISSING", "Test iPhone"); ok {
		t.Fatal("a name overrode an explicit UDID")
	}
	if got, ok := selectPhysicalDevice(devices, "REAL-2", ""); !ok || got.UDID != "REAL-2" {
		t.Fatalf("got=%#v ok=%v", got, ok)
	}
}
