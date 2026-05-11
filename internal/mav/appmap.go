package mav

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MapDirName        = ".mav/map"
	MapScreensDirName = ".mav/map/screens"
	MapIndexFile      = ".mav/map/index.json"
	MapCurrentFile    = ".mav/map/current.json"
	MapPendingFile    = ".mav/map/pending.json"
)

type AppMap struct {
	AppID   string            `json:"app_id"`
	Start   string            `json:"start"`
	Screens map[string]Screen `json:"-"`
}

type Screen struct {
	ID          string       `json:"id"`
	Title       string       `json:"title,omitempty"`
	Driver      string       `json:"driver,omitempty"`
	AssertID    string       `json:"assert_id,omitempty"`
	AssertText  string       `json:"assert_text,omitempty"`
	Recognizers []Recognizer `json:"recognizers,omitempty"`
	Elements    []Element    `json:"elements,omitempty"`
	Edges       []Edge       `json:"edges,omitempty"`
	TreeFile    string       `json:"tree_file,omitempty"`
	UpdatedAt   string       `json:"updated_at,omitempty"`
}

type Recognizer struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Element struct {
	ID      string `json:"id,omitempty"`
	Label   string `json:"label,omitempty"`
	Role    string `json:"role,omitempty"`
	Value   string `json:"value,omitempty"`
	Frame   string `json:"frame,omitempty"`
	Enabled string `json:"enabled,omitempty"`
	Subrole string `json:"subrole,omitempty"`
	Title   string `json:"title,omitempty"`
	PID     string `json:"pid,omitempty"`
	Focused string `json:"focused,omitempty"`
	Depth   int    `json:"depth,omitempty"`
}

type Edge struct {
	From          string `json:"from,omitempty"`
	To            string `json:"to"`
	ID            string `json:"id,omitempty"`
	Text          string `json:"text,omitempty"`
	Value         string `json:"value,omitempty"`
	X             string `json:"x,omitempty"`
	Y             string `json:"y,omitempty"`
	Wait          string `json:"wait,omitempty"`
	Driver        string `json:"driver,omitempty"`
	Source        string `json:"source,omitempty"`
	Confidence    string `json:"confidence,omitempty"`
	LastFailure   string `json:"last_failure,omitempty"`
	FailureCount  int    `json:"failure_count,omitempty"`
	RecordedAt    string `json:"recorded_at,omitempty"`     // RFC3339 timestamp of first observation.
	LastSuccessAt string `json:"last_success_at,omitempty"` // RFC3339 timestamp of last successful route replay.
}

// DefaultEdgeTTL is the maximum age, since the last observed success
// (or the original recording if never replayed), after which edges
// are considered stale and get demoted in BFS scoring. Users can
// override it via `--edge-ttl` on `mav go` or the `route.edge_ttl`
// config key.
//
// 14 days is empirical: short enough that a UI refactor caught in
// the same release cycle won't keep stale edges alive, but long
// enough that weekly-cadence test runs never trip the gate.
const DefaultEdgeTTL = 14 * 24 * time.Hour

// IsEdgeStale reports whether `now - edge.LastSuccessAt` (or
// `RecordedAt` when the edge has never been successfully replayed)
// exceeds the supplied TTL. A zero TTL disables the check.
func IsEdgeStale(edge Edge, ttl time.Duration, now time.Time) bool {
	if ttl <= 0 {
		return false
	}
	stamp := edge.LastSuccessAt
	if stamp == "" {
		stamp = edge.RecordedAt
	}
	if stamp == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return false
	}
	return now.Sub(t) > ttl
}

type mapIndex struct {
	AppID   string   `json:"app_id"`
	Start   string   `json:"start"`
	Screens []string `json:"screens"`
}

type mapCurrent struct {
	Screen string `json:"screen"`
	Run    string `json:"run,omitempty"`
}

type pendingMapAction struct {
	From   string `json:"from"`
	ID     string `json:"id,omitempty"`
	Text   string `json:"text,omitempty"`
	Value  string `json:"value,omitempty"`
	X      string `json:"x,omitempty"`
	Y      string `json:"y,omitempty"`
	Driver string `json:"driver,omitempty"`
}

type screenIdentity struct {
	ID         string
	ElementID  string
	Recognizer Recognizer
}

