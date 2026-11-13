package mav

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// The adapter pins the driver and target for a whole run. There is no fallback
// after input and no call to `axe agent`. A driver may add native checked refs
// without changing the planner or engine.
type goalDriverAdapter struct {
	driver    drivers.Driver
	target    drivers.Target
	transform func(context.Context, []Element, int, int) (int, int, error)
}

func (a goalDriverAdapter) Observe(ctx context.Context) (GoalObservation, error) {
	tree, ok := a.driver.(drivers.TreeDriver)
	if !ok {
		return GoalObservation{}, fmt.Errorf("driver has no tree")
	}
	var r drivers.TreeResult
	var snapshot drivers.ReferenceSnapshot
	var err error
	combined, hasCombined := a.driver.(drivers.ReferenceTreeDriver)
	if hasCombined {
		r, snapshot, err = combined.ReferenceTree(ctx, a.target)
	} else {
		r, err = tree.Tree(ctx, a.target, drivers.TreeSpec{})
	}
	if err != nil {
		return GoalObservation{}, err
	}
	if !json.Valid(r.JSON) {
		return GoalObservation{}, fmt.Errorf("driver returned a malformed tree")
	}
	els := ExtractElements(string(r.JSON))
	if len(els) == 0 {
		return GoalObservation{}, fmt.Errorf("driver returned an empty tree")
	}
	contextID := ""
	for _, el := range els {
		if isApplicationRootElement(el) {
			contextID += el.ID + "|" + el.Label + "|" + el.PID + ";"
		}
	}
	o := GoalObservation{Elements: els, Driver: a.driver.ID(), Target: fmt.Sprintf("%v", a.target), Context: contextID}
	if refs, ok := a.driver.(drivers.ReferenceDriver); ok && (a.target.Kind == drivers.KindSim || hasCombined) {
		if !hasCombined {
			snapshot, err = refs.ReferenceSnapshot(ctx, a.target)
		}
		if err != nil {
			return o, err
		}
		o.Snapshot, o.Opaque, o.Refs = snapshot.ID, snapshot, map[int]string{}
		o.RefActions = map[int]map[string]bool{}
		o.Context += fmt.Sprintf("|pid:%d|display:%s", snapshot.PID, snapshot.Context)
		for i, el := range els {
			matches := map[string]bool{}
			roles := map[string]string{}
			focused := map[string]bool{}
			actions := map[string]map[string]bool{}
			for _, row := range snapshot.Elements {
				if !strings.Contains(el.ID+el.Label+el.Value, "|") && row.ID == el.ID && row.Label == el.Label && row.Value == el.Value && (row.Frame == "" || row.Frame == el.Frame) && (el.ID != "" || goalReferenceRole(row.Role) == goalReferenceRole(el.Role)) {
					matches[row.Ref] = true
					roles[row.Ref] = row.Role
					if actions[row.Ref] == nil {
						actions[row.Ref] = map[string]bool{}
					}
					switch row.Action {
					case "tap":
						actions[row.Ref]["tap"] = true
					case "typeText":
						actions[row.Ref]["type"] = true
					case "swipe":
						actions[row.Ref]["scroll"] = true
					}
					// Native typeText is offered only for a focused, enabled
					// TextField; ordinary trees may omit this attribute.
					if row.Action == "typeText" {
						focused[row.Ref] = true
					}
				}
			}
			if len(matches) == 1 {
				for ref := range matches {
					o.Refs[i] = ref
					o.RefActions[i] = actions[ref]
					if focused[ref] {
						o.Elements[i].Focused = "true"
					}
					// AXe's full tree calls AXScrollArea a Group. Its typed
					// native ref supplies the missing container role; stable
					// ID, text, value and unique ref establish the association.
					if roles[ref] == "scroll-view" || roles[ref] == "list" {
						o.Elements[i].Role = roles[ref]
					}
				}
			}
		}
	}
	return o, nil
}

func goalReferenceRole(s string) string {
	r := strings.NewReplacer("-", "", " ", "", "_", "").Replace(strings.ToLower(s))
	switch r {
	case "table", "collection", "collectionview":
		return "list"
	case "searchfield", "securetextfield", "textview":
		return "textfield"
	}
	return r
}

