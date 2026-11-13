# Goal engine: AXe source analysis and adopted guarantees

This analysis covers AXe **v2.0.4** and MAV's pre-goal implementation at
`bcce9291c1ff41f30a8d9b2eb28a4be791930fb4` (the baseline containing MAV 0.34.0).
It distinguishes executable checks from model instructions. It does not establish
that one agent performs better: that requires the controlled evaluation described
below. The decisions in this document are the contract for the goal engine, not
claims that every driver already provides AXe's native protections.

The older [goto design](goto.md) and [written-flow design](flow-steps.md) preserve
useful experiments and rejected approaches. Some sections describe earlier
implementations. The linked source below is authoritative for the baseline.

## Four mechanisms with different responsibilities

| Mechanism | Chooses the next operation? | Execution protection | Completion authority |
| --- | --- | --- | --- |
| AXe commands with `--agent` | The caller chooses the operation and ref/selector | Broker-owned snapshot, target, process and display admission; native delivery receipts | No goal or goal evaluator |
| `axe agent "goal"` | AXe's model policy chooses operation, target and sometimes text | Its own fresh-tree comparison and semantic resolution, followed by simulator HID input | Model's `DONE` becomes `completed` |
| MAV `goto` before goal | MAV selects a next tap toward the same objective | Fresh tree between taps, but coordinates selected before model latency | Explicit route/text criterion, or model-selected observed destination identified separately |
| MAV YAML flows | The author supplies order, conditions and data; model selectors can choose an element | Driver protections plus flow retry/evidence policy | Each authored step and its conditions; `verify.ask` is a model judgement |

