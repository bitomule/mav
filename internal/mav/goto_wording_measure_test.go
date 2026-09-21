package mav

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
)

// THE WORDING MEASUREMENT for GotoStepQuestion.
//
// The defect being measured: on Boxy's category grid, with the short goal
// "la primera categoría", `find`'s question picks Moving Boxes (the first
// category, top-left at x=16 y=132) 40 times out of 40, and goto's question
// picks Test Category 1 instead about nine times in ten. Same screen, same
// candidates, same model; only the wording differs.
//
// Everything below the wording is held fixed: the same two screens, read from
// the raw `axe describe-ui` JSON that the real path reads, the same candidate
// extraction, the same option list with `none` on it, the same answer
// interpretation and the same veto. Only the QUESTION moves.
//
//	MAV_WORDING=1 go test ./internal/mav -run TestGotoWordingTable -v -timeout 40m
//
// MAV_WORDING_RUNS sets the runs per cell (default 10), MAV_WORDING_ONLY
// restricts the arms by name, MAV_WORDING_CELLS restricts the cells by name.

// The two screens, captured live on 21 sep from Boxy launched with
// TEST_CREATE_CATEGORIES + TEST_CREATE_PACKING_BATCH on an iPhone 17 Pro /
// iOS 26.3 simpool slot. Stored as the raw axe JSON rather than printed tree
// output, because the raw JSON is what the resolution path actually parses.
//
// NOTHING here may assert on a category uuid or a box code: both are
// regenerated on every launch (`category_E59D4FEE-...`, `boxRow_8993`). The
// names are the fixture's and they are stable.
const (
	wordingCategories = "categories.axe.json"
	wordingBoxes      = "boxes.axe.json"
)

func loadWordingScreen(t *testing.T, name string) []Element {
	t.Helper()
	data, err := os.ReadFile("testdata/wording/" + name)
	if err != nil {
		t.Fatal(err)
	}
	els := ExtractElements(string(data))
	if len(els) == 0 {
		t.Fatalf("%s parsed to no elements", name)
	}
	return els
}

// wordingArm is one phrasing of the question the goto loop puts to the model.
type wordingArm struct {
	name string
	ask  func(goal string) string
}

const wordingNoneDefence = "Answer `none` only if no element on this screen is it and none leads any closer. " +
	"Answering `none` is a correct and expected answer: a caller that gets `none` stops " +
	"and looks for itself, whereas a caller that gets the wrong number taps the wrong " +
	"thing and walks further away. Do not guess."

const wordingHeader = "Below is the list of elements currently on one screen of an iOS app, one per line, numbered.\n"

