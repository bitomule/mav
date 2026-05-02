package mav

import (
	"strings"
	"testing"
)

func TestRouteAndMaestroFlow(t *testing.T) {
	m := AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home": {
				ID:    "home",
				Edges: []Edge{{To: "settings", ID: "settings_button", Wait: "1000"}},
			},
			"settings": {ID: "settings", AssertID: "settings_view"},
		},
	}
	route, err := Route(m, "settings")
	if err != nil {
		t.Fatal(err)
	}
	if len(route) != 1 || route[0].ID != "settings_button" {
		t.Fatalf("route=%+v", route)
	}
	flow := MaestroFlow(m, route, "settings")
	for _, want := range []string{"appId: com.example.demo", "id: settings_button", "id: settings_view"} {
		if !strings.Contains(flow, want) {
			t.Fatalf("flow missing %q:\n%s", want, flow)
		}
	}
}

func TestRouteUnknownScreen(t *testing.T) {
	m := DefaultAppMap("com.example.demo")
	_, err := Route(m, "settings")
	if err == nil || err.Error() != "screen_not_found" {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateMissingEdgeTarget(t *testing.T) {
	m := DefaultAppMap("com.example.demo")
	screen := m.Screens["start"]
	screen.Edges = []Edge{{To: "missing", ID: "button"}}
	m.Screens["start"] = screen
	err := ValidateAppMap(m)
	if err == nil || !strings.Contains(err.Error(), "target_not_found") {
		t.Fatalf("err=%v", err)
	}
}
