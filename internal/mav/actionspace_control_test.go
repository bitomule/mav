package mav

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// THE CONTROL. The acceptance test for the action space is not that it works,
// it is that TAP STILL MEASURES THE SAME.
//
// Measured the night before this was built, on the real Boxy category screen,
// five request shapes x 10 runs on the cell that decides ("la primera
// categoría"): the production shape — a numbered list in text plus `--options`
// — answered correctly 9-10 times out of 10, and every variation on it
// collapsed to wrong 9-10 times out of 10. (That 9-10/10 is FIND's question.
// goto's question on the same cell is 2/30 — see TestWhatBreaksTheControlCell.
// The shape finding stands; the baseline it was quoted against was the wrong
// command's.) Structured records instead of the
// text list: 9/10 wrong. `instructions` as an object: 10/10 wrong. Both at
// once: 10/10 wrong. A change to the SHAPE of the request is not a neutral
// refactor here; it is the failure mode.
//
// So this puts both shapes down the same path, on the same screen, in the same
// batch, ALTERNATING, and prints what each picked. Alternating rather than one
// arm then the other: the provider is not a pure function of the request, and
// twenty consecutive calls of one shape followed by twenty of the other
// attributes any drift in the service to the change.
//
//	MAV_ACTIONSPACE_CONTROL=1 go test ./internal/mav -run TestActionSpaceControl -v
//
// It costs 3 model calls per run x the run count, so it is off by default.
//
// IT COUNTS RIGHT, WRONG AND ABSTAINED SEPARATELY, and never just "it
// answered". TestAblationTable next door counts resolutions and says so at
// length, because reading a resolution as a correct answer is how three wrong
// picks were recorded for a day as successes. This does not repeat that: the
// correct answer is declared below, in code, with the reason.
//
// WHAT COUNTS AS CORRECT HERE, and it is not a matter of taste. The goal is
// "la primera categoría" and the screen is a two-column grid:
//
//	Moving Boxes      frame x=16  y=132     <- first, top-left
//	Test Category 1   frame x=209 y=132
//	Test Category 2   frame x=16  y=309
//
// `Moving Boxes` is first by reading order AND first in the tree, so it is
// candidate 3 and `Test Category 1` is candidate 4. THE CORRECT ANSWER IS
// `Moving Boxes`. `Test Category 1` is the other reading — "first" as part of
// the NAME — and it is the failure you can watch happen: goto opens the wrong
// category. Counting it as the convergent answer, which an earlier version of
// this comment did, turns the table upside down and makes a broken base read
// as a healthy one.
//
// WHAT IT SAID, 21 sep, mav 0.27.0, jevi 0.4.0, typesafe/jev-1.13, six batches
// of 10 alternating, load average 3.7-5.0 throughout, screen fingerprint
// 3a7a749a… over 9 candidates on every batch (that capture; the arms now read
// the raw axe capture of the same screen, whose nine candidates differ only in
// the per-launch uuids):
//
//	                                     right   wrong   abstained
//	production, one head, --options      11/60   48/60      1/60
//	action space, two heads               7/60   53/60      0/60
//	action space, four heads             11/60   49/60      0/60
//
// Wrong is `Test Category 1` in every single case, in all three arms. Not one
// run of the 180 picked anything else on this screen.
//
// TWO THINGS COME OUT OF THAT, and the first is bigger than this branch.
//
//  1. PRODUCTION IS ALREADY WRONG 80% OF THE TIME ON THIS CELL, and
//     TestWhatBreaksTheControlCell below says why: it is GOTO'S QUESTION.
//     `mav ui find`, asked the identical phrase about the identical screen,
//     is right 40/40. It is not the fixture, not the screen, and not the
//     short goal on its own.
//  2. THE CONTROL IS THEREFORE INCONCLUSIVE, NOT PASSED. Four heads (11/60)
//     against one head (11/60) is the same number. Two heads (7/60) is lower
//     than production, and two heads is not a hypothetical — it is what
//     the loop sends whenever the caller declared no inputs, which is every
//     ordinary goto today. On the numbers here that arm cannot be called
//     harmless. Nobody should wire the tap path to this on the strength of
//     this table; what the table does establish is that the extra heads are
//     not the catastrophic shape change that structured records and an
//     `instructions` object were (~10/10 wrong), which is what was feared.
//
// A seventh batch was run after the arms were repointed at the raw axe capture
// rather than the printed one, to check that the repoint changed nothing:
// 1/10, 0/10, 2/10 — the same regime as the six above.
//
// The `type` path is a different matter: it is measured separately below and
// it is 10/10, on a screen where tapping was never an answer at all.
const actionSpaceControlRuns = 10