var wordingArms = []wordingArm{
	// The two ends of the measurement. `current` is what ships and what the
	// defect was measured on; `find` is the ceiling, and the arm that is NOT
	// available as a fix because it stops goto at a screen whose right answer
	// only leads to the destination.
	{"0-old-goto", func(goal string) string {
		return untrustedTextPreamble + wordingHeader +
			"Someone is trying to reach: " + goal + "\n\n" +
			"Which numbered element should they tap next? Two kinds of answer are equally right:\n" +
			"  - the element that IS what they are looking for, if it is on this screen;\n" +
			"  - the element that LEADS towards it — a section that contains it, a row on the way.\n" +
			"Answer with that number.\n" + wordingNoneDefence
	}},
	{"1-find", FindQuestion},
	{"9-shipped-goto", GotoStepQuestion},

	// The two v0.25.1 wordings, the release the 3/40 defect was reported
	// against. Neither carried untrustedTextPreamble then; both carry it now.
	// Measured here so that "the preamble fixed it" is a reading of a table
	// rather than a guess.
	{"0b-old-goto-nopreamble", func(goal string) string {
		return wordingHeader +
			"Someone is trying to reach: " + goal + "\n\n" +
			"Which numbered element should they tap next? Two kinds of answer are equally right:\n" +
			"  - the element that IS what they are looking for, if it is on this screen;\n" +
			"  - the element that LEADS towards it — a section that contains it, a row on the way.\n" +
			"Answer with that number.\n" + wordingNoneDefence
	}},
	{"1b-find-nopreamble", func(goal string) string {
		return strings.TrimPrefix(FindQuestion(goal), untrustedTextPreamble)
	}},

	// The two mechanism probes. They split goto's question into its two
	// departures from find's and move one at a time, so the table says which
	// half carries the defect instead of leaving it to be guessed at.
	//
	// `probe-reach-only` keeps find's body and swaps in only goto's framing
	// line ("trying to reach" instead of "described what they want to tap").
	// `probe-leads-only` keeps find's framing line and adds only goto's LEADS
	// clause.
	{"2-probe-reach-only", func(goal string) string {
		return untrustedTextPreamble + wordingHeader +
			"Someone is trying to reach: " + goal + "\n\n" +
			"Which numbered element is it? Answer with that number.\n" +
			"Answer `none` if no element on this screen is the one described. " +
			"Answering `none` is a correct and expected answer. Do not guess."
	}},
	{"3-probe-leads-only", func(goal string) string {
		return untrustedTextPreamble + wordingHeader +
			"A user described what they want to tap as: " + goal + "\n\n" +
			"Which numbered element should they tap next? Two kinds of answer are equally right:\n" +
			"  - the element that IS what they are looking for, if it is on this screen;\n" +
			"  - the element that LEADS towards it — a section that contains it, a row on the way.\n" +
			"Answer with that number.\n" + wordingNoneDefence
	}},

	// The candidate fixes. All three keep both cases open — the element that
	// IS it and the element that LEADS to it — and differ in how they order
	// them.
	{"4-is-first", func(goal string) string {
		return untrustedTextPreamble + wordingHeader +
			"A user described what they want to reach as: " + goal + "\n\n" +
			"Which numbered element is it? Answer with that number.\n" +
			"If no element on this screen is it, but one of them leads there — a section that " +
			"contains it, a row on the way — answer with that number instead.\n" +
			wordingNoneDefence
	}},
	{"5-is-wins", func(goal string) string {
		return untrustedTextPreamble + wordingHeader +
			"Someone is trying to reach: " + goal + "\n\n" +
			"Which numbered element should they tap next? Two kinds of answer are right, in this order:\n" +
			"  1. the element that IS what they are looking for, if one on this screen is it;\n" +
			"  2. only if none of them is it, the element that LEADS towards it — a section that " +
			"contains it, a row on the way.\n" +
			"Never answer 2 when an element answers 1. Answer with that number.\n" +
			wordingNoneDefence
	}},
	{"6-is-first-ambiguity", func(goal string) string {
		return untrustedTextPreamble + wordingHeader +
			"A user described what they want to reach as: " + goal + "\n\n" +
			"Which numbered element is it? Answer with that number.\n" +
			"If no element on this screen is it, but one of them leads there — a section that " +
			"contains it, a row on the way — answer with that number instead.\n" +
			"Answer `none` if no element here is it and none leads any closer, or if two or more " +
			"could equally be it. " +
			"Answering `none` is a correct and expected answer: a caller that gets `none` stops " +
			"and looks for itself, whereas a caller that gets the wrong number taps the wrong " +
			"thing and walks further away. Do not guess."
	}},
	// The finalist: goto's question with ONLY the sentence that names the goal
	// replaced, and "reach" kept because goto's goal really is a destination.
	{"7-described-reach", func(goal string) string {
		return untrustedTextPreamble + wordingHeader +
			"A user described where they want to get to as: " + goal + "\n\n" +
			"Which numbered element should they tap next? Two kinds of answer are equally right:\n" +
			"  - the element that IS what they are looking for, if it is on this screen;\n" +
			"  - the element that LEADS towards it — a section that contains it, a row on the way.\n" +
			"Answer with that number.\n" + wordingNoneDefence
	}},
	{"8-described-reach-ambiguity", func(goal string) string {
		return untrustedTextPreamble + wordingHeader +
			"A user described where they want to get to as: " + goal + "\n\n" +
			"Which numbered element should they tap next? Two kinds of answer are equally right:\n" +
			"  - the element that IS what they are looking for, if it is on this screen;\n" +
			"  - the element that LEADS towards it — a section that contains it, a row on the way.\n" +
			"Answer with that number.\n" +
			"Answer `none` if no element on this screen is it and none leads any closer, or if " +
			"two or more could equally be it. " +
			"Answering `none` is a correct and expected answer: a caller that gets `none` stops " +
			"and looks for itself, whereas a caller that gets the wrong number taps the wrong " +
			"thing and walks further away. Do not guess."
	}},
}

// wordingCell is one screen plus one goal, with the right answer declared and
// the reason it is right written down. `want` is matched against the chosen
// element's label; an empty `want` means the right answer is `none`.
type wordingCell struct {
	name   string
	screen string
	goal   string
	want   string
	why    string
}

