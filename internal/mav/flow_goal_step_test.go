package mav

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// goal carries no selector, so its parameters live at the top level of the
// step. This pins the spelling of every one of them.
func TestParseFlowGoalStepKeepsParamsAtTopLevel(t *testing.T) {
	flow, err := ParseFlow([]byte(`
name: goal_step
steps:
  - goal:
      goal: the screen where a category is created
      succeededWhen: 'title:"New Category"'
      maxSteps: 6
      timeout: 30s
      dismissPermission: "Don't Allow"
`))
	if err != nil {
		t.Fatal(err)
	}
	step := flow.Steps[0]
	if step.Action != "goal" {
		t.Fatalf("action=%q", step.Action)
	}
	if !step.Where.IsZero() {
		t.Fatalf("goal must not resolve a selector, got where=%+v", step.Where)
	}
	for key, want := range map[string]string{
		"goal":              "the screen where a category is created",
		"succeededWhen":     `title:"New Category"`,
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
func TestLoadFlowRejectsGoalWithoutSucceededWhen(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: goal_without_criterion
steps:
  - goal:
      goal: the screen where a category is created
`)
	_, err := LoadFlow(path)
	if err == nil {
		t.Fatal("a goal step without succeededWhen must not load")
	}
	if !strings.Contains(err.Error(), "goal.succeededWhen") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFlowRejectsGoalWithoutGoal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: goal_without_goal
steps:
  - goal:
      succeededWhen: 'title:"New Category"'
`)
	_, err := LoadFlow(path)
	if err == nil || !strings.Contains(err.Error(), "goal.goal: a goal objective is mandatory") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFlowRejectsGoalWithSelector(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: goal_with_selector
steps:
  - goal:
      goal: the screen where a category is created
      succeededWhen: 'title:"New Category"'
      where:
        id: createCategoryButton
`)
	_, err := LoadFlow(path)
	if err == nil || !strings.Contains(err.Error(), "goal.where") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadFlowRejectsGoalBudgetsOutOfRange(t *testing.T) {
	for _, body := range []string{
		"maxSteps: 99",
		"maxSteps: 0",
		"timeout: 10m",
		"timeout: nonsense",
	} {
		root := t.TempDir()
		path := filepath.Join(root, "flow.yaml")
		writeTestFlow(t, path, `
name: goal_budget
steps:
  - goal:
      goal: the screen where a category is created
      succeededWhen: 'title:"New Category"'
      `+body+"\n")
		if _, err := LoadFlow(path); err == nil {
			t.Fatalf("%s must not load", body)
		}
	}
}

func TestLoadFlowAcceptsGoalMixedWithDeclaredSteps(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: mixed
inputs:
  nombre: Test Category
steps:
  - goal:
      goal: the list of categories
      succeededWhen: 'screen:categoriesView'
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

func TestLintFlowAcceptsGoalStep(t *testing.T) {
	flow, err := ParseFlow([]byte(`
name: goal_lint
steps:
  - goal:
      goal: the list of categories
      succeededWhen: 'screen:categoriesView'
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
func TestLoadFlowRejectsGoalWithAnUnknownArrivalPrefix(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "flow.yaml")
	writeTestFlow(t, path, `
name: goal_bad_prefix
steps:
  - goal:
      goal: the screen where a category is created
      succeededWhen: 'titel:"New Category"'
`)
	_, err := LoadFlow(path)
	if err == nil {
		t.Fatal("a misspelled arrival prefix must not load")
	}
	if !strings.Contains(err.Error(), "steps[0].goal") || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("err=%v", err)
	}
}

// The actual flow execution boundary must reject a model report without
// matching evidence, while accepting an initially verified condition.
type flowGoalVerdictDriver struct{ portableTapDriver }

func (*flowGoalVerdictDriver) Provides(drivers.Target) drivers.CapabilitySet {
	return drivers.NewSet(drivers.CapTreeAX, drivers.CapCoordTap)
}
func (*flowGoalVerdictDriver) Tree(context.Context, drivers.Target, drivers.TreeSpec) (drivers.TreeResult, error) {
	return drivers.TreeResult{JSON: []byte(`[{"role":"application","label":"Fixture App","identifier":"example.fixture"},{"role":"heading","label":"Home"}]`)}, nil
}

func TestGoalFlowStepRequiresVerifiedSuccess(t *testing.T) {
	fakeJev(t, "done", 0)
	for _, tc := range []struct {
		condition, succeeded, outcome string
		fails                         bool
	}{
		{`title:"Home"`, "true", "already_satisfied", false},
		{`title:"Details"`, "false", "unverified", true},
	} {
		c, _, _, _ := nativeCLI(t, "device")
		driver := &flowGoalVerdictDriver{}
		c.driverRegistry().Register(driver)
		step := FlowStep{Action: "goal", Params: map[string]string{"goal": "open Details", "succeededWhen": tc.condition}}
		fields, err := c.executeGoalFlowStep(t.Context(), GlobalOptions{PreferDriver: "portable"}, step)
		if (err != nil) != tc.fails || fields["succeeded"] != tc.succeeded || fields["outcome"] != tc.outcome || driver.taps != 0 {
			t.Fatalf("condition=%s fields=%v err=%v taps=%d", tc.condition, fields, err, driver.taps)
		}
		if tc.fails && err.Error() != "goal_unverified" {
			t.Fatalf("err=%v", err)
		}
	}
}
