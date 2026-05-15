package mav

import (
	"sort"
	"strings"
)

// AgentElement is the LLM-facing projection of Element. It drops fields agents
// rarely use (Frame by default, redundant Hint-like fields), promotes the
// "is this useful for the current decision?" verdict (Actionable), and ranks
// the list so the most relevant ~40 nodes are at the top.
//
// The goal is the same that drove the RocketSim CLI's --agent mode: ~50%+
// fewer tokens vs the full tree without losing decision-grade information.
type AgentElement struct {
	ID         string `json:"id,omitempty"`
	Label      string `json:"label,omitempty"`
	Role       string `json:"role,omitempty"`
	Value      string `json:"value,omitempty"`
	Title      string `json:"title,omitempty"`
	Subrole    string `json:"subrole,omitempty"`
	Focused    string `json:"focused,omitempty"`
	Enabled    string `json:"enabled,omitempty"`
	Frame      string `json:"frame,omitempty"`
	Actionable bool   `json:"actionable"`
}

// AgentTreeOptions controls the compact projection.
type AgentTreeOptions struct {
	// Max caps the output length. Zero -> agentDefaultMax (40).
	Max int
	// WithFrame keeps the Frame field on each element. Off by default
	// because frames are 30-50 bytes of noise per element for most agent
	// decisions.
	WithFrame bool
}

const agentDefaultMax = 40

// AgentTree projects elements to the agent-facing shape, ranked by relevance:
//
//  1. focused first (the field the user is in matters most)
//  2. then actionable + enabled + visible-ish (taps/inputs the agent can use)
//  3. then everything else, in original order
//
// Ranking is stable across runs because Element ordering from ExtractElements
// is already deterministic. The cap is applied after sort.
func AgentTree(elements []Element, opts AgentTreeOptions) []AgentElement {
	if opts.Max <= 0 {
		opts.Max = agentDefaultMax
	}

	out := make([]AgentElement, 0, len(elements))
	for _, el := range elements {
		ae := AgentElement{
			ID:         el.ID,
			Label:      el.Label,
			Role:       el.Role,
			Value:      el.Value,
			Title:      el.Title,
			Subrole:    el.Subrole,
			Focused:    el.Focused,
			Enabled:    el.Enabled,
			Actionable: isActionable(el),
		}
		if opts.WithFrame {
			ae.Frame = el.Frame
		}
		out = append(out, ae)
	}

	// Stable sort: rank then original index (preserved by SliceStable).
	sort.SliceStable(out, func(i, j int) bool {
		return rankFor(out[i]) < rankFor(out[j])
	})

	if len(out) > opts.Max {
		out = out[:opts.Max]
	}
	return out
}

// rankFor returns a lower-is-better priority for the agent ranking. The
// numbers are arbitrary; only their relative ordering matters.
func rankFor(el AgentElement) int {
	if strings.EqualFold(el.Focused, "true") {
		return 0
	}
	if el.Actionable && el.Enabled != "false" {
		return 10
	}
	if el.Actionable {
		return 20
	}
	if el.ID != "" {
		return 30
	}
	return 50
}

// isActionable returns true when the element looks like something an agent
// can target with a tap, type, or selection. Conservative: false-negatives
// are fine (the agent can still use it via coordinates), false-positives
// (claiming a static label is actionable) are not.
func isActionable(el Element) bool {
	role := strings.ToLower(strings.TrimSpace(el.Role))
	if role == "" {
		// No role string but a non-empty id often means a custom container
		// the app exposed for testing -- treat as actionable.
		return el.ID != ""
	}
	for _, hit := range actionableRoles {
		if strings.Contains(role, hit) {
			return true
		}
	}
	return false
}

// actionableRoles is the substring list checked against an element's
// (lower-cased) role string. Covers the common AX/XCUITest role names
// across drivers.
var actionableRoles = []string{
	"button",
	"textfield", "text field",
	"textview", "text view",
	"switch",
	"tab",
	"cell",
	"link",
	"checkbox",
	"slider",
	"picker",
	"menuitem", "menu item",
	"radiobutton", "radio button",
	"searchfield", "search field",
}
