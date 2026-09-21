package mav

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// gotoDefaultTimeout bounds the whole run, alongside the step budget. Both,
// because a loop can burn its steps in two seconds or spend ninety on one.
const gotoDefaultTimeout = 90 * time.Second

func (c CLI) gotoScreen(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	goal := strings.TrimSpace(strings.Join(gotoPositional(args), " "))
	if goal == "" {
		return Fail("goto_goal_missing", map[string]string{
			"usage": `mav goto "the camera settings screen" [--arrived-when 'title:"Cámara"']`,
		}).Write(c.Stdout)
	}

	// Same refusal as find, and for the same reason one step louder: a loop
	// that spends money and presses buttons has no place in a pipeline.
	if reason := findIsRefusedHere(); reason != "" {
		return c.writeGotoResult(GotoResult{
			Arrived: "unverified", Outcome: GotoCIRefused, Goal: goal,
			CriterionSource: CriterionNone,
			Next:            "goto does not drive a screen in CI",
		}, map[string]string{})
	}

	criterion := ParseArrivalCriterion(flagValue(args, "--arrived-when"))
	maxSteps := gotoMaxSteps
	if raw := flagValue(args, "--max-steps"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= gotoMaxSteps {
			maxSteps = n
		}
	}
	timeout := gotoDefaultTimeout
	if raw := flagValue(args, "--timeout"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 && d <= gotoDefaultTimeout {
			timeout = d
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dismiss := flagValue(args, "--dismiss-permission")
	result, err := c.runGotoLoop(ctx, cfg, opts, goal, criterion, maxSteps, dismiss)
	if err != nil {
		return err
	}
	return c.writeGotoResult(result, map[string]string{
		"steps": strconv.Itoa(len(result.Steps)),
	})
}

// executeGotoFlowStep runs the same loop the command runs, and judges its
// result by the stricter rule a flow needs.
//
// Outside a flow, `arrived=unverified` is an honest answer -- the command says
// it cannot confirm and whoever reads it decides. Inside a flow it is an
// unchecked premise the following steps are going to act on, and a flow that
// carries on over a false arrival touches where it should not. A flow that
// fails is annoying; one that does strange things in somebody's app is
// something else. So unverified FAILS the step; it does not pass it.
//
// The criterion itself is mandatory, and rejected at lint time rather than
// here (validateGotoFlowStep). This is the second half of the same guarantee:
// even with a criterion written, a run that cannot confirm arrival against it
// stops the flow instead of handing the next step a place it never checked.
func (c CLI) executeGotoFlowStep(ctx context.Context, opts GlobalOptions, step FlowStep) (map[string]string, error) {
	cfg, cfgErr := c.mustLoadConfig()
	if cfgErr != nil {
		return flowStepTargetFailure(step, cfgErr)
	}

	goal := strings.TrimSpace(step.Params["goal"])
	criterion := ParseArrivalCriterion(step.Params["arrivedWhen"])
	maxSteps := gotoMaxSteps
	if raw := step.Params["maxSteps"]; raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= gotoMaxSteps {
			maxSteps = n
		}
	}
	timeout := gotoDefaultTimeout
	if raw := step.Params["timeout"]; raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 && d <= gotoDefaultTimeout {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The loop writes its own fail lines; inside a flow the step's line is the
	// one the run prints, so its output goes to a sink and only the code and
	// the first line survive as fields.
	var sink strings.Builder
	result, err := c.withStdout(&sink).runGotoLoop(ctx, cfg, opts, goal, criterion, maxSteps, step.Params["dismissPermission"])

	fields := map[string]string{
		"goal":             goal,
		"arrived":          result.Arrived,
		"outcome":          result.Outcome,
		"criterion":        criterion.String(),
		"criterion_source": result.CriterionSource,
		"steps":            strconv.Itoa(len(result.Steps)),
	}
	if !result.RouteFinal.IsZero() {
		fields["route"] = result.RouteFinal.String()
	}
	if result.Next != "" {
		fields["next"] = result.Next
	}
	for _, d := range result.Dismissed {
		fields["dismissed_permission"] = d.Modal
		fields["dismissed_action"] = d.Action
	}
	if err != nil {
		if detail := firstLine(strings.TrimSpace(sink.String())); detail != "" {
			fields["detail"] = detail
		}
		return fields, err
	}

	return fields, gotoFlowStepVerdict(result.Arrived)
}

// gotoFlowStepVerdict turns what the loop reported into the step's verdict.
// Only a confirmed arrival passes: `unverified` is a premise nobody checked,
// and the steps after this one would act on it as if it were an arrival.
func gotoFlowStepVerdict(arrived string) error {
	switch arrived {
	case "true":
		return nil
	case "unverified":
		return errors.New("goto_arrival_unverified")
	default:
		return errors.New("goto_did_not_arrive")
	}
}

func (c CLI) runGotoLoop(ctx context.Context, cfg Config, opts GlobalOptions,
	goal string, criterion ArrivalCriterion, maxSteps int, dismiss string) (GotoResult, error) {

	result := GotoResult{Arrived: "false", Goal: goal, CriterionSource: CriterionNone}
	if !criterion.IsZero() {
		result.CriterionSource = CriterionExplicit
		result.Criterion = criterion.String()
	}

	elements, err := c.gotoReadScreen(ctx, cfg, opts)
	if err != nil {
		return result, err
	}
	route := ExtractRoute(elements)
	result.RouteInitial = route
	result.RouteFinal = route
	result.observed = RecordObservedScreen(result.observed, route, elements)

	// The check that exists because a real bug hid here: if the criterion
	// already holds where we are standing, the loop would declare victory at
	// step zero without tapping anything. Refuse instead of arriving.
	if !criterion.IsZero() && criterion.MatchesRoute(route, elements) {
		result.Outcome = GotoAmbiguousCriterion
		result.Arrived = "unverified"
		result.Next = "that criterion already holds on the screen you are on; name the destination in a way that does not match where you start"
		return result, nil
	}

	seen := NewSeenRoutes()
	seen.Visit(screenFingerprint(elements))
	unchanged, abstentions := 0, 0

	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			result.Outcome = GotoTimeout
			result.Next = "ran out of time; the route it reached is in route_final"
			return c.finishGoto(ctx, cfg, opts, result, criterion, elements), nil
		}

		// A modal on top is not a screen you navigate through, and tapping
		// blindly under one is how a loop dismisses something that mattered.
		//
		// The one exception is a button the CALLER named, and it fails closed:
		// if that exact label is not here, this stops like it always did. With
		// a three-option permission alert where two options grant, the quiet
		// failure is the one that grants.
		if route.Modal != "" && !criterion.MatchesRoute(route, elements) {
			btn := FindDismissButton(elements, dismiss)
			if btn == nil || len(result.Dismissed) >= gotoMaxDismissals {
				result.Outcome = GotoRefused
				result.Next = "a modal is on top (" + route.Modal + "); goto does not tap under one. " +
					"Name the button that grants nothing with --dismiss-permission to let it through"
				return c.finishGoto(ctx, cfg, opts, result, criterion, elements), nil
			}
			x, y, ok := TapPoint(*btn)
			if !ok {
				result.Outcome = GotoRefused
				result.Next = "the declared dismiss button has no frame to tap"
				return c.finishGoto(ctx, cfg, opts, result, criterion, elements), nil
			}
			if err := c.gotoTap(ctx, cfg, opts, x, y); err != nil {
				return result, err
			}
			result.Dismissed = append(result.Dismissed, PermissionDismissal{
				Modal: route.Modal, Action: btn.Label,
			})
			elements, err = c.gotoReadScreen(ctx, cfg, opts)
			if err != nil {
				return result, err
			}
			route = ExtractRoute(elements)
			result.RouteFinal = route
			// Answering a dialog is not progress towards the goal, so it does
			// not consume a step and cannot be mistaken for one in the record.
			step--
			continue
		}

		found := c.resolveGotoStep(ctx, elements, goal)
		if found.Element == nil {
			abstentions++
			if abstentions >= gotoMaxAbstentions {
				// Two different facts, and they used to share a label.
				// Never moved: there was no way to start. Moved and then
				// ran out of onward moves: the route was walked and this
				// screen offers nothing further — which is what the
				// destination looks like from the inside.
				if gotoMoved(result.Steps) {
					result.Outcome = GotoDeadEnd
					result.Next = "walked the route and nothing on this screen leads any further (" + found.Reason + "); route_final is where it stopped"
				} else {
					result.Outcome = GotoNoRoute
					result.Next = "nothing on this screen clearly leads to the goal (" + found.Reason + "); read `mav ui tree`"
				}
				return c.finishGoto(ctx, cfg, opts, result, criterion, elements), nil
			}
			continue
		}
		abstentions = 0

		// Stricter than find's, with no escape hatch: in goto nobody reads
		// anything between the decision and the finger.
		if GotoRefusesDestructive(found.Element) {
			el := *found.Element
			result.Refused = &el
			result.Outcome = GotoRefused
			result.Next = "the way forward goes through something that destroys data; that is a decision for a person"
			return c.finishGoto(ctx, cfg, opts, result, criterion, elements), nil
		}

		x, y, ok := TapPoint(*found.Element)
		if !ok {
			result.Outcome = GotoNoRoute
			result.Next = "the chosen element has no frame to tap"
			return c.finishGoto(ctx, cfg, opts, result, criterion, elements), nil
		}

		before := screenFingerprint(elements)
		record := GotoStep{
			Tapped: found.Element, ResolvedBy: found.ResolvedBy, RouteBefore: route,
		}
		if err := c.gotoTap(ctx, cfg, opts, x, y); err != nil {
			return result, err
		}

		elements, err = c.gotoReadScreen(ctx, cfg, opts)
		if err != nil {
			return result, err
		}
		route = ExtractRoute(elements)
		result.observed = RecordObservedScreen(result.observed, route, elements)
		record.RouteAfter = route
		record.Changed = screenFingerprint(elements) != before
		result.Steps = append(result.Steps, record)
		result.RouteFinal = route

		if !record.Changed {
			unchanged++
			if unchanged >= gotoMaxUnchanged {
				result.Outcome = GotoStuck
				result.Next = "two taps in a row changed nothing; the screen is not responding to them"
				return c.finishGoto(ctx, cfg, opts, result, criterion, elements), nil
			}
			continue
		}
		unchanged = 0

		if seen.Visit(screenFingerprint(elements)) {
			result.Outcome = GotoLooping
			result.Next = "came back to a screen it had already been on"
			return c.finishGoto(ctx, cfg, opts, result, criterion, elements), nil
		}

		// The one place quiescence is worth its read: a criterion that matches
		// a half-drawn screen would report arrival at somewhere that is not
		// finished being itself. Confirmed on a settled screen or not at all.
		if !criterion.IsZero() && criterion.MatchesRoute(route, elements) {
			settled, settleErr := c.gotoSettle(ctx, cfg, opts)
			if settleErr != nil {
				return result, settleErr
			}
			elements = settled
			route = ExtractRoute(elements)
			result.RouteFinal = route
			if criterion.MatchesRoute(route, elements) {
				result.Outcome = GotoArrived
				result.Arrived = "true"
				return result, nil
			}
		}
	}

	result.Outcome = GotoExhausted
	result.Next = "ran out of steps; the route it reached is in route_final"
	return c.finishGoto(ctx, cfg, opts, result, criterion, elements), nil
}

