package mav

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentTreeFocusedFirst(t *testing.T) {
	in := []Element{
		{ID: "a", Role: "Button"},
		{ID: "b", Role: "TextField", Focused: "true"},
		{ID: "c", Role: "StaticText"},
	}
	out := AgentTree(in, AgentTreeOptions{})
	if out[0].ID != "b" {
		t.Fatalf("focused element should be first, got %s", out[0].ID)
	}
}

func TestAgentTreeActionableBeforeStatic(t *testing.T) {
	in := []Element{
		{ID: "label", Role: "StaticText"},
		{ID: "btn", Role: "Button", Enabled: "true"},
	}
	out := AgentTree(in, AgentTreeOptions{})
	if out[0].ID != "btn" {
		t.Fatalf("actionable element should outrank static, got %s", out[0].ID)
	}
	if !out[0].Actionable {
		t.Fatalf("expected Actionable=true on the button")
	}
	if out[1].Actionable {
		t.Fatalf("static text should not be actionable")
	}
}

func TestAgentTreeCapsAt40(t *testing.T) {
	in := make([]Element, 100)
	for i := range in {
		in[i] = Element{ID: "x", Role: "StaticText"}
	}
	out := AgentTree(in, AgentTreeOptions{})
	if len(out) != 40 {
		t.Fatalf("expected default cap of 40, got %d", len(out))
	}
}

func TestAgentTreeCustomCap(t *testing.T) {
	in := make([]Element, 100)
	for i := range in {
		in[i] = Element{ID: "x"}
	}
	out := AgentTree(in, AgentTreeOptions{Max: 5})
	if len(out) != 5 {
		t.Fatalf("expected cap of 5, got %d", len(out))
	}
}

func TestAgentTreeDropsFrameByDefault(t *testing.T) {
	in := []Element{{ID: "btn", Role: "Button", Frame: "{0,0,100,40}"}}
	out := AgentTree(in, AgentTreeOptions{})
	if out[0].Frame != "" {
		t.Fatalf("expected Frame dropped, got %q", out[0].Frame)
	}
}

func TestAgentTreeKeepsFrameWhenRequested(t *testing.T) {
	in := []Element{{ID: "btn", Role: "Button", Frame: "{0,0,100,40}"}}
	out := AgentTree(in, AgentTreeOptions{WithFrame: true})
	if out[0].Frame != "{0,0,100,40}" {
		t.Fatalf("expected Frame preserved, got %q", out[0].Frame)
	}
}

func TestAgentTreeJSONOmitsEmpty(t *testing.T) {
	in := []Element{{ID: "btn", Role: "Button", Enabled: "true"}}
	out := AgentTree(in, AgentTreeOptions{})
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	// Frame should not appear at all (omitempty), Title should not appear.
	for _, banned := range []string{"\"frame\"", "\"title\"", "\"subrole\""} {
		if strings.Contains(string(body), banned) {
			t.Errorf("expected %s omitted, got %s", banned, body)
		}
	}
	// Actionable always appears (not omitempty) so agents can rely on it.
	if !strings.Contains(string(body), "\"actionable\":true") {
		t.Errorf("expected actionable field present, got %s", body)
	}
}

func TestAgentTreeActionableHeuristic(t *testing.T) {
	for _, c := range []struct {
		role string
		want bool
	}{
		{"Button", true},
		{"TextField", true},
		{"TextView", true},
		{"text field", true},
		{"Cell", true},
		{"StaticText", false},
		{"Image", false},
		{"AXApplication", false},
	} {
		got := isActionable(Element{Role: c.role, ID: ""})
		if got != c.want {
			t.Errorf("isActionable(role=%q): got=%v want=%v", c.role, got, c.want)
		}
	}
	// No role but with ID -> actionable (custom containers).
	if !isActionable(Element{ID: "custom"}) {
		t.Errorf("expected element with ID and no role to be actionable")
	}
}

func TestAgentTreeDisabledOutranksUnactionable(t *testing.T) {
	// A disabled button still ranks above a static label.
	in := []Element{
		{ID: "label", Role: "StaticText"},
		{ID: "btn", Role: "Button", Enabled: "false"},
	}
	out := AgentTree(in, AgentTreeOptions{})
	if out[0].ID != "btn" {
		t.Fatalf("expected disabled button to outrank static text, got %s", out[0].ID)
	}
}