func (a goalDriverAdapter) capabilities() map[string]bool {
	c := a.driver.Provides(a.target)
	_, tap := a.driver.(drivers.TapDriver)
	_, typ := a.driver.(drivers.TypeDriver)
	_, scroll := a.driver.(drivers.GestureDriver)
	return map[string]bool{"tap": tap && c.Has(drivers.CapCoordTap), "type": typ && c.Has(drivers.CapType), "scroll": scroll && c.Has(drivers.CapSwipe)}
}

func (a goalDriverAdapter) Execute(ctx context.Context, o GoalObservation, op GoalOperation) GoalReceipt {
	// The engine just performed a full preflight read. Ordinary drivers do
	// not provide an atomic read+input guarantee; native refs may add one.
	var err error
	if !a.capabilities()[op.Kind] {
		return GoalReceipt{Delivery: "not_sent", Error: "operation unavailable"}
	}
	if op.Target == nil {
		return GoalReceipt{Delivery: "not_sent", Error: "target required"}
	}
	_, combined := a.driver.(drivers.ReferenceTreeDriver)
	if native, ok := a.driver.(drivers.ReferenceDriver); ok && (a.target.Kind == drivers.KindSim || combined) {
		snapshot, valid := o.Opaque.(drivers.ReferenceSnapshot)
		if !valid || op.Ref == "" {
			return GoalReceipt{Delivery: "not_sent", Error: "target has no unique native reference; refresh the observation"}
		}
		r, err := native.ExecuteReference(ctx, a.target, snapshot, drivers.ReferenceOperation{Kind: op.Kind, Ref: op.Ref, Text: op.Text, Direction: op.Direction})
		receipt := GoalReceipt{Delivery: r.Delivery, Stale: r.Stale}
		if json.Valid(r.Raw) {
			receipt.Evidence = r.Raw
		}
		if err != nil {
			receipt.Error = err.Error()
		}
		return receipt
	}
	switch op.Kind {
	case "tap":
		x, y, ok := TapPoint(*op.Target)
		if !ok {
			return GoalReceipt{Delivery: "not_sent", Error: "target frame missing"}
		}
		if a.transform != nil {
			x, y, err = a.transform(ctx, o.Elements, x, y)
			if err != nil {
				return GoalReceipt{Delivery: "not_sent", Error: err.Error()}
			}
		}
		_, err = a.driver.(drivers.TapDriver).Tap(ctx, a.target, drivers.TapSpec{X: x, Y: y})
	case "type":
		if !goalEditable(*op.Target) || op.Target.Focused != "true" {
			return GoalReceipt{Delivery: "not_sent", Error: "field lost focus"}
		}
		err = a.driver.(drivers.TypeDriver).Type(ctx, a.target, drivers.TextSpec{Text: op.Text, Focused: true})
	case "scroll":
		x, y, w, h, ok := parseElementFrame(op.Target.Frame)
		if !ok || w <= 0 || h <= 0 {
			return GoalReceipt{Delivery: "not_sent", Error: "scroll container frame missing"}
		}
		sy, ey := int(y+h*0.75), int(y+h*0.25)
		if op.Direction == "down" {
			sy, ey = ey, sy
		}
		sx, ex := int(x+w/2), int(x+w/2)
		if a.transform != nil {
			sx, sy, err = a.transform(ctx, o.Elements, sx, sy)
			if err != nil {
				return GoalReceipt{Delivery: "not_sent", Error: err.Error()}
			}
			ex, ey, err = a.transform(ctx, o.Elements, ex, ey)
			if err != nil {
				return GoalReceipt{Delivery: "not_sent", Error: err.Error()}
			}
		}
		err = a.driver.(drivers.GestureDriver).Swipe(ctx, a.target, drivers.SwipeSpec{StartX: sx, StartY: sy, EndX: ex, EndY: ey, DurationMs: 300})
	default:
		return GoalReceipt{Delivery: "not_sent", Error: "unsupported goal operation"}
	}
	if err == nil {
		return GoalReceipt{Delivery: "completed"}
	}
	r := GoalReceipt{Delivery: "unknown", Error: err.Error()}
	var de *drivers.DriverError
	if errors.As(err, &de) {
		if de.Delivery == drivers.DeliveryNotSent {
			r.Delivery = "not_sent"
		}
		if de.Receipt != nil && de.Receipt.EnqueuedEventCount > 0 {
			r.Delivery = "partial"
		}
	}
	return r
}