type ScreenObservation struct {
	Screen         string
	Source         string
	PreviousScreen string
	Elements       []Element
}

func DefaultAppMap(bundleID string) AppMap {
	return AppMap{
		AppID:   bundleID,
		Start:   "start",
		Screens: map[string]Screen{"start": {ID: "start", Title: "Start", Recognizers: []Recognizer{{Kind: "launch"}}}},
	}
}

func LoadAppMap(root string) (AppMap, error) {
	if exists(filepath.Join(root, MapIndexFile)) {
		return loadJSONAppMap(root)
	}
	if exists(filepath.Join(root, AppMapFile)) {
		return loadYAMLAppMap(root)
	}
	return AppMap{}, fmt.Errorf("app_map_not_found path=%s", filepath.Join(root, MapIndexFile))
}

func EnsureAppMap(root string, cfg Config) (AppMap, error) {
	m, err := LoadAppMap(root)
	if err == nil {
		if m.AppID == "" {
			m.AppID = cfg.BundleID
		}
		return m, nil
	}
	m = DefaultAppMap(cfg.BundleID)
	return m, SaveAppMap(root, m)
}

func loadJSONAppMap(root string) (AppMap, error) {
	data, err := os.ReadFile(filepath.Join(root, MapIndexFile))
	if err != nil {
		return AppMap{}, err
	}
	var idx mapIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return AppMap{}, err
	}
	m := AppMap{AppID: idx.AppID, Start: idx.Start, Screens: map[string]Screen{}}
	for _, id := range idx.Screens {
		screen, err := LoadScreen(root, id)
		if err == nil {
			m.Screens[screen.ID] = screen
		}
	}
	if m.Start == "" {
		m.Start = "start"
	}
	return m, nil
}

func LoadScreen(root, id string) (Screen, error) {
	data, err := os.ReadFile(screenPath(root, id))
	if err != nil {
		return Screen{}, err
	}
	var screen Screen
	if err := json.Unmarshal(data, &screen); err != nil {
		return Screen{}, err
	}
	if screen.ID == "" {
		screen.ID = id
	}
	return screen, nil
}

func SaveAppMap(root string, m AppMap) error {
	if err := os.MkdirAll(filepath.Join(root, MapScreensDirName), 0o755); err != nil {
		return err
	}
	if m.Start == "" {
		m.Start = "start"
	}
	keys := sortedScreenKeys(m.Screens)
	idx := mapIndex{AppID: m.AppID, Start: m.Start, Screens: keys}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, MapIndexFile), append(data, '\n'), 0o644); err != nil {
		return err
	}
	for _, id := range keys {
		if err := SaveScreen(root, m.Screens[id]); err != nil {
			return err
		}
	}
	return nil
}