The first two AXe mechanisms are separate implementations. In particular, using
AXe's standalone agent does not make its executor use the stronger snapshot/ref
broker contract. [`AgentCommand`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Commands/AgentCommand.swift#L5-L25)
constructs a simulator agent; [`SimulatorAgentExecutor.execute`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Agent/SimulatorAgentExecutor.swift#L26-L124)
dispatches directly to app launch, HID taps, text replacement, scrolling and Return.
Its scope is a booted simulator, not a universal hardware-agent interface.

## What snapshot/ref mode actually guarantees

`AgentModeController` owns **one current table** per controller. Each record binds a
UUID, sequence, capture time, process, display context and complete reference table.
Its reservation spans reading, action and replacement; concurrent use is refused.
The lifetime is **60 seconds**. A different snapshot ID or expiry refuses input;
refs are therefore opaque, scoped handles, not persistent element identities.
See [`Record`, `perform`, `requireRecordIdentity`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeController.swift#L6-L76)
and [identity/expiry checks](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeController.swift#L583-L618).
This mode is simulator-only in v2.0.4; physical-device automation must retain its
separately supported driver guarantees.
[`AgentModeCommandAdapter.send`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeCommandAdapter.swift#L25-L28).

An equal semantic screen hash alone does not preserve refs. Reuse also requires
the native fingerprint and protected targets to match, the same process/context,
and an unexpired record. Reuse advances the sequence but retains the original
capture time; reading does not renew that table forever.
[`canReuseSnapshot`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeController.swift#L41-L49),
[`describe` reuse](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeController.swift#L253-L274).

For reference taps, admission uses the native action's own current tree, validates
the process and display context, compares the visible fingerprint, checks the
protected reference and target, and resolves execution authority before input.
It does not merely look up a previously captured center point.
[`AgentReferenceTapAdmission.validateOwnedSnapshot`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentReferenceTapAdmission.swift#L68-L100).
The visible fingerprint intentionally excludes hidden/off-display inert content
and focus; keyboard entry has a separate field guard. This is a defined admission
projection, not a promise that every UI property remains identical.
[`AgentModeSnapshot`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeSnapshot.swift#L5-L23).

The controller invalidates the table **before input**, including partial or
uncertain delivery. Its receipt preserves outcome, event count, cleanup,
quarantine and retry information.
[`performReserved`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeController.swift#L130-L161),
[`receipt`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeController.swift#L485-L490).
Recovery retries only a refusal with zero enqueued events, no quarantine and no
uncertain cleanup. It can rebind the same control; a moved control must stabilize
across reads. Target identity includes type, identifier, label, value, process,
enabled/visible state and native identity when both observations provide it.
Retries are bounded at five and restricted to supported operation kinds.
[`refusal`, `refusalRefreshes`, `reboundReference`, `sameControl`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeController.swift#L363-L455).
These are retries before delivery, not replay after a partly performed action.

After a completed action it publishes **one** replacement observation immediately;
that snapshot can still contain a transition. If the replacement read fails, it
retains the action receipt, marks the cache invalid and instructs the caller to
capture again without replaying the action. A quarantine remains a failure with
the original action evidence.
[`publishReplacement`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeController.swift#L493-L545).
Thus a protected action, a completed delivery and a settled goal are three
different facts. Snapshot/ref mode does not verify a natural-language objective.

## What the autonomous AXe agent adds

`AgentPolicy.prepareRequest` builds a dynamic menu from current targets and
actions: launch, tap, text entry/clear, Return, scroll, wait, done and blocked are
offered only under their relevant conditions. One model request contains the
operation question and applicable target/value questions. `resolveDecision`
validates selections against the offered choices. The state carries a compact
screen and the last **10** actions.
[`prepareRequest`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXeCore/AgentPolicy.swift#L26-L135),
[state and history](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXeCore/AgentPolicy.swift#L198-L237),
[`resolveDecision`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXeCore/AgentPolicy.swift#L258-L344).
The useful idea is bounded joint choice, not allowing arbitrary generated command
lines. AXe can generate missing text through a separately configured text helper;
that is a policy difference MAV must retain control over.
[`SimulatorAgentExecutor` text branch](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Agent/SimulatorAgentExecutor.swift#L56-L85).

While the model decides, the runner starts a fresh observation. `AgentFreshness`
accepts an unchanged fingerprint; otherwise it distinguishes unchanged compact
rows, unique semantic re-resolution, screen-independent operations and stale
decisions. Changed target sets invalidate a selected element even if it can still
be found behind a modal. A moved target receives another frame-stability read.
Terminal/focus-dependent decisions require matching compact rows and target
layout. A discarded decision still counts as a billed call.
[`run`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Agent/SimulatorAgentRunner.swift#L176-L261),
[`AgentFreshness.evaluate` and `resolve`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXeCore/AgentFreshness.swift#L28-L98).

This overlap reduces the critical path, but is **not atomic admission at input**:
the read can finish before the model reply, and the executor subsequently sends
HID events. Text replacement taps, waits and sends a composite key sequence;
this executor is distinct from the snapshot/ref keyboard protection.
[`replaceText`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Agent/SimulatorAgentExecutor.swift#L149-L175).
MAV must not describe an ordinary read-before-tap driver as having native temporal
or focus guarantees that it does not implement.

Most importantly, `DONE` is a model assertion. The policy asks for visible
evidence, but the runner maps `.done` directly to `.completed`, records the fresh
screen and returns. It has no caller-defined deterministic condition evaluator.
Freshness checks establish that the decision still applies to the observation;
they do not establish that the goal is true.
[`DONE` result branch](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Agent/SimulatorAgentRunner.swift#L305-L339).

AXe defaults to 40 actions (configurable 1–200), caps model calls at
`max(steps*3, steps+5)`, and blocks after three identical actions with no observed
change. Its result/trace records calls, reads, timing and usage, including stale
decisions. These are useful evaluation measurements, not MAV's navigation budget.
[`AgentCommand` limits](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Commands/AgentCommand.swift#L21-L54),
[call budget](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Agent/SimulatorAgentRunner.swift#L176-L182),
[no-progress check](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Agent/SimulatorAgentRunner.swift#L581-L590).

## What MAV preserves and corrects

The baseline's `ArrivalCriterion` combines all declared `title:`, `screen:` and
`text:` terms. Titles are checked against the extracted route, screen identity
matches whole, and text checks content. This avoids confusing a destination's
button with its actual screen. Flows require a criterion and reject unverified
arrival.
[`MatchesRoute`](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/goto.go#L266-L301),
[`validateGotoFlowStep`](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/flow.go#L834-L863),
[`gotoFlowStepVerdict`](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/goto_cmd.go#L142-L153).

Preserve navigation's 12-action/90-second limits, two consecutive abstentions,
two unchanged actions, visited-screen detection, destructive-action veto and
explicit permission dismissal policy. A modal prevents blind navigation under
it. The baseline rejects a criterion already true at entry as ambiguous. The
approved goal contract deliberately changes this to `already_satisfied`, with
zero actions and no attribution of effects to the run.
[`budgets`](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/goto.go#L328-L337),
[`runGotoLoop`](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/goto_cmd.go#L181-L337).

Two implementation limits must not become goal guarantees. First, goto selects
an element, waits for the model and taps its saved coordinates without validating
the decision against a new observation. Second, `gotoSettle` returns its latest
tree even when equality was not achieved; `finishGoto` can fall back to a previous
tree after a read error. Goal must explicitly represent failed observation or
unproven stability rather than infer successful verification from either case.
[`choice to tap`](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/goto_cmd.go#L241-L290),
[`finishGoto` fallback](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/goto_cmd.go#L374-L379),
[`gotoSettle`](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/goto_cmd.go#L511-L529).

Flows remain explicit composition: selectors, branches, loops, declared inputs,
conditions, evidence and failure policy. `text.ask` selects a declared input key;
it cannot generate keyboard text. Preserve this default for goal action mode.
`verify.ask` is a separate model judgement and even `unclear` can return without
an error; it must not substitute for deterministic `succeededWhen`. Preserve the
flow interlock that forbids retry after sent/uncertain input or a failed `after`
following an acting step.
[`resolveFlowStepText`](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/flow_text.go#L100-L161),
[`runVerifyStep`](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/flow_verify.go#L80-L109),
[flow retry policy](https://github.com/bitomule/mav/blob/bcce9291c1ff41f30a8d9b2eb28a4be791930fb4/internal/mav/cli.go#L6124-L6163).

## Adopted decisions for goal

| Decision | Guarantee and limit |
| --- | --- |
| A MAV-owned observe–plan–act–verify loop | No invocation of `axe agent`; the same engine accepts driver implementations and a replaceable planner. |
| Dynamic operation/target menu | Offer only operations permitted by mode, policy and driver capability. Reject a planner choice absent from that exact observation's menu. Navigation retains its bounded operation set; action mode adds supported operations without relaxing verification. |
| Short action history | Supply recent operations, outcomes and progress to planning; keep the complete evidence/cost record outside the prompt. History is context, not proof of success. |
| Opaque observation references | Keep driver state opaque. Use native snapshot/ref admission when available. An expired ref never becomes an old coordinate tap. A driver with only semantic reads must advertise that weaker guarantee and revalidate before action. |
| Conditional parallel observation | Overlap a read with planning only if the driver says that read cannot replace the decision's table or interfere with input. AXe owns one current table: an arbitrary new capture can invalidate existing refs. Reuse is safe only according to the driver's actual returned identity. Otherwise read sequentially. |
| Stale-decision discard | Refresh/replan before any input. Preserve counts and cost for rejected choices; a discarded choice is not an executed action. A non-atomic revalidation must not be labelled atomic. |
| Delivery separate from observation | Keep completed, partial, unknown and not-sent evidence. Refresh failure cannot erase delivery or cause replay. Only a proven not-sent refusal can be retried under the bounded policy. |
| Independent success evaluator | Migrate arrival terms into `succeededWhen`; flows require explicit conditions. Evaluate post-action evidence, including modal/context and stability requirements. The model's done signal only requests verification; without sufficient evidence return unverified/uncertain. |
| Honest initial state | A condition already true returns `already_satisfied` and zero actions. It asserts state, not that the run caused an effect. |
| Separate navigation and action semantics | A screen match does not prove a form was submitted or a value persisted. Action goals need conditions expressing their observable result; unsupported or absent evidence cannot produce verified success. |
| Literal and declared input policy | The planner selects offered literal/declared data. AXe's optional free-text generator is not inherited as MAV's default. |

## Evidence required before retirement of goto

Use the same initial states and externally checked conditions for the old goto,
goal and autonomous AXe arms. Keep driver and provider identical between MAV
arms. Report AXe's `completed` separately from independently verified completion;
record false positives against the external condition, not against AXe's label.
Count errors, wrong/repeated actions, abstentions, all model calls (including
stale choices), all reads, wall-clock latency and measured usage/cost. Missing
provider pricing or usage is unavailable evidence, never zero cost.

The code analysis identifies a reproducible regression candidate: move or replace
a selected control during model latency. The old loop uses the old frame; goal
must discard or resolve under its declared driver guarantee. Additional fixtures
must cover focus loss, keyboard movement, modal/app changes, partial delivery,
refresh failure and missing capabilities. These are test requirements; source
inspection alone does not count as the experiment.

Retirement requires one reproduced old regression resolved by goal, at least
**27/30 positive completions** without falling below the old arm, and **zero
false successes in negative cases**. Until measurements establish those gates,
this analysis does not justify removing goto or claiming goal is faster, cheaper
or more accurate. Use synthetic app identities, text and states in published
fixtures and traces.

## Adapter contract for snapshot/ref support

Use the versioned [canonical full-result schema](https://github.com/cameroncooke/AXe/blob/v2.0.4/schemas/axe.output.agent-result.v1.schema.json).
The CLI defaults to a `reading` projection; an integration must request
`--view full` to decode the original `axe.output.agent-result`, version `"1"`.
This is a different envelope from ordinary `axe.output.command-result`.
Unknown additive properties must be tolerated. `outcome`, `cleanup` and cache
statuses are open string sets; prose in `nextSteps` is not a stable API.

These are invocation templates: each ref and snapshot must come from the same
current capture. Angle-bracket values are placeholders, never real device data.

```sh
axe describe-ui --agent --view full --udid <simulator-uuid>
axe tap --agent --view full --snapshot-id <snapshot-uuid> --ref e12 --expected-pid 123 --udid <simulator-uuid>
axe type 'Example' --agent --view full --snapshot-id <snapshot-uuid> --ref e13 --focus --expected-pid 123 --udid <simulator-uuid>
axe swipe --agent --view full --snapshot-id <snapshot-uuid> --ref e20 --direction up --distance 1 --duration 0.5 --expected-pid 123 --udid <simulator-uuid>
```

The flag is **`--snapshot-id`**, not `--snapshot`. `type --focus` performs guarded
focus-and-type; without it, the field must already have focus. This command types
into the field, rather than inheriting the standalone agent's replacement
semantics. Agent text accepts 1–256 UTF-8 bytes. Ref swipe directions describe the
finger movement; to reveal lower content, swipe `up`. Distance is in `(0,1]` and
duration in `(0,10]` seconds. Do not mix ref swipes with coordinate flags.
[`AgentModeOptions`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeOptions.swift#L6-L52),
[`Type`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Commands/Type.swift#L32-L84),
[`Swipe`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Commands/Swipe.swift#L116-L153).

The full response has these fields (optional fields are marked `?`):

```text
schema: "axe.output.agent-result"
schemaVersion: "1"
didError: boolean
error?: {code, message}
data:
  capture?:
    type: "runtime-snapshot" | "runtime-snapshot-unchanged"
    rs: "1"
    snapshotID, screenHash, seq, count, udid, applicationPID
    context: {bootIdentity, screenID, screenUUID, logicalWidth, logicalHeight,
              rotationDegrees, nativePixelWidth, nativePixelHeight, scale, poseRevision}
    targets: ["e12|tap|button|Save||saveButton", ...]
    scroll:  ["e20|swipe|scroll-view|||listView", ...]
    text:    ["e25|text|text|Example||", ...]
    rowOffset, rowLimit, targetCount, scrollCount, textCount
    truncated, expiresInSeconds, totalRows?, nextRowOffset?
    keyboard?, redundantTextRefs?, unavailableReferences?, customActions?
    truncatedCustomActionRefs?, scrollPreview?
  action?: {kind, outcome, enqueuedEventCount, cleanup, replayed,
            quarantine?, retried?, slider?, sliderValue?}
  cache: {status, reason?, requiresRefresh}
  context?, quarantine?, observation?, detail?, batch?
nextSteps: [string, ...]
```

Rows have exactly six cells: `ref|action|role|label|value|id`. Literal pipes become
slashes. Actions can be `unavailable`; those refs must not be offered for input.
The original table, frames and native identities remain inside AXe, not in these
rows. The compact roles also do not retain the full native heading/route traits.
A goal evaluator must not turn arbitrary text rows into title evidence. When
combining with a full MAV tree, do not assign refs by array position or by an
ambiguous label; maintain the relationship to the actual driver observation and
use only uniquely established associations.
[`capture`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeSnapshot%2BCapture.swift#L6-L83),
[`row`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXeCore/RuntimeSnapshot.swift#L774-L794).

For pages beyond the initial 64 rows, use `describe-ui --agent --view full
--snapshot-id <snapshot-uuid> --row-offset <nextRowOffset> --row-limit 64` with the
same simulator. Do not merge a refused page or a different snapshot's rows into
an earlier table. `--since-screen-hash` requests a new observation and is mutually
exclusive with `--snapshot-id`; only a returned unchanged result with the same
identity authorizes reuse. Exact text and optional frame/actions can be read with
`--read-ref e12 --text-field label --text-offset 0 --text-limit 4096`, together
with `--agent --view full --snapshot-id` and `--udid`. This returns `data.detail`
with `ref`, `field`, `offset`, `totalLength`, `text`, `actions`, optional `frame`
and `nextOffset`. Offsets count Unicode scalars.
[`DescribeUI`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Commands/DescribeUI.swift#L34-L80).

Decode receipts even when `didError` or the process exit code indicates failure.
`enqueuedAndDrained` with released cleanup describes delivery, not app success;
`failedNoReplay` may include sent events. A malformed/missing action response to
a mutation has unknown delivery. Zero events alone is insufficient to retry when
cleanup is uncertain or quarantine exists. A completed receipt without a new
capture and `requiresRefresh=true` means refresh before another choice and never
replay. Preserve raw evidence and map typed errors such as `screenChanged`,
`targetChanged`, `snapshotMissing`, `snapshotExpired`, `contextChanged`,
`targetCovered`, `inputIncomplete` and `cleanupUncertain` without deriving
semantics from error prose.
[`AgentModeResult`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModeProtocol.swift#L416-L448),
[`nativeCode`](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/AgentModePublicErrorProjection.swift#L17-L36).