// actionSpaceControlAnswer is the label of the correct pick on the control
// cell. By label, never by id: the category uuids are regenerated on every
// launch of the app and appear three times each in the tree.
const actionSpaceControlAnswer = "Moving Boxes"

func TestActionSpaceControl(t *testing.T) {
	if os.Getenv("MAV_ACTIONSPACE_CONTROL") == "" {
		t.Skip("set MAV_ACTIONSPACE_CONTROL=1 to spend model calls on the tap control")
	}
	key, _, ok := ResolveJevKey()
	if !ok {
		t.Skip("no jev key")
	}

	const goal = "la primera categoría"
	// Down the live read path — raw axe JSON through ExtractElements, exactly
	// what find gets from the driver — so that "you measured a file, not a
	// screen" is not an available objection to any number below. The printed
	// capture of the same screen offers the same nine candidates; that is
	// asserted next door and costs no model call.
	batch, _ := FindBatch(FindCandidates(loadRawAXEFixture(t, fixtureCategoriesThreeAXE)))
	text := RenderFindCandidates(batch)
	// The canary is mav's own screen fingerprint — the sorted (id, label,
	// role) of what was sent, never a node count. Logged beside the numbers so
	// a drifted fixture is visible next to whatever was concluded from it.
	t.Logf("screen fingerprint %s over %d candidates:\n%s", screenFingerprint(batch), len(batch), text)

	production := &armTally{}
	tapOnlySpace := &armTally{}
	fullSpace := &armTally{}
	inputs := []string{"color", "nombre"}

	for run := 0; run < actionSpaceControlRuns; run++ {
		answer, err := askJevChoice(t.Context(), key, GotoStepQuestion(goal), text, FindOptions(batch))
		if err != nil {
			t.Fatalf("production arm could not ask: %v", err)
		}
		element, reason := InterpretGotoAnswer(answer.Label, batch)
		production.record(element, reason)

		choice, err := ResolveActionSpace(t.Context(), key, ActionSpace{Goal: goal, Batch: batch})
		if err != nil {
			t.Fatalf("tap-only action space could not ask: %v", err)
		}
		tapOnlySpace.record(choice.Element, choice.Reason)

		choice, err = ResolveActionSpace(t.Context(), key,
			ActionSpace{Goal: goal, Batch: batch, InputKeys: inputs})
		if err != nil {
			t.Fatalf("full action space could not ask: %v", err)
		}
		fullSpace.record(choice.Element, choice.Reason)
	}

	t.Logf("%-28s %-22s %s", "arm", "right/wrong/abstained", "what came back")
	t.Logf("%-28s %s", "production (1 head)", production)
	t.Logf("%-28s %s", "action space (2 heads)", tapOnlySpace)
	t.Logf("%-28s %s", "action space (4 heads)", fullSpace)
}

// armTally keeps the three outcomes apart. A pick that is not the declared
// answer is WRONG, and an abstention is neither right nor wrong: on this cell
// it is the one honest thing the model can do about a genuinely ambiguous goal,
// and folding it into either column hides that.
type armTally struct {
	right     int
	wrong     int
	abstained int
	picks     map[string]int
}

func (a *armTally) record(el *Element, reason string) {
	if a.picks == nil {
		a.picks = map[string]int{}
	}
	a.picks[describeActionPick(el, reason)]++
	switch {
	case el == nil:
		a.abstained++
	case el.Label == actionSpaceControlAnswer:
		a.right++
	default:
		a.wrong++
	}
}

func (a *armTally) String() string {
	return fmt.Sprintf("%-22s %s",
		fmt.Sprintf("%d/%d/%d", a.right, a.wrong, a.abstained), formatPicks(a.picks))
}

