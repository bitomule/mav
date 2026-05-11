package mav

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testExplicitScreen(id string) Screen {
	return Screen{ID: id, Recognizers: []Recognizer{{Kind: "id", Value: "mav.screen." + id}}}
}

func testExplicitScreenWithEdges(id string, edges ...Edge) Screen {
	screen := testExplicitScreen(id)
	screen.Edges = edges
	return screen
}

func TestRoute(t *testing.T) {
	m := AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home": {
				ID:    "home",
				Edges: []Edge{{To: "settings", ID: "settings_button", Wait: "1000"}},
			},
			"settings": testExplicitScreen("settings"),
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
			"start":    testExplicitScreenWithEdges("start", Edge{To: "wrong", ID: "buy", Confidence: "low"}),
			"home":     {ID: "home", Edges: []Edge{{To: "settings", ID: "settings_button"}}},
			"wrong":    {ID: "wrong"},
			"settings": testExplicitScreen("settings"),
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
			"home":     testExplicitScreenWithEdges("home", Edge{To: "settings", X: "400", Y: "90"}),
			"settings": testExplicitScreen("settings"),
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
			"home":     testExplicitScreenWithEdges("home", Edge{To: "settings", Value: "Email"}),
			"settings": testExplicitScreen("settings"),
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

func TestScreenRecognizerRequiresExplicitScreenID(t *testing.T) {
	screen := Screen{ID: "settings", Recognizers: []Recognizer{{Kind: "text", Value: "Settings"}}}
	elements := []Element{
		{Label: "uUndolly", Role: "application"},
		{ID: "home_settings_button", Label: "Settings", Role: "button"},
		{Label: "Tab Bar", Role: "group"},
	}
	if screenMatches(screen, "", elements) {
		t.Fatalf("text recognizer should not match a mappable screen")
	}
	screen = testExplicitScreen("settings")
	elements = append(elements, Element{ID: "mav.screen.settings", Role: "group"})
	if !screenMatches(screen, "", elements) {
		t.Fatalf("explicit screen id should match settings screen")
	}
	screen.AssertText = "Settings"
	if screenMatches(screen, "", []Element{{Label: "Settings", Role: "heading"}}) {
		t.Fatalf("live tree without explicit screen id should not match")
	}
	screen = Screen{ID: "settings", Recognizers: []Recognizer{{Kind: "id", Value: "mav.screen.home"}}}
	if screenHasExplicitScreenIdentity(screen) || screenMatches(screen, "", []Element{{ID: "mav.screen.home"}}) {
		t.Fatalf("mismatched mav.screen id should not validate or match")
	}
}

func TestValidateAllowsLaunchStartRecognizer(t *testing.T) {
	m := DefaultAppMap("com.example.demo")
	if err := ValidateAppMap(m); err != nil {
		t.Fatalf("launch start should validate: %v", err)
	}
}

func TestObserveScreenDoesNotReuseStaleCurrentForUnmatchedTree(t *testing.T) {
	root := t.TempDir()
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":            testExplicitScreen("start"),
			"photos-to-delete": testExplicitScreen("photos-to-delete"),
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
	if observed.Screen != "unknown" || observed.Source != "identity_missing" || observed.PreviousScreen != "photos-to-delete" {
		t.Fatalf("observed=%+v", observed)
	}
	loaded, err := LoadAppMap(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Screens["photos-to-delete"].Edges) != 0 {
		t.Fatalf("unexpected edge=%+v", loaded.Screens["photos-to-delete"].Edges)
	}
	if _, ok := peekPendingMapAction(root); ok {
		t.Fatalf("pending action should be discarded when destination has no screen identity")
	}
}