func SaveScreen(root string, screen Screen) error {
	if screen.ID == "" {
		return fmt.Errorf("screen_id_missing")
	}
	if err := os.MkdirAll(filepath.Join(root, MapScreensDirName), 0o755); err != nil {
		return err
	}
	screen.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(screen, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(screenPath(root, screen.ID), append(data, '\n'), 0o644)
}

func screenPath(root, id string) string {
	return filepath.Join(root, MapScreensDirName, safeFileName(id)+".json")
}

func sortedScreenKeys(screens map[string]Screen) []string {
	keys := make([]string, 0, len(screens))
	for key := range screens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ValidateAppMap(m AppMap) error {
	if m.Start == "" {
		return fmt.Errorf("app_map_start_missing")
	}
	if _, ok := m.Screens[m.Start]; !ok {
		return fmt.Errorf("app_map_start_not_found start=%s", m.Start)
	}
	for id, screen := range m.Screens {
		if !screenHasExplicitScreenIdentity(screen) {
			return fmt.Errorf("app_map_screen_identity_missing screen=%s", id)
		}
		for _, edge := range screen.Edges {
			if edge.To == "" {
				return fmt.Errorf("app_map_edge_target_missing screen=%s", id)
			}
			if _, ok := m.Screens[edge.To]; !ok {
				return fmt.Errorf("app_map_edge_target_not_found screen=%s target=%s", id, edge.To)
			}
			if edge.ID == "" && edge.Text == "" && edge.Value == "" && (edge.X == "" || edge.Y == "") {
				return fmt.Errorf("app_map_edge_action_missing screen=%s target=%s", id, edge.To)
			}
		}
	}
	return nil
}

func Route(m AppMap, target string) ([]Edge, error) {
	return RouteFrom(m, m.Start, target)
}

func RouteFrom(m AppMap, start, target string) ([]Edge, error) {
	return RouteFromWithTTL(m, start, target, DefaultEdgeTTL, time.Now().UTC())
}

// RouteFromWithTTL exposes the BFS with a configurable edge TTL so
// the route engine can demote stale edges without skipping them
// outright. Stale edges are visited in a second BFS pass only when
// the fresh-edge pass returned no route — this preserves the "always
// shortest path among healthy edges" guarantee while still finding
// SOMETHING if all known edges are old.
func RouteFromWithTTL(m AppMap, start, target string, ttl time.Duration, now time.Time) ([]Edge, error) {
	if _, ok := m.Screens[target]; !ok {
		return nil, fmt.Errorf("screen_not_found")
	}
	if start == "" {
		start = m.Start
	}
	if _, ok := m.Screens[start]; !ok {
		return nil, fmt.Errorf("route_start_not_found")
	}
	if target == start {
		return nil, nil
	}
	// First pass: only fresh, non-low-confidence edges. Most calls
	// finish here. The pass over stale edges runs as a fallback so
	// the engine can still propose a path on a long-quiet map
	// instead of bailing out with `route_not_found`.
	if route, err := bfsRoute(m, start, target, ttl, now, false); err == nil {
		return route, nil
	}
	return bfsRoute(m, start, target, ttl, now, true)
}

func bfsRoute(m AppMap, start, target string, ttl time.Duration, now time.Time, allowStale bool) ([]Edge, error) {
	type node struct {
		id    string
		route []Edge
	}
	queue := []node{{id: start}}
	seen := map[string]bool{start: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, edge := range m.Screens[cur.id].Edges {
			if edge.Confidence == "low" {
				continue
			}
			if !allowStale && IsEdgeStale(edge, ttl, now) {
				continue
			}
			if seen[edge.To] {
				continue
			}
			if edge.From != "" && edge.From != cur.id {
				continue
			}
			edge.From = cur.id
			nextRoute := append(append([]Edge{}, cur.route...), edge)
			if edge.To == target {
				return nextRoute, nil
			}
			seen[edge.To] = true
			queue = append(queue, node{id: edge.To, route: nextRoute})
		}
	}
	return nil, fmt.Errorf("route_not_found")
}

func loadYAMLAppMap(root string) (AppMap, error) {
	file, err := os.Open(filepath.Join(root, AppMapFile))
	if err != nil {
		return AppMap{}, fmt.Errorf("app_map_not_found path=%s", filepath.Join(root, AppMapFile))
	}
	defer file.Close()

	m := AppMap{Screens: map[string]Screen{}}
	var current *Screen
	var currentEdge *Edge
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(raw, "  ") {
			key, value, ok := splitYAMLKV(line)
			if !ok {
				continue
			}
			switch key {
			case "app_id":
				m.AppID = value
			case "start":
				m.Start = value
			}
			continue
		}
		if strings.HasPrefix(raw, "  ") && !strings.HasPrefix(raw, "    ") && strings.HasSuffix(line, ":") {
			id := strings.TrimSuffix(line, ":")
			screen := Screen{ID: id, Title: strings.Title(strings.ReplaceAll(id, "-", " "))}
			m.Screens[id] = screen
			current = &screen
			currentEdge = nil
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			edge := Edge{Source: "legacy"}
			rest := strings.TrimPrefix(line, "- ")
			key, value, ok := splitYAMLKV(rest)
			if ok && key == "to" {
				edge.To = value
			}
			current.Edges = append(current.Edges, edge)
			currentEdge = &current.Edges[len(current.Edges)-1]
			continue
		}
		key, value, ok := splitYAMLKV(line)
		if !ok {
			continue
		}
		if currentEdge != nil && strings.HasPrefix(raw, "      ") {
			switch key {
			case "id":
				currentEdge.ID = value
			case "text":
				currentEdge.Text = value
			case "value":
				currentEdge.Value = value
			case "x":
				currentEdge.X = value
			case "y":
				currentEdge.Y = value
			case "wait":
				currentEdge.Wait = value
			case "driver":
				currentEdge.Driver = value
			}
		} else {
			switch key {
			case "driver":
				current.Driver = value
			case "assert_id":
				current.AssertID = value
			case "assert_text":
				current.AssertText = value
				current.Recognizers = append(current.Recognizers, Recognizer{Kind: "text", Value: value})
			}
		}
		m.Screens[current.ID] = *current
	}
	if err := scanner.Err(); err != nil {
		return AppMap{}, err
	}
	if m.Start == "" {
		m.Start = "start"
	}
	return m, nil
}

func SetCurrentScreen(root, screenID, runID string) {
	_ = os.MkdirAll(filepath.Join(root, MapDirName), 0o755)
	data, _ := json.MarshalIndent(mapCurrent{Screen: screenID, Run: runID}, "", "  ")
	_ = os.WriteFile(filepath.Join(root, MapCurrentFile), append(data, '\n'), 0o644)
}

func CurrentScreen(root string) string {
	data, err := os.ReadFile(filepath.Join(root, MapCurrentFile))
	if err != nil {
		return ""
	}
	var current mapCurrent
	if err := json.Unmarshal(data, &current); err != nil {
		return ""
	}
	return current.Screen
}

func ClearPendingMapAction(root string) {
	_ = os.Remove(filepath.Join(root, MapPendingFile))
}

func SetPendingMapAction(root string, pending pendingMapAction) {
	if pending.From == "" {
		return
	}
	_ = os.MkdirAll(filepath.Join(root, MapDirName), 0o755)
	data, _ := json.MarshalIndent(pending, "", "  ")
	_ = os.WriteFile(filepath.Join(root, MapPendingFile), append(data, '\n'), 0o644)
}

func consumePendingMapAction(root string) (pendingMapAction, bool) {
	data, err := os.ReadFile(filepath.Join(root, MapPendingFile))
	if err != nil {
		return pendingMapAction{}, false
	}
	_ = os.Remove(filepath.Join(root, MapPendingFile))
	var pending pendingMapAction
	if err := json.Unmarshal(data, &pending); err != nil || pending.From == "" {
		return pendingMapAction{}, false
	}
	return pending, true
}

func peekPendingMapAction(root string) (pendingMapAction, bool) {
	data, err := os.ReadFile(filepath.Join(root, MapPendingFile))
	if err != nil {
		return pendingMapAction{}, false
	}
	var pending pendingMapAction
	if err := json.Unmarshal(data, &pending); err != nil || pending.From == "" {
		return pendingMapAction{}, false
	}
	return pending, true
}

func ObserveScreen(root string, cfg Config, run RunState, rawTree string) (string, error) {
	observed, err := ObserveScreenDetailed(root, cfg, run, rawTree)
	return observed.Screen, err
}

func ObserveScreenDetailed(root string, cfg Config, run RunState, rawTree string) (ScreenObservation, error) {
	return ObserveScreenDetailedWithDriver(root, cfg, run, rawTree, "")
}

func ObserveScreenDetailedWithDriver(root string, cfg Config, run RunState, rawTree, driver string) (ScreenObservation, error) {
	m, err := LoadAppMap(root)
	if err != nil {
		m = DefaultAppMap(cfg.BundleID)
	}
	if m.AppID == "" {
		m.AppID = cfg.BundleID
	}
	elements := ExtractElements(rawTree)
	current := CurrentScreen(root)
	pending, hasPending := peekPendingMapAction(root)
	screenID := ""
	source := ""
	if screenID == "" && current != "" && !hasPending {
		if screen, ok := m.Screens[current]; ok && currentScreenMatches(screen, rawTree, elements) {
			screenID = current
			source = "current"
		}
	}
	identity, hasIdentity := explicitScreenIdentity(elements)
	if screenID != "" && hasIdentity && identity.ID != screenID {
		screenID = ""
		source = ""
	}
	if screenID == "" && hasIdentity {
		screenID = identity.ID
		source = identityScreenSource(m, identity.ID, elements)
	}
	if screenID == "" {
		avoid := ""
		if hasPending {
			avoid = pending.From
		}
		screenID = recognizeScreenAvoiding(m, rawTree, elements, avoid)
		if screenID != "" {
			source = "recognized"
		}
	}
	if screenID == "" {
		ClearPendingMapAction(root)
		SetCurrentScreen(root, "", run.ID)
		return ScreenObservation{Screen: "unknown", Source: "identity_missing", PreviousScreen: current, Elements: elements}, nil
	}
	screen := m.Screens[screenID]
	screen.ID = screenID
	if hasIdentity && !screenHasExplicitScreenIdentity(screen) && blankScreen(screen) {
		screen.Recognizers = append(screen.Recognizers, identity.Recognizer)
	}
	if driver == "appium" || (driver != "" && screen.Driver != "appium") {
		screen.Driver = driver
	}
	screen.Elements = elements
	if screen.Title == "" {
		screen.Title = strings.Title(strings.ReplaceAll(screenID, "-", " "))
	}
	if run.ID != "" {
		treeFile := filepath.Join(run.Dir, "trees", safeFileName(screenID)+".json")
		_ = os.MkdirAll(filepath.Dir(treeFile), 0o755)
		_ = os.WriteFile(treeFile, []byte(rawTree), 0o644)
		screen.TreeFile = treeFile
	}
	m.Screens[screenID] = screen
	if pending, ok := consumePendingMapAction(root); ok && pending.From != screenID {
		from := m.Screens[pending.From]
		from.ID = pending.From
		from.Edges = upsertEdge(from.Edges, Edge{
			From:       pending.From,
			To:         screenID,
			ID:         pending.ID,
			Text:       pending.Text,
			Value:      pending.Value,
			X:          pending.X,
			Y:          pending.Y,
			Wait:       "1s",
			Driver:     pending.Driver,
			Source:     "observed",
			Confidence: "high",
			RecordedAt: time.Now().UTC().Format(time.RFC3339),
		})
		m.Screens[pending.From] = from
	}
	if err := SaveAppMap(root, m); err != nil {
		return ScreenObservation{Screen: screenID, Source: source, PreviousScreen: current, Elements: elements}, err
	}
	SetCurrentScreen(root, screenID, run.ID)
	return ScreenObservation{Screen: screenID, Source: source, PreviousScreen: current, Elements: elements}, nil
}

func ObserveExpectedScreen(root string, cfg Config, run RunState, rawTree, screenID string) error {
	return ObserveExpectedScreenWithDriver(root, cfg, run, rawTree, screenID, "")
}

func ObserveExpectedScreenWithDriver(root string, cfg Config, run RunState, rawTree, screenID, driver string) error {
	if screenID == "" {
		return fmt.Errorf("screen_id_missing")
	}
	m, err := LoadAppMap(root)
	if err != nil {
		m = DefaultAppMap(cfg.BundleID)
	}
	if m.AppID == "" {
		m.AppID = cfg.BundleID
	}
	elements := ExtractElements(rawTree)
	identity, hasIdentity := explicitScreenIdentity(elements)
	if !hasIdentity {
		return fmt.Errorf("screen_identity_missing")
	}
	if identity.ID != screenID {
		return fmt.Errorf("screen_identity_mismatch expected=%s actual=%s", screenID, identity.ID)
	}
	screen := m.Screens[screenID]
	screen.ID = screenID
	if !screenHasExplicitScreenIdentity(screen) && blankScreen(screen) {
		screen.Recognizers = append(screen.Recognizers, identity.Recognizer)
	}
	if driver == "appium" || (driver != "" && screen.Driver != "appium") {
		screen.Driver = driver
	}
	screen.Elements = elements
	if screen.Title == "" {
		screen.Title = strings.Title(strings.ReplaceAll(screenID, "-", " "))
	}
	if run.ID != "" {
		treeFile := filepath.Join(run.Dir, "trees", safeFileName(screenID)+".json")
		_ = os.MkdirAll(filepath.Dir(treeFile), 0o755)
		_ = os.WriteFile(treeFile, []byte(rawTree), 0o644)
		screen.TreeFile = treeFile
	}
	m.Screens[screenID] = screen
	if err := SaveAppMap(root, m); err != nil {
		return err
	}
	SetCurrentScreen(root, screenID, run.ID)
	return nil
}

func blankScreen(screen Screen) bool {
	return screen.AssertID == "" && screen.AssertText == "" && len(screen.Recognizers) == 0 && len(screen.Elements) == 0
}

func ExtractElements(rawTree string) []Element {
	var parsed any
	if err := json.Unmarshal([]byte(rawTree), &parsed); err != nil {
		return nil
	}
	out := []Element{}
	walkAX(parsed, &out, 0)
	return compactElements(out)
}

func walkAX(value any, out *[]Element, depth int) {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			walkAX(child, out, depth)
		}
	case map[string]any:
		el := Element{
			ID:      stringField(node, "AXIdentifier", "identifier", "AXUniqueId"),
			Label:   stringField(node, "AXLabel", "label", "title"),
			Role:    stringField(node, "role_description", "role", "type"),
			Value:   stringField(node, "AXValue", "value"),
			Frame:   stringField(node, "AXFrame", "frame"),
			Enabled: boolStringField(node, "AXEnabled", "enabled"),
			Subrole: stringField(node, "AXSubrole", "subrole"),
			Title:   stringField(node, "AXTitle", "title"),
			PID:     stringField(node, "AXPid", "AXPID", "pid"),
			Focused: boolStringField(node, "AXFocused", "focused", "hasFocus"),
			Depth:   depth,
		}
		if el.ID != "" || el.Label != "" || el.Role != "" || el.Value != "" || el.Frame != "" || el.Enabled != "" || el.Subrole != "" || el.Title != "" || el.PID != "" {
			*out = append(*out, el)
		}
		for _, childKey := range []string{"children", "Children", "AXChildren"} {
			if child, ok := node[childKey]; ok {
				walkAX(child, out, depth+1)
			}
		}
	}
}

