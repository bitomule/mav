package mav

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Two real screens of Boxy, captured on 21 sep during the 80-run ablation and
// kept as fixtures: the category list (no box anywhere on it) and the box
// list. They are here so that questions about what find answers can be asked
// off a simulator - deterministic, repeatable, and not competing for a device
// with whatever else is measuring.
//
// The trees are `mav ui tree` output rather than the raw axe JSON find reads,
// which is a known and bounded difference: the printer drops nodes the
// extraction keeps. It is checked below against the candidate count the real
// runs recorded (8 on both screens), and if that stops matching, the fixture
// has drifted from what it claims to represent and the check says so.

const (
	fixtureCategories = "categories-view.txt"
	fixtureBoxes      = "boxes-view.txt"
)

// treeFieldPattern finds where each field of a `node ...` line starts. Split
// this way rather than on spaces because values contain them unquoted:
// `value=0 % enabled=true` is one node with a value of "0 %".
var treeFieldPattern = regexp.MustCompile(`(^|\s)(index|id|label|role|value|enabled|selected|visible|subrole|title|pid|focused|frame)=`)

func loadFixtureScreen(t *testing.T, name string) []Element {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ablation", name))
	if err != nil {
		t.Fatal(err)
	}
	elements := []Element{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "node ") {
			continue
		}
		elements = append(elements, parseTreeLine(strings.TrimPrefix(line, "node ")))
	}
	if len(elements) == 0 {
		t.Fatalf("%s carries no nodes", name)
	}
	return elements
}

func parseTreeLine(line string) Element {
	starts := treeFieldPattern.FindAllStringSubmatchIndex(line, -1)
	el := Element{}
	for i, match := range starts {
		key := line[match[4]:match[5]]
		valueStart := match[5] + 1
		valueEnd := len(line)
		if i+1 < len(starts) {
			valueEnd = starts[i+1][2]
		}
		value := strings.TrimSpace(line[valueStart:valueEnd])
		value = strings.TrimSuffix(strings.TrimPrefix(value, `"`), `"`)
		switch key {
		case "id":
			el.ID = value
		case "label":
			el.Label = value
		case "role":
			el.Role = value
		case "value":
			el.Value = value
		case "enabled":
			el.Enabled = value
		case "selected":
			el.Selected = value
		case "visible":
			el.Visible = value
		case "subrole":
			el.Subrole = value
		case "title":
			el.Title = value
		case "focused":
			el.Focused = value
		case "frame":
			el.Frame = value
		}
	}
	return el
}

func TestTheAblationFixturesStillDescribeWhatWasMeasured(t *testing.T) {
	// The 80 recorded runs report candidates=8 on both screens.
	//
	// The category screen - the one the defect lives on - reproduces that
	// exactly, which is what matters for anything concluded here.
	//
	// The box screen yields 7, and the difference is understood rather than
	// waved at: this tree was captured in a different launch than the runs
	// were, and the fixture that creates the boxes does not produce the same
	// one twice (this capture holds box 1000; the measured runs all picked
	// 1001). The printed tree also drops nodes the extraction keeps. Both
	// numbers are asserted so that drift in either fixture is caught.
	for name, want := range map[string]int{fixtureCategories: 8, fixtureBoxes: 7} {
		got := FindCandidates(loadFixtureScreen(t, name))
		if len(got) != want {
			t.Errorf("%s: expected %d candidates, this fixture yields %d:\n%s",
				name, want, len(got), RenderFindCandidates(got))
		}
	}
}

func TestTheCategoryScreenHasNoBoxAndKeepsItsSearchField(t *testing.T) {
	// The screen the defect lives on: there is no box anywhere, and the search
	// field is a legitimate candidate (visible, enabled, actionable). Whether
	// offering it is the mistake is exactly what the defect is about.
	candidates := FindCandidates(loadFixtureScreen(t, fixtureCategories))
	searchFields := 0
	for _, el := range candidates {
		if strings.Contains(strings.ToLower(el.Role), "search") {
			searchFields++
		}
		if strings.Contains(strings.ToLower(el.ID), "box") {
			t.Fatalf("the category screen must carry no box: %+v", el)
		}
	}
	if searchFields != 1 {
		t.Fatalf("expected the one search field among the candidates, got %d:\n%s",
			searchFields, RenderFindCandidates(candidates))
	}
}

// ablationPhrases are the four the 80-run table was built on.
var ablationPhrases = []string{
	"the box inside Test Category 2",
	"the box inside this category",
	"open the box in Test Category 2",
	"la primera caja",
}

// TestAblationTable is the control. It puts the real questions down the real
// resolution path against the two fixture screens, five times each, and prints
// the table. It costs 40 model calls, so it only runs when asked for:
//
//	MAV_ABLATION=1 go test ./internal/mav -run TestAblationTable -v
//
// The recorded table (21 sep, mav 0.25.1) is in the output next to what this
// run produced, so a change is read off rather than remembered.
func TestAblationTable(t *testing.T) {
	if os.Getenv("MAV_ABLATION") == "" {
		t.Skip("set MAV_ABLATION=1 to spend 40 model calls on the control table")
	}
	c := CLI{}
	for _, screen := range []string{fixtureCategories, fixtureBoxes} {
		elements := loadFixtureScreen(t, screen)
		for _, phrase := range ablationPhrases {
			resolved, picks := 0, map[string]int{}
			for run := 0; run < 5; run++ {
				result := c.resolveFind(t.Context(), elements, phrase)
				if result.Element != nil {
					resolved++
					picks[describeAblationPick(*result.Element)]++
					continue
				}
				picks["none:"+result.Reason]++
			}
			t.Logf("%-20s %-32q %d/5 %v", strings.TrimSuffix(screen, ".txt"), phrase, resolved, picks)
		}
	}
}

func describeAblationPick(el Element) string {
	return fmt.Sprintf("%s|%s|%s|%s", el.ID, el.Label, el.Role, el.Value)
}
