# Controlled goal evaluation

This harness compares the pinned MAV 0.34.0 goto baseline, the candidate goal
engine and AXe v2.0.4's standalone agent. It builds a synthetic UIKit fixture
with no accounts, permissions, network or personal data. All generated apps,
raw traces and machine identifiers stay in `.scratch/`.

The corpus contains 30 positive runs: 10 direct, 10 multi-step, 5 scroll and 5
form tasks. Multi-step goals name their containing section; scroll goals name
the swipe direction. Six negative runs cover missing destinations and blocking
modals. Four additional diagnostics exercise a transition, focus loss, an app
change and a reported save without completion. The transition is not added to
the 30-run acceptance denominator.
Every arm is launched from the same scenario and initial Home state. Arm order
rotates. A fresh, independent AX tree oracle checks the first heading twice,
250ms apart, rather than trusting `arrived`, `succeeded`, AXe's `DONE`, or an
agent's own final snapshot. The oracle also checks the fixture process identity.
Both MAV arms use the same AXe installation and Jevi binary, environment and MAV
key. Goto uses its ordinary input path; goal additionally uses native checked
refs. Results must identify that execution difference, not attribute every gain
to the planner alone.

Build with `python3 tools/goal-bench/build.py`. Use a dedicated iOS simulator,
install `.scratch/GoalFixture.app`, pin a MAV 0.34.0 binary, and build the
candidate with `make build`. Then run:

```sh
python3 tools/goal-bench/run.py --udid "$BENCH_SIMULATOR" \
  --baseline .scratch/mav-0.34.0 --candidate .build/mav
```

`TYPESAFE_API_KEY` is required for the real `axe agent` arm. No substitute or
scripted model satisfies that arm. Both MAV arms use MAV's existing configured
key and provider. Do not copy secrets into public commands, traces or reports.
The harness records versions, the baseline commit and binary hashes, and freezes
both binaries for the whole run. Logs
include raw local evidence; publish only reviewed aggregate/scenario metrics
without machine IDs, paths or process IDs.

`--arms goto goal --cases direct-01` is useful for development. Such a partial
run deliberately exits with a failing acceptance gate; it cannot authorize
retirement. Missing arms and missing cases never count as successes.

The harness records independent completion, public command completion claims, false
success, execution errors, nonzero exits, model calls, explicit tree-read commands, logical
observations, abstentions, discarded decisions, steps and latency. Transparent
Jevi/AXe shims count subprocesses without logging credentials or screen text.
Wrong distractor taps and consecutive repetitions without progress are recorded
separately from delivery/process errors. AXe's supplied cost estimate and MAV's
provider-reported bill are labeled separately.
Explicit read commands and AXe's internal observation count are different
measures; do not compare them as if both counted every internal native read.
Cost is `null` when absent from provider evidence. The corpus and measurements
are versioned tools, not a claim that an unrun comparison has passed.
The reviewed `model_reported_success` field separately records goal's model
declaration or AXe's model-driven completion. A goal model's premature `done`
followed by `succeeded=unverified` is an unverified model report, not a public
success claim. The retirement gate counts false public success claims against
the independent oracle.
Each trial owns a process group; timing out stops its shims and agent children
before another trial starts. AXe abstentions and discarded decisions come from its
retained trace.

Abstentions count model choices, including block confirmations. Discards are
split into stale evidence and unchanged BLOCKED confirmations; AXe uses the same
`discardedAsStale` field for both. Wrong-action metrics count selection of named
distractors, rather than every possible incorrect ordering or a physical event log.

`summarize.py` can re-extract those counters from unchanged trial evidence,
without rerunning or selecting trials:

```sh
python3 tools/goal-bench/summarize.py .scratch/goal-evaluation \
  .scratch/goal-evaluation-reviewed.json
```

The exported JSON contains only the allowed metric and version fields. Review
it before publishing; the original stdout, traces and machine identities remain
local.
Execution errors mean missing/unreadable results, uncertain observation/input,
failed status or a failed/partial/unknown delivery receipt. Nonzero exits are
separate: AXe also uses one for an ordinary `blocked` outcome. A harness timeout
has no parsed result and is recorded as `process_error`, a nonzero exit and an
execution error. It is never counted as a successful abstention.

The deterministic test `TestGoalResolvesGotoStaleCoordinateRegression` gives
the same correct model choice to both implementations while the target moves
during planning. The old loop sends the original point; goal discards that
decision, replans against the new observation and verifies the destination.
This isolates a reproducible executor regression from model randomness.

Before retiring goto, require all corpus arms, zero goal false successes,
at least 27/30 positives with no regression against goto, and the regression
test plus ablation evidence. Retain raw failures when fixing the candidate;
rerun the full corpus for the final candidate rather than combining favorable
runs from different builds.

Development probes found that a bare "Open Details 01" abstained on a state
where that target was off-screen; naming an explicit swipe made the operation
available to the model's choice. Retain those failed probes in the evaluation
report as a limitation. The declared-scroll corpus does not establish that the
planner can infer every hidden navigation route.

Run the gate/parser tests with:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools/goal-bench
```
