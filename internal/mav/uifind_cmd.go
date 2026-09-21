package mav

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// uiFind is `mav ui find "<what you want to tap>"`.
//
// It reads the screen the way `mav ui tree` does and then answers one question:
// which element is the one described? The reason it exists is not token thrift —
// measured, deciding from mav trees is about 0.7% of a session's spend. It is
// that `mav ui tree` prints at most 80 nodes and the line meant to announce the
// cut cannot fire, because the list is capped before it is handed to the printer.
// On a 202-node screen that hides more than half of it in silence, which leaves
// elements no command can show you. find reads the uncapped extraction.
//
// The exit code carries no answers. 0 whenever find could answer at all,
// resolved_by=none included; non-zero only when it could not run.
func (c CLI) uiFind(ctx context.Context, opts GlobalOptions, cfg Config, args []string) error {
	goal := strings.TrimSpace(strings.Join(positionalArgs(args), " "))
	if goal == "" {
		return Fail("find_goal_missing", map[string]string{
			"usage": `mav ui find "the button that opens camera settings"`,
		}).Write(c.Stdout)
	}

	prefer, err := c.normalizePreferDriver(opts.PreferDriver)
	if err != nil {
		return Fail("prefer_driver_invalid", map[string]string{"usage": c.preferDriverUsage()}).Write(c.Stdout)
	}
	treeStarted := time.Now()
	described, err := c.describeUITree(ctx, cfg, prefer, hasFlag(args, "--include-system"))
	treeMS := time.Since(treeStarted).Milliseconds()
	if err != nil {
		return c.writeUITreeToolError(cfg, err)
	}
	if described.Result.Err != nil {
		return Fail("ui_tree_failed", map[string]string{"stderr": firstLine(described.Result.Stderr)}).Write(c.Stdout)
	}
	if isEmptyAXTree(described.Result.Stdout) {
		return Fail("ui_tree_empty", map[string]string{
			"driver": described.Driver,
			"reason": "simulator_accessibility_unavailable",
		}).Write(c.Stdout)
	}

	// Raw, not Compact: the 80-element cap is the hole this command exists to
	// fill, so find must not read through it.
	elements := ExtractElements(described.Result.Stdout)
	result := c.resolveFind(ctx, elements, goal)
	// Reading the screen is part of what the answer cost, and on a real
	// simulator it is most of it. Left out, total_ms would flatter find by
	// hiding the expensive half.
	result.Cost.TreeMS = treeMS
	result.Cost.TotalMS += treeMS

	fields := map[string]string{
		"driver":      described.Driver,
		"nodes":       strconv.Itoa(len(elements)),
		"resolved_by": result.ResolvedBy,
		"candidates":  strconv.Itoa(result.Candidates),
		// Two of the four on the human line: the one a person asks about, and
		// the one that says whether the wait was the model or us.
		"total_ms": strconv.FormatInt(result.Cost.TotalMS, 10),
		"model_ms": strconv.FormatInt(result.Cost.ModelMS, 10),
	}
	if result.Reason != "" {
		fields["reason"] = result.Reason
	}
	if result.Omitted > 0 {
		fields["omitted"] = strconv.Itoa(result.Omitted)
	}
	// On the ok line too, not only in the JSON: this is the line a person
	// reads, and "which key did that use" is the question that cost us time.
	if result.KeySource != "" {
		fields["key_source"] = result.KeySource
	}
	if err := c.OK("ui.find", fields).Write(c.Stdout); err != nil {
		return err
	}
	return writeFindResult(c.Stdout, result)
}