// finishGoto is the last thing every unhappy path goes through, and it LOOKS
// ONE MORE TIME before accepting that the run did not arrive.
//
// That second look is the fix for a measured defect: goto walked two steps into
// Boxy, landed on the destination — `label="Test Category 2: 1000" role=heading`
// was on screen afterwards — and reported arrived=false, with both a title: and
// a text: criterion naming exactly that.
//
// The cause was an asymmetry in the loop rather than anything about matching.
// Arrival was tested on the ONE read taken right after a tap, and the loop
// settles only when that read already matches. A screen that finishes drawing a
// moment later was therefore missed, and nothing ever looked again: the loop
// went round, found nothing left to tap, abstained twice and reported no_route
// while standing on the destination.
//
// So the symmetry is restored. The loop already refuses to declare arrival on a
// half-drawn screen; it now equally refuses to declare failure on one. The
// check is the same criterion against a settled read, so it cannot turn a
// journey that did not arrive into one that did — there is no new leniency
// here, only a second look at the same question.
func (c CLI) finishGoto(ctx context.Context, cfg Config, opts GlobalOptions,
	result GotoResult, criterion ArrivalCriterion, elements []Element) GotoResult {

	// A run that never moved has one screen to its name, which is the screen it
	// started on. There is nothing to name a destination out of and nothing to
	// settle, so it costs nothing: no extra read, no model call.
	if criterion.IsZero() && !gotoMoved(result.Steps) {
		result.Arrived = "unverified"
		if result.Next != "" {
			result.Next += ". No --arrived-when was given and nothing moved, so arrival cannot be confirmed either way"
		}
		return result
	}

	settled, err := c.gotoSettle(ctx, cfg, opts)
	if err != nil || settled == nil {
		settled = elements
	}
	route := ExtractRoute(settled)
	result.RouteFinal = route

	if criterion.IsZero() {
		criterion = c.nameObservedDestination(ctx, &result, route, settled)
	}
	if criterion.IsZero() {
		result.Arrived = "unverified"
		if result.Next != "" {
			result.Next += ". No --arrived-when was given and the destination was not named among the screens this run stood on, so arrival cannot be confirmed either way"
		}
		return result
	}

	if criterion.MatchesRoute(route, settled) {
		result.Outcome = GotoArrived
		result.Arrived = "true"
		result.Next = ""
		if result.CriterionSource == CriterionObserved {
			result.Next = "pin it next time with --arrived-when '" + criterion.String() + "'"
		}
		return result
	}
	// A named destination that is not where this stopped stays UNVERIFIED and
	// never becomes false. Naming among observed screens can only ever turn an
	// unverified into a true; downgrading a run it got wrong would make the
	// command worse than not having asked.
	if result.CriterionSource == CriterionObserved {
		result.Arrived = "unverified"
		result.Next = "the destination was named " + criterion.String() +
			", which is not the screen this stopped on (" + route.String() + ")"
	}
	return result
}

