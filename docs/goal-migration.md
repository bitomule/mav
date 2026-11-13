# Migrating from goto to goal

MAV 0.35.0 replaces `goto` with one objective command:

```sh
mav goal "<objective>"
```

Navigation and action use the same observe–plan–act–verify engine. Use
`--mode navigation` for the narrower navigation menu. The default `action`
mode also offers supported scrolling and typing into focused fields, using
only caller-declared or quoted text. MAV does not run AXe's autonomous agent.

## Command line

Before:

```sh
mav goto "the settings screen" --arrived-when 'title:"Settings"' \
  --max-steps 6 --timeout 45s --prefer-driver axe
```

After:

```sh
mav goal "the settings screen" --mode navigation \
  --succeeded-when 'title:"Settings"' \
  --max-steps 6 --timeout 45s --prefer-driver axe
```

The retired command returns a nonzero `goto_removed` diagnostic with a
shell-quoted equivalent command. It performs no configuration, target, model
or input work. It is not an alias. `--arrived-when` is also rejected by `goal`
with an instruction to use `--succeeded-when`.

An action example:

```sh
mav goal "Create a note saying 'Sample note' and save it" \
  --succeeded-when 'title:"Saved"'
```

The condition must describe evidence the app actually exposes. A `Saved`
heading is useful only if the app reserves it for completed saves; it does
not by itself prove a remote server committed data. Available predicates are
`title:`, `screen:`, `text:` and exact `value:"field-id=value"`; all supplied
terms must hold.

## YAML flows

Before:

```yaml
- goto:
    goal: the screen that lists every category
    arrivedWhen: 'screen:categoriesView'
    maxSteps: 6
    timeout: 45s
```

After:

```yaml
- goal:
    goal: the screen that lists every category
    mode: navigation
    succeededWhen: 'screen:categoriesView'
    maxSteps: 6
    timeout: 45s
```

| Old spelling | New spelling |
| --- | --- |
| `mav goto` | `mav goal`, with `--mode navigation` to retain the navigation menu |
| `--arrived-when` | `--succeeded-when` |
| YAML step `goto` | YAML step `goal` |
| `arrivedWhen` | `succeededWhen` |

Flows require an explicit `succeededWhen` and stop unless it is verified.
Lint rejects old steps and properties before execution, including nested
blocks and included files. Quoted literals in the objective and declared flow
inputs supply the closed set of text values; the planner selects an offered
value rather than generating keyboard text.

## Results and safeguards

Replace checks of `arrived` with `succeeded=true|false|unverified`. Keep
`reported_success` separate: it records the model's declaration, which cannot
verify the caller's condition. Verification requires two equal fresh
observations 250ms apart. Read failures and foreground changes do not reuse
earlier evidence to claim success.

Unlike goto's ambiguous-criterion refusal, a condition true at entry returns
`outcome=already_satisfied` and zero actions. This reports existing state and
does not claim that the run caused it.

The limits remain 12 goal actions and 90 seconds, with at most three exact,
explicitly named non-destructive permission dismissals. Abstention, cycle
detection, lack of progress and the destructive-action veto still stop a run.
Partial or unknown input is not repeated. A later observation failure retains
the original delivery evidence, and flows cannot retry the whole goal after
earlier input was delivered.

AXe simulator references provide native snapshot/process admission. Other
drivers retain their documented guarantees; a fresh tree followed by an
ordinary operation is not atomic. See the [engine contract](design/goal-engine.md)
and [source analysis](design/goal-axe-analysis.md).
