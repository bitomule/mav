package mav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// GoalObservation belongs to one driver, target and foreground context. Refs
// are opaque to the engine; only the executor which issued them may use them.
type GoalObservation struct {
	Elements   []Element               `json:"elements"`
	Driver     string                  `json:"driver"`
	Target     string                  `json:"target"`
	Context    string                  `json:"context"`
	Snapshot   string                  `json:"snapshot,omitempty"`
	Refs       map[int]string          `json:"refs,omitempty"`
	RefActions map[int]map[string]bool `json:"ref_actions,omitempty"`
	Opaque     any                     `json:"-"`
}

// Freshness includes geometry, values, focus and foreground identity. The old
// progress fingerprint intentionally omits these and cannot protect input.
func (o GoalObservation) fingerprint() string {
	b, _ := json.Marshal(struct {
		Elements                []Element
		Driver, Target, Context string
		Actions                 map[int]map[string]bool
	}{o.Elements, o.Driver, o.Target, o.Context, o.RefActions})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func goalProgressFingerprint(o GoalObservation, mode string) string {
	if mode == "navigation" {
		if o.Refs != nil {
			visible := []Element{}
			for i, el := range o.Elements {
				if o.Refs[i] != "" {
					visible = append(visible, el)
				}
			}
			route, _ := json.Marshal(ExtractRoute(o.Elements))
			return string(route) + "|" + screenFingerprint(visible)
		}
		return screenFingerprint(o.Elements)
	}
	o.Elements = append([]Element{}, o.Elements...)
	for i := range o.Elements {
		o.Elements[i].Frame = ""
	}
	return o.fingerprint()
}

type GoalObserver interface {
	Observe(context.Context) (GoalObservation, error)
}
type GoalPlanner interface {
	Plan(context.Context, GoalRequest, GoalObservation, []GoalOperation, []GoalStep) (GoalDecision, error)
}
type GoalExecutor interface {
	Execute(context.Context, GoalObservation, GoalOperation) GoalReceipt
}
type GoalVerifier interface {
	Declared() bool
	Satisfied(GoalObservation) bool
}

// Concurrent observers opt in only when reads cannot invalidate opaque refs,
// change focus or interfere with input. Ordinary CLI tree readers do not opt in.
type GoalConcurrentObserver interface{ SafeDuringPlanning() bool }

type GoalDestinationNamer interface {
	NameDestination(context.Context, string, Route, []ObservedScreen) (SuccessCondition, GoalDecision)
}

type GoalRequest struct {
	Goal              string            `json:"goal"`
	Mode              string            `json:"mode"` // navigation or action
	MaxSteps          int               `json:"max_steps"`
	Timeout           time.Duration     `json:"-"`
	Inputs            map[string]string `json:"-"` // caller-declared values; never generated
	DismissPermission string            `json:"-"`
}

type GoalOperation struct {
	Kind      string   `json:"kind"`
	Target    *Element `json:"target,omitempty"`
	Input     string   `json:"input,omitempty"` // key into the caller's declaration
	Text      string   `json:"-"`
	Direction string   `json:"direction,omitempty"`
	Ref       string   `json:"ref,omitempty"`
}

type GoalDecision struct {
	Choice          int // 1-based menu index, 0 abstains
	ReportedSuccess bool
	ModelCalls      int
	ModelMS         int64
	CostUSD         *float64
}

type GoalReceipt struct {
	Delivery string          `json:"delivery"` // not_sent, completed, partial, unknown
	Stale    bool            `json:"stale,omitempty"`
	Error    string          `json:"error,omitempty"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
}

type GoalStep struct {
	Operation GoalOperation `json:"operation"`
	Receipt   GoalReceipt   `json:"receipt"`
	Before    Route         `json:"before"`
	After     Route         `json:"after"`
	Changed   bool          `json:"changed"`
}

type GoalMetrics struct {
	ModelCalls  int   `json:"model_calls"`
	Reads       int   `json:"reads"`
	Abstentions int   `json:"abstentions"`
	Discarded   int   `json:"discarded_decisions"`
	ModelMS     int64 `json:"model_ms"`
	LatencyMS   int64 `json:"latency_ms"`
	// nil means the provider did not supply billing; zero would invent a cost.
	CostUSD *float64 `json:"cost_usd"`
}

type GoalResult struct {
	Goal            string      `json:"goal"`
	Mode            string      `json:"mode"`
	Outcome         string      `json:"outcome"`
	Succeeded       string      `json:"succeeded"` // true, false, unverified
	ReportedSuccess bool        `json:"reported_success"`
	CriterionSource string      `json:"criterion_source"`
	Criterion       string      `json:"criterion,omitempty"`
	Initial         Route       `json:"route_initial"`
	Final           Route       `json:"route_final"`
	Steps           []GoalStep  `json:"steps"`
	Metrics         GoalMetrics `json:"metrics"`
	Next            string      `json:"next,omitempty"`
	ObservedScreens []string    `json:"observed_screens,omitempty"`
	observed        []ObservedScreen
}

// GoalEngine never invokes a provider or a CLI. All four phases are injected,
// allowing identical policy tests with native refs, selectors or coordinates.
type GoalEngine struct {
	Observer     GoalObserver
	Planner      GoalPlanner
	Executor     GoalExecutor
	Verifier     GoalVerifier
	Capabilities map[string]bool
	// Tests inject a deterministic wait. Production samples verification
	// 250ms apart, as goto did.
	Wait func(context.Context) error
}

func (e GoalEngine) Run(ctx context.Context, req GoalRequest) (result GoalResult) {
	started := time.Now()
	result = GoalResult{Goal: req.Goal, Mode: req.Mode, Succeeded: "false", CriterionSource: CriterionNone, Steps: []GoalStep{}}
	zeroCost := 0.0
	result.Metrics.CostUSD = &zeroCost
	recordCost := func(d GoalDecision) {
		if d.ModelCalls > 0 {
			if d.CostUSD == nil {
				result.Metrics.CostUSD = nil
			} else if result.Metrics.CostUSD != nil {
				*result.Metrics.CostUSD += *d.CostUSD
			}
		}
	}
	defer func() { result.Metrics.LatencyMS = time.Since(started).Milliseconds() }()
	if e.Observer == nil || e.Planner == nil || e.Executor == nil || e.Verifier == nil {
		result.Outcome, result.Next = "blocked", "missing goal phase"
		return
	}
	if req.Mode != "navigation" && req.Mode != "action" {
		result.Outcome, result.Next = "blocked", "mode must be navigation or action"
		return
	}
	if req.MaxSteps <= 0 || req.MaxSteps > gotoMaxSteps {
		req.MaxSteps = gotoMaxSteps
	}
	if req.Timeout <= 0 || req.Timeout > gotoDefaultTimeout {
		req.Timeout = gotoDefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	if e.Verifier.Declared() {
		result.CriterionSource = CriterionExplicit
		if s, ok := e.Verifier.(fmt.Stringer); ok {
			result.Criterion = s.String()
		}
	} else {
		result.Succeeded = "unverified"
	}
	read := func() (GoalObservation, error) { result.Metrics.Reads++; return e.Observer.Observe(ctx) }
	observe := func(o GoalObservation) {
		result.Final = ExtractRoute(o.Elements)
		result.observed = RecordObservedScreen(result.observed, result.Final, o.Elements)
	}
	current, err := read()
	if err != nil {
		result.Outcome, result.Next = "uncertain", err.Error()
		return
	}
	observe(current)
	result.Initial = result.Final
	identity := current
	// Verification requires two equal fresh observations. Exhausting settling
	// reads, cancellation and observation errors never fall back to old evidence.
	verify := func(o GoalObservation) (GoalObservation, bool, error) {
		if !e.Verifier.Declared() || !e.Verifier.Satisfied(o) {
			return o, false, nil
		}
		for i := 0; i < 4; i++ {
			if e.Wait != nil {
				if er := e.Wait(ctx); er != nil {
					return o, false, er
				}
			} else {
				select {
				case <-ctx.Done():
					return o, false, ctx.Err()
				case <-time.After(250 * time.Millisecond):
				}
			}
			next, er := read()
			if er != nil {
				return o, false, er
			}
			if !sameGoalContext(identity, next) {
				return next, false, fmt.Errorf("foreground context changed")
			}
			if next.fingerprint() == o.fingerprint() && e.Verifier.Satisfied(next) {
				return next, true, nil
			}
			o = next
			if !e.Verifier.Satisfied(o) {
				return o, false, nil
			}
		}
		return o, false, nil
	}
	defer func() {
		switch result.Outcome {
		case "no_route", "dead_end", "stuck", "looping", "exhausted", "unverified":
		default:
			return
		}
		if !e.Verifier.Declared() {
			if req.Mode != "navigation" || len(result.Steps) == 0 || ctx.Err() != nil {
				return
			}
			namer, ok := e.Planner.(GoalDestinationNamer)
			if !ok {
				return
			}
			condition, d := namer.NameDestination(ctx, req.Goal, result.Initial, result.observed)
			result.ObservedScreens = ObservedNames(ObservedScreens(result.observed))
			result.Metrics.ModelCalls += d.ModelCalls
			result.Metrics.ModelMS += d.ModelMS
			recordCost(d)
			if !condition.Declared() {
				return
			}
			e.Verifier = condition
			result.CriterionSource, result.Criterion = CriterionObserved, condition.String()
		}
		fresh, er := read()
		if er != nil {
			result.Outcome, result.Next = "uncertain", er.Error()
			return
		}
		if !sameGoalContext(identity, fresh) {
			result.Outcome, result.Next = "blocked", "foreground context changed"
			return
		}
		settled, ok, er := verify(fresh)
		observe(settled)
		if er != nil {
			result.Outcome, result.Next = "uncertain", er.Error()
			return
		}
		if ok {
			result.Outcome, result.Succeeded = "succeeded", "true"
			result.Next = ""
		}
	}()
	current, satisfied, err := verify(current)
	observe(current)
	if err != nil {
		result.Outcome, result.Next = "uncertain", err.Error()
		return
	}
	if satisfied {
		result.Succeeded, result.Outcome = "true", "already_satisfied"
		return
	}
	seen := map[string]int{goalProgressFingerprint(current, req.Mode): 1}
	unchanged, abstentions, dismissals := 0, 0, 0
	navigationSteps := 0
	for attempts := 0; attempts < req.MaxSteps+gotoMaxDismissals+req.MaxSteps; attempts++ {
		if ctx.Err() != nil {
			result.Outcome = "timeout"
			return
		}
		if navigationSteps >= req.MaxSteps {
			result.Outcome = "exhausted"
			break
		}
		menu := GoalOperations(req, current, e.Capabilities)
		modal := ExtractRoute(current.Elements).Modal
		var decision GoalDecision
		if modal != "" {
			btn := FindDismissButton(current.Elements, req.DismissPermission)
			if btn == nil || dismissals >= gotoMaxDismissals || !e.Capabilities["tap"] {
				result.Outcome, result.Next = "blocked", "modal requires the exact non-destructive --dismiss-permission button"
				return
			}
			menu = []GoalOperation{{Kind: "tap", Target: btn}}
			decision.Choice = 1
			dismissals++
		} else {
			history := result.Steps
			if len(history) > 4 {
				history = history[len(history)-4:]
			}
			// Optional overlap has a strict opt-in; the result is checked below
			// just like any preflight observation. Never race read with input.
			var prefetched chan struct {
				o   GoalObservation
				err error
			}
			if safe, ok := e.Observer.(GoalConcurrentObserver); ok && safe.SafeDuringPlanning() {
				prefetched = make(chan struct {
					o   GoalObservation
					err error
				}, 1)
				result.Metrics.Reads++
				go func() {
					o, er := e.Observer.Observe(ctx)
					prefetched <- struct {
						o   GoalObservation
						err error
					}{o, er}
				}()
			}
			decision, err = e.Planner.Plan(ctx, req, current, menu, history)
			result.Metrics.ModelCalls += decision.ModelCalls
			result.Metrics.ModelMS += decision.ModelMS
			recordCost(decision)
			if prefetched != nil {
				var p struct {
					o   GoalObservation
					err error
				}
				select {
				case p = <-prefetched:
				case <-ctx.Done():
					result.Outcome = "timeout"
					return
				}
				if p.err != nil {
					err = p.err
				} else if p.o.fingerprint() != current.fingerprint() {
					if !sameGoalContext(identity, p.o) {
						result.Outcome, result.Next = "blocked", "foreground context changed"
						return
					}
					current, satisfied, err = verify(p.o)
					observe(current)
					result.Metrics.Discarded++
					if err != nil {
						result.Outcome, result.Next = "uncertain", err.Error()
						return
					}
					if satisfied {
						result.Succeeded, result.Outcome = "true", "succeeded"
						return
					}
					continue
				}
			}
			if err != nil {
				result.Outcome, result.Next = "blocked", err.Error()
				return
			}
		}
		if ctx.Err() != nil {
			result.Outcome = "timeout"
			return
		}
		if decision.ReportedSuccess {
			result.ReportedSuccess = true
		}
		// A model's DONE is a report, not evidence of any effect.
		if decision.ReportedSuccess || decision.Choice == 0 {
			abstentions++
			result.Metrics.Abstentions++
			fresh, er := read()
			if er != nil {
				result.Outcome, result.Next = "uncertain", er.Error()
				return
			}
			if !sameGoalContext(identity, fresh) {
				result.Outcome = "blocked"
				result.Next = "foreground context changed"
				return
			}
			current, satisfied, er = verify(fresh)
			observe(current)
			if er != nil {
				result.Outcome, result.Next = "uncertain", er.Error()
				return
			}
			if satisfied {
				result.Succeeded, result.Outcome = "true", "succeeded"
				return
			}
			if decision.ReportedSuccess {
				result.Outcome = "unverified"
				return
			}
			if abstentions >= gotoMaxAbstentions {
				result.Outcome = "no_route"
				if len(result.Steps) > 0 {
					result.Outcome = "dead_end"
				}
				return
			}
			continue
		}
		if decision.Choice < 1 || decision.Choice > len(menu) {
			result.Outcome, result.Next = "blocked", "planner chose an operation outside the menu"
			return
		}
		op := menu[decision.Choice-1]
		if op.Target != nil && GotoRefusesDestructive(op.Target) {
			result.Outcome, result.Next = "blocked", "destructive operation refused"
			return
		}
		fresh, er := read()
		if er != nil {
			result.Outcome, result.Next = "uncertain", er.Error()
			return
		}
		if !sameGoalContext(identity, fresh) {
			result.Outcome, result.Next = "blocked", "foreground context changed"
			return
		}
		if fresh.fingerprint() != current.fingerprint() {
			current, satisfied, er = verify(fresh)
			observe(current)
			result.Metrics.Discarded++
			if er != nil {
				result.Outcome, result.Next = "uncertain", er.Error()
				return
			}
			if satisfied {
				result.Succeeded, result.Outcome = "true", "succeeded"
				return
			}
			continue // discard the decision, not a coordinate fallback
		}
		// Preflight may have invalidated the original opaque refs. Only refs
		// in the fresh observation are passed to the executor.
		op.Ref = ""
		for i, el := range fresh.Elements {
			if op.Target != nil && reflect.DeepEqual(el, *op.Target) {
				op.Ref = fresh.Refs[i]
				break
			}
		}
		if ctx.Err() != nil {
			result.Outcome = "timeout"
			return
		}
		receipt := e.Executor.Execute(ctx, fresh, op)
		if receipt.Stale && receipt.Delivery == "not_sent" {
			result.Metrics.Discarded++
			current, err = read()
			if err != nil {
				result.Outcome, result.Next = "uncertain", err.Error()
				return
			}
			observe(current)
			if !sameGoalContext(identity, current) {
				result.Outcome, result.Next = "blocked", "foreground context changed"
				return
			}
			current, satisfied, err = verify(current)
			observe(current)
			if err != nil {
				result.Outcome, result.Next = "uncertain", err.Error()
				return
			}
			if satisfied {
				result.Outcome, result.Succeeded = "succeeded", "true"
				return
			}
			continue
		}
		record := GoalStep{Operation: op, Receipt: receipt, Before: ExtractRoute(fresh.Elements)}
		result.Steps = append(result.Steps, record)
		if modal == "" {
			navigationSteps++
		}
		if receipt.Delivery != "completed" || receipt.Error != "" {
			result.Outcome = "uncertain"
			if receipt.Delivery == "not_sent" {
				result.Outcome = "blocked"
			}
			result.Next = receipt.Error
			return
		}
		abstentions = 0
		after, er := read()
		if er != nil {
			result.Outcome, result.Next = "uncertain", er.Error()
			return
		}
		if !sameGoalContext(identity, after) {
			result.Outcome, result.Next = "blocked", "foreground context changed after input"
			return
		}
		current, satisfied, er = verify(after)
		observe(current)
		last := &result.Steps[len(result.Steps)-1]
		last.After = result.Final
		last.Changed = goalProgressFingerprint(fresh, req.Mode) != goalProgressFingerprint(current, req.Mode)
		if er != nil {
			result.Outcome, result.Next = "uncertain", er.Error()
			return
		}
		if satisfied {
			result.Succeeded, result.Outcome = "true", "succeeded"
			return
		}
		if modal != "" {
			continue
		} // dismissing a dialog is separate from goal progress
		if !last.Changed {
			unchanged++
		} else {
			unchanged = 0
		}
		if unchanged >= gotoMaxUnchanged {
			result.Outcome = "stuck"
			return
		}
		if last.Changed {
			fingerprint := goalProgressFingerprint(current, req.Mode)
			seen[fingerprint]++
			if seen[fingerprint] > 1 {
				result.Outcome = "looping"
				return
			}
		}
	}
	if result.Outcome == "" {
		result.Outcome = "exhausted"
	}
	return
}

func sameGoalContext(a, b GoalObservation) bool {
	return a.Driver == b.Driver && a.Target == b.Target && a.Context == b.Context
}

// GoalOperations is a closed action/target menu. The model never supplies
// executable text, selectors, coordinates or driver flags.
func GoalOperations(req GoalRequest, o GoalObservation, caps map[string]bool) []GoalOperation {
	ops := []GoalOperation{}
	refFor := func(el Element) string {
		for i, node := range o.Elements {
			if reflect.DeepEqual(el, node) {
				return o.Refs[i]
			}
		}
		return ""
	}
	candidates, _ := FindBatch(FindCandidates(o.Elements))
	allowed := func(el Element, kind string) bool {
		if o.RefActions == nil {
			return true
		}
		for i, node := range o.Elements {
			if reflect.DeepEqual(el, node) {
				return o.RefActions[i][kind]
			}
		}
		return false
	}
	for _, el := range candidates {
		el := el
		if GotoRefusesDestructive(&el) {
			continue
		}
		if o.Refs != nil && refFor(el) == "" {
			continue
		}
		if caps["tap"] && allowed(el, "tap") {
			ops = append(ops, GoalOperation{Kind: "tap", Target: &el})
		}
		if req.Mode == "action" && caps["type"] && allowed(el, "type") && el.Focused == "true" && goalEditable(el) {
			keys := make([]string, 0, len(req.Inputs))
			for k := range req.Inputs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				ops = append(ops, GoalOperation{Kind: "type", Target: &el, Input: k, Text: req.Inputs[k]})
			}
		}
	}
	for _, el := range o.Elements {
		role := strings.ToLower(el.Role)
		if !allowed(el, "scroll") {
			continue
		}
		if o.Refs != nil && refFor(el) == "" {
			continue
		}
		if caps["scroll"] && isOfferableState(el) && (strings.Contains(role, "scroll") || strings.Contains(role, "table") || strings.Contains(role, "collection") || role == "list") {
			el := el
			for _, d := range []string{"up", "down"} {
				ops = append(ops, GoalOperation{Kind: "scroll", Target: &el, Direction: d})
			}
			break
		}
	}
	return ops
}

func goalEditable(el Element) bool {
	r := strings.ToLower(el.Role)
	return strings.Contains(r, "textfield") || strings.Contains(r, "text field") || strings.Contains(r, "textview") || strings.Contains(r, "text view") || strings.Contains(r, "search")
}