// TestActionSpaceCanWrite is the other half: the thing that could not be
// attempted at all before. The new-category sheet is a real screen with one
// text field among three dozen colour buttons, and the goal names a category
// to create. A run that comes back `type` on the name field with the declared
// key is the whole capability; anything else is printed rather than asserted,
// because this is a measurement and not a unit test.
//
// WHAT IT SAID, 21 sep, over 68 candidates, goal "crea una categoría con
// nombre Test category", declared inputs {nombre, color} — measured twice, once
// off the printed capture and once off the live raw axe JSON, which offer the
// same candidate list:
//
//	printed capture   10/10  type · Campo de texto del nombre… · key `nombre` · "Test category"
//	live raw axe      10/10  type · Campo de texto del nombre… · key `nombre` · "Test category"
//
// Note what the value is: the caller's own string, fetched by the key the model
// named. The model never emitted those characters.
//
// One wording change earned that, and it is worth keeping because it names the
// mistake: worded only as "choose type when the goal needs text in a field",
// the operation head answered `tap` on `Crear categoria` 9 times out of 10 —
// the toolbar button BEHIND the sheet, the one that had already been pressed to
// open the very field it was being asked about. Saying so plainly — that the
// field being in front of you means you are already where that button leads —
// took it to 10/10. The tap head was not touched by that change.
//
//	MAV_ACTIONSPACE_CONTROL=1 go test ./internal/mav -run TestActionSpaceCanWrite -v
func TestActionSpaceCanWrite(t *testing.T) {
	if os.Getenv("MAV_ACTIONSPACE_CONTROL") == "" {
		t.Skip("set MAV_ACTIONSPACE_CONTROL=1 to spend model calls on the write measurement")
	}
	key, _, ok := ResolveJevKey()
	if !ok {
		t.Skip("no jev key")
	}

	const goal = "crea una categoría con nombre Test category"
	inputs := map[string]string{"nombre": "Test category", "color": "Jade"}
	// Down the live read path — the raw axe JSON, as find gets it — not the
	// printed capture. The two offer the same 68 candidates here, asserted
	// next door, and this is the one that needs no argument.
	batch, _ := FindBatch(FindCandidates(loadRawAXEFixture(t, fixtureNewCategorySheetAXE)))
	t.Logf("screen fingerprint %s over %d candidates", screenFingerprint(batch), len(batch))

	picks := map[string]int{}
	for run := 0; run < actionSpaceControlRuns; run++ {
		choice, err := ResolveActionSpace(t.Context(), key, ActionSpace{
			Goal: goal, Batch: batch, InputKeys: flowInputKeys(inputs),
		})
		if err != nil {
			t.Fatalf("could not ask: %v", err)
		}
		typed, _ := ActionSpaceText(choice, inputs)
		picks[fmt.Sprintf("%s:%s:%s:%q", choice.Operation,
			describeActionPick(choice.Element, choice.Reason), choice.InputKey, typed)]++
	}
	t.Logf("%-28s %s", "write on the new-category sheet", formatPicks(picks))
}

