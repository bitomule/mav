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

func TestRouteFromUsesObservedStartAndSkipsLowConfidenceEdges(t *testing.T) {
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":    {ID: "start", Edges: []Edge{{To: "wrong", ID: "buy", Confidence: "low"}}},
			"home":     {ID: "home", Edges: []Edge{{To: "settings", ID: "settings_button"}}},
			"wrong":    {ID: "wrong"},
			"settings": {ID: "settings", AssertID: "settings_view"},
		},
	}
	if _, err := RouteFrom(m, "start", "wrong"); err == nil || err.Error() != "route_not_found" {
		t.Fatalf("low confidence edge should not route: %v", err)
	}
	route, err := RouteFrom(m, "home", "settings")
	if err != nil {
		t.Fatal(err)
	}
	if len(route) != 1 || route[0].From != "home" {
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

func TestValidateAllowsValueEdge(t *testing.T) {
	m := AppMap{
		Start: "home",
		Screens: map[string]Screen{
			"home":     {ID: "home", Edges: []Edge{{To: "settings", Value: "Email"}}},
			"settings": {ID: "settings"},
		},
	}
	if err := ValidateAppMap(m); err != nil {
		t.Fatalf("value edge should be valid: %v", err)
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

func TestObserveScreenDoesNotReuseStaleCurrentForUnmatchedTree(t *testing.T) {
	root := t.TempDir()
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":            {ID: "start", AssertText: "Home"},
			"photos-to-delete": {ID: "photos-to-delete", AssertText: "Photos to Delete"},
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "photos-to-delete", "run1")
	SetPendingMapAction(root, pendingMapAction{From: "photos-to-delete", ID: "next_button"})
	raw := `[{"AXLabel":"Delete","role":"button"}]`
	observed, err := ObserveScreenDetailed(root, Config{BundleID: "com.example.demo"}, RunState{ID: "run1", Dir: t.TempDir()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Screen != "unknown" || observed.Source != "unmatched" || observed.PreviousScreen != "photos-to-delete" {
		t.Fatalf("observed=%+v", observed)
	}
	loaded, err := LoadAppMap(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Screens["photos-to-delete"].Edges) != 0 {
		t.Fatalf("unexpected edge=%+v", loaded.Screens["photos-to-delete"].Edges)
	}
	if _, ok := consumePendingMapAction(root); !ok {
		t.Fatalf("pending action should remain for a confident observation")
	}
}

func TestObserveScreenDoesNotTreatBlankStartAsCatchAll(t *testing.T) {
	root := t.TempDir()
	m := DefaultAppMap("com.example.demo")
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "start", "run1")
	raw := `[{"AXLabel":"Welcome","role":"heading"}]`
	observed, err := ObserveScreenDetailed(root, Config{BundleID: "com.example.demo"}, RunState{ID: "run1", Dir: t.TempDir()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Screen == "start" || observed.Source == "start" {
		t.Fatalf("blank start should not catch all trees: %+v", observed)
	}
	if observed.Screen != "welcome" || observed.Source != "inferred" {
		t.Fatalf("observed=%+v", observed)
	}
}

func TestRecognizeScreenPrefersSpecificTextOverApplicationRootStart(t *testing.T) {
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start": {ID: "start", Recognizers: []Recognizer{{Kind: "id", Value: "Wallapop"}}},
			"home":  {ID: "home", Recognizers: []Recognizer{{Kind: "text", Value: "Home"}}},
		},
	}
	elements := []Element{
		{ID: "Wallapop", Role: "XCUIElementTypeApplication"},
		{Label: "Home", Role: "heading"},
	}
	if got := recognizeScreen(m, "", elements); got != "home" {
		t.Fatalf("screen=%q", got)
	}
}

func TestInferScreenTitleRejectsPersonalizedFeedHeading(t *testing.T) {
	elements := []Element{
		{Label: "Elegidos para conchita", Role: "heading"},
		{ID: "Inicio", Label: "Inicio", Role: "tab", Value: "1"},
	}
	if got := inferScreenTitle(elements, "home"); got != "Home" {
		t.Fatalf("title=%q", got)
	}
	if got := inferScreenID(elements); got != "home" {
		t.Fatalf("screen id=%q", got)
	}
	elements[1].Role = "XCUIElementTypeButton"
	if got := inferScreenID(elements); got != "home" {
		t.Fatalf("appium button tab screen id=%q", got)
	}
	if !isScreenTitle("Sign in to continue") {
		t.Fatalf("stable connector title should be accepted")
	}
}

func TestObserveScreenPersistsDriverOnScreenAndEdge(t *testing.T) {
	root := t.TempDir()
	m := AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home":     {ID: "home", AssertText: "Home"},
			"settings": {ID: "settings", AssertText: "Settings"},
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", "run1")
	SetPendingMapAction(root, pendingMapAction{From: "home", ID: "settings_button", Driver: "appium"})
	raw := `[{"AXLabel":"Settings","role":"heading"}]`
	observed, err := ObserveScreenDetailedWithDriver(root, Config{BundleID: "com.example.demo"}, RunState{ID: "run1", Dir: t.TempDir()}, raw, "appium")
	if err != nil {
		t.Fatal(err)
	}
	if observed.Screen != "settings" {
		t.Fatalf("observed=%+v", observed)
	}
	loaded, err := LoadAppMap(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Screens["settings"].Driver != "appium" {
		t.Fatalf("screen driver=%q", loaded.Screens["settings"].Driver)
	}
	edges := loaded.Screens["home"].Edges
	if len(edges) != 1 || edges[0].Driver != "appium" {
		t.Fatalf("edges=%+v", edges)
	}
}

func TestObserveScreenPersistsValueTapEdge(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	run := RunState{ID: "run-value", Dir: filepath.Join(os.TempDir(), "mav", "run-value")}
	t.Cleanup(func() { _ = os.RemoveAll(run.Dir) })
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.app",
		Start: "home",
		Screens: map[string]Screen{
			"home":     {ID: "home", AssertText: "Home"},
			"settings": {ID: "settings", AssertText: "Settings"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", run.ID)
	SetPendingMapAction(root, pendingMapAction{From: "home", Value: "Email", Driver: "appium"})
	if _, err := ObserveScreenDetailedWithDriver(root, cfg, run, `[{"AXLabel":"Settings","role":"heading"}]`, "appium"); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadAppMap(root)
	if err != nil {
		t.Fatal(err)
	}
	edges := loaded.Screens["home"].Edges
	if len(edges) != 1 || edges[0].Value != "Email" || edges[0].Driver != "appium" {
		t.Fatalf("edges=%+v", edges)
	}
}

func TestObserveScreenPreservesAppiumDriverHint(t *testing.T) {
	root := t.TempDir()
	m := AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home":     {ID: "home", AssertText: "Home"},
			"settings": {ID: "settings", AssertText: "Settings", Driver: "appium"},
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", "run1")
	raw := `[{"AXLabel":"Settings","role":"heading"}]`
	if _, err := ObserveScreenDetailedWithDriver(root, Config{BundleID: "com.example.demo"}, RunState{ID: "run1", Dir: t.TempDir()}, raw, "axe"); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadAppMap(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Screens["settings"].Driver != "appium" {
		t.Fatalf("screen driver=%q", loaded.Screens["settings"].Driver)
	}
}

func TestUpsertEdgePreservesAppiumDriverHint(t *testing.T) {
	edges := []Edge{{To: "settings", ID: "settings_button", Driver: "appium"}}
	updated := upsertEdge(edges, Edge{To: "settings", ID: "settings_button", Driver: "axe"})
	if len(updated) != 1 || updated[0].Driver != "appium" {
		t.Fatalf("edges=%+v", updated)
	}
}

func TestUpsertEdgePreservesFromConfidenceAndFailures(t *testing.T) {
	edges := []Edge{{From: "home", To: "settings", ID: "settings_button", Confidence: "high", FailureCount: 1, LastFailure: "then"}}
	updated := upsertEdge(edges, Edge{To: "settings", ID: "settings_button", Driver: "axe"})
	if len(updated) != 1 || updated[0].From != "home" || updated[0].Confidence != "high" || updated[0].FailureCount != 1 || updated[0].LastFailure != "then" {
		t.Fatalf("edges=%+v", updated)
	}
}
