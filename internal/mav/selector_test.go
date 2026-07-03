package mav

import (
	"strings"
	"testing"
)

func boolPtr(value bool) *bool { return &value }
func intPtr(value int) *int    { return &value }

func TestMatchElementsCombinesTypedPredicates(t *testing.T) {
	elements := []Element{
		{ID: "save", Label: "Save Category", Role: "AXButton", Enabled: "false", Frame: "{{300, 20}, {80, 44}}"},
		{ID: "create", Label: "Create Category", Role: "AXButton", Enabled: "true", Frame: "{{300, 800}, {80, 44}}"},
	}
	got, err := MatchElements(elements, Selector{
		TextContains: "create",
		TextRegex:    "^Create",
		Role:         "button",
		Enabled:      boolPtr(true),
		Bounds:       "bottomHalf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "create" {
		t.Fatalf("got=%+v", got)
	}
}

func TestMatchElementsIndexAfterFiltering(t *testing.T) {
	elements := []Element{
		{ID: "a", Label: "Row", Role: "cell"},
		{ID: "button", Label: "Row", Role: "button"},
		{ID: "b", Label: "Row", Role: "cell"},
	}
	got, err := MatchElements(elements, Selector{Text: "Row", Role: "cell", Index: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("got=%+v", got)
	}
}

func TestMatchElementsNearAndParentOf(t *testing.T) {
	elements := []Element{
		{ID: "form", Role: "group", Frame: "{{0, 100}, {400, 400}}", Depth: 0},
		{ID: "label", Label: "Email", Role: "text", Frame: "{{20, 140}, {100, 30}}", Depth: 1},
		{ID: "field", Role: "textField", Frame: "{{20, 185}, {300, 44}}", Depth: 1},
		{ID: "outside", Role: "button", Frame: "{{20, 700}, {100, 44}}", Depth: 0},
	}
	got, err := MatchElements(elements, Selector{
		Role: "textField",
		Near: &NearSelector{
			Where:       Selector{Text: "Email"},
			Direction:   "below",
			MaxDistance: 120,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "field" {
		t.Fatalf("near got=%+v", got)
	}

	parents, err := MatchElements(elements, Selector{
		ID:       "form",
		ParentOf: &Selector{ID: "field"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(parents) != 1 || parents[0].ID != "form" {
		t.Fatalf("parent got=%+v", parents)
	}
}

func TestSelectorValidation(t *testing.T) {
	if _, err := MatchElements(nil, Selector{TextRegex: "["}); err == nil || !strings.Contains(err.Error(), "selector_regex_invalid") {
		t.Fatalf("err=%v", err)
	}
	if _, err := MatchElements(nil, Selector{Bounds: "corner"}); err == nil || !strings.Contains(err.Error(), "selector_bounds_invalid") {
		t.Fatalf("err=%v", err)
	}
}

func TestSelectorInfersVisibleFromFrameWhenDriverOmitsField(t *testing.T) {
	visible := true
	matches, err := MatchElements([]Element{{Label: "Saved", Frame: "{{10, 20}, {100, 40}}"}}, Selector{Text: "Saved", Visible: &visible})
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches=%v err=%v", matches, err)
	}
}
