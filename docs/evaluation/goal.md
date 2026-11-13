# Goal evaluation

This report compares MAV 0.34.0's goto, MAV's own goal engine and AXe v2.0.4's
standalone agent. The [harness](../../tools/goal-bench/README.md) and synthetic
UIKit fixture are versioned with the implementation. Raw local traces are not
published; reviewed per-trial measurements are in [goal-initial.jsonl](goal-initial.jsonl).

The final complete cohort passes the retirement gate: goal verified **30/30**
acceptance positives, versus **20/30** for goto, with **zero false public goal
success claims** across all nine negative cases. All 120 trials are retained in
[goal-final.jsonl](goal-final.jsonl). The first complete cohort failed at 26/30;
it remains below and is not combined with the final cohort.

## Final complete evaluation

| Measure, 40 runs per arm | Goto 0.34.0 | Final goal | AXe agent |
| --- | ---: | ---: | ---: |
| Verified acceptance positives | 20/30 | 30/30 | 30/30 |
| False public success claims | 0 | 0 | 3 |
| Unverified model completion reports | 0 | 0 | 3 |
| Observed execution errors | 0 | 0 | 0 |
| Nonzero exits | 0 | 0 | 6 |
| Model calls | 72 | 76 | 117 |
| Explicit tree-read commands | 184 | 444 | 0 |
| Logical/internal observations | unavailable | 222 | 4,617 |
| Abstentions | 8 | 6 | 12 |
| Discarded decisions | unavailable | 0 | 11 |
| Discarded for stale evidence | unavailable | 0 | 5 |
| Discarded to confirm a block | unavailable | 0 | 6 |
| Wrong distractor selections | 0 | 0 | 0 |
| Repetitions without progress | 6 | 3 | 3 |
| Median latency, ms | 4,850.0 | 12,552.5 | 5,613.0 |
| 95th-percentile latency, ms | 6,930 | 23,967 | 7,095 |
| Total cost, USD | 0.002672964 | 0.002796066 | 0.006443220 |

The frozen final candidate was built from
`c2dcb4303a62d73ab3f166f359fc66921e112b3c` with a clean source tree. Binary,
fixture-source and corpus hashes are recorded in the final JSONL metadata.
The same fixture and goals were used across all three arms. Each arm completed
all 30 acceptance positives or recorded its failures; no trial was substituted.

Goal completed direct, multi-step, declared-scroll and form tasks (10/10,
10/10, 5/5 and 5/5). Goto completed the first two families and failed all scroll
and form tasks. AXe completed all 30 positives but declared completion in the
false-final, focus-loss and app-change diagnostics without meeting the external
condition. Goal returned stuck, stuck and blocked respectively. All three arms
completed the separate positive transition diagnostic.

`TestGoalResolvesGotoStaleCoordinateRegression` executes the historical loop
with a correct supplied model choice while its target moves during planning.
Goto sends its saved (50,20) coordinate; goal discards the decision, replans
against the current target and verifies the destination. This regression is
resolved independently of model randomness. Fifteen guard ablations produced
executed failing assertions without the guard and passed after restoration,
including stale data, partial/unknown delivery, flow replay, cancellation,
rotation, progress, condition context and expected-value redaction.

## Initial complete evaluation

All three arms ran the same 30 acceptance-positive tasks, six ordinary
negative tasks and four diagnostics. The latter include three negatives
(false final, focus loss and app change) and one positive transition, which
does not enlarge the 30-run acceptance denominator.