func stringField(node map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := node[key].(type) {
		case string:
			return strings.TrimSpace(value)
		case nil:
		default:
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func boolStringField(node map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := node[key].(type) {
		case bool:
			return strconv.FormatBool(value)
		case string:
			text := strings.TrimSpace(value)
			if strings.EqualFold(text, "true") || strings.EqualFold(text, "false") {
				return strings.ToLower(text)
			}
		case nil:
		default:
			text := strings.TrimSpace(fmt.Sprint(value))
			if strings.EqualFold(text, "true") || strings.EqualFold(text, "false") {
				return strings.ToLower(text)
			}
		}
	}
	return ""
}

func compactElements(elements []Element) []Element {
	seen := map[string]bool{}
	out := []Element{}
	for _, el := range elements {
		key := el.ID + "\x00" + el.Label + "\x00" + el.Role + "\x00" + el.Value + "\x00" + el.Frame + "\x00" + el.Enabled + "\x00" + el.Subrole + "\x00" + el.Title + "\x00" + el.PID + "\x00" + el.Focused + "\x00" + strconv.Itoa(el.Depth)
		if elementEmpty(el) || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, el)
	}
	return out
}

func elementEmpty(el Element) bool {
	return el.ID == "" && el.Label == "" && el.Role == "" && el.Value == "" && el.Frame == "" && el.Enabled == "" && el.Subrole == "" && el.Title == "" && el.PID == "" && el.Focused == ""
}

