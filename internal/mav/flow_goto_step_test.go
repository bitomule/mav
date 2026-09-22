package mav

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goto carries no selector, so its parameters live at the top level of the
// step. This pins the spelling of every one of them.
func TestParseFlowGotoStepKeepsParamsAtTopLevel(t *testing.T) {
	flow, err := ParseFlow([]byte(`
name: goto_step
steps:
  - goto:
      goal: the screen where a category is created
      arrivedWhen: 'title:"New Category"'
      maxSteps: 6
      timeout: 30s
      dismissPermission: "Don't Allow"
`))
	if err != nil {
		t.Fatal(err)
	}
	step := flow.Steps[0]
	if step.Action != "goto" {
		t.Fatalf("action=%q", step.Action)
	}
	if !step.Where.IsZero() {
		t.Fatalf("goto must not resolve a selector, got where=%+v", step.Where)
	}
	for key, want := range map[string]string{
		"goal":              "the screen where a category is created",
		"arrivedWhen":       `title:"New Category"`,
		"maxSteps":          "6",
		"timeout":           "30s",
		"dismissPermission": "Don't Allow",
	} {
		if got := step.Params[key]; got != want {
			t.Fatalf("params[%q]=%q, want %q", key, got, want)
		}
	}
}

// The first safety belt: inside a flow the arrival criterion is mandatory, and
// its absence is a load/lint failure rather than something discovered three
// screens into a run.
func TestLoadFlowRejectsGotoWithoutArrivedWhen(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: goto_without_criterion
steps:
  - goto:
      goal: the screen where a category is created
`)
	_, err := LoadFlow(path)
	if err == nil {
		t.Fatal("a goto step without arrivedWhen must not load")
	}
	if !strings.Contains(err.Error(), "goto.arrivedWhen") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFlowRejectsGotoWithoutGoal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: goto_without_goal
steps:
  - goto:
      arrivedWhen: 'title:"New Category"'
`)
	_, err := LoadFlow(path)
	if err == nil || !strings.Contains(err.Error(), "goto.goal") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFlowRejectsGotoWithSelector(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: goto_with_selector
steps:
  - goto:
      goal: the screen where a category is created
      arrivedWhen: 'title:"New Category"'
      where:
        id: createCategoryButton
`)
	_, err := LoadFlow(path)
	if err == nil || !strings.Contains(err.Error(), "goto.where") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFlowRejectsGotoBudgetsOutOfRange(t *testing.T) {
	for _, body := range []string{
		"maxSteps: 99",
		"maxSteps: 0",
		"timeout: 10m",
		"timeout: nonsense",
	} {
		root := t.TempDir()
		path := filepath.Join(root, "flow.yaml")
		writeTestFlow(t, path, `
name: goto_budget
steps:
  - goto:
      goal: the screen where a category is created
      arrivedWhen: 'title:"New Category"'
      `+body+"\n")
		if _, err := LoadFlow(path); err == nil {
			t.Fatalf("%s must not load", body)
		}
	}
}

func TestLoadFlowAcceptsGotoMixedWithDeclaredSteps(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: mixed
inputs:
  nombre: Test Category
steps:
  - goto:
      goal: the list of categories
      arrivedWhen: 'screen:categoriesView'
  - tap:
      where:
        find: the button that creates a new category
  - type:
      where:
        find: the field for the category name
      text:
        from: nombre
`)
	flow, err := LoadFlow(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Steps) != 3 {
		t.Fatalf("steps=%d", len(flow.Steps))
	}
}

// The second safety belt: `unverified` fails the step. Outside a flow it is an
// honest answer; inside one it is an unchecked premise the following steps
// would act on.
func TestGotoFlowStepVerdictFailsUnverified(t *testing.T) {
	if err := gotoFlowStepVerdict("true"); err != nil {
		t.Fatalf("arrived=true must pass, got %v", err)
	}
	err := gotoFlowStepVerdict("unverified")
	if err == nil {
		t.Fatal("arrived=unverified must fail the step")
	}
	if err.Error() != "goto_arrival_unverified" {
		t.Fatalf("code=%q", err.Error())
	}
	if err := gotoFlowStepVerdict("false"); err == nil || err.Error() != "goto_did_not_arrive" {
		t.Fatalf("err=%v", err)
	}
}