// nameObservedDestination asks the one question, and only where it can be
// answered. Every gate below is decidable in code before a penny is spent:
//
//   - the screen it stopped on has neither a title nor a screen identity, so no
//     pick could ever be checked against it;
//   - fewer than two distinct screens were seen, which would leave the model
//     holding a yes/no about the one screen it stopped on — that IS asking it
//     whether it arrived, and it is the line this must not cross;
//   - no key, no network, or the model declines.
//
// Each of those falls back to the zero criterion, which is exactly today's
// behaviour. Nothing here can make a run worse than not having asked.
func (c CLI) nameObservedDestination(ctx context.Context, result *GotoResult, final Route, settled []Element) ArrivalCriterion {
	if _, ok := nameObservedRoute(final); !ok {
		return ArrivalCriterion{}
	}
	result.observed = RecordObservedScreen(result.observed, final, settled)
	menu := ObservedScreens(result.observed)
	if len(menu) < 2 {
		return ArrivalCriterion{}
	}
	names := ObservedNames(menu)
	result.ObservedScreens = names

	key, _, ok := ResolveJevKey()
	if !ok {
		return ArrivalCriterion{}
	}
	answer, err := askJevChoice(ctx, key, GotoObservedQuestion(result.Goal),
		RenderObservedMenu(names), GotoObservedOptions(names))
	if err != nil {
		return ArrivalCriterion{}
	}
	pick, picked := InterpretGotoObservedAnswer(answer.Label, menu)
	if !picked {
		return ArrivalCriterion{}
	}
	criterion, accepted := AcceptObservedCriterion(pick, result.RouteInitial)
	if !accepted {
		return ArrivalCriterion{}
	}
	result.CriterionSource = CriterionObserved
	result.Criterion = criterion.String()
	return criterion
}