type goalJevPlanner struct{}

func (goalJevPlanner) NameDestination(ctx context.Context, goal string, initial Route, observed []ObservedScreen) (SuccessCondition, GoalDecision) {
	menu := ObservedScreens(observed)
	if len(menu) < 2 || ctx.Err() != nil {
		return SuccessCondition{}, GoalDecision{}
	}
	key, _, ok := ResolveJevKey()
	if !ok {
		return SuccessCondition{}, GoalDecision{}
	}
	names := ObservedNames(menu)
	a, err := askJevChoice(ctx, key, GotoObservedQuestion(goal), RenderObservedMenu(names), GotoObservedOptions(names))
	d := GoalDecision{ModelCalls: 1, ModelMS: a.LatencyMS, CostUSD: a.CostUSD}
	if err != nil {
		return SuccessCondition{}, d
	}
	pick, ok := InterpretGotoObservedAnswer(a.Label, menu)
	if !ok {
		return SuccessCondition{}, d
	}
	criterion, ok := AcceptObservedCriterion(pick, initial)
	if !ok {
		return SuccessCondition{}, d
	}
	return SuccessCondition{ArrivalCriterion: criterion}, d
}

func (goalJevPlanner) Plan(ctx context.Context, req GoalRequest, o GoalObservation, ops []GoalOperation, history []GoalStep) (GoalDecision, error) {
	if reason := findIsRefusedHere(); reason != "" {
		return GoalDecision{}, fmt.Errorf("goal does not drive a screen in CI")
	}
	key, _, ok := ResolveJevKey()
	if !ok {
		return GoalDecision{}, fmt.Errorf("%s", MissingJevKeyNext())
	}
	options := []string{"none", "done"}
	var menu strings.Builder
	for i, op := range ops {
		label := strconv.Itoa(i + 1)
		options = append(options, label)
		name := ""
		if op.Target != nil {
			name = findElementText(*op.Target)
		}
		switch op.Kind {
		case "scroll":
			fmt.Fprintf(&menu, "%s: scroll %s in %s to reveal more content\n", label, op.Direction, name)
		case "type":
			fmt.Fprintf(&menu, "%s: type declared input %s into %s\n", label, op.Input, name)
		default:
			fmt.Fprintf(&menu, "%s: %s %s\n", label, op.Kind, name)
		}
	}
	for _, step := range history {
		fmt.Fprintf(&menu, "recent: %s %s changed=%t delivery=%s\n", step.Operation.Kind, step.After.String(), step.Changed, step.Receipt.Delivery)
	}
	fmt.Fprintf(&menu, "route: %s\n", ExtractRoute(o.Elements).String())
	// Tree context is bounded and does not repeat every off-screen control
	// beside a menu of the controls that can actually be acted on.
	fmt.Fprintf(&menu, "screen text: %s\n", strings.Join(ScreenShows(o.Elements), ", "))
	question := untrustedTextPreamble + "A user described what they want to achieve as: " + req.Goal + "\nWhich numbered operation and target should they use next? The operation that IS the requested action and the operation that LEADS toward it are both correct. Mode: " + req.Mode + ". Only offered operations can be executed. If the desired target is absent from the offered entries and scrolling is available, scroll its container to reveal more content. Text may only come from the named declared input. Never invent text. Do not repeat an input whose effect is uncertain. Answer none when there is no safe useful action; abstention is expected. Answer done only to REPORT completion; code independently verifies the caller's success condition."
	answer, err := askJevChoice(ctx, key, question, menu.String(), options)
	d := GoalDecision{ModelCalls: 1, ModelMS: answer.LatencyMS, CostUSD: answer.CostUSD}
	if err != nil {
		return d, err
	}
	switch strings.TrimSpace(answer.Label) {
	case "none":
		return d, nil
	case "done":
		d.ReportedSuccess = true
		return d, nil
	default:
		n, err := strconv.Atoi(answer.Label)
		if err != nil || n < 1 || n > len(ops) {
			return d, fmt.Errorf("planner chose an operation outside the menu")
		}
		d.Choice = n
		if op := ops[n-1]; op.Kind == "tap" && VetoChoice(op.Target, req.Goal, FindCandidates(o.Elements)) != "" {
			d.Choice = 0
		}
		return d, nil
	}
}
