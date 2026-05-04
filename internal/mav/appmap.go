package mav

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	ID    string `json:"id,omitempty"`
	Label string `json:"label,omitempty"`
	Role  string `json:"role,omitempty"`
	Value string `json:"value,omitempty"`
	Frame string `json:"frame,omitempty"`
}

type Edge struct {
	To     string `json:"to"`
	ID     string `json:"id,omitempty"`
	Text   string `json:"text,omitempty"`
	X      string `json:"x,omitempty"`
	Y      string `json:"y,omitempty"`
	Wait   string `json:"wait,omitempty"`
	Source string `json:"source,omitempty"`
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
	From string `json:"from"`
	ID   string `json:"id,omitempty"`
	Text string `json:"text,omitempty"`
	X    string `json:"x,omitempty"`
	Y    string `json:"y,omitempty"`
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
		Screens: map[string]Screen{"start": {ID: "start", Title: "Start"}},
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
		for _, edge := range screen.Edges {
			if edge.To == "" {
				return fmt.Errorf("app_map_edge_target_missing screen=%s", id)
			}
			if _, ok := m.Screens[edge.To]; !ok {
				return fmt.Errorf("app_map_edge_target_not_found screen=%s target=%s", id, edge.To)
			}
			if edge.ID == "" && edge.Text == "" && (edge.X == "" || edge.Y == "") {
				return fmt.Errorf("app_map_edge_action_missing screen=%s target=%s", id, edge.To)
			}
		}
	}
	return nil
}

