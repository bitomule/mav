package mav

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AppMap struct {
	AppID   string
	Start   string
	Screens map[string]Screen
}

type Screen struct {
	ID         string
	AssertID   string
	AssertText string
	Edges      []Edge
}

type Edge struct {
	To   string
	ID   string
	Text string
	Wait string
}

func DefaultAppMap(bundleID string) AppMap {
	return AppMap{
		AppID:   bundleID,
		Start:   "start",
		Screens: map[string]Screen{"start": {ID: "start"}},
	}
}

func LoadAppMap(root string) (AppMap, error) {
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
			screen := Screen{ID: id}
			m.Screens[id] = screen
			current = &screen
			currentEdge = nil
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			edge := Edge{}
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
			case "wait":
				currentEdge.Wait = value
			}
		} else {
			switch key {
			case "assert_id":
				current.AssertID = value
			case "assert_text":
				current.AssertText = value
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

func SaveAppMap(root string, m AppMap) error {
	if err := os.MkdirAll(filepath.Join(root, MavDir), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("app_id: ")
	b.WriteString(yamlQuote(m.AppID))
	b.WriteString("\n")
	b.WriteString("start: ")
	b.WriteString(yamlQuote(m.Start))
	b.WriteString("\n")
	b.WriteString("screens:\n")
	keys := sortedScreenKeys(m.Screens)
	for _, id := range keys {
		screen := m.Screens[id]
		b.WriteString("  ")
		b.WriteString(id)
		b.WriteString(":\n")
		if screen.AssertID != "" {
			b.WriteString("    assert_id: ")
			b.WriteString(yamlQuote(screen.AssertID))
			b.WriteString("\n")
		}
		if screen.AssertText != "" {
			b.WriteString("    assert_text: ")
			b.WriteString(yamlQuote(screen.AssertText))
			b.WriteString("\n")
		}
		if len(screen.Edges) > 0 {
			b.WriteString("    edges:\n")
			for _, edge := range screen.Edges {
				b.WriteString("      - to: ")
				b.WriteString(yamlQuote(edge.To))
				b.WriteString("\n")
				if edge.ID != "" {
					b.WriteString("        id: ")
					b.WriteString(yamlQuote(edge.ID))
					b.WriteString("\n")
				}
				if edge.Text != "" {
					b.WriteString("        text: ")
					b.WriteString(yamlQuote(edge.Text))
					b.WriteString("\n")
				}
				if edge.Wait != "" {
					b.WriteString("        wait: ")
					b.WriteString(yamlQuote(edge.Wait))
					b.WriteString("\n")
				}
			}
		}
	}
	return os.WriteFile(filepath.Join(root, AppMapFile), []byte(b.String()), 0o644)
}

func sortedScreenKeys(screens map[string]Screen) []string {
	keys := make([]string, 0, len(screens))
	for key := range screens {
		keys = append(keys, key)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
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
			if edge.ID == "" && edge.Text == "" {
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

type MaestroFlowOptions struct {
	CaptureSteps bool
}

func MaestroFlow(m AppMap, route []Edge, target string, options ...MaestroFlowOptions) string {
	opts := MaestroFlowOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	var b strings.Builder
	if m.AppID != "" {
		b.WriteString("appId: ")
		b.WriteString(m.AppID)
		b.WriteString("\n---\n")
	}
	b.WriteString("- launchApp\n")
	if opts.CaptureSteps {
		b.WriteString("- takeScreenshot: mav_step_00_launch\n")
	}
	for index, edge := range route {
		if edge.ID != "" {
			b.WriteString("- tapOn:\n")
			b.WriteString("    id: ")
			b.WriteString(edge.ID)
			b.WriteString("\n")
		} else if edge.Text != "" {
			b.WriteString("- tapOn:\n")
			b.WriteString("    text: ")
			b.WriteString(edge.Text)
			b.WriteString("\n")
		}
		if edge.Wait != "" {
			b.WriteString("- waitForAnimationToEnd:\n")
			b.WriteString("    timeout: ")
			b.WriteString(edge.Wait)
			b.WriteString("\n")
		}
		if opts.CaptureSteps {
			b.WriteString("- takeScreenshot: mav_step_")
			b.WriteString(fmt.Sprintf("%02d", index+1))
			b.WriteString("_")
			b.WriteString(edge.To)
			b.WriteString("\n")
		}
	}
	screen := m.Screens[target]
	if screen.AssertID != "" {
		b.WriteString("- assertVisible:\n")
		b.WriteString("    id: ")
		b.WriteString(screen.AssertID)
		b.WriteString("\n")
	}
	if screen.AssertText != "" {
		b.WriteString("- assertVisible:\n")
		b.WriteString("    text: ")
		b.WriteString(screen.AssertText)
		b.WriteString("\n")
	}
	if opts.CaptureSteps {
		b.WriteString("- takeScreenshot: mav_final_")
		b.WriteString(target)
		b.WriteString("\n")
	}
	return b.String()
}