var wordingCells = []wordingCell{
	{
		name: "A-broken", screen: wordingCategories,
		goal: "la primera categoría", want: "Moving Boxes",
		why: "The grid is Moving Boxes (x=16 y=132, top-left), Test Category 1 to its right, " +
			"Test Category 2 below. The first category in reading order is Moving Boxes. " +
			"`Test Category 1` is a name that merely contains a 1; it is the second cell.",
	},
	{
		name: "B-leads", screen: wordingCategories,
		goal: "los contenidos de la primera categoría, primera caja", want: "Moving Boxes",
		why: "The destination is two taps away and is NOT on this screen: box contents live " +
			"inside a box, which lives inside a category. The right answer is the row that " +
			"LEADS — the first category. Abstaining here is the no_route failure the LEADS " +
			"clause exists to prevent.",
	},
	{
		name: "C-is-at-leaf", screen: wordingBoxes,
		goal: "los contenidos de la primera categoría, primera caja", want: "Office cables",
		why: "Standing inside Moving Boxes, the first box row IS the thing to open. This is " +
			"the cell the first attempt at a wider question broke: told that the user is not " +
			"there yet, the model hesitated on the row that IS the destination.",
	},
	{
		name: "D1-absent-cat", screen: wordingCategories,
		goal: "la bandeja de entrada del correo electrónico", want: "",
		why: "Boxy has no mail. Nothing on the category grid is an inbox and nothing leads to " +
			"one, so the only correct answer is `none`. The service does not abstain on its " +
			"own, so this column is what a gained hit may not be paid for with.",
	},
	{
		name: "D2-absent-box", screen: wordingBoxes,
		goal: "la bandeja de entrada del correo electrónico", want: "",
		why: "Same goal on the box list: no inbox, nothing leading to one, `none` is correct.",
	},
	{
		name: "D3-absent-cat2", screen: wordingCategories,
		goal: "la pantalla de pago de la cesta de la compra", want: "",
		why: "Boxy sells nothing and has no cart or checkout; `none` is correct.",
	},
	{
		name: "D4-absent-box2", screen: wordingBoxes,
		goal: "la pantalla de pago de la cesta de la compra", want: "",
		why: "Same, on the box list.",
	},
}

type wordingResult struct {
	arm, cell            string
	hit, miss, abstained int
	picks                map[string]int
}

func askWording(ctx context.Context, key, question string, batch []Element) (string, error) {
	cmd := exec.CommandContext(ctx, "jevi", "ask",
		"--options", strings.Join(FindOptions(batch), ","),
		"--json", "--soft", question)
	cmd.Stdin = strings.NewReader(RenderFindCandidates(batch))
	cmd.Env = append(os.Environ(), jevProviderEnvVar+"="+key)
	out, _ := cmd.Output()
	var doc struct {
		OK      bool `json:"ok"`
		Answers map[string]struct {
			Label string `json:"label"`
		} `json:"answers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &doc); err != nil || !doc.OK {
		return "", fmt.Errorf("jev unavailable: %s", strings.TrimSpace(string(out)))
	}
	for _, a := range doc.Answers {
		return a.Label, nil
	}
	return "", fmt.Errorf("no answer")
}

func TestGotoWordingTable(t *testing.T) {
	if os.Getenv("MAV_WORDING") == "" {
		t.Skip("set MAV_WORDING=1 to spend model calls on the wording table")
	}
	runs := 10
	if n := os.Getenv("MAV_WORDING_RUNS"); n != "" {
		fmt.Sscanf(n, "%d", &runs)
	}
	key, _, ok := ResolveJevKey()
	if !ok {
		t.Fatal("no jev key")
	}

	batches := map[string][]Element{}
	for _, s := range []string{wordingCategories, wordingBoxes} {
		b, _ := FindBatch(FindCandidates(loadWordingScreen(t, s)))
		batches[s] = b
	}

	type job struct{ a, c int }
	var jobs []job
	for ai := range wordingArms {
		if only := os.Getenv("MAV_WORDING_ONLY"); only != "" && !strings.Contains(only, wordingArms[ai].name) {
			continue
		}
		for ci := range wordingCells {
			if only := os.Getenv("MAV_WORDING_CELLS"); only != "" && !strings.Contains(only, wordingCells[ci].name) {
				continue
			}
			jobs = append(jobs, job{ai, ci})
		}
	}

	out := make([]wordingResult, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			arm, cell := wordingArms[j.a], wordingCells[j.c]
			batch := batches[cell.screen]
			r := wordingResult{arm: arm.name, cell: cell.name, picks: map[string]int{}}
			for run := 0; run < runs; run++ {
				sem <- struct{}{}
				label, err := askWording(t.Context(), key, arm.ask(cell.goal), batch)
				<-sem
				if err != nil {
					r.picks["ERR:no_network"]++
					continue
				}
				chosen, reason := InterpretGotoAnswer(label, batch)
				if reason == "" {
					if veto := VetoChoice(chosen, cell.goal, batch); veto != "" {
						chosen, reason = nil, veto
					}
				}
				if reason != "" {
					r.abstained++
					r.picks["none:"+reason]++
					if cell.want == "" {
						r.hit++
					}
					continue
				}
				r.picks[chosen.Label]++
				switch {
				case cell.want != "" && strings.Contains(chosen.Label, cell.want):
					r.hit++
				default:
					r.miss++
				}
			}
			out[i] = r
		}(i, j)
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool {
		if out[i].cell != out[j].cell {
			return out[i].cell < out[j].cell
		}
		return out[i].arm < out[j].arm
	})
	for _, r := range out {
		keys := make([]string, 0, len(r.picks))
		for k := range r.picks {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s x%d", k, r.picks[k]))
		}
		t.Logf("ROW\t%s\t%s\thit=%d\tmiss=%d\tabstained=%d\tof=%d\t%s",
			r.cell, r.arm, r.hit, r.miss, r.abstained, runs, strings.Join(parts, " | "))
	}
}