func TestObserveScreenIdentityMissingClearsCurrentAndPending(t *testing.T) {
	root := t.TempDir()
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
			"home":  testExplicitScreen("home"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", "run1")
	SetPendingMapAction(root, pendingMapAction{From: "home", ID: "details_button"})
	observed, err := ObserveScreenDetailed(root, Config{BundleID: "com.example.demo"}, RunState{ID: "run1", Dir: t.TempDir()}, `[{"AXLabel":"Dynamic","role":"heading"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Source != "identity_missing" || CurrentScreen(root) != "" {
		t.Fatalf("observed=%+v current=%q", observed, CurrentScreen(root))
	}
	if _, ok := peekPendingMapAction(root); ok {
		t.Fatalf("pending action should be discarded when destination has no screen identity")
	}
}

func TestObserveScreenUsesLaunchRecognizerForCurrentStart(t *testing.T) {
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
	if observed.Screen != "start" || observed.Source != "current" {
		t.Fatalf("observed=%+v", observed)
	}
}

func TestObserveScreenPrefersExplicitIdentityOverCurrentLaunch(t *testing.T) {
	root := t.TempDir()
	m := DefaultAppMap("com.example.demo")
	m.Screens["home"] = testExplicitScreen("home")
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "start", "run1")
	raw := `[{"AXIdentifier":"mav.screen.home","role":"group","children":[{"AXLabel":"Home","role":"heading"}]}]`
	observed, err := ObserveScreenDetailed(root, Config{BundleID: "com.example.demo"}, RunState{ID: "run1", Dir: t.TempDir()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Screen != "home" || observed.Source != "recognized" {
		t.Fatalf("observed=%+v", observed)
	}
}

func TestObserveScreenDoesNotRecognizeLaunchStartWithoutCurrent(t *testing.T) {
	root := t.TempDir()
	m := DefaultAppMap("com.example.demo")
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	raw := `[{"AXLabel":"Welcome","role":"heading"}]`
	observed, err := ObserveScreenDetailed(root, Config{BundleID: "com.example.demo"}, RunState{ID: "run1", Dir: t.TempDir()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Screen != "unknown" || observed.Source != "identity_missing" {
		t.Fatalf("observed=%+v", observed)
	}
}

func TestRecognizeScreenUsesExplicitScreenIDOverApplicationRoot(t *testing.T) {
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start": testExplicitScreen("start"),
			"home":  testExplicitScreen("home"),
		},
	}
	elements := []Element{
		{ID: "Wallapop", Role: "XCUIElementTypeApplication"},
		{ID: "mav.screen.home", Role: "group"},
	}
	if got := recognizeScreen(m, "", elements); got != "home" {
		t.Fatalf("screen=%q", got)
	}
}

func TestExplicitScreenIdentityIgnoresPersonalizedFeedHeading(t *testing.T) {
	elements := []Element{
		{Label: "Elegidos para conchita", Role: "heading"},
		{ID: "mav.screen.inicio", Label: "Inicio", Role: "tab", Value: "1"},
	}
	identity, ok := explicitScreenIdentity(elements)
	if !ok || identity.ID != "inicio" || identity.Recognizer.Value != "mav.screen.inicio" {
		t.Fatalf("identity=%+v ok=%v", identity, ok)
	}
}

func TestExplicitScreenIdentityUsesNaturalIdentifier(t *testing.T) {
	elements := []Element{
		{ID: "UploadFormView", Role: "group"},
	}
	identity, ok := explicitScreenIdentity(elements)
	if !ok || identity.ID != "upload-form-view" || identity.Recognizer.Value != "UploadFormView" {
		t.Fatalf("identity=%+v ok=%v", identity, ok)
	}
	screen := Screen{ID: "upload-form-view", Recognizers: []Recognizer{identity.Recognizer}}
	if !screenMatches(screen, "", elements) {
		t.Fatalf("natural id should match")
	}
	if err := ValidateAppMap(AppMap{Start: "upload-form-view", Screens: map[string]Screen{"upload-form-view": screen}}); err != nil {
		t.Fatalf("natural id should validate: %v", err)
	}
}

func TestExplicitScreenIdentityIgnoresNestedNaturalWrapper(t *testing.T) {
	elements := []Element{
		{ID: "SettingsSection", Role: "group"},
	}
	if identity, ok := explicitScreenIdentity(elements); ok {
		t.Fatalf("nested wrapper should not be a screen identity: %+v", identity)
	}
}

func TestExplicitScreenIdentityIgnoresWordsEndingInView(t *testing.T) {
	for _, id := range []string{"Preview", "Review", "Overview"} {
		if identity, ok := explicitScreenIdentity([]Element{{ID: id, Role: "group"}}); ok {
			t.Fatalf("%s should not be a screen identity: %+v", id, identity)
		}
	}
}

func TestExplicitScreenIdentityIgnoresDottedSubElementIDs(t *testing.T) {
	// Auto-generated sub-element ids that follow the `<Container>.<element>`
	// convention (codegen tools, manually-typed accessibility ids) must NOT
	// be promoted to screens, even when the trailing segment ends in a
	// screen identifier suffix such as `View`. These are addressable
	// members of a screen, not screens themselves.
	//
	// Cases also cover degenerate edges (leading/trailing dot, multi-dot
	// nesting) so we don't accidentally regress to suffix-only matching.
	subElementIDs := []string{
		"SettingsView.searchField",
		"FormView.headerContainer",
		"NavigationBar.titleLabel",
		"ListView.headerView",
		"Container.Inner.leafView",
		"SettingsView.",
		".searchField",
		".",
	}
	for _, id := range subElementIDs {
		if identity, ok := explicitScreenIdentity([]Element{{ID: id, Role: "group"}}); ok {
			t.Fatalf("sub-element %q should not be a screen identity: %+v", id, identity)
		}
	}
}

func TestNaturalScreenIdentityUsesShallowestScreenIdentifier(t *testing.T) {
	elements := []Element{
		{ID: "HeaderView", Role: "group", Depth: 2},
		{ID: "UploadFormView", Role: "group", Depth: 1},
	}
	identity, ok := explicitScreenIdentity(elements)
	if !ok || identity.ID != "upload-form-view" || identity.Recognizer.Value != "UploadFormView" {
		t.Fatalf("identity=%+v ok=%v", identity, ok)
	}
}

func TestScreenIdentitySlugSplitsAcronyms(t *testing.T) {
	cases := map[string]string{
		"URLImportView": "url-import-view",
		"UploadURLView": "upload-url-view",
		"HTTP2View":     "http2-view",
	}
	for input, want := range cases {
		if got := screenIdentityIDFromSuffix(input); got != want {
			t.Fatalf("%s -> %s, want %s", input, got, want)
		}
	}
}

func TestPrefixedScreenIdentityWinsOverNaturalIdentifier(t *testing.T) {
	elements := []Element{
		{ID: "UploadFormView", Role: "group"},
		{ID: "mav.screen.home", Role: "group"},
	}
	identity, ok := explicitScreenIdentity(elements)
	if !ok || identity.ID != "home" {
		t.Fatalf("identity=%+v ok=%v", identity, ok)
	}
	screen := Screen{ID: "upload-form-view", Recognizers: []Recognizer{{Kind: "id", Value: "UploadFormView"}}}
	if screenMatches(screen, "", elements) {
		t.Fatalf("natural id should not match when tree declares a different mav.screen id")
	}
}

func TestNaturalIdentifierWinsOverExistingRecognizer(t *testing.T) {
	root := t.TempDir()
	cfg := Config{BundleID: "com.example.demo"}
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":            DefaultAppMap("com.example.demo").Screens["start"],
			"old-details":      {ID: "old-details", Recognizers: []Recognizer{{Kind: "id", Value: "StaleDetailsView"}}},
			"upload-form-view": {ID: "upload-form-view", Recognizers: []Recognizer{{Kind: "id", Value: "UploadFormView"}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	raw := `[{"AXIdentifier":"UploadFormView","role":"group"},{"AXIdentifier":"StaleDetailsView","role":"group"}]`
	observed, err := ObserveScreenDetailed(root, cfg, RunState{ID: "run1", Dir: t.TempDir()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Screen != "upload-form-view" || observed.Source != "recognized" {
		t.Fatalf("observed=%+v", observed)
	}
}

func TestExplicitScreenIdentityRejectsInvalidSuffix(t *testing.T) {
	for _, value := range []string{"mav.screen.", "mav.screen.!!!", "screen.   ", "mav.screen.step", "screen.step"} {
		if id, ok := screenIDFromElementID(value); ok {
			t.Fatalf("value=%q id=%q should be rejected", value, id)
		}
	}
	if id, ok := screenIDFromElementID("mav.screen.Fóo Bar"); !ok || id != "foo-bar" {
		t.Fatalf("id=%q ok=%v", id, ok)
	}
}

func TestLoadAppMapDoesNotMigrateExplicitAssertIDRecognizer(t *testing.T) {
	root := t.TempDir()
	if err := SaveAppMap(root, AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":    testExplicitScreen("start"),
			"settings": {ID: "settings", AssertID: "mav.screen.settings"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadAppMap(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAppMap(loaded); err == nil || !strings.Contains(err.Error(), "app_map_screen_identity_missing screen=settings") {
		t.Fatalf("legacy assert_id should not satisfy explicit screen identity: %v", err)
	}
	if screenHasExplicitScreenIdentity(loaded.Screens["settings"]) {
		t.Fatalf("screen=%+v", loaded.Screens["settings"])
	}
}

func TestObserveScreenPersistsDriverOnScreenAndEdge(t *testing.T) {
	root := t.TempDir()
	m := AppMap{
		AppID: "com.example.demo",
		Start: "home",
		Screens: map[string]Screen{
			"home":     testExplicitScreen("home"),
			"settings": testExplicitScreen("settings"),
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", "run1")
	SetPendingMapAction(root, pendingMapAction{From: "home", ID: "settings_button", Driver: "appium"})
	raw := `[{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"}]}]`
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

func TestObserveScreenDoesNotMigrateLegacyTextOnlyScreen(t *testing.T) {
	root := t.TempDir()
	m := AppMap{
		AppID: "com.example.demo",
		Start: "start",
		Screens: map[string]Screen{
			"start":    testExplicitScreen("start"),
			"settings": {ID: "settings", Recognizers: []Recognizer{{Kind: "text", Value: "Settings"}}},
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	raw := `[{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"}]}]`
	observed, err := ObserveScreenDetailed(root, Config{BundleID: "com.example.demo"}, RunState{ID: "run1", Dir: t.TempDir()}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Screen != "settings" || observed.Source != "explicit_id" {
		t.Fatalf("observed=%+v", observed)
	}
	loaded, err := LoadAppMap(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAppMap(loaded); err == nil || !strings.Contains(err.Error(), "app_map_screen_identity_missing screen=settings") {
		t.Fatalf("legacy text-only screen should not be migrated: %v", err)
	}
	if screenHasExplicitScreenIdentity(loaded.Screens["settings"]) {
		t.Fatalf("screen=%+v", loaded.Screens["settings"])
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
			"home":     testExplicitScreen("home"),
			"settings": testExplicitScreen("settings"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", run.ID)
	SetPendingMapAction(root, pendingMapAction{From: "home", Value: "Email", Driver: "appium"})
	if _, err := ObserveScreenDetailedWithDriver(root, cfg, run, `[{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"}]}]`, "appium"); err != nil {
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
			"home":     testExplicitScreen("home"),
			"settings": {ID: "settings", Driver: "appium", Recognizers: []Recognizer{{Kind: "id", Value: "mav.screen.settings"}}},
		},
	}
	if err := SaveAppMap(root, m); err != nil {
		t.Fatal(err)
	}
	SetCurrentScreen(root, "home", "run1")
	raw := `[{"AXIdentifier":"mav.screen.settings","role":"group","children":[{"AXLabel":"Settings","role":"heading"}]}]`
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

func TestUpsertEdgePreservesRecordedAtAndLastSuccess(t *testing.T) {
	// Re-observation of the same transition must not reset the
	// staleness clock — the original RecordedAt and the most
	// recent LastSuccessAt are TTL inputs that the BFS demote
	// rule depends on.
	original := "2026-04-01T10:00:00Z"
	lastSuccess := "2026-05-09T12:00:00Z"
	edges := []Edge{{To: "settings", ID: "settings_button", RecordedAt: original, LastSuccessAt: lastSuccess}}
	updated := upsertEdge(edges, Edge{To: "settings", ID: "settings_button", Driver: "axe"})
	if len(updated) != 1 {
		t.Fatalf("expected single edge, got %d", len(updated))
	}
	if updated[0].RecordedAt != original {
		t.Fatalf("RecordedAt=%q want %q", updated[0].RecordedAt, original)
	}
	if updated[0].LastSuccessAt != lastSuccess {
		t.Fatalf("LastSuccessAt=%q want %q", updated[0].LastSuccessAt, lastSuccess)
	}
}

func TestIsEdgeStaleHonoursLastSuccessAndRecorded(t *testing.T) {
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		edge Edge
		ttl  time.Duration
		want bool
	}{
		{name: "fresh by LastSuccessAt", edge: Edge{LastSuccessAt: now.Add(-3 * 24 * time.Hour).Format(time.RFC3339), RecordedAt: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339)}, ttl: 14 * 24 * time.Hour, want: false},
		{name: "stale by LastSuccessAt", edge: Edge{LastSuccessAt: now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)}, ttl: 14 * 24 * time.Hour, want: true},
		{name: "no LastSuccessAt, fresh RecordedAt", edge: Edge{RecordedAt: now.Add(-5 * 24 * time.Hour).Format(time.RFC3339)}, ttl: 14 * 24 * time.Hour, want: false},
		{name: "no LastSuccessAt, stale RecordedAt", edge: Edge{RecordedAt: now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)}, ttl: 14 * 24 * time.Hour, want: true},
		{name: "no timestamps", edge: Edge{}, ttl: 14 * 24 * time.Hour, want: false},
		{name: "ttl zero disables", edge: Edge{RecordedAt: now.Add(-90 * 24 * time.Hour).Format(time.RFC3339)}, ttl: 0, want: false},
		{name: "garbage timestamp", edge: Edge{RecordedAt: "not-a-date"}, ttl: 14 * 24 * time.Hour, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsEdgeStale(tc.edge, tc.ttl, now); got != tc.want {
				t.Fatalf("IsEdgeStale=%v want %v", got, tc.want)
			}
		})
	}
}

func TestRouteFromWithTTLPrefersFreshEdgesAndFallsBackToStale(t *testing.T) {
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	stale := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)

	t.Run("fresh edge wins when both exist", func(t *testing.T) {
		m := AppMap{
			AppID: "x",
			Start: "home",
			Screens: map[string]Screen{
				"home": testExplicitScreenWithEdges("home",
					Edge{From: "home", To: "settings_via_stale", ID: "old_btn", RecordedAt: stale},
					Edge{From: "home", To: "target", ID: "new_btn", RecordedAt: fresh},
				),
				"settings_via_stale": testExplicitScreen("settings_via_stale"),
				"target":             testExplicitScreen("target"),
			},
		}
		route, err := RouteFromWithTTL(m, "home", "target", 14*24*time.Hour, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(route) != 1 || route[0].ID != "new_btn" {
			t.Fatalf("route=%+v", route)
		}
	})

	t.Run("stale edge used when no fresh alternative exists", func(t *testing.T) {
		m := AppMap{
			AppID: "x",
			Start: "home",
			Screens: map[string]Screen{
				"home":   testExplicitScreenWithEdges("home", Edge{From: "home", To: "target", ID: "ancient_btn", RecordedAt: stale}),
				"target": testExplicitScreen("target"),
			},
		}
		route, err := RouteFromWithTTL(m, "home", "target", 14*24*time.Hour, now)
		if err != nil {
			t.Fatalf("expected fallback to stale edge, got err=%v", err)
		}
		if len(route) != 1 || route[0].ID != "ancient_btn" {
			t.Fatalf("route=%+v", route)
		}
	})

	t.Run("zero ttl disables staleness gate entirely", func(t *testing.T) {
		m := AppMap{
			AppID: "x",
			Start: "home",
			Screens: map[string]Screen{
				"home":   testExplicitScreenWithEdges("home", Edge{From: "home", To: "target", ID: "ancient_btn", RecordedAt: stale}),
				"target": testExplicitScreen("target"),
			},
		}
		route, err := RouteFromWithTTL(m, "home", "target", 0, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(route) != 1 {
			t.Fatalf("route=%+v", route)
		}
	})
}
