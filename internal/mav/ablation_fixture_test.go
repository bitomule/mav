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
	// The screens a user actually sees, captured 21 sep from a simpool slot
	// (iPhone 17 Pro / iOS 26.3) with the eight-box fixture. They exist
	// because the two above do not describe what anyone looks at: the capture
	// there holds TWO categories and the live screen holds THREE, and that
	// difference changes the answers. Measurements about what goto will do go
	// against these.
	//
	// The category uuids in them are regenerated on every launch and appear
	// three times each. Nothing may be asserted against one.
	fixtureCategoriesThree  = "categories-three-view.txt"
	fixtureNewCategorySheet = "new-category-sheet.txt"
	// The SAME screen as categories-three-view.txt, captured in the same
	// state as the raw `axe describe-ui` JSON that find actually reads,
	// rather than as printed tree lines. It exists to settle whether the
	// printed fixtures misrepresent a live run — the header above warns that
	// the printer drops nodes the extraction keeps, and that warning had
	// never been tested against a candidate list.
	//
	// Measured: on this screen both reads yield THE SAME NINE CANDIDATES, in
	// the same order, differing only in the per-launch category uuids. The
	// dropped nodes are untitled groups that were never candidates.
	// TestWhatBreaksTheControlCell asserts the two stay in agreement.
	fixtureCategoriesThreeAXE  = "categories-three-view.axe.json"
	fixtureNewCategorySheetAXE = "new-category-sheet.axe.json"
)

// The claim the printed fixtures rest on, asserted rather than assumed, and it
// costs no model call. The header above has always warned that these .txt
// fixtures are `mav ui tree` OUTPUT while find reads the raw axe JSON, and that
// the printer drops nodes the extraction keeps — a warning that was used for
// months to doubt numbers taken off them without anyone checking what it costs
// at the only place it could matter, the CANDIDATE list.
//
// Measured on both screens: it costs nothing. The nodes the printer drops are
// untitled groups, which FindCandidates rejects anyway for carrying no text. A
// printed capture and a live read of the same screen offer the model the same
// menu, and the only thing that differs is the per-launch category uuids.
//
// So a measurement off these fixtures IS a measurement about the candidate
// list a live run would send. That does not make the fixture the live screen
// in every respect — it is still a frozen moment — but the shape of the
// question is the same one, and that is what the shape measurements turn on.
func TestThePrintedFixturesOfferTheSameMenuAsALiveRead(t *testing.T) {
	for _, pair := range []struct{ printed, raw string }{
		{fixtureCategoriesThree, fixtureCategoriesThreeAXE},
		{fixtureNewCategorySheet, fixtureNewCategorySheetAXE},
	} {
		printed, _ := FindBatch(FindCandidates(loadFixtureScreen(t, pair.printed)))
		raw, _ := FindBatch(FindCandidates(loadRawAXEFixture(t, pair.raw)))
		if len(printed) != len(raw) {
			t.Errorf("%s: printed offers %d candidates, the live read offers %d",
				pair.printed, len(printed), len(raw))
			continue
		}
		for i := range printed {
			// By label and role. Not by id: the category uuids are
			// regenerated on every launch, so the two captures never share
			// them and asserting on one would fail for the wrong reason.
			if printed[i].Label != raw[i].Label || printed[i].Role != raw[i].Role {
				t.Errorf("%s: candidate %d differs — printed %q/%s, live %q/%s",
					pair.printed, i+1, printed[i].Label, printed[i].Role, raw[i].Label, raw[i].Role)
			}
		}
	}
}

// loadRawAXEFixture reads a capture down the path find uses in a live run:
// the driver's own JSON through ExtractElements, with no printer in between.
func loadRawAXEFixture(t *testing.T, name string) []Element {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ablation", name))
	if err != nil {
		t.Fatal(err)
	}
	elements := ExtractElements(string(data))
	if len(elements) == 0 {
		t.Fatalf("%s carries no elements", name)
	}
	return elements
}

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

// THE SEARCH-FIELD DEFECT, measured off these fixtures and left unfixed on
// purpose. Written down here so nobody re-derives it, and so nobody "fixes" it
// with the two things that are forbidden.
//
// `mav ui find "la primera caja"` on the category list - which has no box on it
// at all - returns the SEARCH FIELD, 5/5. Every run below is 5 against these
// fixtures, down the real resolution path.
//
// What it is NOT:
//
//   - Not the Spanish word. "the first box" in English picks the same search
//     field, 5/5. The suspicion that "caja" reads as "caja de búsqueda" is
//     refuted: the same thing happens in a language where it does not.
//   - Not a catch-all. The search field is not where the model dumps anything
//     it cannot place: "the delete button" 0/5, "the shopping cart icon" 0/5 -
//     both abstain. The abstention works on this exact screen.
//   - Not a missing state filter. The field is visible, enabled and actionable,
//     so it is a legitimate candidate, and dropping it would be dropping a real
//     tap target.
//
// What it IS: a bare noun that genuinely denotes a text box in both languages.
// Give the phrase anything that disambiguates and the model abstains
// correctly - "a cardboard box for storing things" 0/5, "the list row for a
// box" 0/5. It is the undisambiguated word that loses, and on that screen the
// only "box" really is the search box.
//
// Three fixes were tried and all three are rejected, measured:
//
//  1. Filtering candidates by text - forbidden, and it would take `Agregar
//     Caja` off the menu, which is the rule in the other direction.
//  2. A confidence cut - forbidden and useless: correct picks score from 0.62
//     and wrong ones reach 0.88.
//  3. The structural trim that the find selector already composes with:
//     `{find: "la primera caja", role: "button"}`. It does NOT fix this. It
//     replaces one wrong answer with another - Test Category 1, 4/5 - because
//     with the search field gone the model still does not abstain. That is
//     worse than the defect: a category is a plausible-looking tap that
//     navigates.
//  4. Prompt wording. Adding "an element that merely relates to it - a field
//     where you could type or search for it - is not it" changed nothing: the
//     seven good cells held and the defect cell stayed at 5/5. The model is not
//     reading the field as "where you would search for a box"; it reads it as
//     being a box. The wording was reverted rather than left in doing nothing.
//
// So it stands, and what a flow author does about it is not a code change:
// name the thing in a way that a text box cannot satisfy.

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