func Route(m AppMap, target string) ([]Edge, error) {
	if _, ok := m.Screens[target]; !ok {
		return nil, fmt.Errorf("screen_not_found")
	}
	if target == m.Start {
		return nil, nil
	}
	type node struct {
		id    string
		route []Edge
	}
	queue := []node{{id: m.Start}}
	seen := map[string]bool{m.Start: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, edge := range m.Screens[cur.id].Edges {
			if seen[edge.To] {
				continue
			}
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
			case "x":
				currentEdge.X = value
			case "y":
				currentEdge.Y = value
			case "wait":
				currentEdge.Wait = value
			}
		} else {
			switch key {
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
	m, err := LoadAppMap(root)
	if err != nil {
		m = DefaultAppMap(cfg.BundleID)
	}
	if m.AppID == "" {
		m.AppID = cfg.BundleID
	}
	elements := ExtractElements(rawTree)
	current := CurrentScreen(root)
	screenID := ""
	source := ""
	if current == m.Start && blankScreen(m.Screens[m.Start]) {
		screenID = m.Start
		source = "start"
	}
	if screenID == "" {
		screenID = recognizeScreen(m, rawTree, elements)
		if screenID != "" {
			source = "recognized"
		}
	}
	if screenID == "" {
		screenID = inferScreenID(elements)
		if screenID != "" {
			source = "inferred"
		}
	}
	if screenID == "" && len(elements) == 0 {
		screenID = current
		if screenID != "" {
			source = "current"
		}
	}
	if screenID == "" {
		return ScreenObservation{Screen: "unknown", Source: "unmatched", PreviousScreen: current, Elements: elements}, nil
	}
	screen := m.Screens[screenID]
	screen.ID = screenID
	screen.Elements = elements
	if screen.Title == "" {
		screen.Title = inferScreenTitle(elements, screenID)
	}
	if screen.AssertText == "" && screen.Title != "" && screen.ID != m.Start {
		screen.AssertText = screen.Title
	}
	if len(screen.Recognizers) == 0 && screen.AssertText != "" {
		screen.Recognizers = append(screen.Recognizers, Recognizer{Kind: "text", Value: screen.AssertText})
	}
	if len(screen.Recognizers) == 0 {
		if id := firstStableElementID(elements); id != "" {
			screen.Recognizers = append(screen.Recognizers, Recognizer{Kind: "id", Value: id})
		}
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
		from.Edges = upsertEdge(from.Edges, Edge{To: screenID, ID: pending.ID, Text: pending.Text, X: pending.X, Y: pending.Y, Wait: "1s", Source: "observed"})
		m.Screens[pending.From] = from
	}
	if err := SaveAppMap(root, m); err != nil {
		return ScreenObservation{Screen: screenID, Source: source, PreviousScreen: current, Elements: elements}, err
	}
	SetCurrentScreen(root, screenID, run.ID)
	return ScreenObservation{Screen: screenID, Source: source, PreviousScreen: current, Elements: elements}, nil
}

func ObserveExpectedScreen(root string, cfg Config, run RunState, rawTree, screenID string) error {
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
	screen := m.Screens[screenID]
	screen.ID = screenID
	screen.Elements = elements
	if screen.Title == "" {
		screen.Title = inferScreenTitle(elements, screenID)
	}
	if screen.AssertText == "" && screen.Title != "" && screen.ID != m.Start {
		screen.AssertText = screen.Title
	}
	if len(screen.Recognizers) == 0 && screen.AssertText != "" {
		screen.Recognizers = append(screen.Recognizers, Recognizer{Kind: "text", Value: screen.AssertText})
	}
	if len(screen.Recognizers) == 0 {
		if id := firstStableElementID(elements); id != "" {
			screen.Recognizers = append(screen.Recognizers, Recognizer{Kind: "id", Value: id})
		}
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
	walkAX(parsed, &out)
	return compactElements(out)
}

func walkAX(value any, out *[]Element) {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			walkAX(child, out)
		}
	case map[string]any:
		el := Element{
			ID:    stringField(node, "AXIdentifier", "identifier", "AXUniqueId"),
			Label: stringField(node, "AXLabel", "label", "title"),
			Role:  stringField(node, "role_description", "role", "type"),
			Value: stringField(node, "AXValue", "value"),
			Frame: stringField(node, "AXFrame", "frame"),
		}
		if el.ID != "" || el.Label != "" || el.Role != "" || el.Value != "" || el.Frame != "" {
			*out = append(*out, el)
		}
		for _, childKey := range []string{"children", "Children", "AXChildren"} {
			if child, ok := node[childKey]; ok {
				walkAX(child, out)
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

func compactElements(elements []Element) []Element {
	seen := map[string]bool{}
	out := []Element{}
	for _, el := range elements {
		key := el.ID + "\x00" + el.Label + "\x00" + el.Role + "\x00" + el.Value + "\x00" + el.Frame
		if key == "\x00\x00\x00\x00\x00" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, el)
		if len(out) >= 80 {
			break
		}
	}
	return out
}

func recognizeScreen(m AppMap, raw string, elements []Element) string {
	for id, screen := range m.Screens {
		if screenMatches(screen, raw, elements) {
			return id
		}
	}
	return ""
}

func screenMatches(screen Screen, raw string, elements []Element) bool {
	_ = raw
	for _, rec := range screen.Recognizers {
		if rec.Value == "" {
			continue
		}
		if rec.Kind == "text" && hasScreenText(elements, rec.Value) {
			return true
		}
		if rec.Kind == "id" {
			for _, el := range elements {
				if el.ID == rec.Value {
					return true
				}
			}
		}
	}
	if screen.AssertText != "" && hasScreenText(elements, screen.AssertText) {
		return true
	}
	if screen.AssertID != "" {
		for _, el := range elements {
			if el.ID == screen.AssertID {
				return true
			}
		}
	}
	return false
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

func firstStableElementID(elements []Element) string {
	for _, el := range elements {
		id := strings.TrimSpace(el.ID)
		if id == "" || strings.Contains(id, ".") {
			continue
		}
		if strings.EqualFold(el.Role, "image") {
			continue
		}
		return id
	}
	return ""
}

func inferScreenID(elements []Element) string {
	title := inferScreenTitle(elements, "")
	if title == "" {
		return ""
	}
	return safeFileName(title)
}

func inferScreenTitle(elements []Element, fallback string) string {
	for _, el := range elements {
		role := strings.ToLower(el.Role)
		if role == "application" || role == "group" || strings.Contains(role, "button") || role == "switch" || strings.Contains(role, "tab") || role == "image" {
			continue
		}
		if isScreenTitle(el.Label) {
			return el.Label
		}
	}
	if fallback != "" {
		return strings.Title(strings.ReplaceAll(fallback, "-", " "))
	}
	return ""
}

func isScreenTitle(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 40 {
		return false
	}
	if strings.ContainsAny(value, "\n0123456789") {
		return false
	}
	reject := map[string]bool{"photos": true, "videos": true, "analysis": true, "suggestions": true, "tab bar": true}
	return !reject[strings.ToLower(value)]
}

func upsertEdge(edges []Edge, edge Edge) []Edge {
	for i, existing := range edges {
		if existing.To == edge.To && existing.ID == edge.ID && existing.Text == edge.Text && existing.X == edge.X && existing.Y == edge.Y {
			edges[i] = edge
			return edges
		}
	}
	return append(edges, edge)
}
