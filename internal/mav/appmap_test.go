package mav

import (
	"strings"
	"testing"
)

func TestRoute(t *testing.T) {
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

func TestValidateAllowsCoordinateEdge(t *testing.T) {
	m := AppMap{
		Start: "home",
		Screens: map[string]Screen{
			"home":     {ID: "home", Edges: []Edge{{To: "settings", X: "400", Y: "90"}}},
			"settings": {ID: "settings"},
		},
	}
	if err := ValidateAppMap(m); err != nil {
		t.Fatalf("err=%v", err)
	}
}
