package mav

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

func selectorFromCLI(args []string) (Selector, error) {
	selector := Selector{
		ID:             flagValue(args, "--id"),
		Text:           flagValue(args, "--text"),
		TextContains:   flagValue(args, "--text-contains"),
		TextStartsWith: flagValue(args, "--text-starts-with"),
		TextRegex:      flagValue(args, "--text-regex"),
		Value:          flagValue(args, "--value"),
		ValueContains:  flagValue(args, "--value-contains"),
		Role:           flagValue(args, "--role"),
		Bounds:         flagValue(args, "--bounds"),
	}
	var err error
	if selector.Enabled, err = optionalBoolFlag(args, "--enabled"); err != nil {
		return Selector{}, err
	}
	if selector.Selected, err = optionalBoolFlag(args, "--selected"); err != nil {
		return Selector{}, err
	}
	if selector.Focused, err = optionalBoolFlag(args, "--focused"); err != nil {
		return Selector{}, err
	}
	if selector.Visible, err = optionalBoolFlag(args, "--visible"); err != nil {
		return Selector{}, err
	}
	if raw := flagValue(args, "--index"); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			return Selector{}, fmt.Errorf("selector_index_invalid")
		}
		selector.Index = &value
	}
	nearID, nearText := flagValue(args, "--near-id"), flagValue(args, "--near-text")
	if nearID != "" || nearText != "" {
		distance := 0.0
		if raw := flagValue(args, "--near-distance"); raw != "" {
			distance, err = strconv.ParseFloat(raw, 64)
			if err != nil {
				return Selector{}, fmt.Errorf("selector_near_distance_invalid")
			}
		}
		selector.Near = &NearSelector{
			Where:       Selector{ID: nearID, Text: nearText},
			Direction:   flagValue(args, "--near-direction"),
			MaxDistance: distance,
		}
	}
	return selector, selector.Validate()
}

func optionalBoolFlag(args []string, name string) (*bool, error) {
	raw := flagValue(args, name)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("selector_bool_invalid field=%s", strings.TrimPrefix(name, "--"))
	}
	return &value, nil
}

func (c FlowCondition) Selector() Selector {
	return Selector{
		ID: c.ID, Text: c.Text, TextContains: c.TextContains,
		TextStartsWith: c.TextStartsWith, TextRegex: c.TextRegex,
		Value: c.Value, ValueContains: c.ValueContains, Role: c.Role,
		Enabled: c.Enabled, Selected: c.Selected, Focused: c.Focused,
		Visible: c.Visible, Index: c.Index, Bounds: c.Bounds,
		Near: c.Near, ParentOf: c.ParentOf,
	}
}