func recognizeScreen(m AppMap, raw string, elements []Element) string {
	return recognizeScreenAvoiding(m, raw, elements, "")
}

func recognizeScreenAvoiding(m AppMap, raw string, elements []Element, avoid string) string {
	fallback := ""
	best := ""
	bestScore := -1
	for _, id := range sortedScreenKeys(m.Screens) {
		screen := m.Screens[id]
		if screenMatches(screen, raw, elements) {
			if avoid != "" && id == avoid {
				fallback = id
				continue
			}
			score := screenMatchSpecificity(screen, elements, id == m.Start)
			if score > bestScore {
				best = id
				bestScore = score
			}
		}
	}
	if best != "" && bestScore > 0 {
		return best
	}
	return fallback
}

func screenMatches(screen Screen, raw string, elements []Element) bool {
	_ = raw
	if conflictsWithExplicitPrefixedIdentity(screen.ID, elements) {
		return false
	}
	for _, rec := range screen.Recognizers {
		if rec.Kind != "id" {
			continue
		}
		value := strings.TrimSpace(rec.Value)
		if !recognizerValidForScreen(screen, rec) {
			continue
		}
		for _, el := range elements {
			if el.ID == value {
				return true
			}
		}
	}
	return false
}

func currentScreenMatches(screen Screen, raw string, elements []Element) bool {
	if screenMatches(screen, raw, elements) {
		return true
	}
	return screenHasLaunchRecognizer(screen) && len(elements) > 0
}

