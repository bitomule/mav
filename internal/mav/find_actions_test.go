package mav

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// `find` used to stop at tap, type, toggle and doubleTap. These tests pin
// where it reaches now, and — just as much — where it deliberately does not.

const findActionUDID = "FIND-ACTIONS"

// A form with something already in it, which is the case erase exists for:
// a name field carrying a value, and a row to long-press.
const findActionTree = `[
 {"AXUniqueId":"nameField","AXLabel":"Nombre","AXValue":"Cocina","type":"TextField","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"},
 {"AXUniqueId":"boxRow_1000","AXLabel":"1000","type":"Button","enabled":true,"AXFrame":"{{0, 200}, {200, 50}}"}
]`

const findActionNameHit = `{"AXUniqueId":"nameField","AXLabel":"Nombre","AXValue":"Cocina","type":"TextField","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"}`

const findActionRowHit = `{"AXUniqueId":"boxRow_1000","AXLabel":"1000","type":"Button","enabled":true,"AXFrame":"{{0, 200}, {200, 50}}"}`

// A find is tapped by coordinate, and the driver that dispatches one varies
// with what is installed; the test only needs to know a tap happened.
func isTapCommand(command string) bool {
	return strings.Contains(command, " tap") && !strings.Contains(command, "describe-ui")
}