// gotoSettle waits for the screen to stop moving, and it is deliberately NOT
// used on every step.
//
// Quiescence costs a second read: during a push the tree holds nodes from both
// views at once and a skeleton screen changes as it populates, so two reads
// that agree is the only way to know the screen has stopped. Paying that on
// every step hands back exactly the saving this command exists for — measured,
// one tap through goto came out 28% SLOWER than today's path, because two
// settle reads replaced the one read a selector tap costs.
//
// So the loop reads once per step and settles only where a half-drawn screen
// could actually mislead someone: immediately before declaring arrival. A
// mid-transition read on an intermediate step costs nothing, because the next
// step reads again anyway and corrects it.
func (c CLI) gotoSettle(ctx context.Context, cfg Config, opts GlobalOptions) ([]Element, error) {
	var last []Element
	for i := 0; i < gotoSettleThreshold+2; i++ {
		elements, err := c.gotoReadScreen(ctx, cfg, opts)
		if err != nil {
			return nil, err
		}
		if last != nil && screenFingerprint(last) == screenFingerprint(elements) {
			return elements, nil
		}
		last = elements
		select {
		case <-ctx.Done():
			return last, nil
		case <-time.After(250 * time.Millisecond):
		}
	}
	return last, nil
}

func (c CLI) gotoReadScreen(ctx context.Context, cfg Config, opts GlobalOptions) ([]Element, error) {
	prefer, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		prefer = "auto"
	}
	described, err := c.describeUITree(ctx, cfg, prefer, false)
	if err != nil {
		return nil, Fail("goto_tree_failed", map[string]string{"detail": err.Error()}).Write(c.Stdout)
	}
	if described.Result.Err != nil {
		return nil, Fail("goto_tree_failed", map[string]string{"stderr": firstLine(described.Result.Stderr)}).Write(c.Stdout)
	}
	return ExtractElements(described.Result.Stdout), nil
}