func TestLintFlowAcceptsGotoStep(t *testing.T) {
	flow, err := ParseFlow([]byte(`
name: goto_lint
steps:
  - goto:
      goal: the list of categories
      arrivedWhen: 'screen:categoriesView'
`))
	if err != nil {
		t.Fatal(err)
	}
	if errs, _ := countLintIssues(lintFlow(flow, Config{})); errs != 0 {
		t.Fatalf("errors=%d", errs)
	}
}

// Merging #118 made an unknown arrival prefix a syntax error rather than a
// silent title. Inside a flow that error has to reach lint, with the step index
// on it, instead of being swallowed into "names nothing to check against".
func TestLoadFlowRejectsGotoWithAnUnknownArrivalPrefix(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: goto_bad_prefix
steps:
  - goto:
      goal: the screen where a category is created
      arrivedWhen: 'titel:"New Category"'
`)
	_, err := LoadFlow(path)
	if err == nil {
		t.Fatal("a misspelled arrival prefix must not load")
	}
	if !strings.Contains(err.Error(), "goto.arrivedWhen") || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("err=%v", err)
	}
}

// Why the second belt is not made redundant by the action-goal gate.
//
// The gate that stops `goto` claiming it did a THING is asked only when the
// caller named no criterion -- a caller who wrote one has already said what
// arriving means. Inside a flow a criterion is mandatory, so the goal is never
// classified there and that gate never fires. Whatever guards a flow against a
// false arrival, it is not that gate.
//
// Measured, 5 runs out of 5 on Boxy: a flow whose goto asks for an action and
// demands the effect dies on `goto_did_not_arrive`, never on the action gate.
func TestTheActionGoalGateIsNeverReachedWithACriterion(t *testing.T) {
	src, err := os.ReadFile("goto_cmd.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)

	guard := strings.Index(body, "if !criterion.IsZero() {")
	if guard < 0 {
		t.Fatal("the criterion branch is gone; this guard needs rewriting, not deleting")
	}
	call := strings.Index(body, "result.GoalKind = c.classifyGoal")
	if call < 0 {
		t.Fatal("the goal is no longer classified; this guard needs rewriting")
	}
	if call < guard {
		t.Fatal("the goal is classified before the criterion is looked at; a flow's goal would be classified too")
	}
	between := body[guard:call]
	if !strings.Contains(between, "} else {") {
		t.Error("classifyGoal is no longer in the else of `!criterion.IsZero()`; with a criterion written -- which a flow always has -- the goal must not be classified at all")
	}
	if strings.Count(between, "\n\tif ") != 0 {
		t.Error("another branch has appeared between the criterion check and classifyGoal; this guard can no longer tell which one reaches it")
	}
}

// The other half of the same argument, and the reason the live measurement of
// the second belt had to be an ambiguous criterion rather than an unreachable
// one: with a criterion written, the ONLY road left to `unverified` is the
// guard that refuses a criterion already holding where the run starts. Every
// other one is behind "no criterion was given".
//
// If a new unverified appears outside both, the belt stops being fully measured
// and nobody would notice.
func TestEveryUnverifiedIsEitherUncriterionedOrTheAmbiguousGuard(t *testing.T) {
	src, err := os.ReadFile("goto_cmd.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	const assign = `result.Arrived = "unverified"`

	found := 0
	for at := 0; ; {
		i := strings.Index(body[at:], assign)
		if i < 0 {
			break
		}
		i += at
		at = i + len(assign)
		found++

		lookback := i - 600
		if lookback < 0 {
			lookback = 0
		}
		context := body[lookback:i]
		// CriterionObserved counts as "no criterion was given": it is the
		// source set when goto had to name the destination itself, which it
		// only ever does after finding the criterion empty.
		guarded := strings.Contains(context, "criterion.IsZero()") ||
			strings.Contains(context, "GotoAmbiguousCriterion") ||
			strings.Contains(context, "CriterionSource == CriterionObserved")
		if !guarded {
			t.Errorf("an unverified at offset %d is reachable with a criterion written and is not the ambiguous-criterion guard; a flow can now reach an unverified the belt was never measured against", i)
		}
	}
	if found == 0 {
		t.Fatal("no unverified assignments found; this guard needs rewriting")
	}
}