// TestWhatBreaksTheControlCell exists because two measurements of the same
// cell came back a factor of fifteen apart and somebody had to find out which
// one predicts what a user gets.
//
// Two explanations were on the table and only one of them survived contact.
//
// THE ONE THAT DIED: "the fixture is not the live screen". It is a reasonable
// suspicion — the .txt fixtures here are `mav ui tree` OUTPUT, whereas find
// reads the raw axe JSON, and the printer drops nodes the extraction keeps, as
// the header of ablation_fixture_test.go has said all along. So the live raw
// axe JSON of this screen was captured alongside the printed tree
// (categories-three-view.axe.json) and the two candidate lists compared:
//
//	printed-tree fixture   9 candidates
//	raw axe JSON           9 candidates
//
// The same nine, in the same order, with the same labels and roles. The ONLY
// difference is the category uuids, which are regenerated on every launch.
// The nodes the printer drops on this screen are all untitled groups, which
// were never candidates. So on this screen the fixture and the live read are
// the same question — and the numbers agree: 6/40 right off the fixture,
// 3/40 right off the live raw axe, same regime, measured in the same batches.
//
// THE ONE THAT HELD: IT IS THE QUESTION. find's question and goto's question
// are different questions, deliberately (see GotoStepQuestion, which explains
// why goto needs a wider one). Asked the identical phrase about the identical
// live screen, four batches of ten, alternating:
//
//	find's question,  live raw axe    40/40 right   `Moving Boxes`
//	goto's question,  live raw axe     3/40 right   37/40 `Test Category 1`
//	goto's question,  printed fixture  6/40 right   34/40 `Test Category 1`
//
// THAT IS A DEFECT IN goto's QUESTION, not in the screen, not in the fixture,
// and not in the action space — every arm of the control above inherits it,
// because every arm asks goto's question. Anyone quoting "production gets this
// cell right 9-10/10" is quoting the FIND path; goto's loop does not get that.
//
// The plausible mechanism, and it is a hypothesis (unverified): goto's question
// invites "the element that LEADS towards it", and on a screen of three
// categories the row literally named `Test Category 1` reads as the lead for
// "la primera categoría" in a way it does not when the question only asks which
// element IS the thing. It also drops find's "two or more could equally be it"
// clause, which is the wording that lets find decline. Neither is tested here.
//
// NOTHING IN THIS BRANCH TOUCHES GotoStepQuestion. It is measured, written
// down and left alone: the loop is somebody else's working tree right now, and
// a question this load-bearing is not something to change on the side.
//
//	MAV_ACTIONSPACE_CONTROL=1 go test ./internal/mav -run TestWhatBreaksTheControlCell -v
func TestWhatBreaksTheControlCell(t *testing.T) {
	if os.Getenv("MAV_ACTIONSPACE_CONTROL") == "" {
		t.Skip("set MAV_ACTIONSPACE_CONTROL=1 to spend model calls on the question comparison")
	}
	key, _, ok := ResolveJevKey()
	if !ok {
		t.Skip("no jev key")
	}

	const goal = "la primera categoría"
	live := loadRawAXEFixture(t, fixtureCategoriesThreeAXE)
	liveBatch, _ := FindBatch(FindCandidates(live))
	fixtureBatch, _ := FindBatch(FindCandidates(loadFixtureScreen(t, fixtureCategoriesThree)))

	if len(liveBatch) != len(fixtureBatch) {
		t.Errorf("the two reads of this screen no longer agree: live %d candidates, fixture %d.\nlive:\n%s\nfixture:\n%s",
			len(liveBatch), len(fixtureBatch), RenderFindCandidates(liveBatch), RenderFindCandidates(fixtureBatch))
	}

	gotoLive, gotoFixture, findLive := &armTally{}, &armTally{}, &armTally{}
	cli := CLI{}
	for run := 0; run < actionSpaceControlRuns; run++ {
		answer, err := askJevChoice(t.Context(), key, GotoStepQuestion(goal),
			RenderFindCandidates(liveBatch), FindOptions(liveBatch))
		if err != nil {
			t.Fatalf("goto question on live could not ask: %v", err)
		}
		gotoLive.record(InterpretGotoAnswer(answer.Label, liveBatch))

		answer, err = askJevChoice(t.Context(), key, GotoStepQuestion(goal),
			RenderFindCandidates(fixtureBatch), FindOptions(fixtureBatch))
		if err != nil {
			t.Fatalf("goto question on the fixture could not ask: %v", err)
		}
		gotoFixture.record(InterpretGotoAnswer(answer.Label, fixtureBatch))

		found := cli.resolveFind(t.Context(), live, goal)
		findLive.record(found.Element, found.Reason)
	}

	t.Logf("%-34s %-22s %s", "arm", "right/wrong/abstained", "what came back")
	t.Logf("%-34s %s", "goto question, live raw axe", gotoLive)
	t.Logf("%-34s %s", "goto question, printed fixture", gotoFixture)
	t.Logf("%-34s %s", "find question, live raw axe", findLive)
}

func describeActionPick(el *Element, reason string) string {
	if el == nil {
		return "none/" + reason
	}
	// By label and role, never by id: the category uuids in these fixtures are
	// regenerated on every launch of the app.
	return el.Label + "|" + el.Role
}

func formatPicks(picks map[string]int) string {
	keys := make([]string, 0, len(picks))
	for key := range picks {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if picks[keys[i]] != picks[keys[j]] {
			return picks[keys[i]] > picks[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%d/%d %s", picks[key], actionSpaceControlRuns, key))
	}
	return strings.Join(parts, "  ")
}