func TestNothingFiltersTheSearchFieldOutByWhatItSays(t *testing.T) {
	// The guard on the fix that must never be made. Taking the search field
	// off the menu because of the defect above would be filtering by text, and
	// the same rule keeps `Agregar Caja` - the button that creates a box on a
	// screen of boxes - on it. This asserts both directions at once, with no
	// model involved.
	for _, name := range []string{fixtureCategories, fixtureBoxes} {
		candidates := FindCandidates(loadFixtureScreen(t, name))
		searchField, createButton := false, false
		for _, el := range candidates {
			if strings.Contains(strings.ToLower(el.Role), "search") {
				searchField = true
			}
			if el.ID == "createBoxButton" || el.ID == "createCategoryButton" {
				createButton = true
			}
		}
		if !searchField {
			t.Errorf("%s: the search field is visible, enabled and tappable; it stays a candidate", name)
		}
		if !createButton {
			t.Errorf("%s: the create button stays a candidate; the filter reads state, never words", name)
		}
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
//
// READ THIS BEFORE CITING ANY NUMBER FROM THIS BENCH: THESE ARE NOT THE SCREENS
// A USER SEES.
//
// `categories-view.txt` holds TWO categories and no `Moving Boxes`. The screen
// the demo records, and the one every real run walks through, holds THREE. That
// difference is not cosmetic — it changes the answers:
//
//   - On this fixture, "la primera caja" returns the SEARCH FIELD 10/10, which
//     is the defect documented at length below.
//   - On the real three-category screen, the same phrase returns `Moving Boxes`
//     10/10. **The search-field defect does not exist on the screen anyone
//     looks at.** It is an artefact of a two-category capture.
//
// Hours went into chasing that one before anybody compared the fixture with the
// live screen. So: this bench is for questions about the resolution path —
// deterministic, repeatable, no simulator — and it is NOT evidence about what a
// user will hit. When the two disagree, the live screen decides, because it is
// the one that decides for them.
//
// The real failure on the live screen is a different phrase entirely:
// "la primera categoría" over three categories, where "first" can be read as
// screen order (`Moving Boxes`) or as the NAME `Test Category 1`. Genuine
// ambiguity, not a message-shape problem and not jevi's. And the fix is
// counter-intuitive enough to be worth writing down: the LONGER goal
// ("los contenidos de la primera categoría, primera caja") resolves 10/10 where
// the short one wobbles. A goal that names each screen in turn is more reliable
// than a goal that names one thing.
//
// IT COUNTS RESOLUTIONS, NOT CORRECT ANSWERS. It reports how often find returns
// SOME element versus abstains, and prints what it picked; it never compares
// against a right answer. So a "5/5" here means "resolved 5 of 5", and reading
// it as "was right 5 of 5" is an interpretation this test does not support --
// which is exactly how three wrong picks on the category screen were recorded
// for a day as successes. docs/design/flow-steps.md §1.4 has which is which.
//
// AND EVERY NUMBER TAKEN THROUGH THE INSTALLED jevi BEFORE 21 SEP WAS MEASURED
// WITH THE QUESTION'S KEY ORDER DESTROYED. jevi deserialised and re-serialised
// each forwarded question through serde_json WITHOUT preserve_order, so every
// object came out alphabetised: a question written {goal, context, rules}
// reached the model as {context, goal, rules}, and an option written
// {role, name, id} as {id, name, role}. Its own comment claimed the question
// travelled untouched -- true of the values, false of the order.
//
// That is not a footnote, it is the whole reason this bench once showed nine
// call shapes producing byte-identical answers: they were nine shapes flattened
// into the same one. Measured on one cell of this fixture, by hand over HTTP:
// caller's order 28/30, alphabetised 3/30. Through the binary, ablated clean:
// 2/30 before the fix, 29/30 after.
//
// So a measurement from this bench is only comparable with another taken on the
// same jevi build. When in doubt, re-run it rather than trusting a number in a
// document -- that is what the bench is for and it costs 40 calls.
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
