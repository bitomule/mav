package mav

import (
	"errors"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

const typedNestedFallbackTree = `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}","children":[{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{0, 0}, {200, 200}}"},{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{10, 10}, {100, 100}}"},{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Group","AXFrame":"{{20, 20}, {10, 10}}"}]}]`

func TestTapFallbackKeepsElementType(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	ambiguousTapRunner(r, typedNestedFallbackTree)
	req := Automation{Command: "tap", Options: map[string]any{"Selector": drivers.ElementSelector{ID: "navbar_close"}, "ElementType": "Button"}}
	err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
	point, _, ok := c.coincidentTapAutomation(t.Context(), cfg, GlobalOptions{}, req, err)
	if ok && (point.Options["X"] != 60 || point.Options["Y"] != 60) {
		t.Fatalf("typed Button selector resolved to the smaller Group: %+v", point)
	}
}

func TestTapFallbackReadsRequestedDisplay(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	ambiguousTapRunner(r, nestedNavBarTree)
	screen := uint32(7)
	req := Automation{Command: "tap", Options: map[string]any{"Selector": drivers.ElementSelector{ID: "navbar_close"}}, Context: &drivers.InputContext{Selected: true, ScreenID: &screen, ExpectedPID: 101}}
	err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
	_, _, ok := c.coincidentTapAutomation(t.Context(), cfg, GlobalOptions{}, req, err)
	if !ok {
		return
	}
	for _, cmd := range r.commands {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "describe-ui") && (!strings.Contains(joined, "--screen-id 7") || !strings.Contains(joined, "--expected-pid 101")) {
			t.Fatalf("selected input fallback observed a different context: %s", joined)
		}
	}
}

func TestTapFallbackRawIncludesSuccessfulFallbackData(t *testing.T) {
	c, r, cfg, out := nativeCLI(t, "simulator")
	ambiguousTapRunner(r, nestedNavBarTree)
	req := Automation{Command: "tap", Options: map[string]any{"Selector": drivers.ElementSelector{ID: "navbar_close"}}}
	if err := c.automationOutput(t.Context(), cfg, GlobalOptions{Raw: true}, req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `{"ok":true}`) {
		t.Fatalf("successful nested fallback discarded requested raw data: %s", out.String())
	}
}

func TestTapFallbackNestedSiblingButtonsRemainAmbiguous(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	tree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}","children":[{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{0, 0}, {200, 200}}"},{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{10, 10}, {100, 100}}"}]}]`
	ambiguousTapRunner(r, tree)
	err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
	_, x, y, ok := c.coincidentTapPoint(t.Context(), cfg, "", Selector{ID: "navbar_close"}, err)
	if ok {
		t.Fatalf("distinct sibling buttons collapsed into one target by frame containment alone: x=%d y=%d", x, y)
	}
}

func TestTapFallbackInactiveNestedTargetRemainsUntapped(t *testing.T) {
	for _, state := range []string{`"enabled":false`, `"visible":false`} {
		t.Run(state, func(t *testing.T) {
			c, r, cfg, _ := nativeCLI(t, "simulator")
			tree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}","children":[{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Group","AXFrame":"{{0, 0}, {200, 200}}","children":[{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{20, 20}, {10, 10}}",` + state + `}]}]}]`
			ambiguousTapRunner(r, tree)
			err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
			_, x, y, ok := c.coincidentTapPoint(t.Context(), cfg, "", Selector{ID: "navbar_close"}, err)
			if ok {
				t.Fatalf("coordinate fallback would tap a known inactive nested button (%s): x=%d y=%d", state, x, y)
			}
		})
	}
}

func TestTapFallbackDoesNotUseCachedCoordinates(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	c = c.withTreeCache()
	c.trees.store("axe", false, describedUITree{Driver: "axe", Result: CommandResult{Stdout: nestedNavBarTree}})
	ambiguousTapRunner(r, separateCloseButtonsTree)
	err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
	if _, x, y, ok := c.coincidentTapPoint(t.Context(), cfg, "axe", Selector{ID: "navbar_close"}, err); ok {
		t.Fatalf("stale cached nesting turned fresh ambiguity into a tap at %d,%d", x, y)
	}
}

func TestTapFallbackDoesNotMergeNestedActionableControls(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	tree := strings.Replace(nestedNavBarTree, `"type":"Group"`, `"type":"Button"`, 1)
	ambiguousTapRunner(r, tree)
	err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
	if _, x, y, ok := c.coincidentTapPoint(t.Context(), cfg, "axe", Selector{ID: "navbar_close"}, err); ok {
		t.Fatalf("two actionable controls became one target at %d,%d", x, y)
	}
}

