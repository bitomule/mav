package mav

import (
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