| Measure, 40 runs per arm | Goto 0.34.0 | Initial goal | AXe agent |
| --- | ---: | ---: | ---: |
| Verified acceptance positives | 20/30 | 26/30 | 30/30 |
| False public success claims | 0 | 0 | 3 |
| Unverified model completion reports | 0 | 3 | 3 |
| Observed execution errors | 0 | 0 | 0 |
| Nonzero exits | 0 | 0 | 6 |
| Model calls | 72 | 81 | 117 |
| Explicit tree-read commands | 184 | 458 | 0 |
| Logical/internal observations | unavailable | 229 | 4,114 |
| Abstentions | 8 | 6 | 12 |
| Discarded decisions | unavailable | 0 | 11 |
| Discarded for stale evidence | unavailable | 0 | 5 |
| Discarded to confirm a block | unavailable | 0 | 6 |
| Wrong distractor actions | 0 | 0 | 0 |
| Repetitions without progress | 6 | 4 | 3 |
| Median latency, ms | 4,530 | 8,098.5 | 5,641.5 |
| 95th-percentile latency, ms | 13,361 | 42,318 | 7,353 |
| Total cost, USD | 0.002670024 | 0.002841216 | 0.006443220 |

Both MAV arms used AXe v2.0.4, Jevi 0.4.0 and the same configured OpenRouter
provider/model (`typesafe/jev-1.13-20260917`). AXe's agent used TypeSafe and
reported `jev-1.14.0`. Its cost is AXe's estimate; MAV costs are provider
reports. These are not interchangeable pricing measurements.

Goto completed direct and multi-step navigation, but all five scroll tasks
stuck and none of the five forms completed. Initial goal completed all
navigation tasks and one form. Two form runs declared completion after typing
without saving; two inserted the requested text twice and could not save it.
Goal returned unverified or stuck and made no public success claim for these
failures. Its planner now receives insertion semantics, the current field
value, recent target/input names and pending-condition context. Expected field
values remain redacted; hints do not change the independent verifier.

AXe completed all 30 positives, but returned `completed` in the false-final,
focus-loss and app-change diagnostics without satisfying the independent
condition. Its six nonzero exits were ordinary blocked outcomes, not six
transport failures. No arm selected one of the fixture's wrong distractors.
That counter does not classify every possible wrong ordering of otherwise
relevant actions; the form failures above remain visible separately.

AXe's 12 abstentions count model choices, including confirmation choices, across
six blocked trials. Its 11 discarded decisions include five stale TAP choices
and six unchanged BLOCKED choices discarded to confirm the block.
The initial cohort predates recording source commit/dirty state and fixture/corpus
hashes. Its retained metadata includes binary hashes; missing provenance is not
reconstructed from later source files.

## Method and limits

Each trial terminates and relaunches the fixture with the same scenario and
number, checks its initial Home state and process identity, then rotates arm
order. An external AX tree oracle checks the expected first heading and
fixture process twice, 250ms apart. For forms, the fixture shows Saved only
when the exact requested note was entered and saved. Neither the command's
status nor its final snapshot is the oracle. Timeout cleanup stops the whole
trial process group before another trial starts.

Multi-step requests name their containing section; scroll requests explicitly
name an upward swipe. Earlier development probes abstained on a bare request
for an off-screen destination. The declared-scroll corpus does not establish
general discovery of hidden routes. Positive repetitions vary the destination
or note number across four scenario families; they are not 30 unrelated apps.

The MAV arms share driver and provider, but use different execution paths:
goto's ordinary input versus goal's checked native refs. The stale-coordinate
regression test supplies the same model choice to both loops and isolates the
old executor defect. Live differences cannot all be attributed to the planner.

Explicit CLI reads, paired goal observations and AXe's internal polling are
different units. Zero explicit read commands for AXe does not mean zero reads.
Latency is wall time on a shared machine, with rotating order; it does not
establish a universal speed ranking. These live measurements cover the AXe
simulator adapter. Other driver guarantees are covered by deterministic tests
and the [engine contract](../design/goal-engine.md), not live hardware results.

The retirement gate is satisfied by the final cohort plus the reproduced
regression test: at least 27/30 verified positives without falling below goto
and zero false goal success claims. The initial cohort is retained in full;
no favorable runs from different builds are combined to meet the gate.
This justifies the planned command retirement, not a universal performance claim.
