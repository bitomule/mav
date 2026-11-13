# MAV 0.35.0

`mav goal` now handles navigation and action objectives through one engine.
**`goto` is retired in this version.** Old invocations fail with a migration
command and perform no input; update scripts and flows before upgrading.

Before:

```sh
mav goto "the settings screen" --arrived-when 'title:"Settings"'
```

After:

```sh
mav goal "the settings screen" --mode navigation \
  --succeeded-when 'title:"Settings"'
```

YAML before:

```yaml
- goto:
    goal: the settings screen
    arrivedWhen: 'title:"Settings"'
```

YAML after:

```yaml
- goal:
    goal: the settings screen
    mode: navigation
    succeededWhen: 'title:"Settings"'
```

Flow lint diagnoses both the old step and `arrivedWhen`, including included
files and nested blocks. The command line uses `--succeeded-when`; YAML uses
`succeededWhen` and requires an explicit condition.

The default action mode also offers supported scrolling and caller-declared
text for focused fields:

```sh
mav goal "Create a note saying 'Sample note' and save it" \
  --succeeded-when 'title:"Saved"'
```

Success must be verified against fresh, stable application evidence. A model's
completion declaration remains separate. A condition true at entry returns
`already_satisfied` with zero actions. Navigation retains the 12-action and
90-second budgets, refusal policies and explicit permission-dismissal rule.
Partial or unknown input is never replayed.

MAV owns the loop; it does not run AXe's autonomous agent. AXe simulator input
uses checked snapshot references. Other drivers retain their documented
guarantees, including the limits of ordinary tree preflight.

Read the [migration guide](https://github.com/bitomule/mav/blob/v0.35.0/docs/goal-migration.md),
[engine contract](https://github.com/bitomule/mav/blob/v0.35.0/docs/design/goal-engine.md)
and [controlled evaluation](https://github.com/bitomule/mav/blob/v0.35.0/docs/evaluation/goal.md).

## AXe 2 command corrections

Ordinary input commands keep native JSON off stdout unless `--raw` is requested;
read commands still print their data, and flow evidence retains the envelope.
The `ui tree` node count now matches its printed, deduplicated elements.

An ambiguous ordinary ID tap can recover when a fresh AXe tree shows the same
ID on structural ancestors and one innermost button. Type filters, active
ancestry and containing frames are required. Other ambiguity and selected
display/process refusals remain errors. This coordinate fallback has ordinary
read-before-input guarantees; goal's checked reference executor does not use it.

Native receipts take precedence over command error codes. Earlier delivered or
uncertain input survives aggregation, and unknown cleanup or quarantine prevents
replay. Without a receipt, only source-checked tap failures can prove that input
was not sent. A slider error remains uncertain because resolution can fail after
the drag.