func findActionCLI(t *testing.T) (CLI, *sequenceRecordingRunner, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.SimulatorUDID = findActionUDID
	cfg.Tools = map[string]bool{"axe": true, "baguette": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		seq:   map[string][]string{},
		calls: map[string]int{},
		out: map[string]string{
			"axe describe-ui --udid " + findActionUDID:                      findActionTree,
			"axe describe-ui --point 100,125 --udid " + findActionUDID:      findActionNameHit,
			"axe describe-ui --point 100,225 --udid " + findActionUDID:      findActionRowHit,
			"baguette describe-ui --udid " + findActionUDID:                 findActionTree,
			"baguette describe-ui --point 100,125 --udid " + findActionUDID: findActionNameHit,
		},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	return cli.withTreeCache(), runner, &out
}

// The one David asked for by name: find a field, then empty it. Before this
// change `--find` reached MatchElements and the step died on
// selector_find_unsupported, so a flow that fills in an already-filled form
// could not be written at all.
func TestEraseAcceptsFindAndFocusesTheFieldFirst(t *testing.T) {
	fakeJev(t, "1", 10)
	cli, runner, out := findActionCLI(t)

	allowFail(t, cli.Run(context.Background(), []string{"ui", "erase", "--find", "el campo del nombre"}))
	if !strings.Contains(out.String(), "ok cmd=ui.erase") {
		t.Fatalf("output=%q commands=%v", out.String(), runner.commands)
	}

	// Emptying has to happen in the field that was named, and no driver can
	// be told that: baguette deletes from whatever holds focus. So the tap
	// that moves focus must come BEFORE the first Backspace, or the step
	// reports ok having emptied some other field.
	tapAt, eraseAt := -1, -1
	for i, command := range runner.commands {
		if tapAt < 0 && isTapCommand(command) {
			tapAt = i
		}
		if eraseAt < 0 && strings.Contains(command, "--code Backspace") {
			eraseAt = i
		}
	}
	if tapAt < 0 || eraseAt < 0 {
		t.Fatalf("expected a tap and a Backspace, got %v", runner.commands)
	}
	if tapAt > eraseAt {
		t.Fatalf("focus must be taken before anything is deleted: tap at %d, erase at %d", tapAt, eraseAt)
	}
}

// The v0.26.0 defect in erase's own shape. `mav ui type` typed its selector
// into the field because `--find` was missing from the where-flags; erase
// reads `--text` as HOW MUCH to delete, so the same leak here would send the
// wrong number of deletions rather than the wrong characters. Either way the
// words must never reach the driver.
func TestEraseNeverForwardsTheFindWordsToTheDriver(t *testing.T) {
	fakeJev(t, "1", 10)
	cli, runner, _ := findActionCLI(t)

	if err := cli.Run(context.Background(), []string{"ui", "erase", "--find", "el campo del nombre"}); err != nil {
		t.Fatal(err)
	}
	for _, command := range runner.commands {
		if strings.Contains(command, "el campo del nombre") {
			t.Fatalf("the selector reached the driver: %q", command)
		}
	}
}

func TestLongPressAcceptsFind(t *testing.T) {
	fakeJev(t, "2", 10)
	cli, runner, out := findActionCLI(t)

	if err := cli.Run(context.Background(), []string{"ui", "longPress", "--find", "la fila de la caja 1000"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=ui.longPress") {
		t.Fatalf("output=%q", out.String())
	}
	// The row centres on 100,225, so a long press that resolved the words
	// landed there and one that fell back to a default did not.
	if !strings.Contains(out.String(), "x=100") || !strings.Contains(out.String(), "y=225") {
		t.Fatalf("long press did not land on the resolved row: %q", out.String())
	}
	if len(runner.commands) == 0 {
		t.Fatal("nothing ran")
	}
}

func TestLongPressStillTakesRawCoordinates(t *testing.T) {
	cli, _, out := findActionCLI(t)
	if err := cli.Run(context.Background(), []string{"ui", "longPress", "--x", "10", "--y", "20"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "x=10") || !strings.Contains(out.String(), "y=20") {
		t.Fatalf("output=%q", out.String())
	}
}

// scrollUntil asks per swipe, so the question is not whether it can ask but
// how often. The cap is a fixed number of consultations, independent of
// maxSwipes, and it is reported rather than applied in silence.
func TestScrollUntilFindIsCappedAndSaysSo(t *testing.T) {
	t.Setenv("CI", "1") // find_unavailable would stop the loop; see below.
	cli, _, _ := findActionCLI(t)
	// With CI set nothing can be asked at all, which is the OTHER half of the
	// contract: a scroll does not burn every swipe against a key that is not
	// there.
	_, err := cli.scrollUntilFlowConditionWithSelector(t.Context(),
		map[string]string{"maxSwipes": "40"}, Selector{Find: "the box"}, "auto")
	if err == nil || err.Error() != "find_unavailable" {
		t.Fatalf("a find that cannot be asked must stop the scroll at once, got %v", err)
	}
}

func TestScrollUntilFindStopsAtTheConsultLimit(t *testing.T) {
	fakeJev(t, "none", 10) // never here: the loop keeps swiping.
	cli, _, _ := findActionCLI(t)

	fields, err := cli.scrollUntilFlowConditionWithSelector(t.Context(),
		map[string]string{"maxSwipes": "40"}, Selector{Find: "una caja que no existe"}, "auto")
	if err == nil || err.Error() != "scroll_until_find_limit" {
		t.Fatalf("expected the consultation cap to bite, got %v", err)
	}
	if fields["find_consultations"] != itoa(findScrollConsultLimit) {
		t.Fatalf("the cap has to be reported, got %v", fields)
	}
}

// find is OUT of the checks, and this is the shape of the refusal: loud, with
// the code on the wire, instead of the silent false it used to be. A
// condition dropped Find on the way in, so the selector read as empty and
// `assert` reported "the screen does not show it" — a wrong answer from the
// one step whose job is to be believed.
func TestConditionsRefuseFindLoudly(t *testing.T) {
	cli, _, _ := findActionCLI(t)
	for _, condition := range []FlowCondition{
		{Find: "the saved banner"},
		{All: []FlowCondition{{Find: "the saved banner"}}},
		{Not: &FlowCondition{Find: "the saved banner"}},
	} {
		_, err := cli.evaluateSingleConditionWithPrefer(t.Context(), condition, "auto")
		if err == nil || err.Error() != "selector_find_unsupported" {
			t.Fatalf("a check must refuse find out loud, got %v for %+v", err, condition)
		}
	}
}

func TestAssertCountRefusesFind(t *testing.T) {
	cli, _, _ := findActionCLI(t)
	step := FlowStep{Action: "assertCount", Params: map[string]string{"count": "2"}, Where: Selector{Find: "a box"}}
	_, err := cli.assertFlowCount(t.Context(), step, "auto")
	if err == nil || !strings.Contains(err.Error(), "selector_find_unsupported") {
		t.Fatalf("assertCount has nothing to count from a find, got %v", err)
	}
}

func TestLintRefusesFindInAConditionStep(t *testing.T) {
	flow, err := ParseFlow([]byte("name: f\nsteps:\n  - assert: { where: { find: \"the saved banner\" } }\n"))
	if err != nil {
		t.Fatal(err)
	}
	issues := lintFlow(flow, DefaultConfig(t.TempDir()))
	found := false
	for _, issue := range issues {
		if issue.Code == "find_unsupported_in_condition" && issue.Severity == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("lint must catch it before a run spends a simulator, got %+v", issues)
	}
}

// The invariant that broke once: `--find` was added to the CLI and left out
// of the list that says which flags describe the WHERE, so `mav ui type`
// typed it. Every action that now takes find is re-checked against both
// lists here, one by one, which is how the omission was missed the first
// time.
func TestEveryFindCapableActionRoundTripsItsSelector(t *testing.T) {
	selector := Selector{Find: "el campo del nombre", Role: "textField"}
	args := selectorCLIArgs(selector)
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") && !isSelectorCLIFlag(arg) {
			t.Fatalf("%s describes WHERE but is not in the where-flag list", arg)
		}
	}
	round, err := selectorFromCLI(args)
	if err != nil {
		t.Fatal(err)
	}
	if round.Find != selector.Find || round.Role != selector.Role {
		t.Fatalf("erase/longPress/scrollUntil all reach their command through these args: %+v", round)
	}
}
