# Goal engine contract

`mav goal "<objective>"` runs MAV's observe–plan–act–verify engine. The four
interfaces (`GoalObserver`, `GoalPlanner`, `GoalExecutor`, `GoalVerifier`) are
independent of the transport and model. `axe agent` is never invoked by MAV.
The [AXe source analysis](goal-axe-analysis.md) records which ideas are adopted
and the limits of their guarantees.

## Completion and delivery

The caller supplies `--succeeded-when` or YAML `succeededWhen`. All predicates
must hold: `title:"..."` matches the first heading, `screen:"..."` matches an
exact screen identity, `text:"..."` matches visible tree content as in goto,
and `value:"field-id=exact value"` requires one visible, enabled field with
that exact accessibility ID and value. Multiple terms are conjunctive. A bare
term is a title. These are observations of the application's accessibility
state, not a claim about a server-side effect.

A condition must hold in two equal fresh observations sampled 250ms apart.
Observation failure, changed foreground context and unstable samples cannot
verify completion. If it holds initially, the outcome is `already_satisfied`
with zero actions. Later it is `succeeded`. Other outcomes include `unverified`,
`blocked`, `uncertain`, `exhausted`, `timeout`, `stuck`, `looping`, `no_route`
and `dead_end`. `succeeded=true|false|unverified` and `reported_success` remain
separate: choosing `done` reports the model's belief without changing the
caller’s condition or proving an effect.

Each attempted input keeps a receipt: `not_sent`, `completed`, `partial` or
`unknown`. Completed means the driver accepted/completed its input operation;
application success is still a separate verification. An observation error
after input retains its receipt and ends `uncertain`. Partial/unknown input is
never replayed, including through a YAML `onFailure` retry or skip policy.
A completed earlier input also prevents retrying an entire failed goal step.

## Planning and policies

The default `action` mode offers a closed menu of available operation/target
pairs: tap, scroll, and typing into focused fields. Text comes only from
quoted literals in the objective or declared `--input name=value` / YAML flow
inputs. The model selects a menu index and input name; it never supplies
executable text, driver flags, selectors or coordinates. Unsupported operations
are absent. CUA currently supplies checked tap/scroll; its tree does not expose
focus evidence needed to offer typing under this policy.

Planning receives the current field value, insertion semantics and recent
target/input names. Typing does not clear an existing field, and writing a
value does not prove it was saved. The verifier may supply a pending-condition
hint; exact expected field values are redacted. This hint is planning context
only: it cannot replace the independent condition or make a model declaration
pass verification.

`--mode navigation` uses the same engine with a narrower menu. Navigation
preserves goto's candidate filtering, destructive-action veto, abstention and
cycle detection. Progress combines the current route with the referenced
viewport when the driver supplies refs, so scrolling can reveal new targets
without losing changes of title or screen identity. Geometry jitter is freshness
data, not navigation progress. Action progress can include field values and focus,
but also ignores geometry jitter.

The caps remain 12 goal actions and 90 seconds. An exact caller-named,
non-destructive `--dismiss-permission` button can dismiss up to three modals
separately from the goal-action budget. Unknown buttons and destructive labels
stop the run. Two abstentions, two actions without progress and repeated
screens stop it. A failed or exhausted path gets a final fresh verification
before being judged, without reusing an old successful tree.

In navigation mode without a declared condition, the planner may name a
destination from the distinct screens actually observed. The menu has no visit
order or endpoint marker. A destination equal to the initial route is rejected;
code checks the chosen destination against stable final evidence under the
original foreground identity and deadline. This is labeled
`criterion_source=observed`. Action mode does not infer that work happened from
reaching the screen where it could happen. YAML goal steps require an explicit
condition and stop unless it is verified. CI refuses model-driven execution.

## Freshness by driver

Every plan is discarded if a fresh observation changes geometry, value, focus,
availability, application, target or driver. A final cancellation check occurs
before input. Read overlap is an opt-in interface for observers that can prove
reads neither invalidate references nor interfere with input. Production
reference adapters do not opt in.

| Adapter | Observation and input guarantees |
| --- | --- |
| AXe on simulator | Full AX tree for predicates plus versioned `--agent --view full` snapshot/ref data for input. Pages must belong to one snapshot/PID/context. Duplicate rows for the same ref are merged; ambiguous or lossy identity matches are not offered. Each verb must also be offered by the native row. Execution passes the fresh snapshot/ref and expected PID to AXe's admission checks. No fallback to old coordinates. |
| CUA on macOS | One combined tree/ref observation. Snapshot tokens are opaque refs, never semantic IDs or progress. Execution uses the latest token directly, without taking another snapshot first. Lost replies are unknown and never trigger mutation retries. |
| Other drivers, including physical AXe | Driver/target pinned for the run, fresh full-tree preflight, then the available ordinary driver operation. This check is not atomic with input and does not imply native snapshot/ref admission. Missing focus evidence prevents typing. |

Metrics distinguish model calls, logical observations, abstentions, discarded
decisions, model round-trip time and total engine latency. Cost remains `null`
when the provider does not supply it; no result implies that a missing price
means zero. Raw receipts and each action's before/after routes remain evidence.

## Evaluation and retirement

Goto remains available while the [controlled comparison](../../tools/goal-bench/README.md)
is incomplete. The retirement gate requires all three arms on the same corpus,
zero goal false successes, at least 27/30 positive completions without falling
below goto, and the reproduced stale-coordinate regression fixed. Numerical
results alone cannot substitute for the regression or independent verification.
