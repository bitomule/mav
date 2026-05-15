package mav

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Element is the framework-neutral view of a single node in an
// accessibility tree (axe or baguette JSON). It collapses the
// differences between drivers so callers (`mav ui tree`,
// `mav ui tap`, flow conditions, gesture helpers, focus probes)
// can reason in a single vocabulary.
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

// ExtractElements parses an AX tree (axe JSON or any structure that
// walkAX understands) and returns a flat, de-duplicated list of
// Elements. Bounded at 80 to keep output tractable for downstream
// callers; the same cap drove `compactElements` historically.
func ExtractElements(rawTree string) []Element {
	var parsed any
	if err := json.Unmarshal([]byte(rawTree), &parsed); err != nil {
		return nil
	}
	out := []Element{}
	walkAX(parsed, &out, 0)
	return compactElements(out)
}

// walkAX is the recursive visitor over the parsed AX value. Accepts
// either lists (multi-root) or maps (single node with optional
// children), so it works with both axe output (arrays) and baguette
// system-UI output (single-root map).
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

// stringField reads the first non-empty string value at any of the
// supplied keys, trimming whitespace. Used widely across drivers
// because each driver names the same AX attribute differently
// (`AXLabel` vs `label`, `AXFrame` vs `frame`, …).
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

// boolStringField normalises a boolean-valued AX attribute to the
// strings `"true"` / `"false"` (or empty when missing). Distinct
// from stringField because some drivers emit raw bools, others
// emit `"true"`/`"YES"`/`"1"` strings.
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

// compactElements de-duplicates Elements by their full attribute
// tuple and caps the result at 80 entries. The cap is historical;
// it prevents the `mav ui tree` output from drowning agents on
// list-heavy screens.
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
		if len(out) >= 80 {
			break
		}
	}
	return out
}

func elementEmpty(el Element) bool {
	return el.ID == "" && el.Label == "" && el.Role == "" && el.Value == "" && el.Frame == "" && el.Enabled == "" && el.Subrole == "" && el.Title == "" && el.PID == "" && el.Focused == ""
}

// explicitScreenIdentity derives a stable screen id from the AX
// tree alone — no persisted map, no recogniser pipeline. The
// shallowest non-control element whose id ends in `View`,
// `ViewController`, or `Screen` (and isn't a UIKit framework class
// name like `UIView` or `UIScrollView`) gives its name. Returns the
// kebab-case id, the raw element id we matched on, and ok=false
// when nothing qualifies.
func explicitScreenIdentity(elements []Element) (id string, elementID string, ok bool) {
	return naturalScreenIdentity(elements)
}

func naturalScreenIdentity(elements []Element) (id string, elementID string, ok bool) {
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
		return "", "", false
	}
	eid := strings.TrimSpace(best.ID)
	id = screenIdentityIDFromSuffix(eid)
	if id == "" || id == "step" {
		return "", "", false
	}
	return id, eid, true
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

func isNaturalScreenIdentifierElement(el Element) bool {
	id := strings.TrimSpace(el.ID)
	if id == "" {
		return false
	}
	// Skip sub-element identifiers. Codebases that auto-generate
	// accessibility ids (Sourcery, etc.) commonly emit
	// `<Container>.<element>` for individual controls. Those are
	// not screens — they are addressable members of a screen.
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

func isApplicationRootElement(el Element) bool {
	role := strings.ToLower(strings.TrimSpace(el.Role))
	return role == "application" || role == "axapplication" || role == "xcuielementtypeapplication"
}