// Selector is the typed, driver-neutral predicate used by CLI commands and
// flows. All populated fields are combined with AND. Index is applied after
// every other filter.
type Selector struct {
	ID             string        `yaml:"id,omitempty" json:"id,omitempty"`
	Text           string        `yaml:"text,omitempty" json:"text,omitempty"`
	TextContains   string        `yaml:"textContains,omitempty" json:"textContains,omitempty"`
	TextStartsWith string        `yaml:"textStartsWith,omitempty" json:"textStartsWith,omitempty"`
	TextRegex      string        `yaml:"textRegex,omitempty" json:"textRegex,omitempty"`
	Value          string        `yaml:"value,omitempty" json:"value,omitempty"`
	ValueContains  string        `yaml:"valueContains,omitempty" json:"valueContains,omitempty"`
	Role           string        `yaml:"role,omitempty" json:"role,omitempty"`
	Enabled        *bool         `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Selected       *bool         `yaml:"selected,omitempty" json:"selected,omitempty"`
	Focused        *bool         `yaml:"focused,omitempty" json:"focused,omitempty"`
	Visible        *bool         `yaml:"visible,omitempty" json:"visible,omitempty"`
	Index          *int          `yaml:"index,omitempty" json:"index,omitempty"`
	Bounds         string        `yaml:"bounds,omitempty" json:"bounds,omitempty"`
	Near           *NearSelector `yaml:"near,omitempty" json:"near,omitempty"`
	ParentOf       *Selector     `yaml:"parentOf,omitempty" json:"parentOf,omitempty"`
}

type NearSelector struct {
	Where       Selector `yaml:"where" json:"where"`
	Direction   string   `yaml:"direction,omitempty" json:"direction,omitempty"`
	MaxDistance float64  `yaml:"maxDistance,omitempty" json:"maxDistance,omitempty"`
}

func (s Selector) IsZero() bool {
	return s.ID == "" && s.Text == "" && s.TextContains == "" &&
		s.TextStartsWith == "" && s.TextRegex == "" && s.Value == "" &&
		s.ValueContains == "" && s.Role == "" && s.Enabled == nil &&
		s.Selected == nil && s.Focused == nil && s.Visible == nil &&
		s.Index == nil && s.Bounds == "" && s.Near == nil && s.ParentOf == nil
}

func (s Selector) Validate() error {
	if s.TextRegex != "" {
		if _, err := regexp.Compile(s.TextRegex); err != nil {
			return fmt.Errorf("selector_regex_invalid: %w", err)
		}
	}
	if s.Index != nil && *s.Index < 0 {
		return fmt.Errorf("selector_index_invalid")
	}
	switch s.Bounds {
	case "", "topHalf", "bottomHalf", "leftHalf", "rightHalf", "center":
	default:
		return fmt.Errorf("selector_bounds_invalid")
	}
	if s.Near != nil {
		if s.Near.Where.IsZero() {
			return fmt.Errorf("selector_near_missing")
		}
		switch s.Near.Direction {
		case "", "any", "above", "below", "left", "right":
		default:
			return fmt.Errorf("selector_near_direction_invalid")
		}
		if s.Near.MaxDistance < 0 {
			return fmt.Errorf("selector_near_distance_invalid")
		}
		if err := s.Near.Where.Validate(); err != nil {
			return err
		}
	}
	if s.ParentOf != nil {
		if s.ParentOf.IsZero() {
			return fmt.Errorf("selector_parent_missing")
		}
		if err := s.ParentOf.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// MatchElements returns matching nodes in tree order. It works against the
// full flattened pre-order tree so parent/descendant relationships remain
// available through Depth.
func MatchElements(elements []Element, selector Selector) ([]Element, error) {
	if selector.IsZero() {
		return nil, fmt.Errorf("selector_missing")
	}
	if err := selector.Validate(); err != nil {
		return nil, err
	}
	var rx *regexp.Regexp
	if selector.TextRegex != "" {
		rx = regexp.MustCompile(selector.TextRegex)
	}
	matches := make([]Element, 0)
	for i, el := range elements {
		if !matchesElement(elements, i, el, selector, rx) {
			continue
		}
		matches = append(matches, el)
	}
	if selector.Index != nil {
		idx := *selector.Index
		if idx >= len(matches) {
			return nil, nil
		}
		return []Element{matches[idx]}, nil
	}
	return matches, nil
}

func matchesElement(elements []Element, index int, el Element, selector Selector, rx *regexp.Regexp) bool {
	if selector.ID != "" && el.ID != selector.ID {
		return false
	}
	text := elementText(el)
	if selector.Text != "" && text != selector.Text {
		return false
	}
	if selector.TextContains != "" && !containsFold(text, selector.TextContains) {
		return false
	}
	if selector.TextStartsWith != "" && !strings.HasPrefix(strings.ToLower(text), strings.ToLower(selector.TextStartsWith)) {
		return false
	}
	if rx != nil && !rx.MatchString(text) {
		return false
	}
	if selector.Value != "" && el.Value != selector.Value {
		return false
	}
	if selector.ValueContains != "" && !containsFold(el.Value, selector.ValueContains) {
		return false
	}
	if selector.Role != "" && !strings.EqualFold(normalizedRole(el.Role), normalizedRole(selector.Role)) {
		return false
	}
	if !matchOptionalBool(el.Enabled, selector.Enabled) ||
		!matchOptionalBool(el.Selected, selector.Selected) ||
		!matchOptionalBool(el.Focused, selector.Focused) ||
		!matchVisible(el, selector.Visible) {
		return false
	}
	if selector.Bounds != "" && !frameInBounds(elements, el.Frame, selector.Bounds) {
		return false
	}
	if selector.Near != nil && !elementNear(elements, el, *selector.Near) {
		return false
	}
	if selector.ParentOf != nil && !elementParentOf(elements, index, *selector.ParentOf) {
		return false
	}
	return true
}

func matchVisible(element Element, expected *bool) bool {
	if expected == nil {
		return true
	}
	if strings.TrimSpace(element.Visible) != "" {
		return matchOptionalBool(element.Visible, expected)
	}
	_, _, width, height, ok := parseElementFrame(element.Frame)
	actual := ok && width > 0 && height > 0
	return actual == *expected
}

func elementText(el Element) string {
	if el.Label != "" {
		return el.Label
	}
	return el.Title
}

func containsFold(value, needle string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(needle))
}

func normalizedRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	role = strings.TrimPrefix(role, "ax")
	role = strings.TrimPrefix(role, "xc uielementtype")
	role = strings.TrimPrefix(role, "xcuielementtype")
	role = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(role)
	switch role {
	case "textfield", "searchfield", "securetextfield":
		return "textfield"
	case "textview":
		return "textview"
	case "statictext", "text":
		return "text"
	}
	return role
}

func matchOptionalBool(raw string, expected *bool) bool {
	if expected == nil {
		return true
	}
	actual, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && actual == *expected
}

func frameInBounds(elements []Element, frame, region string) bool {
	x, y, width, height, ok := parseElementFrame(frame)
	if !ok {
		return false
	}
	cx, cy := x+width/2, y+height/2
	screenWidth, screenHeight := 0.0, 0.0
	for _, element := range elements {
		ex, ey, ew, eh, valid := parseElementFrame(element.Frame)
		if valid {
			screenWidth = math.Max(screenWidth, ex+ew)
			screenHeight = math.Max(screenHeight, ey+eh)
		}
	}
	if screenWidth <= 0 || screenHeight <= 0 {
		return false
	}
	switch region {
	case "topHalf":
		return cy <= screenHeight/2
	case "bottomHalf":
		return cy > screenHeight/2
	case "leftHalf":
		return cx <= screenWidth/2
	case "rightHalf":
		return cx > screenWidth/2
	case "center":
		return cx >= screenWidth*.25 && cx <= screenWidth*.75 &&
			cy >= screenHeight*.25 && cy <= screenHeight*.75
	default:
		return true
	}
}

func elementNear(elements []Element, candidate Element, near NearSelector) bool {
	x, y, width, height, ok := parseElementFrame(candidate.Frame)
	if !ok {
		return false
	}
	anchors, err := MatchElements(elements, near.Where)
	if err != nil {
		return false
	}
	maxDistance := near.MaxDistance
	if maxDistance == 0 {
		maxDistance = 120
	}
	cx, cy := x+width/2, y+height/2
	for _, anchor := range anchors {
		ax, ay, aw, ah, ok := parseElementFrame(anchor.Frame)
		if !ok {
			continue
		}
		acx, acy := ax+aw/2, ay+ah/2
		dx, dy := cx-acx, cy-acy
		if math.Hypot(dx, dy) > maxDistance {
			continue
		}
		switch near.Direction {
		case "", "any":
			return true
		case "above":
			if dy < 0 {
				return true
			}
		case "below":
			if dy > 0 {
				return true
			}
		case "left":
			if dx < 0 {
				return true
			}
		case "right":
			if dx > 0 {
				return true
			}
		}
	}
	return false
}

func elementParentOf(elements []Element, index int, descendant Selector) bool {
	depth := elements[index].Depth
	for i := index + 1; i < len(elements); i++ {
		if elements[i].Depth <= depth {
			break
		}
		matches, err := MatchElements([]Element{elements[i]}, descendant)
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	return false
}

func selectorFromLegacy(params map[string]string) Selector {
	return Selector{
		ID:    params["id"],
		Text:  params["text"],
		Value: params["value"],
	}
}

func selectorCLIArgs(selector Selector) []string {
	args := []string{}
	appendValue := func(flag, value string) {
		if value != "" {
			args = append(args, flag, value)
		}
	}
	appendValue("--id", selector.ID)
	appendValue("--text", selector.Text)
	appendValue("--text-contains", selector.TextContains)
	appendValue("--text-starts-with", selector.TextStartsWith)
	appendValue("--text-regex", selector.TextRegex)
	appendValue("--value", selector.Value)
	appendValue("--value-contains", selector.ValueContains)
	appendValue("--role", selector.Role)
	appendValue("--bounds", selector.Bounds)
	if selector.Enabled != nil {
		appendValue("--enabled", strconv.FormatBool(*selector.Enabled))
	}
	if selector.Selected != nil {
		appendValue("--selected", strconv.FormatBool(*selector.Selected))
	}
	if selector.Focused != nil {
		appendValue("--focused", strconv.FormatBool(*selector.Focused))
	}
	if selector.Visible != nil {
		appendValue("--visible", strconv.FormatBool(*selector.Visible))
	}
	if selector.Index != nil {
		appendValue("--index", strconv.Itoa(*selector.Index))
	}
	if selector.Near != nil {
		appendValue("--near-id", selector.Near.Where.ID)
		appendValue("--near-text", selector.Near.Where.Text)
		appendValue("--near-direction", selector.Near.Direction)
		if selector.Near.MaxDistance > 0 {
			appendValue("--near-distance", strconv.FormatFloat(selector.Near.MaxDistance, 'f', -1, 64))
		}
	}
	return args
}