func screenHasExplicitScreenIdentity(screen Screen) bool {
	for _, rec := range screen.Recognizers {
		switch rec.Kind {
		case "launch":
			if screen.ID == "start" {
				return true
			}
		case "id":
			if recognizerValidForScreen(screen, rec) {
				return true
			}
		}
	}
	return false
}

func screenHasLaunchRecognizer(screen Screen) bool {
	if screen.ID != "start" {
		return false
	}
	for _, rec := range screen.Recognizers {
		if rec.Kind == "launch" {
			return true
		}
	}
	return false
}

func explicitScreenIdentity(elements []Element) (screenIdentity, bool) {
	if identity, ok := prefixedScreenIdentity(elements); ok {
		return identity, true
	}
	return naturalScreenIdentity(elements)
}

func naturalScreenIdentity(elements []Element) (screenIdentity, bool) {
	best := Element{}
	bestOK := false
	for _, el := range elements {
		if !isNaturalScreenIdentifierElement(el) {
			continue
		}
		if !bestOK || el.Depth < best.Depth {
			best = el
			bestOK = true
		}
	}
	if !bestOK {
		return screenIdentity{}, false
	}
	elementID := strings.TrimSpace(best.ID)
	id := screenIdentityIDFromSuffix(elementID)
	if id == "" || id == "step" {
		return screenIdentity{}, false
	}
	return screenIdentity{
		ID:        id,
		ElementID: elementID,
		Recognizer: Recognizer{
			Kind:  "id",
			Value: elementID,
		},
	}, true
}