func TestTapFallbackInactiveIntermediateAncestorRemainsUntapped(t *testing.T) {
	for _, state := range []string{`"enabled":false`, `"visible":false`} {
		t.Run(state, func(t *testing.T) {
			c, r, cfg, _ := nativeCLI(t, "simulator")
			tree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}","children":[{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Group","AXFrame":"{{0, 0}, {200, 200}}","children":[{"AXUniqueId":"intermediate","type":"Group","AXFrame":"{{0, 0}, {100, 100}}",` + state + `,"children":[{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{20, 20}, {10, 10}}"}]}]}]}]`
			ambiguousTapRunner(r, tree)
			err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
			_, x, y, ok := c.coincidentTapPoint(t.Context(), cfg, "", Selector{ID: "navbar_close"}, err)
			if ok {
				t.Fatalf("target inherited an explicitly inactive ancestor but fallback still permits input (%s): x=%d y=%d", state, x, y)
			}
		})
	}
}

func TestTapFallbackCannotDropAnExplicitGroupType(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	ambiguousTapRunner(r, nestedNavBarTree)
	req := Automation{Command: "tap", Options: map[string]any{"Selector": drivers.ElementSelector{ID: "navbar_close"}, "ElementType": "Group"}}
	err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
	if point, _, ok := c.coincidentTapAutomation(t.Context(), cfg, GlobalOptions{}, req, err); ok {
		t.Fatalf("explicit Group type became an innermost Button coordinate tap: %+v", point)
	}
}

func TestTapFallbackIgnoresInactiveUnrelatedSiblings(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	tree := strings.Replace(nestedNavBarTree, `"children":[
{`, `"children":[{"AXUniqueId":"hidden_sibling","type":"Group","enabled":false,"AXFrame":"{{0, 0}, {1, 1}}"},{`, 1)
	if !strings.Contains(tree, "hidden_sibling") {
		t.Fatal("fixture did not insert hidden sibling")
	}
	ambiguousTapRunner(r, tree)
	err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
	if _, _, _, ok := c.coincidentTapPoint(t.Context(), cfg, "axe", Selector{ID: "navbar_close"}, err); !ok {
		t.Fatal("an inactive unrelated sibling must not disable the active target's ancestry")
	}
}

func TestTapFallbackRequiresFrameContainmentInsideRealAncestry(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	tree := strings.Replace(nestedNavBarTree, `{{24, 66}, {28, 36}}`, `{{240, 66}, {28, 36}}`, 1)
	ambiguousTapRunner(r, tree)
	err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
	if _, x, y, ok := c.coincidentTapPoint(t.Context(), cfg, "axe", Selector{ID: "navbar_close"}, err); ok {
		t.Fatalf("descendant outside its container became a tap at %d,%d", x, y)
	}
}

func TestTapFallbackNeverReplaysUncertainOrPartialDelivery(t *testing.T) {
	for _, receipt := range []string{
		`{"outcome":"failedNoReplay","enqueuedEventCount":2,"cleanup":"released"}`,
		`{"outcome":"failedBeforeInput","enqueuedEventCount":0,"cleanup":"uncertain"}`,
		`{"outcome":"unknownFuture","enqueuedEventCount":0,"cleanup":"released"}`,
	} {
		t.Run(receipt, func(t *testing.T) {
			c, r, _, out := nativeCLI(t, "simulator")
			ambiguousTapRunner(r, nestedNavBarTree)
			prior := r.respond
			r.respond = func(args []string) (CommandResult, bool) {
				joined := strings.Join(args, " ")
				if strings.Contains(joined, " tap ") && strings.Contains(joined, "--id navbar_close") {
					raw := `{"command":"tap","schema":"axe.output.command-result","schemaVersion":"1","didError":true,"error":{"code":"ambiguousElement","message":"ambiguous"},"data":{"result":` + receipt + `}}`
					return CommandResult{Stdout: raw, Code: 1, Err: errors.New("exit 1")}, true
				}
				return prior(args)
			}
			err := c.Run(t.Context(), []string{"ui", "tap", "--id", "navbar_close"})
			if err == nil || len(coordinateTaps(r)) != 0 || !strings.Contains(out.String(), "retry=unsafe") {
				t.Fatalf("unsafe input was retried: err=%v commands=%v output=%s", err, r.commands, out.String())
			}
		})
	}
}

func TestTapFallbackRequiresActualParentage(t *testing.T) {
	c, r, cfg, _ := nativeCLI(t, "simulator")
	tree := `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}","children":[{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Group","AXFrame":"{{20, 66}, {36, 36}}"},{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{24, 66}, {28, 36}}"}]}]`
	ambiguousTapRunner(r, tree)
	err := &drivers.DriverError{Code: "ambiguousElement", Delivery: drivers.DeliveryNotSent}
	if _, x, y, ok := c.coincidentTapPoint(t.Context(), cfg, "axe", Selector{ID: "navbar_close"}, err); ok {
		t.Fatalf("sibling container and button became a target at %d,%d", x, y)
	}
}