// gotoTap taps the point goto already resolved. Coordinates, not a selector:
// a selector tap re-reads the tree, which is the read this command exists to
// avoid.
func (c CLI) gotoTap(ctx context.Context, cfg Config, opts GlobalOptions, x, y int) error {
	var sink strings.Builder
	inner := c
	inner.Stdout = &sink
	return inner.uiTap(ctx, opts, cfg, []string{"--x", strconv.Itoa(x), "--y", strconv.Itoa(y)})
}

func (c CLI) writeGotoResult(result GotoResult, extra map[string]string) error {
	fields := map[string]string{
		"arrived": result.Arrived,
		"outcome": result.Outcome,
	}
	if result.CriterionSource != "" {
		fields["criterion_source"] = result.CriterionSource
	}
	// A criterion nobody wrote is never printed as one somebody wrote.
	if result.CriterionSource == CriterionObserved {
		fields["criterion_observed"] = result.Criterion
	}
	if !result.RouteFinal.IsZero() {
		fields["route"] = result.RouteFinal.String()
	}
	for _, d := range result.Dismissed {
		fields["dismissed_permission"] = d.Modal
		fields["dismissed_action"] = d.Action
	}
	for k, v := range extra {
		fields[k] = v
	}
	if err := c.OK("goto", fields).Write(c.Stdout); err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.Stdout, "goto json=%s\n", quoteIfNeeded(string(data)))
	return err
}

// gotoMoved reports whether any tap this run actually changed the screen.
func gotoMoved(steps []GotoStep) bool {
	for _, s := range steps {
		if s.Changed {
			return true
		}
	}
	return false
}

// gotoPositional drops flags and the values of the ones that take a value.
func gotoPositional(args []string) []string {
	valued := map[string]bool{"--arrived-when": true, "--max-steps": true,
		"--timeout": true, "--dismiss-permission": true}
	out := make([]string, 0, len(args))
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(a, "--") {
			skip = valued[a] && !strings.Contains(a, "=")
			continue
		}
		out = append(out, a)
	}
	return out
}

// gotoCommand loads the config and the target the way `mav ui` does, then runs
// the loop. It is its own top-level command rather than a `ui` verb because it
// drives the screen over many steps instead of performing one action.
func (c CLI) gotoCommand(ctx context.Context, opts GlobalOptions, args []string) error {
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return c.failConfig(err)
	}
	c.resolveConfigTools(&cfg)
	if _, err := c.resolveConfigTarget(&cfg); err != nil {
		return c.failTargetCommand(err)
	}
	return c.gotoScreen(ctx, opts, cfg, args)
}

// resolveGotoStep is resolveFind with the loop's own question. Everything else
// is shared on purpose — the candidate filter, the abstention rule, the veto
// that can only remove a yes — because those were measured for find and none of
// the measurements depend on the wording. What changes is only what is asked.
func (c CLI) resolveGotoStep(ctx context.Context, elements []Element, goal string) FindResult {
	candidates := FindCandidates(elements)
	batch, omitted := FindBatch(candidates)
	result := FindResult{
		ResolvedBy: ResolvedByNone, Goal: goal,
		Candidates: len(candidates), Omitted: omitted,
	}
	if len(candidates) == 0 {
		result.Reason = ReasonNoCandidates
		return result
	}
	key, source, ok := ResolveJevKey()
	if !ok {
		result.Reason = ReasonNoKey
		result.Next = MissingJevKeyNext()
		return result
	}
	result.KeySource = string(source)

	answer, err := askJevChoice(ctx, key,
		GotoStepQuestion(goal), RenderFindCandidates(batch), FindOptions(batch))
	if err != nil {
		if errors.Is(err, errJevTooOld) {
			result.Reason = ReasonJevTooOld
			result.Next = err.Error()
			return result
		}
		result.Reason = ReasonNoNetwork
		return result
	}
	result.Verdict = answer.Verdict
	chosen, reason := InterpretGotoAnswer(answer.Label, batch)
	if reason != "" {
		result.Reason = reason
		return result
	}
	// The veto is unchanged and still the only thing that can overrule the
	// model, still only ever able to remove a yes.
	if veto := VetoChoice(chosen, goal, batch); veto != "" {
		result.Reason = veto
		return result
	}
	result.ResolvedBy = ResolvedByModel
	result.Element = chosen
	return result
}