// resolveFind is the decision, with no I/O of its own beyond the one call to
// jev. Split from uiFind so the whole ladder is reachable from a test without a
// simulator.
//
// The timing lives out here rather than inside decideFind because decideFind
// returns from a dozen places and a cost filled in at each of them is a cost
// that will be forgotten at the thirteenth.
func (c CLI) resolveFind(ctx context.Context, elements []Element, goal string) FindResult {
	started := time.Now()
	result := c.decideFind(ctx, elements, goal)
	result.Cost.TotalMS = time.Since(started).Milliseconds()
	// What is left once the provider's round trip is taken out is ours.
	result.Cost.LocalMS = result.Cost.TotalMS - result.Cost.ModelMS
	if result.Cost.LocalMS < 0 {
		// jev timed its own call and mav timed the process around it, so two
		// clocks are being subtracted. They agree to well within a
		// millisecond in practice; clamping means the arithmetic of the three
		// parts never reads as nonsense on the day they do not.
		result.Cost.LocalMS = 0
	}
	return result
}

func (c CLI) decideFind(ctx context.Context, elements []Element, goal string) FindResult {
	candidates := FindCandidates(elements)
	batch, omitted := FindBatch(candidates)

	result := FindResult{
		ResolvedBy: ResolvedByNone,
		Goal:       goal,
		Candidates: len(candidates),
		Omitted:    omitted,
	}

	if len(candidates) == 0 {
		result.Reason = ReasonNoCandidates
		result.Next = "no actionable element on this screen carries text; read `mav ui tree` and target by frame"
		return result
	}

	// The deterministic pass runs first and needs neither key nor network, so
	// a goal that is simply an element's own text resolves everywhere.
	if el, ok := FindLiteral(candidates, goal); ok {
		if veto := VetoChoice(el, goal, candidates); veto != "" {
			result.Reason = veto
			result.Next = findNextFor(veto)
			return result
		}
		result.ResolvedBy = ResolvedByLiteral
		result.Element = el
		return result
	}

	if reason := findIsRefusedHere(); reason != "" {
		result.Reason = reason
		result.Next = "find does not consult a model in CI. Read `mav ui tree` and match in your own code"
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
		FindQuestion(goal), RenderFindCandidates(batch), FindOptions(batch))
	if err != nil {
		if errors.Is(err, errJevTooOld) {
			result.Reason = ReasonJevTooOld
			result.Next = err.Error()
			return result
		}
		result.Reason = ReasonNoNetwork
		result.Next = "jev could not be asked. The screen was not judged; read `mav ui tree`"
		return result
	}

	// Recorded before the answer is judged: what the round trip cost is true
	// whether the answer survives the vetoes or not, and the vetoed runs are
	// exactly the ones worth knowing the price of.
	result.Cost.ModelMS = answer.LatencyMS

	// Reported, never read. It is what jevi said, and printing it is how it
	// was measured to be `yes` on all 40 answers including the abstentions.
	result.Verdict = answer.Verdict
	chosen, reason := InterpretFindAnswer(answer.Label, batch)
	if reason != "" {
		result.Reason = reason
		result.Next = findNextFor(reason)
		return result
	}
	// The veto runs last and can only ever take this yes away.
	if veto := VetoChoice(chosen, goal, batch); veto != "" {
		result.Reason = veto
		result.Next = findNextFor(veto)
		return result
	}
	result.ResolvedBy = ResolvedByModel
	result.Element = chosen
	return result
}

func findNextFor(reason string) string {
	switch reason {
	case ReasonDestructiveGuard:
		return "the best match destroys something and your words did not ask for that. Name the destructive action explicitly if you meant it"
	case ReasonNotACandidate:
		return "the answer did not name an element that was on this screen; read `mav ui tree`"
	case ReasonAbstained:
		return "the screen was read and no element is clearly the one described; read `mav ui tree`"
	}
	return "read `mav ui tree`"
}

// writeFindResult prints the JSON document. One line, so a caller can pipe it
// straight into a parser, and resolved_by is first in the struct so it is first
// on the line.
func writeFindResult(w io.Writer, result FindResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "find json=%s\n", quoteIfNeeded(string(data)))
	return err
}

// positionalArgs drops flags and their values, leaving the goal. Flags here are
// all boolean (`--include-system`), so a `--flag value` pair does not arise.
func positionalArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			continue
		}
		out = append(out, a)
	}
	return out
}
