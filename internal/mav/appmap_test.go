package mav

import (
	"os"
	"path/filepath"
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

func TestSaveAppMapWritesJSONIndexAndScreens(t *testing.T) {
	root := t.TempDir()
	m := AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home":     {ID: "home", Title: "Home", Edges: []Edge{{To: "settings", ID: "settings_button"}}},
			"settings": {ID: "settings", Title: "Settings", AssertText: "Settings"},
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, MapIndexFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, MapScreensDirName, "settings.json")); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadAppMap(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Start != "home" || loaded.Screens["settings"].AssertText != "Settings" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestEmptyAXTreeDetection(t *testing.T) {
	raw := `[{"AXFrame":"{{0, 0}, {0, 0}}","role":"AXApplication","children":[]}]`
	if !isEmptyAXTree(raw) {
		t.Fatalf("expected empty AX tree")
	}
	nonEmpty := `[{"AXFrame":"{{0, 0}, {440, 956}}","role":"AXApplication","children":[{"AXLabel":"Settings","role":"AXStaticText"}]}]`
	if isEmptyAXTree(nonEmpty) {
		t.Fatalf("expected non-empty AX tree")
	}
}

func TestScreenTextRecognizerIgnoresButtonsAndGroups(t *testing.T) {
	screen := Screen{ID: "settings", AssertText: "Settings", Recognizers: []Recognizer{{Kind: "text", Value: "Settings"}}}
	elements := []Element{
		{Label: "uUndolly", Role: "application"},
		{ID: "home_settings_button", Label: "Settings", Role: "button"},
		{Label: "Tab Bar", Role: "group"},
	}
	if screenMatches(screen, "", elements) {
		t.Fatalf("home settings button should not match settings screen")
	}
	elements = append(elements, Element{Label: "Settings", Role: "heading"})
	if !screenMatches(screen, "", elements) {
		t.Fatalf("settings heading should match settings screen")
	}
}