func prefixedScreenIdentity(elements []Element) (screenIdentity, bool) {
	for _, el := range elements {
		id, ok := screenIDFromElementID(el.ID)
		if !ok {
			continue
		}
		return screenIdentity{
			ID:        id,
			ElementID: strings.TrimSpace(el.ID),
			Recognizer: Recognizer{
				Kind:  "id",
				Value: strings.TrimSpace(el.ID),
			},
		}, true
	}
	return screenIdentity{}, false
}

func conflictsWithExplicitPrefixedIdentity(screenID string, elements []Element) bool {
	identity, ok := prefixedScreenIdentity(elements)
	return ok && identity.ID != screenID
}

func identityScreenSource(m AppMap, screenID string, elements []Element) string {
	screen, ok := m.Screens[screenID]
	if ok && screenMatches(screen, "", elements) {
		return "recognized"
	}
	return "explicit_id"
}

func screenIDFromElementID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for _, prefix := range screenIdentityPrefixes() {
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		suffix := strings.TrimSpace(strings.TrimPrefix(value, prefix))
		if suffix == "" {
			return "", false
		}
		id := screenIdentityIDFromSuffix(suffix)
		if id == "" || id == "step" {
			return "", false
		}
		return id, true
	}
	return "", false
}

func screenIdentityIDFromSuffix(value string) string {
	value = strings.TrimSpace(transliterateLatin(splitCamelIdentifier(value)))
	value = strings.ToLower(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func splitCamelIdentifier(value string) string {
	var b strings.Builder
	runes := []rune(value)
	for i, r := range runes {
		if i > 0 && shouldSplitIdentifierRune(runes[i-1], r, nextRune(runes, i)) {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func nextRune(runes []rune, index int) rune {
	if index+1 >= len(runes) {
		return 0
	}
	return runes[index+1]
}

func shouldSplitIdentifierRune(prev, cur, next rune) bool {
	if cur < 'A' || cur > 'Z' {
		return false
	}
	if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
		return true
	}
	return prev >= 'A' && prev <= 'Z' && next >= 'a' && next <= 'z'
}

func screenIdentityPrefixes() []string {
	return []string{"mav.screen.", "screen."}
}

func isNaturalScreenIdentifierElement(el Element) bool {
	id := strings.TrimSpace(el.ID)
	if id == "" {
		return false
	}
	if _, ok := screenIDFromElementID(id); ok {
		return false
	}
	// Skip sub-element identifiers. Codebases that auto-generate
	// accessibility ids (Sourcery, etc.) commonly emit `<Container>.<element>`
	// for individual controls (buttons, fields, …). Those are not screens —
	// they are addressable members of a screen — so we ignore any id with a
	// dot regardless of suffix. Prefixed screen ids such as `mav.screen.<id>`
	// or `screen.<id>` are matched earlier by `screenIDFromElementID` and
	// never reach this branch.
	if strings.Contains(id, ".") {
		return false
	}
	if isApplicationRootElement(el) {
		return false
	}
	role := strings.ToLower(strings.TrimSpace(el.Role))
	for _, blocked := range []string{"button", "textfield", "text field", "textview", "text view", "statictext", "static text", "switch", "tab", "cell", "image", "slider", "picker"} {
		if strings.Contains(role, blocked) {
			return false
		}
	}
	return hasScreenIdentifierSuffix(id)
}

func hasScreenIdentifierSuffix(id string) bool {
	id = strings.TrimSpace(id)
	return strings.HasSuffix(id, "View") || strings.HasSuffix(id, "ViewController") || strings.HasSuffix(id, "Screen")
}

func recognizerValidForScreen(screen Screen, rec Recognizer) bool {
	if rec.Kind != "id" {
		return false
	}
	value := strings.TrimSpace(rec.Value)
	if value == "" {
		return false
	}
	if id, ok := screenIDFromElementID(value); ok {
		return id == screen.ID
	}
	return true
}

func screenMatchSpecificity(screen Screen, elements []Element, isStart bool) int {
	score := 0
	for _, rec := range screen.Recognizers {
		switch rec.Kind {
		case "launch":
			if isStart && len(elements) > 0 {
				score = max(score, 5)
			}
		case "text":
			if hasScreenText(elements, rec.Value) {
				score = max(score, 30+min(len([]rune(rec.Value)), 60))
			}
		case "id":
			for _, el := range elements {
				if el.ID == rec.Value {
					if isApplicationRootElement(el) {
						score = max(score, 1)
					} else {
						score = max(score, 20)
					}
				}
			}
		}
	}
	if screen.AssertText != "" && hasScreenText(elements, screen.AssertText) {
		score = max(score, 35+min(len([]rune(screen.AssertText)), 60))
	}
	if screen.AssertID != "" {
		for _, el := range elements {
			if el.ID == screen.AssertID {
				if isApplicationRootElement(el) {
					score = max(score, 1)
				} else {
					score = max(score, 25)
				}
			}
		}
	}
	if isStart && score <= 1 {
		return 0
	}
	return score
}

func isApplicationRootElement(el Element) bool {
	role := strings.ToLower(strings.TrimSpace(el.Role))
	return role == "application" || role == "axapplication" || role == "xcuielementtypeapplication" || role == "appiumaut"
}

func hasScreenText(elements []Element, label string) bool {
	for _, el := range elements {
		role := strings.ToLower(el.Role)
		if role == "application" || role == "group" || strings.Contains(role, "button") || role == "switch" || strings.Contains(role, "tab") {
			continue
		}
		if el.Label == label {
			return true
		}
	}
	return false
}

func upsertEdge(edges []Edge, edge Edge) []Edge {
	for i, existing := range edges {
		if existing.To == edge.To && existing.ID == edge.ID && existing.Text == edge.Text && existing.Value == edge.Value && existing.X == edge.X && existing.Y == edge.Y {
			if existing.Driver == "appium" && edge.Driver != "appium" {
				edge.Driver = existing.Driver
			}
			if edge.From == "" {
				edge.From = existing.From
			}
			if edge.Confidence == "" {
				edge.Confidence = existing.Confidence
			}
			if existing.FailureCount > 0 && edge.FailureCount == 0 {
				edge.FailureCount = existing.FailureCount
				edge.LastFailure = existing.LastFailure
			}
			// Preserve the original `RecordedAt` so re-observing the
			// same transition doesn't reset the staleness clock. A
			// successful replay updates `LastSuccessAt` separately
			// (via markRouteEdgeSuccess); both are TTL inputs.
			if edge.RecordedAt == "" {
				edge.RecordedAt = existing.RecordedAt
			}
			if edge.LastSuccessAt == "" {
				edge.LastSuccessAt = existing.LastSuccessAt
			}
			edges[i] = edge
			return edges
		}
	}
	return append(edges, edge)
}
