# Changelog

## Unreleased

### `goto` is a flow step now, and inside a flow it has to prove it arrived

`goto` navigates; the declared steps do the work. A flow can now mix them in one
file: `goto` walks to the screen, and `tap` / `type` press the buttons once
there. It carries no selector, so `goal`, `arrivedWhen`, `maxSteps` and
`timeout` sit at the top level of the step, next to `where` rather than inside
it.

Two rules hold inside a flow and nowhere else.

**`arrivedWhen` is mandatory.** A `goto` step without one is a lint error,
caught by `mav flow lint` before anything runs.

**`arrived=unverified` fails the step.** On the command line `unverified` is an
honest answer — goto says it cannot confirm arrival and whoever reads it
decides. Inside a flow it is an unchecked premise the following steps are going
to act on, and a flow that carries on over a false arrival touches where it
should not. A flow that fails is annoying; one that does strange things in
somebody's app is something else.

There is a measured reason behind both: `goto` can declare arrival on opening
the screen an action lives on, without having done the action. It knows how to
check that it reached a PLACE, not that it did a THING. With a criterion
required, that inferred-arrival route is never taken inside a flow.

## v0.28.0

### `goto` says it arrived without being told what to expect

`--arrived-when` still works and still wins when you write it. What changes is
that you no longer have to. Naming the destination up front was the part that
broke the promise: if you already know the screen's exact title, much of the
point of asking in your own words has gone.

An earlier attempt asked the model, before setting off, what the destination
would be called. It scored 0/10, and correctly — the candidate names come only
from the starting screen, and on this route the destination is not on it. It was
being asked to guess a name it could not see.

So the criterion is not guessed before the walk; it is chosen after it. `goto`
records every screen it actually stood on, and when the walk is over it lists
them — each by its title, or by mav's own `screen=` identity when it has none,
with some of the text it showed — **alphabetically, with no step numbers and no
marker of where it ended** — and asks which one is the destination, with `none`
on the menu. Code, which alone knows which of them was the last, derives arrival:
`true` only when the pick IS where it stopped and is not where it started.

The blindness is structural rather than a matter of prompt. The model cannot
identify the endpoint in an alphabetical menu, so it cannot flatter the run by
picking it, and it is never asked whether it arrived — it is asked to choose
among places, which is a different question from grading its own trip.

Measured on Boxy with eight named boxes, ten runs a lane, no `--arrived-when`
anywhere:

| | result |
|---|---|
| false arrivals | **0 of 30** |
| confirms a real arrival | **9 of 10** (0/10 before) |
| cost | **one model call, 324 ms**, only when there is something to answer |

That last row is the gate the earlier attempt could not have: nothing moved,
fewer than two distinct screens, or a final screen with no name are all decidable
in code before anything is spent.

The ablation: a goal whose destination does not exist ends on the very screen the
first lane confirms, with the same three-entry menu — only the goal differs — and
answers `none` 10/10, `arrived=unverified` 10/10.

A names-only menu measured 0/10, which is worth knowing: "the FIRST category,
FIRST box" is positional, and a name does not say which is first.

### One sentence was costing `goto` a fifth of its choices

On a three-category screen, asked for the first category, `find` picked the right
one 40 times out of 40 and `goto` managed 29 to 34. Same model, same screen, same
nine candidates. The difference was the sentence that names the goal:

```
- "Someone is trying to reach: " + goal
+ "A user described where they want to get to as: " + goal
```

Now 40/40, and the loss travels with the sentence: put `goto`'s phrasing into
`find`'s question and `find` drops to 35/40.

The obvious suspect was wrong. `goto`'s clause offering "the element that LEADS
towards it" is untouched — it exists because without it `goto` stopped at the
Settings root when the goal was inside General, and it is not what was costing
anything.

Four controls, forty runs each, reading raw `axe` JSON on live screens: the
broken cell 40/40, the multi-step case the LEADS clause protects still 40/40, the
direct case still 40/40, and four absent goals still abstaining 40/40. End to end
12/12 before and after.

Also measured: the untrusted-text preamble was costing about 8 hits in 40 under
the old wording. Under the new one it costs nothing, so the defence stays without
a trade.

## Unreleased

### `goto` reads the goal as a description again, and stops losing the first of three

On Boxy's category grid — Moving Boxes top-left, Test Category 1 to its right,
Test Category 2 below — the goal `la primera categoría` came back as **Test
Category 1**, the row that merely has a 1 in its name. `mav ui find` never made
that mistake on the same screen with the same candidates. Only the wording
differed.

The suspicion was `goto`'s LEADS clause — the one that lets it answer with a row
that is not the destination but leads to it. It measured innocent. What carried
the loss was the single sentence that names the goal: `Someone is trying to
reach: X` invites X to be read as the name of a destination, and one row on that
screen is named "1". `A user described where they want to get to as: X` marks X
as the user's own words, and "primera" is then read as a position.

Measured 21 sep off the raw `axe describe-ui` JSON of two live Boxy screens,
iPhone 17 Pro / iOS 26.3 from a simpool slot, load 3.1–6.4, 40 runs a cell, with
hits, misses and abstentions counted separately:

| cell | right answer | before | after |
|---|---|---|---|
| first category, on the grid | the row that IS it | 29–34 hits / 6–10 misses | **40 hits / 0 misses** |
| box contents, from the grid | the row that LEADS (destination two taps away) | 40 hits | **40 hits** |
| box contents, from the box list | the row that IS it | 40 hits | **40 hits** |
| four goals absent from the screen | `none` | 40 abstentions each | **40 abstentions each** |

End to end, `mav goto "los contenidos de la primera categoría, primera caja"`
arrives **12/12** before and after, tap canary green on every run.

Nothing else moved: same candidate extraction, same `none` on the option list,
same answer reading, same veto. No confidence threshold, no second model, no
filtering of candidates by text.

### `goto` knows it arrived without you telling it the destination's name first

Measured on Boxy with eight named boxes, iPhone 17 Pro / iOS 26.3 from a simpool
slot, load 4.1–6.4 throughout, tap canary green. Ten runs a lane, no
`--arrived-when` anywhere.

| lane | goal | `arrived=true` | **arrived on the WRONG screen** |
|---|---|---|---|
| reaches it | contents of the first category's first box | **9/10** | **0/10** |
| does not (returns to the start) | Dropbox sync settings | 0/10 | **0/10** |
| does not, and **ends on the other lane's destination** | change history of the first box | 0/10 | **0/10** |

The tenth run of the first lane had not arrived — `goto` opened a different
category and stopped on `Vista de cajas vacia` — so that is **9 of the 9 runs
that got there**, which is what a hand-written `--arrived-when` scores.

**What it does.** Arrival is still decided by code comparing routes. What is new
is where the criterion comes from when you did not write one: at the END of the
walk, `goto` lists every distinct screen it actually stood on — each by its title
or, for a screen without one, by its internal screen name, followed by some of
the text it showed — **alphabetically, with no step numbers and no marker of
where it ended** — and asks which of them is the destination, with `none` on the
menu. Code, which alone knows which one was the last, decides.

**It is never asked whether it arrived**, and it cannot tell which entry the
answer would be. Choosing between screens it observed is not grading its own
work. And it is monotone: a pick that is not where the run stopped stays
`unverified`, never `false`, so this can only ever turn an unverified into a
true.

**It does not charge for saying nothing.** The question is skipped when nothing
moved, when fewer than two distinct screens were seen, or when the screen it
stopped on has neither a title nor a screen identity — all decidable in code
before a penny is spent. One call, 324 ms median, only where there is something
to answer.

The third lane is the ablation that matters: it ends on the **same screen with
the same three-entry menu** as the lane that confirms, and only the goal differs.
`none`, ten times out of ten.

New in the output: `criterion_source=observed`, `criterion_observed`, and
`observed_screens` — the menu the destination was named out of, so a pick is
readable next to what it was picked from. A criterion nobody wrote is never
printed as one you wrote, and an explicit `--arrived-when` always wins.

### `--arrived-when` takes `screen:"..."`

`mav`'s own screen identity — the `screen=` that `mav ui tree` already prints —
is now part of a route and can be named as an arrival criterion, compared whole
rather than as a substring. It exists because a Spanish-locale SwiftUI app mostly
has no navigation-bar headings: on Boxy neither the category grid nor the box
list has one, and without this there was nothing to call them.

## v0.27.0

### `goto` arrives, and now it says so

Measured on Boxy with eight named boxes, ten fresh runs from the root:
`mav goto "los contenidos de la primera categoría, primera caja"` reaches the
destination **10 out of 10**, against 18 of 24 before. Every run tapped the right
category and then the first box, verified by reading the tree rather than by the
command's own word for it.

What it could not do was report it. With no `--arrived-when` the loop ended in
`outcome=no_route` on all ten — the same label it uses when there was never a way
to start. Two different facts had been sharing one word, and the one you get while
standing on your destination is the one that reads as failure.

They are separate now, on a fact the loop already recorded and nobody read:
whether any tap changed the screen.

- **`no_route`** — nothing here leads to the goal, and nothing ever did.
- **`dead_end`** — the route was walked and this screen offers nothing further,
  which is what a destination looks like from the inside.

No model is asked. Arrival is still decided in code, and without a criterion
`goto` still says `arrived=unverified` rather than claiming anything.

The output also carries `criterion_source` (`explicit` or `none`) and prints the
criterion back in the syntax `--arrived-when` takes.

### `mav ui tap --find` never worked outside a flow

A defect shipped in 0.26.0, not a refinement. The interlock that spends a
resolution once — so a retry cannot act twice on one decision — was stored in the
tree cache, and only `mav run` turns that cache on. Every bare `mav ui tap --find`
therefore had nowhere to record its decision, and the consume immediately after
found nothing and returned `find_decision_consumed` without moving a finger. The
CLI half of the feature was dead from the day it was released.

It now has a home the CLI has too: the run's when there is a run, a private one
when there is not. Nothing was relaxed, and the interlock was extended to the one
decision in that file which had never passed through it.

### `mav ui type --find` typed its selector into the field

Also from 0.26.0. `mav ui type` splits its arguments into where-to-type and
what-to-type, and `--find` was missing from the list that describes the where — so
it was treated as text. The field ended up holding
`Kitchen ''find the search field in the bottom toolbar`.

A test now ties the two flag lists together, which is the invariant `--find`
broke: a new selector flag must appear in both or it gets typed.

### Write the longer goal

Counter-intuitive and measured, ten runs on a three-category screen:

| goal | result |
|---|---|
| `"la primera categoría"` | 3/10 |
| `"los contenidos de la primera categoría, primera caja"` | **10/10** |

Not a prompt-shape problem. With three categories on screen, "the first" reads
either as screen order or as the name `Test Category 1`, and both readings are
reasonable. The longer goal disambiguates itself because each fragment anchors on
a different screen. If you shorten a goal, measure it.

### The ablation bench is not the screen anyone sees

Written into the bench itself, because its numbers have been cited as if they
predicted real behaviour. Its category fixture holds two categories; the screen a
user walks through holds three, and the answers differ: on the fixture
`"la primera caja"` returns the search field 10/10, and on the real screen it
returns the right category 10/10. The defect documented there does not exist on
the screen anyone looks at. When fixture and live screen disagree, the live screen
decides.

## Unreleased

### `find` works outside a flow, and `type` stops typing its own selector

Two defects shipped with the `find` selector in v0.26.0. Both are fixed here, and
both were measured on a simpool slot (iPhone 17 Pro / iOS 26.3, Boxy fixture),
reading the result out of the accessibility tree rather than out of an exit code.

**`mav ui tap --find "..."` always failed.** The interlock that spends a
resolution's decision before anything touches the screen — so nothing can act
twice on one choice — was stored on the tree cache. That cache is opt-in and only
`mav run` turns it on, so every `mav ui ...` invocation had nowhere to write the
decision, the consume that follows found nothing, and the command died
`find_decision_consumed` before moving a finger. It is now a ledger of its own:
the run's when there is a run, a private one when there is not, so the interlock
holds either way. `mav ui type --find` inherited the failure through the tap it
runs to focus the field.

**A `type` step typed its own `where` into the field.** `mav ui type` splits its
arguments into a target and the characters to type, and `--find` was missing from
the list of flags that describe the target — so it was typed. Measured before the
fix:

```yaml
- type: { where: { find: "the search field" }, text: "Kitchen" }
```

left the field reading `Kitchen --find the search field` while the step reported
`ok`. The same step with a structural `where` was correct, which is what made this
look like `find` being broken for `type` and fine for `tap`: `tap` never splits its
arguments into a target and a payload, so it never asks the question.

`toggle` and `doubleTap` take `find` and resolve it once, so only the first defect
reached them; `erase`, `longPress`, `assert` and `scrollUntil` do not accept a
`find` at all — a `find` reaching a plain match still fails loudly with
`selector_find_unsupported`.


## v0.26.0

### Two abstentions in a row no longer mean the same thing as never starting

`outcome=no_route` covered both "there was no way to begin" and "I walked the route and
this screen leads nowhere further". The second is what the destination looks like from the
inside, and reporting it with the same label is what made `goto` look broken while
standing exactly where it was sent — measured over six takes of a video, all six
`no_route`, five of them on the right screen with the right `route_final`.

It is now `outcome=dead_end`, split on a fact the run already recorded and nobody read:
whether any tap changed the screen. No model opines here — it is `record.Changed`, which
was already being computed.

Every run now also reports `criterion_source=explicit|none`, and prints the criterion back
in the syntax `--arrived-when` takes, so whoever reads the JSON does not have to remember
what was passed in.

### A flow can say what it wants in words, and it beats `goto` on the clock

`goto` already arrives. The thing it could not do is arrive *predictably*: it hands
the same sentence to every screen on the route, so a wording that anchors on one
screen unanchors on the next. Measured: `"the box inside Test Category 2"` resolves
5/5 on Boxy's category list and 0/5 on its box list, and `"the box inside this
category"` does exactly the reverse.

So the route stops being the model's problem. A flow writes the steps down and the
model resolves one element per screen, which is what it was already good at:

```yaml
name: boxy_primera_categoria_primera_caja
steps:
  - tap: { where: { find: "la primera categoría" } }
  - tap: { where: { find: "la primera caja" } }
```

```sh
mav ui tap --find "la primera categoría"
```

`find` is a **selector kind**, not a new command, because `Selector` is one struct
read by both the CLI flags and the YAML tags. So it lands on both surfaces at once —
neither is a wrapper around the other — and every action that already took a
selector (`tap`, `type`, `longPress`, `toggle`) can use it without being touched.
The structural fields still run first and cut the candidates down; the words only
choose among what survives.

**Measured against free navigation**, one alternated batch, 10 runs a lane, same
binary, Boxy on iPhone 17 Pro / iOS 26.3:

| lane | median | arrived | right category |
|---|---|---|---|
| `goto --arrived-when` | 4,260 ms | 10/10 | 10/10 |
| the flow, with its `assert` | **3,801 ms** | 10/10 | 10/10 |

458 ms faster, 10.8%. Arrival was read back off the tree, never from the command's
own `arrived=true`, and the category came from the navigation title, never from a
box code — `createTestBoxes()` hands `1000`/`1001` to the categories in unordered
fetch order, and both turned up across the runs.

### Text the model never writes

A flow declares its inputs; a step names one, or lets the model name one:

```yaml
inputs:
  nombre: "Caja de herramientas"
steps:
  - type: { where: { find: "el campo del nombre" }, text: { from: nombre } }
  - type: { where: { find: "el campo de cantidad" }, text: { ask: "lo que toca escribir aquí" } }
```

`from` is a map lookup and costs nothing. `ask` puts the **keys** on the menu and
the code substitutes the value, so nothing the model says is ever typed. That closes
hallucinated text and injection from UI labels in the same move.

### `verify`, fenced out of every decision it must not make

`verify: { ask: "¿la caja que se ve abierta está vacía?" }` is for judgements about
content, where there is nothing structural to consult. A `no` fails the step; an
`unclear` does not, because declining is not a negative verdict.

What it may never do is decide whether the screen changed or whether the flow
arrived. Those are code, on a fingerprint of sorted `(id, label, role)` — asking a
model whether its own last action worked is a judge with correlated errors, and a
test enforces the fence in three directions.

### Two guards that were not guarding anything

- **`element_moved` could not fire on any screen.** The check asked whether the
  re-resolved element was still in the tree it had just come out of, which can only
  answer yes. It now asks the live screen with `axe describe-ui --point`, which
  costs 128 ms against 287 ms for the whole tree. With the old check the step
  dispatched a blind tap; with the new one it fails and dispatches nothing.
- **The verdict branch in `find` never ran.** `jevi` returned `verdict: "yes"` on 40
  answers out of 40, abstentions included. Removed — abstention rests on the `none`
  option, which is the only thing that was ever holding it up.

### A window nobody is looking at no longer reroutes every swipe

`mav ui swipe` was reported as returning `ok` without moving the screen. It was not:
six of six moved it, and the original evidence was a list already scrolled to its
end. What was real sat next to it — every swipe on that simulator came back
`rotation_unavailable=180` while the device was in portrait. The rotation belonged
to a *Simulator.app window* that a headless boot never opens, left behind in the
host's preferences for that UDID. `mav` already refused to use the angle, so it bent
no coordinates; it only routed the gesture away from AXe and told the caller its
gesture had gone sideways when it had not.

### Corrected measurements

Numbers in the tree had drifted far enough to mislead anyone planning against them.
Reading the screen is **320 ms**, not 630; `jev` deciding is **350 ms**, not 550.
And the note claiming a tap by coordinate costs 277 ms against 1,480 ms by text was
out by an order of magnitude: it is **786 ms against 899 ms**. That gap was the
stated justification for a design decision, and it is 113 ms, not 1,200.

### Known, measured, and deliberately not fixed

`find "la primera caja"` on a screen with no box on it returns the **search field**.
It is not a Spanish pun — `"the first box"` does the same, and abstention works fine
on that screen for other phrasings. What loses is the bare noun, which denotes a text
box in both languages. Four fixes were measured and rejected, including narrowing by
`role`, which makes it *worse* by swapping the search field for a category that
actually navigates. The mitigation is in the skill: name the thing, not its part of
speech.

## v0.25.1

### `goto` no longer says it failed while standing on the destination

It walked two steps into Boxy, landed on the box contents, and reported
`arrived=false outcome=no_route`. The heading `label="Test Category 2: 1000"
role=heading` was on screen afterwards, and both a `title:` and a `text:`
criterion naming exactly that came back denied.

The cause was an asymmetry in the loop, not anything about matching. Arrival was
tested on the **one** read taken right after a tap, and the loop settles only
when that read already matches — so a screen that finished drawing a moment
later was missed, and nothing ever looked again. The loop went round, found
nothing left to tap, abstained twice and reported `no_route` while standing on
the destination.

The loop already refuses to declare **arrival** on a half-drawn screen. It now
equally refuses to declare **failure** on one: before any unhappy outcome is
returned, the same criterion is re-asked against a settled read. Re-asking the
same question is what keeps this from being leniency — a criterion that does not
hold still does not hold.

Ablation on the real route, four runs each:

| | reached the destination | of those, reported false |
| --- | --- | --- |
| v0.25.0 | 3/4 | **3** |
| now | 3/4 | **0** |

And the control on the other side, which matters more than the fix: the run that
genuinely did not arrive (one step, destination absent) still reports
`arrived=false` on both builds, and so does a destination that does not exist at
all. A fix that made everything arrive would be worse than the defect.

## v0.25.0

### `goto --dismiss-permission`: one door through the modal guard, and the caller holds the key

Some routes cannot avoid a permission alert. Boxy asks for speech recognition on
the way in, and `simctl privacy` has **no service for speech** — measured,
`revoke all` on the bundle does not suppress it — so "deny it beforehand and
change nothing" does not exist there.

goto still stops at every modal. What this adds is one button it may press, and
**you name it**, because goto cannot work it out. Two detectors were proposed
and both were measured against a real **three-option** alert:

```
role=sheet   "¿Permitir que la app Mapas use tu ubicación?"
role=button  "Permitir una vez"
role=button  "Permitir al usarse la app"
role=button  "No permitir"            <- grants nothing, and it is LAST
```

- **`kTCCService*` in the tree: zero markers on that alert**, in the app tree
  and in the system tree. It identifies some alerts and silently misses others,
  and the ones it misses are the multi-option ones.
- **Position**: the non-granting option was last here and first elsewhere. Two
  of these three buttons grant, so a rule that guesses wrong **grants the
  permission**, and one sample is not enough to bet a permission on.

Matching the button text is language-dependent and worse than it sounds: the
same alert came back in Spanish from an app launched in English, because the app
resolved its InfoPlist strings to `es.lproj`. goto cannot read the label — but
the person running it can. **Declaring it is an instruction, not a heuristic,
and an instruction cannot guess wrong.**

It **fails closed**. If that exact label is not on the modal, goto stops exactly
as before. Matching folds case and accents and nothing else — no prefix, no
substring, no nearest match — because a near-miss on a permission alert is a
granted permission. A declared label naming something destructive is refused
even though you asked for it, which is the one place goto overrules you: `find`
would return it, since its caller reads the answer before anything is tapped,
and goto has nobody between the decision and the finger.

Every dismissal is reported (`dismissed_permission`, `dismissed_action`), at
most three per run, and answering a dialog does not consume a navigation step.

Verified against that real alert, controls first: with no flag, with a label in
the wrong language, and with a substring of a *granting* button, goto refused
all three and touched nothing. With `--dismiss-permission "No permitir"` it
pressed that button and carried on.

## v0.24.0

### Ready for jevi 0.3.0, which stops classifying a choice

jevi no longer attaches a confidence-derived verdict to a `choice` answer —
the upstream fix for the defect that made goto discard correct picks. mav needed
nothing for it, and that is worth saying precisely rather than gratefully:
`find` declines on **two** independent signals, the verdict and the label, and
only the first goes inert. The one that was actually catching abstentions is
still there.

`find` now also treats an ABSENT verdict as "not classified" rather than as a
refusal, so it keeps working against both jevi versions instead of silently
abstaining on every answer the day the new one lands. A verdict that says `no`
or `unsure` is still a refusal; a test asserts both halves.

And the load-bearing part is now documented where someone would delete it: the
service **never abstains on its own**. Given four options where none fitted it
picked one anyway, 3 times out of 3, with low confidence. The abstention exists
only because `none` is on the menu. Removing it looks like tidying and turns
every irrelevant screen into a confident wrong answer.

### `mav goto` arrives, and every tap is 157ms cheaper

Two fixes from profiling a real goto step end to end, and three measured dead
ends recorded so nobody pays to rediscover them.

**goto now arrives.** It was stopping with `no_route` on screens whose
destination was plainly there — Settings → General → Información, one visible
tap away. The row was in the tree AND among the candidates sent to the model
(proven by `mav ui find "Información"` resolving `resolved_by=literal` on that
same screen), so neither scrolling nor the candidate filter was to blame.

The model was picking the RIGHT element and jevi was marking the answer
`unsure`, because its confidence sat at 0.37 — under jevi's own default cut.
mav read the verdict, so a correct answer was discarded. That is mav inheriting
someone else's numeric threshold, which is precisely what it is not supposed to
have.

Measured on one ten-row screen, eight goals whose answer was on it and twelve
whose answer was not:

| | picks the right row | declines when it should |
| --- | --- | --- |
| reading the verdict | 4/8 | 10/12 |
| reading the label | **8/8** | 9/12 |

The verdict cost half the correct answers and bought almost nothing: two of the
three wrong picks carried `verdict: yes` anyway. So **goto reads the choice**,
and the abstention still lives where the model can express it — `none` is an
option, chosen 9 times in 12 when nothing fitted. **`find` keeps reading the
verdict**: its caller taps what it returns with no loop underneath to catch a
wrong lead.

**Every tap is 157ms faster** (1,003 → 846 ms, 7/7 still delivered).
`resolveCapabilities` was 310ms of a 1,194ms tap, and 192ms of that was a
single `idb --version`: idb is a Python tool, starting it costs 116ms, and
every mav command paid it to produce a hint read in exactly two places — a
coordinate tap that has already failed for want of a driver, and `mav doctor`.
Both now ask for it themselves.

### Three dead ends, with the numbers that closed them

So they are not re-attempted. `axe` costs ~615ms of fixed session setup per
invocation and only ~139ms per gesture (measured through its own batch mode:
one tap 755ms, two 894, three 1,276). That block is about 65% of a goto step,
so it was worth attacking three ways:

- **Swapping to idb.** Faster at both and wrong at both: `idb ui tap` is 117ms
  against axe's 601 but **delivered 0/5**, and `idb ui describe-all` is 186ms
  against 462 but returns **13 nodes where axe returns 124**. mav's router
  already overrides `--prefer-driver idb` for coordinate taps, correctly.
- **Configuring axe to skip the setup.** `describe-ui` has no such option, and
  the tap's `--pre-delay`/`--post-delay` are already effectively zero: 821ms
  with defaults against 817ms with both set to 0. The cost is work, not sleep.
- **Fusing the read and the tap into one axe session.** `axe batch` takes
  gestures only; `describe-ui` is not a valid step.

What that leaves is a persistent axe session, and it is now established by
measurement rather than assumed.

Also measured and negative: jev's latency does not depend on how many
candidates it is given — 358ms at 3 candidates, 414ms at 12, 387ms at 20. There
is nothing to win by sending fewer.

## v0.23.0

### `mav goto` — navigate to a screen in one call

Reads the screen, asks which element gets it closer, taps the point it already
resolved, reads again. Until it arrives or gives up.

**Measured on a real simulator**, Settings → General → Idioma y región, two
steps: **7,336 and 7,540 ms against 9,733 and 9,225** for today's
agent+tree+`tap --text`, arriving correctly both times. About **22% faster**,
and that comparison *excludes* the agent's own model turn per step, which
today's path needs and goto does not.

**At one step goto is slower** (5,012 vs 4,474 ms) and that is stated rather
than buried: the saving is per additional step, because goto reads the screen
once per step where the old path reads it twice — once for the agent and once
inside the selector tap.

Arrival is decided by **code**, against the screen's **route** — navigation
title, selected tab, any modal on top — and never by searching free text
anywhere in the tree. `--arrived-when` takes `title:"…"` and `text:"…"` terms,
all required, so a parameterised screen is expressible. A criterion that
**already holds on the screen you start from is refused** rather than reported
as instant arrival. With no criterion it reports `arrived=unverified`, never
true: there is no second model asked to confirm its own work.

It never taps anything destructive, with **no escape hatch** — unlike
`mav ui find`, because nobody reads anything between the decision and the
finger. It stops on arrival, 12 steps, 90s, two taps that changed nothing, a
screen already visited, two abstentions in a row, a modal on top, or a
destructive element in the way. The outcome says which, and the output is
evidence — every step, both routes — not a verdict.

### A row the tree mentions twice is one candidate

A real defect in `mav ui find`, found by the loop refusing to move. iOS renders
a list row as **two** accessibility elements — a container button and an inner
one — with the same label, role and id. Settings → General carries
`Idioma y región` four times and has **not one unique label on the whole
screen**.

Sent to the model as separate options they read as indistinguishable, so the
instruction to decline when two candidates are equally plausible declined on
**every row of every list**. And find's literal path — the one that needs no
key and no network — could never resolve anything there.

They are one row the tree mentions twice. Candidates are now de-duplicated by
identity, with a test that two genuinely different rows sharing a label stay
two.

### The cheaper-way hook suggests `goto` for chained navigation

One more rule, and deliberately narrower than the `find` one. find is worth
suggesting on any tree because it always buys context; goto only wins from the
second step, so nudging a single tap towards it would be advice that makes
things slower. The rule fires on two or more navigation commands issued in one
line, which is the only chain a stateless hook can see — and staying stateless
is worth more than catching every chain, because a per-session counter would be
wrong after a compaction and would nag on every tap.

## v0.22.0

### A coordinate tap and a swipe no longer imply they were delivered

Measured on 2026-09-19, iPhone 17 Pro / iOS 26.3 on a simpool slot: `axe tap -x
364 -y 84` prints `✓ Tap at (364.0, 84.0) completed successfully` and the
accessibility tree is identical before and after — on two different targets, and
`idb`'s own CLI cannot even reach its companion. A tap by selector works on the
same screen in the same second, so the point was right and the HID path is what
swallowed it. mav printed `ok` for all of it.

So `ui tap --x --y` and `ui swipe` now say what they actually know:

- Without `--verify`: `delivered=unconfirmed`, plus a `next` saying the driver
  accepted the gesture and nothing here says the app received it.
- With `--verify`: `verified=changed|unchanged`, and on `unchanged` a `next`
  pointing at the selector path, which works.

**`--verify` is still opt-in and the default is still fast.** Verifying costs a
tree read (323-537 ms measured) and the hot loop is guaranteed not to take one.
That trade is a decision for a person; what is not a decision is a line that
reads like delivery when nothing checked.

**The verification itself was wrong for this job and is fixed too.** It compared
trees with `TreeDiff`, which includes `frame` — and two reads of a perfectly
still screen disagree there by fractions of a point, so a gesture that did
nothing came back `changed`. It now compares a fingerprint of identity only (id,
label, role, sorted), with `frame` and `value` left out, `value` because clocks
and spinners move on their own. Counting nodes is not an alternative: 80 nodes
before a tap and 80 after, with the screen changed entirely, is measured.

First thing it caught, the same hour: a swipe reported broken on one screen came
back `verified=changed` on another. The defect is real and it is not universal —
which is exactly the distinction nobody could make before.

### `mav ui find` now says what it cost

A command that does not say what it cost cannot be optimised, and the figure
everyone quotes for jev — 378 ms against 3.05 s for the large model — lived in a
note on one laptop rather than in anything you could re-run. Now you run the
command and read it off.

Every `find` prints a `cost` block:

| field | what it is |
| --- | --- |
| `total_ms` | the whole answer |
| `tree_ms` | reading the screen; on a real simulator, most of it |
| `model_ms` | the provider round trip, as jev measured it. `0` means no model was asked |
| `local_ms` | what is left: candidate selection, rendering, the vetoes — the only part changing mav can move |

The split is the point: a single `total_ms` mixes a network round trip with our
own work and tells you nothing about which to attack. `model_ms` is read from
jev's own measurement rather than timed around the subprocess, so mav's fork and
exec are not charged to jev.

No token counts, on purpose. `find` asks jev, not a large model, so what it
spends is not where the saving is — the saving is the screen the caller stops
pasting into its own context, and that is measured on the caller's side.

A run that ends in no element still reports what it spent: a find that pays for a
round trip and then vetoes the answer has spent it.

## v0.21.0

### The 80-node cap on `mav ui tree` is gone

v0.20.0 made the truncation announce itself. That was the wrong half of the fix:
a warning is what you need when something is missing, and the answer to
"elements are missing" is to stop dropping them. So the cap is removed, not
described.

**Measured on a real screen** rather than estimated — iOS Settings → General, the
released v0.20.0 binary against this one:

| | node lines | bytes | bytes/node |
| --- | --- | --- | --- |
| v0.20.0, capped | 80 | 8,868 | 110 |
| now | 177 | 19,819 | 111 |

So a dense screen roughly doubles. That is the cost, it is stated rather than
guessed, and it is the reason the `mav ui find` advisory now fires on every tree
instead of above a threshold: the two decisions hold each other up.

**What the cap broke was visibility, not reachability, and the difference is
worth stating exactly.** `mav ui tap --id` queries the driver, not the printed
list, so an element past the cap was always tappable *by someone who already
knew its id*. Nobody did — the only command that hands out ids is the one that
was hiding them. An earlier draft of this entry claimed taps failed; a control
run on a real screen showed the same tap failing identically in both builds, for
ambiguity rather than for the cap, so the claim is corrected here rather than
left standing.

The compact/full split in persisted evidence goes with it: the two files
differed only by the cap, so with the cap gone they were byte-identical. There
is one tree file per step now, and `tree_full_path` is gone from the evidence
record.

There is no `node_more` line any more either. What guards against a cap creeping
back is a test asserting *printed == extracted*, which fails the build rather
than printing a line nobody reads — which is precisely how the original hole
survived.

### The `mav ui find` advisory fires on every tree

The size gate is gone. It stayed quiet under 40 nodes on the reasoning that
reading a dozen elements is cheaper than asking anything — true about one call,
and wrong about an advisory: one that appears on some screens and not others is
one nobody learns, and the agent cannot tell which kind of screen it is going to
get before it asks. The availability gate stays: with no key there is nothing to
recommend.

## v0.20.0

### `mav ui find`, and a tree that stops hiding half a screen

`mav ui tree` prints **at most 80 nodes**, and the line meant to announce that
cut **could never fire**: the list was capped by `Compact` before it reached the
printer that warns above 80, so `i >= maxNodes` was unreachable. Measured live
on an iOS Settings screen at **213 nodes — 80 printed, 133 gone with nothing in
the output saying so**. There were elements no command could show you.

That, and not token thrift, is why this exists: deciding from mav trees is about
**0.7% of a session's spend**, which never justified anything.

**`mav ui find "<what you want to tap>"`** resolves one element from a
description in your own words, reading the uncapped extraction so it can see
what the tree does not print. It answers with an element or with nothing, and
nothing is a real answer — it means read the tree yourself.

Three rules, each from a measurement:

- **No numeric threshold anywhere.** Correct picks score from 0.76 and wrong ones
  reach 0.88, so no cut separates them. What separates them is the model
  declining, so that is what is read: the function that interprets an answer
  **takes no confidence argument at all**.
- **It never returns an element it is unsure of.** It abstains, and the caller
  falls back to the tree it was going to read anyway.
- **A veto in code that can only remove a yes, never add one.** Asked for
  something absent from the screen, the model chose an element anyway **13 times
  out of 26**, so an answer naming something outside the batch is discarded
  rather than looked up. The same veto refuses a destructive element unless your
  own words asked for one.

The exit code carries no answers: 0 whenever `find` could answer at all,
`resolved_by=none` included. `reason` keeps apart the two states callers confuse
— **"could not ask"** (`no_key`, `no_network`, `ci_refused`) and **"looked and am
not sure"** (`abstained`, `veto_*`). It **refuses** to consult a model when `CI`
is set, so "not used in CI" is a guarantee rather than a convention.

Measured live against a real simulator, a real screen and a real model, controls
first: **4 of 4** targets absent from the screen resolved to `none`; **2 of 2**
literal goals resolved with no model at all; **2 of 4** oblique descriptions
resolved correctly and the other two abstained. **Zero wrong answers in 10.** The
abstention rate on oblique goals is real and is not hidden here.

And `mav ui tree` now hands the printer the whole list, so `node_more` fires. The
80-node cap itself is unchanged; it just stops lying about it.

### Where the jev key lives

`MAV_JEV_API_KEY`, then the system keychain (service `mav-jev`), then
`~/.config/bitomule/mav/config.json` at mode 0600 — inside the namespace that
already exists rather than a new top-level directory. Both names sit in **one
line of the code**, so pointing several tools at one shared secret later is a
two-constant change.

`mav jev set-key < key.txt` reads from stdin and never from an argument, so the
secret does not reach shell history. `mav jev doctor` says whether there is a key
and where it came from without printing it. Every `find` that consults a model
reports `key_source=env|keychain|file`, because the alternative is remembering
what you configured. With no key, `find` does not go quiet: it names all three
places it looked and what to type.

mav reads neither musts' key nor jevi's. Inheriting another tool's credential
silently is how "it works on my machine and I do not know why" starts.

### `mav goto` — design only, no code

`docs/design/goto.md`. The loop is not written; what is written is when it stops,
how it knows it arrived without the model grading its own work, how it notices it
is going in circles, and what it never touches.

Worth knowing even if the loop is never built: **`browser-use`, the leading
browser-driving agent loop, does not verify arrival in code.** Its `done` action
returns the `success` the model passed and the loop ends there; its only
independent checker is a second model looking at screenshots, it runs after the
agent has stopped, and its own docstring says it does not override the
self-report. There was nothing to copy.

### A hook that says "that had a cheaper form", because saying it in the docs did not work

A model reads "prefer X" in a skill, repeats it back, and issues the expensive
call anyway: comprehension is not compliance. Measured over **782 real agent
tool calls across 51 sessions**, a cheaper form that was documented, free and
already available was taken **6.9% of the time**, and almost every session that
did take it had the flag written into its task text rather than having read the
documentation.

So the skill now ships one hook, delivered by the same `mav install-skills` that
installs the skill: `skills/mav/hooks/cheaper-way.sh`, wired through a new
`skills/mav/.claude-plugin/plugin.json`. It runs after a Bash call and says one
sentence when the call had a cheaper form that was not used.

One rule today, and the design is a table so the second costs a line:

| You ran | It says so when | Because |
| --- | --- | --- |
| `jevi ask "<question>"` without `-f` | always | jevi's own help says the positional form is "for a one-off from a terminal. Use `-f` for anything you run twice", and an agent's questions are always run twice |

**The rule for `mav ui tree` points at `mav ui find`.** An earlier draft pointed
at `mav ui tree --agent` and that flag was rejected as the form to recommend: it
caps the screen at **40 elements with no flag to raise the cap** and it **ranks
after capping**, so its ordering cannot rescue an element that already fell
outside the 40. A rule pointing at a form we do not recommend is worse than no
rule, so it was removed. `find` is a form we do recommend, so the rule is back
pointing there, behind two gates:

| gate | fires when | because |
| --- | --- | --- |
| size | 40 or more printed nodes | with a dozen elements, reading them is cheaper than asking anything, and advice that costs more than it saves is worse than silence |
| truncation | a `node_more` marker, whatever the printed count | elements were not shown at all, so `find` is not a cheaper way but the only way to see them — this overrides the size gate |
| availability | a key exists in any of the three places | without one `find` answers `resolved_by=none` and the advice is noise. Checked by asking whether a key EXISTS, never by reading one and never over the network: the variable, the keychain queried without `-w` so no secret is fetched, then the file's presence |

Measured at **40 ms worst of five runs** against the `timeout: 2` pin; the
keychain query is local and adds nothing worth noticing.

**It says it every time, and keeps no state.** No counter, no per-session file,
nothing that can go stale. An earlier draft said it twice and then every tenth
call, out of a worry about noise; that was the wrong worry. If a cheap documented
form exists and the expensive one is used, that is a mistake, and a mistake does
not stop being one on the third repetition. Being stateless is the bonus: there is
nothing left in the script that can be wrong about what happened earlier.

What it deliberately cannot do, each for a reason we have already paid for:

- **It never blocks and never rewrites.** It is on `PostToolUse`, which can do
  neither, and that is why it is there. A mechanism that can rewrite a command
  will eventually rewrite it into a cheaper form that quietly drops data the
  caller needed; a sentence cannot.
- **It emits no `permissionDecision`, "allow" included** — that would
  auto-approve the call and walk past the user's own ask/deny rules. A test
  asserts its absence rather than trusting review.
- **`timeout: 2`, explicit.** The default hook timeout is 600 seconds; on a hook
  that runs after every Bash call that is ten minutes of a wedged session. It
  exits 0 on every path, uses no network, and runs no `mav`, no VCS command and
  no build.

## v0.19.3

### The target override that was documented and did nothing

`MAV_TARGET_UDID` on its own did not pin anything. The whole environment
overlay hung off `MAV_TARGET_KIND` being set, so the three companion
variables were only read when a kind was set beside them — an undocumented
dependency between two variables the docs describe as independent. SKILL.md
has said since v0.18 that these "pin the target, beating both a config pin
and `target_command`".

Measured on the released 0.19.2 and on this fix, in a project with
`target_command` configured, reading `mav doctor`:

| What is set | 0.19.2 | v0.19.3 |
| --- | --- | --- |
| nothing | `target_source=target_command udid=CMD-AAAA-1111` | unchanged |
| `MAV_TARGET_UDID=ENV-BBBB-2222` | `target_source=target_command udid=CMD-AAAA-1111` | `target_source=env udid=ENV-BBBB-2222` |
| `MAV_TARGET_KIND` + `MAV_TARGET_UDID` | `target_source=env udid=ENV-BBBB-2222` | unchanged |
| `simulator_udid` pinned, nothing exported | `target_source=config udid=PIN-CCCC-3333` | unchanged |
| `simulator_udid` pinned + `MAV_TARGET_UDID` | `target_source=config udid=PIN-CCCC-3333` | `target_source=env udid=ENV-BBBB-2222` |

The consequence on a machine where several agents lease slots was not an
error message: the command ran, reported `ok`, and drove the simulator
`target_command` had returned — someone else's.

**Both halves of the resolution were wrong, and fixing only the first would
have been worse than fixing neither.** `resolveConfigTarget` decided the
`target_source` label by re-reading `MAV_TARGET_KIND` too, so applying the
overlay alone produced the right UDID under `target_source=config` — a
target from the environment, attributed to a file that never named it, on a
green line. That field exists precisely so nobody has to compare identifiers
by hand, so a wrong provenance is the one failure it cannot afford.

`MAV_TARGET_NAME` and `MAV_TARGET_RUNTIME` are now documented for what they
are: they narrow the reported target but do not select one, because nothing
in mav resolves a name or a runtime to a UDID. On their own the target still
comes from a pin, `target_command`, or the booted simulator.

### `--target` outside `mav run` is refused instead of ignored

`--target` is a `mav run` flag and nothing else reads it. Every other command
accepted it and carried on: `mav ui tree --target udid=30F898A9` inspected
whatever the config resolved to and printed a clean `ok` for it — a wrong
answer in the shape of a right one. Any command but `mav run` now fails with
`code=flag_unsupported`, naming `MAV_TARGET_UDID` as the spelling that works
everywhere.

This is narrower than the defect behind it: mav has no central flag parser
and does not reject unknown flags anywhere. Doing that properly needs a table
of every flag of every command, which is not a patch-release change.

### `selector_ambiguous` now says how to choose

Refusing to tap when a selector matches several elements is correct. Refusing
without saying what to do next is what stalled four separate agents on the
same screen in one day: iOS Settings lists "Pantalla y tamaño del texto" four
times, and the advice on offer — use `--id`, or a longer `--text` — has no
answer there. A label the screen repeats verbatim has no longer spelling, and
the cells expose no id.

The refusal now reports how many elements matched and names `--index N`,
which mav has had all along and which nothing pointed at.

### Remediations stop arriving HTML-escaped

`mav sim select <udid>` is not a command. Every quoted field went
through `json.Marshal`, which escapes `<`, `>` and `&` for embedding in a web
page — including the remediation of `ambiguous_booted_simulator`, the
most-read failure of this release line and one whose whole job is to be typed
back. Quoting is otherwise unchanged and the output is still valid JSON.

## v0.19.2

### The config error v0.19.1 produced and then threw away

v0.19.1 made an unrecognised key in `.mav/config.yaml` fail the load. **Not
one line of output ever said so**, which is worse than the silence it
replaced.

Measured on the released 0.19.1, same machine, same version, the only
difference being the file:

| Command | Config with a legacy `tools:` section | Same file without it |
| --- | --- | --- |
| `mav doctor` | `ok ... launch_recipe=missing`, no `udid` | `ok ... launch_recipe=ok`, `udid=…` |
| `mav ui tree` | `fail code=config_not_found next="mav setup"` | `ok` |

Two separate swallows:

- **Every command flattened any load error into `config_not_found next="mav
  setup"`.** That was already wrong for a bad profile or an invalid
  `target_kind`, and here it named the opposite of the problem — the file
  was there and one key from correct — while pointing at the command that
  rewrites it. Each reason now reports its own code:
  `config_unknown_key`, `profile_unknown_key`, `profile_not_found`,
  `target_kind_invalid`, `vm_unsupported_target`. `config_not_found` now
  means the file really is absent.
- **`mav doctor` discarded the error outright and answered `ok`.** With an
  unloadable config every field it printed described a project mav did not
  know — no launch recipe, no bundle id, no `target_command` to ask the pool
  for a slot — and the status line said everything was fine. doctor still
  prints the whole diagnosis, because that is what it is for, but now under
  `fail code=<the load error>`.

The one case that stays `ok`: **no config file at all.** Running `mav
doctor` before `mav setup` is how you find out which tools you are missing,
and an absent config is not a broken one.

With a config carrying `tools:`, `mav doctor`, `mav ui tree` and `mav open`
now all exit non-zero with `code=config_unknown_key key=tools`. There is no
longer any way to get an `ok` out of that file.

## v0.19.1

Two defects with the same shape: **mav acted on something that was not what
you thought, and said `ok`.** Both were found in one afternoon on a machine
running a dozen agents, and both had been invisible for exactly that reason —
success was reported either way.

### A tap that reported success and never landed

`mav ui tap --text` was navigating about half the time, and reporting `ok`
every time. It is not mav's code and it is not the iPhone Duo, where it was
first noticed: AXe's `tap` has a `--tap-style` whose default, `automatic`,
routes anything that is not a switch through FBSimulator's `tapAt`. **`tapAt`
drops the touch under load and still exits 0**, printing
`✓ Tap ... completed successfully`.

Measured on an iPhone 17 Pro / iOS 26.3 from a pooled slot, tapping the same
Settings row from a clean launch each time and counting accessibility-tree
nodes before and after (135 on the root screen, 188 after navigating):

| `--tap-style` | Navigated |
| --- | --- |
| `automatic` (AXe's default, what mav sent) | 6 of 12, then 0 of 8 |
| `simulator` (`tapAt`, explicit) | 2 of 10 |
| `physical` (touch down/up) | 10 of 10 |

All 30 invocations exited 0 and printed success. End to end through
`mav ui tap --text`, same simulator, same minute: **0 of 10 before, 10 of 10
after.**

mav now passes `--tap-style physical` on every semantic tap, which makes
**AXe 1.8.0 the version floor**.

Two things this also explains, both reported as separate bugs:

- `ui tap --id` failing to find an identifier `ui tree --agent` had just
  printed. The tree was read before a tap that silently evaporated, so it no
  longer described the screen.
- "semantic taps are broken, coordinates work". `axe tap -x -y` failed 8 of 12
  in the same session — the comparison that looked decisive was AXe-semantic
  against **idb**-coordinates, which are two different pipes. What fails is
  `tapAt`, whatever names the point.

### A simulator nobody chose

With several simulators booted and nothing naming one, target resolution
returned the first entry of a `range` over simctl's runtime→devices map. Go
randomises map iteration, so two consecutive commands could drive two
different devices — and the only trace was the `udid=` field.

Measured with three booted: **ten consecutive resolutions in one project
picked two different devices, nine of them a slot another agent had leased.**
`ok` all ten times.

- `ambiguous_booted_simulator` refuses the choice and names the candidates.
  "Booted" is all mav knows about any of them, so there is no criterion here
  that picks correctly; saying which one to use costs one command, and a
  measurement taken on the wrong device costs however long it takes somebody
  to notice. One booted simulator is still an unambiguous answer and still
  works with no configuration at all.
- `mav doctor`, whose job is to diagnose rather than dispatch, reports it in
  `target_command_warn` instead of failing.

Nothing changes for a project that pins `simulator_udid`, sets
`target_command` (the pool-manager hook `simpool lease` plugs into), or runs
under `MAV_TARGET_UDID`.

### Every `ok` line says where the target came from

Reading which simulator mav used off a UDID means comparing identifiers by
hand, which is the step everybody skips. Success lines now carry
`target_source=`: `env`, `config`, `target_command`, `booted`, or
`localhost`. **`booted` is the only value that means mav chose the device
itself.**

### An unrecognised key in `.mav/config.yaml` is an error

`simulator: {udid: ...}` instead of `simulator_udid: ...` used to load clean:
YAML decoding drops what it does not recognise, so mav resolved the target as
if nothing had been configured and reported `ok`. That is how the wrong
simulator got driven in the first place. A config file ignored in silence is
worse than no config file, because whoever wrote it believes it is in effect.

`config_unknown_key` now names the key and lists every valid one, at the top
level as it already did inside a profile. The known keys are read off the
config struct's own tags, so the list cannot drift.

**The one migration:** delete the legacy `tools:` section if your config
still has one. Tool detection has been a run-time probe for several releases
and that section has had no effect since.

## v0.19.0

### `mav sim language` — the iPad status bar stops shipping in the wrong language

**An App Store screenshot of an iPad shows the date, and the date was in the
simulator's language.** Boxy's published English iPad screenshot reads
`Lunes 7 de septiembre`; it had been on the store for several versions. The
app's language is a launch argument (`open: { language: en }`) and reaches one
process — the status bar is drawn by SpringBoard, which follows the SIMULATOR's
language, and nothing in the pipeline was setting it. iPhone captures hid the
defect because an iPhone status bar has no date on it.

`--preset appstore` was never the cause and is not the fix: it sets the clock,
the battery and the signal bars, none of which carry a language.

```yaml
- sim.language.set: { language: "${params.language}", locale: "${params.locale}" }
- sim.statusbar.set: { preset: appstore }
```

Measured on iPad Pro 13-inch (M4) / iOS 26.3, with the ablation both ways:
`es-ES` → `Viernes 18 de septiembre`, `en-US` → `Fri Sep 18`, back to `es-ES` →
the Spanish date returns.

Three details that are the whole reason this is a command and not a line of
shell:

- **A bare subtag falls back to English, silently.** `--language fr` is accepted
  by simctl and produces an English status bar. It is refused, both at flow-lint
  time and at run time — unless `--locale` carries the region, so the
  `language=de locale=de_DE` pair every screenshot script already passes builds
  `de-DE` on its own.
- **SpringBoard has to restart, and the capture has to wait for it.** A fixed
  sleep is either too long on every cell of the matrix or too short on the one
  cold run that matters, and the short one produces a screenshot in the previous
  language while reporting `ok`. It polls for the job's pid instead (~5s).
- **Setting a language it is already on does nothing**, so a three-language
  matrix pays the restart three times, not once per capture.

It also fixes what nobody had looked at yet: the 12h/24h clock, the date order
and the separators all come from the same setting.

## v0.18.1

### `ui longPress` holds through idb, and is no longer refused on a device

**The hold reached idb and was thrown away.** `uiLongPress` computes its 800ms
default, puts it in `TapSpec.Duration` and routes; the idb driver built
`idb ui tap X Y` and never looked at the field. What went out was an ordinary
tap, so the command reported `ok` while SwiftUI's `onLongPressGesture` never
fired — the most expensive kind of green, because nothing in the output said
anything was missing.

The trap in fixing it is that the two sides disagree on units: MAV carries a
tap hold in **milliseconds**, `idb ui tap --duration` reads **seconds**
(measured, not assumed: `--duration 4` takes 4.4s of wall clock). Passing the
field straight through would have asked a simulator for an 800-second press.
It is converted, the way baguette's driver already converted it, and a tap
with no hold still goes out with no `--duration` at all so idb's own 0.05s
press is not replaced by an instantaneous one.

This was only ever reachable when idb won the route, which on a simulator it
does not: baguette ties on cost, wins on name, and has always sent the hold.
The path that was broken is the one a physical device has — and a device could
not get there at all, because:

**A long press is not multitouch.** It was gated with pinch, rotate and
two-finger pan behind `gesture_unsupported_on_device` ("use sim for
multitouch"), which is the wrong company: one finger goes down and comes back
up, and `idb ui tap --duration` holds on a device exactly as it does on a
simulator. The gate is gone, and `mav ui longPress` now routes normally on a
device, where idb is the only tap driver there is.

**The result line named the wrong tool.** `driver=baguette` was a string
literal, printed whatever actually ran — including on a device, where baguette
declares no capabilities and cannot touch the target. It now reports the driver
the router picked.

Verified by ablation against a SwiftUI `onLongPressGesture(minimumDuration:
0.5)`: through idb, 0 of 5 runs opened the gesture's sheet before the fix and 5
of 5 after, with the dispatch going from 0.2s of wall clock to 1.1s.

## v0.18.0

### Every coordinate gesture is rotation-aware, and MAV can rotate the simulator itself

v0.17.0 compensated `ui tap` and `ui swipe` for a rotated simulator and said
plainly that the rest were not covered. They are now, and the reason the gap
existed turned out to be smaller than the reason it was hard to close.

**The gestures.** `longPress`, `pinch`, `twoFingerPan`, `doubleTap`'s worker
fast path, `drag` and `dragPath` all dispatch through baguette, whose
coordinate space was never measured. It was measured: baguette consumes the
same native-portrait points idb does — the same transformed point lands the
same element through both — so they now take the same transform, reported the
same way. An anchor rotates as a position; a pan delta rotates as a
*displacement* (only the axis swap, no screen-width translation), because
rotating it as a point would send the gesture off the surface. `drag`'s two
endpoints, and every point in a `dragPath`, rotate atomically — one endpoint
rotated and the other left raw would turn a straight drag into a diagonal
one, so a disagreement (a bounds-guard trip on one point) sends the whole
gesture out raw instead.

`ui rotate` gets the same treatment for the day it works: baguette's
`Provides()` still excludes `CapRotate`, so the router picks nothing for it and
the command cannot dispatch at all.

**Direction swipes are axis-compensated.** `ui swipe --direction up` used to
send fixed portrait-space constants whatever the screen was doing, so on a
rotated simulator it dragged sideways — and `ui scrollUntil`, which only ever
swipes by direction, burned every attempt and reported a timeout. The endpoints
are now re-derived as fractions of the rotated screen and rotated like any
tree-space pair, so "up" is up on the screen you are looking at. The result
line says `direction_endpoints=derived` and carries the dispatched
`hid_start`/`hid_end`. When no screen size can be resolved the constants still
go out as written and `rotation_unavailable=` says so.

**`mav ui orientation <portrait|landscape-left|landscape-right|portrait-upside-down>`.**
Only two things leave a trace MAV can read about a rotation: Simulator.app
writes a window angle to a user default, and this command writes its own
record. A simulator turned by `baguette orientation`, a raw GSEvent, or an app
that is landscape-only leaves neither — and the accessibility tree alone cannot
say *which* of the two landscapes is in effect. They differ by 180°, so a guess
puts every tap in the diagonally opposite corner.

So this is not a convenience wrapper. On a headless boot — every `simpool` run,
where Simulator.app has no window to have an angle at all — it is the only way
coordinate gestures stay correct. A declaration outranks the window angle,
survives across commands, is dropped if the rotation itself failed, and
invalidates the screen-size cache so the next gesture re-probes. Result lines
carry `rotation_source=mav` or `rotation_source=window` so it is visible which
one was believed.

`portrait-upside-down` is applied but never compensated, and says so: an
upside-down tree is portrait-shaped exactly like an app that refused to flip.

Where a tree has already been read for another reason (a selector tap, a
`--verify` snapshot) and it is landscape while nothing claims a rotation, the
result now carries `rotation_unavailable=unknown_landscape` and points at this
command. It is not checked on a bare `--x/--y` tap, because reading a tree per
tap would cost seconds in the hot loop.

**AXe 1.7+ refuses rotated coordinate gestures, so MAV routes around it.**

```
Error: Unable to determine rotated simulator orientation. AXe can read
landscape coordinates only when SimulatorKit reports the current UI
orientation; the screenshot confirms the simulator is rotated, but the
private orientation probe was unavailable.
```

That is a correct refusal — better than dispatching into the wrong space — but
SimulatorKit does not report the orientation on a headless boot, which is every
`simpool` run, and MAV already knows the rotation and has applied it. AXe is
canonical (cost 0) for `CapSwipe`, so *preferring* another driver was not
enough: it is taken out of the running for the call, and the result line says
`rotation_rerouted=axe`. Same for `longPress`, which dispatches as a coordinate
tap and lost the alphabetical tie to AXe. Unrotated runs still route to AXe.

### The baguette swipe driver sent flags baguette rejects

`baguette swipe` takes `--start-x/--start-y/--end-x/--end-y`; MAV sent
`--startX/--startY/--endX/--endY` and got

```
Error: Missing expected argument '--start-x <start-x>'
```

Nobody had seen it because AXe won every swipe route, so the path was
unreachable — until a rotated simulator started routing around AXe. The unit
test that covered it was named `TestSwipeUsesCamelCaseFlags` and passed,
because the fake executor accepts any arguments; it now pins the spelling the
real binary accepts and fails on the old one.

## v0.17.0

### Coordinate gestures land where the tree says, on a rotated simulator

After rotating a simulator to landscape, `mav ui tap --x --y` kept dispatching
into the device's **portrait** point space while `mav ui tree` reported the
rotated one — the only place a caller gets coordinates from. A tap read off the
tree and handed straight back to mav landed somewhere else on screen, or on
nothing at all. Every flow that ran in landscape had to carry the rotation by
hand, which is a transformation that belongs to MAV.

Simulator.app rotates the window, not the touch surface: idb and axe dispatch
HID events in the device's native portrait space whatever the window is doing.
MAV now reads the rotation Simulator.app applied (a per-device user default,
~13ms) and rotates coordinate gestures into that space before dispatching.

- **Nothing changes when nothing is rotated.** An angle of 0 — every headless
  run, every simulator nobody rotated — is the identity, costs one `defaults
  read`, and neither touches the result line nor reads the tree.
- When a rotation is applied, the result line carries `rotation=90|270` and
  the `hid_x`/`hid_y` the gesture actually went out at, next to the `x`/`y` you
  asked for. A 180 rotation is never applied: an upside-down tree is
  portrait-shaped just like an app that never flipped, so there is no way to
  prove which space the coordinates are in, and most apps (and SpringBoard on
  home-button-less iPhones) never rotate to upside-down at all. Coordinates
  go out untouched with `rotation_unavailable=180`.
- The device's portrait size is probed from the accessibility tree once per
  UDID and cached in `.mav/screens/`. Only rotated runs ever probe it. The
  probe checks the tree's own shape against the angle before trusting it — a
  portrait-locked app under a rotated window is not rotated — and the cache
  records the angle it was probed under, so a rotation *change* re-probes.
  A cache hit at the same angle does not re-check the foreground app, so a
  screen that went portrait-shaped after the probe can still get its taps
  rotated; `ui tap --verify` closes that for free by checking the shape of
  the snapshot it already reads and downgrading a contradicted rotation to
  `rotation_unavailable=` with a raw dispatch.
- A rotation that would send the gesture off the touch surface is never
  reported as applied. That is proof the point was not in the space the angle
  claimed, so the coordinates go out untouched with `rotation_unavailable=`
  instead of an `ok` carrying a negative `hid_x`. For a swipe the two
  endpoints are atomic: if the guard rejects either one, both are dispatched
  raw — one rotated and one raw endpoint would turn a vertical drag into a
  diagonal one.
- `ui swipe` now requires all four of `--start-x/--start-y/--end-x/--end-y` or
  none of them: `swipe_coordinates_incomplete`. Each endpoint left out kept a
  direction default, which is a portrait-HID-space constant, and the rotation
  gate then transformed it as though the caller had read it off the tree.
- `ui swipe --direction` (and therefore `ui scrollUntil`, which only ever
  swipes by direction) still dispatches its fixed portrait-space defaults
  unrotated — and now says that on a rotated simulator the drag is **not**
  axis-compensated, so an "up" swipe runs sideways. It carries
  `rotation_unavailable=` and a `next` pointing at explicit coordinates rather
  than returning a bare `ok` for a gesture that did nothing.

**Breaking for anyone already compensating by hand.** A flow that pre-rotates
its own coordinates for a landscape simulator will now be rotated twice. Remove
the manual compensation and use the coordinates `mav ui tree` reports. No flow
in this repo's examples did this, and a sweep of the flows in the repos this was
reported from found none either — the compensation was being done interactively.

Covered: `ui tap` (and therefore every selector tap, which resolves to
coordinates) and `ui swipe`, both endpoints. **Not** covered: gestures that go
through baguette — `longPress`, `pinch`, `rotate`, `twoFingerPan`, and
`doubleTap`'s worker fast path. Their HID space was not measured, and
half-fixing `doubleTap` so it behaves differently depending on whether the
worker is up would be worse than leaving it.

### A selector tap no longer dies with the tool's decoding error

`mav ui tap --id X` and `mav ui tap --text X` failed with
`Expected to decode Dictionary<String, Any> but found an array instead` for
elements `mav ui tree` had just listed with their id and label, while a
coordinate tap on the same element worked every time. The cause is upstream and
narrow: AXe below 1.7.0 cannot decode an accessibility tree that carries a
numeric `AXValue`, so a single slider anywhere on screen breaks selector
resolution for *every* element on that screen. Reading the tree is a different
code path in AXe and stays fine, which is why the two disagreed.

MAV now retries through the tree when the tool's own selector resolution fails:
it resolves the element from the tree it can already read and taps the centre of
its frame. The result line says so — `selector_via=tree`, the tool's error in
`selector_error`, and `next` pointing at `brew upgrade cameroncooke/axe/axe`
when the failure is that decoding bug. The semantic path is still tried first,
because AXe reaches a few elements the tree does not expose (SwiftUI `TabView`
items among them); when the tree cannot resolve the selector either, the tool's
original failure is reported unchanged.

### A skipped optional step stops passing for a done one

An `optional: true` tap that failed was swallowed whole: the run recorded
`skipped=true` with no reason, the step went into the trail as `status: ok`, and
the flow finished green counting it as done. A flow that dismissed no banner
read exactly like one that did.

Optional taps now go through the same failure policy as every other optional
step, which records why the step was skipped. Skipped steps are recorded as
`status: skipped` rather than `ok`, `run.json` lists them, and the pass line
carries `skipped=N` with `skipped_steps=<step>:<action>` so a green run says out
loud what it did not do. The fail line carries the same two fields, so a run
that skipped something before it broke does not read like one that did not.

`skipped=N` always means top-level steps. A `when` or `whileNotVisible` step
that skipped an optional child of its `do:` block reports `skipped_children=N`
on its own record instead, and its `executed=N` now counts only the children
that ran -- `when` used to report the whole block as executed.

## v0.16.6

### The launch recipe's environment prefix reaches the app

A recipe written as `FOO=bar xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"`
was routed to the simctl driver, which read the bundle id and nothing else: the
assignment was dropped without a warning, a failure or a line in the evidence.
The app started, so whoever wrote it had every reason to believe the variable
had arrived, and then read the app's unchanged behaviour as an app bug. The
cost of that silence is a whole session; the variable had to be delivered by
relaunching by hand with `SIMCTL_CHILD_*`.

The prefix is now parsed and carried into the app, translated per target:
`SIMCTL_CHILD_*` on a simulator, `IDB_*` on a physical device, the process
environment on macOS. Values may refer to the recipe's own `MAV_*` variables
(`OUT=$MAV_RUN_DIR/out`). The run's commands trail names what was passed —
`launch.launch driver=simctl env=FOO` — and never the values, because evidence
gets pasted around and a recipe can carry a token.

What cannot be delivered fails instead of being half-obeyed. On a physical
device a variable named after one idb reads for itself (`UDID`, `COMPANION`,
`COMPANION_TLS`) would retarget idb rather than reach the app, so it is refused
by name. A value using command substitution is refused too: the driver path has
no shell, so shipping it would deliver the text of the command. A launch line
that parses as nothing but assignments — one missing quote does it — fails with
`launch_command_only_env` instead of launching the bundle as if the command had
run. Where the prefix reaches the shell rather than a driver and MAV can tell
the drop is certain, the run carries a `launch_env_not_translated` warning.

A prefix on `install` is not translated at all: those variables are for the
install tool, so that step goes to the shell, where they mean exactly what they
say. Its values are redacted in the trail, same guarantee as the launch path.

Reproduced end to end against a real simulator with an app that dumps its
environment: before the fix the app saw nothing and the trail said
`launch.launch driver=simctl`; after it, the app sees `FOO=bar` and the trail
says `env=FOO`.

## v0.16.5

### Recording video inside a flow works again

Two independent defects meant that a flow which captured anything before it
started recording produced no usable video, and neither of them said why.
Both were reproduced against real simulators before and after the fix.

**`video.start` refused any run that already had a screenshot step.** The
cleanliness check `evidence start` runs treated `evidence.jsonl` and `steps/`
as leftovers from a previous session, so the documented shape of an evidence
flow — capture the navigation that gets the app into position, then record the
behaviour actually under test — failed at the recording step with
`evidence_run_not_clean`, surfacing through the flow runner as a bare
`video_start_failed` in under a millisecond. The standalone command worked
because a fresh run has no steps yet, which is exactly why this looked like a
flow-only bug. Present since the check was introduced; it is not a regression
in v0.16.4.

- The check now covers only what actually conflicts with a new recording: a
  recorder already running for this run, or a video file already on disk. A
  simulator has one recording slot and simctl will not overwrite an existing
  file; screenshots contend for neither. Steps captured before the recording
  keep a zero video offset, which is what "this happened before the video"
  should render as.

**`evidence start` reported success before the recorder was recording.**
`Start()` only reports that the process was forked, and `xcrun` reaches
`simctl` through a shell wrapper that resolves the active developer directory
first — measured at over four seconds on a machine busy with builds. Whatever
the flow did in that window was recorded by nobody: a short step left an empty
`video.log` and no file at all, a five-second step produced 658ms of video.
Standalone use hid it because whoever types the next command spends those
seconds anyway.

- `video.start` now waits until simctl reports that it is recording, bounded
  at 30 seconds, and fails the step if the recorder reports an error instead —
  killing it rather than leaving it holding the simulator's recording slot.
  An occupied slot ("Host recording is already in progress") now fails at
  `video.start`, where it is actionable, instead of at the end of a run whose
  evidence is already lost.

**The failures said nothing.** The evidence steps discarded the command's own
`fail code=` line and flattened every cause into the same opaque step code, so
`video_start_failed` could not be told apart from "this target has no
recorder"; and the recorder log's failure was reported as its first line,
which simctl always fills with a "Note: No display specified" preamble that
says nothing is wrong.

- `video.start`, `video.stop`, `evidence.start` and `evidence.stop` now carry
  the inner failure as `detail=`, the same way the `open` step already did,
  and the recorder log reports the line that actually names the error.

## v0.16.4

### An interrupted run no longer strands its build, its logs and its simulator

Two independent defects turned an agent dying mid-run into a machine paying
for it all night. Both were measured on a Mac with 288 iOS runtime processes
alive across two simulators nobody was using, 19.3 GB of swap in use, and a
`mav run` that had been alive for 6h46m against 4.46s of CPU.

**The run worker never started inside a git worktree.** Its Unix socket lived
at `<run dir>/worker.sock`, and in a worktree
(`.../.claude/worktrees/<branch>/.mav/runs/<id>/worker.sock`) that path
measured 106 bytes against the 104 the platform allows. Every startup failed
with `bind: invalid argument`; MAV degraded to "direct" mode, logged one line,
and carried on. The worker is what watches a run's lease and reaps the run
when nobody renews it, so in the layout every agent actually works in, the
only cleanup mechanism MAV had was silently absent.

- The socket now falls back to a short, per-run path under the system temp
  directory whenever the natural one would not fit, and stays where it was
  when it does.

**An `exec` step leaked its grandchildren and then hung on their pipes.**
The step ran `/bin/bash -lc` without a process group of its own, so its
timeout signalled only the shell: `make` and the bazel client underneath it
survived, reparented to launchd. Worse, with the step's stdout and stderr
going to pipes and no `WaitDelay`, `Wait` read those pipes until EOF — which
those same orphans never gave. The timeout could not rescue the step it was
there to bound. A hung run then kept renewing its simulator lease every 60
seconds for as long as it existed, which is why a pool manager doing exactly
the right thing still never got its slot back.

- The step now runs in its own process group, cancels by signalling the whole
  group, bounds how long it will wait on pipes it no longer owns, and kills
  whatever is left of the group once it is over — including after a shell
  that exited cleanly while leaving a background process running.

## v0.16.3

### A failing `target_command` is an error, not a different simulator

`target_command` had a 10 second timeout. The one consumer it was designed
for -- `simpool lease`, which blocks on `xcrun simctl bootstatus` -- takes
about two minutes on a cold lease. Every cold lease therefore timed out, and
a timeout resolved to *whatever simulator happened to be booted*. The warning
that said so was returned as a string that nearly every call site discarded,
and any caller redirecting stdout (a screenshot script piping `mav run` to
`/dev/null`) never saw it at all. The failure was then cached for two
minutes, so the next run failed the same way without retrying.

Net effect: MAV drove the wrong simulator, silently. For a capture that means
publishing images from an unintended device; for a validation it means
evidence describing a device nobody chose.

- A configured `target_command` that exits non-zero, prints nothing, or
  exceeds its timeout now **fails the command**. Three codes, because the
  next step differs: `target_command_failed`, `target_command_timeout`,
  `target_command_empty`. Each names the command, the timeout it was given,
  the underlying detail, and carries `fallback=none`.
- `target_command_required: false` in `.mav/config.yaml` is the explicit
  opt-out, restoring the previous warn-and-fall-back behaviour. Unset means
  required: nobody keeps the silent behaviour by inaction.
- New `target_command_timeout`, a Go duration, default `3m` -- past the
  documented cold-lease cost, and equal to simpool's own default lease TTL
  rather than under it. A malformed value fails as
  `target_command_timeout_invalid` rather than silently reverting to the
  default.
- Failures are cached for the run only under the opt-out, where the run
  carries on and would otherwise pay the timeout on every command that
  follows. A required failure is never cached: the command already exited,
  and caching it made the next run fail on evidence it never re-tested.
- Every `resolveConfigTarget` call site now handles the error instead of
  discarding it. `mav open`, `mav ui *`, `mav capture`, `mav crashes`,
  `mav evidence start|step`, `mav sim boot`, `mav time`, `mav app`,
  `mav openURL`, `mav location`, `mav clipboard` and every flow step that
  dispatches a UI action report the same failure shape.
- Three exceptions, each deliberate. `mav doctor` reports the failure as
  `target_command_warn` and still produces its diagnosis: it is the command
  you run *because* the target is broken, and refusing to run would withhold
  the tool and driver state at exactly the wrong moment. `mav sim select`
  does not resolve `target_command` at all -- pinning `simulator_udid` beats
  it in the precedence order and is the documented escape from a broken pool
  manager, so failing there would close the only exit. `mav evidence stop`
  reports it too rather than failing: everything after that point is
  teardown that cannot be retried, and a recording whose index never gets
  written is unrecoverable.
- A failure that kills a `mav run` now leaves evidence: a `commands.jsonl`
  entry with a non-zero code, a `run.json` marking the run failed, and the
  report. A non-zero exit with nothing on disk would only move the problem
  for the script that pipes mav to `/dev/null`.
- `mav help target_command` documents the field, the two knobs and the three
  codes.

The keepalive ping `mav run` sends to a pool manager stays non-fatal: it is a
liveness signal, not a resolution, and the run's target was already fixed
before the first step.

### The `target_command` timeout now actually bounds the wait

`exec.CommandContext` kills the direct child only. `target_command` runs
through `/bin/bash -lc`, whose grandchild inherits the output pipe, so
`cmd.Wait` stayed blocked on a pipe nobody would close and MAV waited out the
grandchild instead of its own timeout -- the exact hang the timeout exists to
prevent. `execTargetCommand` now returns on its own deadline rather than
waiting for the runner.

Deliberately scoped to `target_command` and not applied to `Runner.Run` as a
whole: a launch recipe that backgrounds a helper holding stdout (`./mock-api
&` before `simctl launch`) is a perfectly ordinary recipe, and a global
cutoff would have started failing it.

### `MAV_EXACT_RUN_DIR` and `--skip-build` are documented

Neither appeared in any markdown file in the repo, so agents kept
rediscovering them from the source. `skills/mav/SKILL.md` now documents
`MAV_EXACT_RUN_DIR` (supported, internal: pins a run's state directory, used
by `mav run --target` for each matrix child) and records that
`MAV_SKIP_BUILD` was removed in v0.16.2 -- `--skip-build` is the supported
spelling.

## v0.16.2

### `--skip-build` reuses an app that is already built

The launch recipe ran its `build` step on every `open`. For a project whose
build is a cold Bazel or Xcode build -- ten to twenty minutes is normal -- a
localized App Store matrix paid for that build once per language, rebuilding an
artifact that had not changed between runs. There was no flow restructuring that
avoided it: the language is a launch argument, so each language is its own
invocation.

- `mav open --skip-build` skips the recipe's `build` step. `app_path`, `install`
  and `launch` still run, so the app is still resolved, installed and launched.
- `mav run flow.yaml --skip-build` applies it to every `open` step the flow
  dispatches, including the ones that do not mention it. Build once, then run the
  matrix per language without editing the flow per invocation.
- `open: { skipBuild: true }` marks a single flow step, for a flow that builds in
  its first `open` and reuses it in the later ones.
- It is applied to the recipe's `build` step rather than to any one build system,
  so it works for every `launch.mode` -- with the caveat that
  `mode: already_installed` has neither a `build` nor an `app_path`, so there it
  is a no-op rather than a saving.
- `--skip-build` is rejected together with `--no-relaunch`, like `--clear-state`
  and `--fixture` and for the same reason: `--no-relaunch` skips the whole
  recipe, so there is no build to skip.

When nothing was ever built, `app_path` has nothing to resolve. `mav open`
returns `build_skipped_app_missing` -- naming the skipped build and pointing at
`rerun without --skip-build` -- instead of whatever the project's Makefile
printed on its way out. The same code covers an `app_path` that prints a path
which is not on disk, which is what a stale recipe looks like when the build
never ran, and the run's `commands.jsonl` records a `launch.skip_build_check`
entry naming the path MAV looked for.

Inside a flow, an `open` step that fails now carries the CLI's own fail line in
a `detail` field. The step code stays `open_failed`, as for every command
wrapped into a flow step, but the code, the stderr and the remedy behind it used
to be discarded, leaving a bare `open_failed` with nothing to read -- for
`--skip-build` and for every other way `open` can fail.

The `--no-relaunch` conflict checks (`--clear-state`, `--fixture`, and now
`--skip-build`) moved above the target-override step. They used to run after it,
so a command that was about to be rejected first booted a simulator and
persisted a new target into `.mav/config.yaml`.

`mav run --target ... --target ...` already built once and told its children not
to rebuild, through a private `MAV_SKIP_BUILD` environment variable. It now uses
the same `--skip-build` flag everyone else does.

## v0.16.1

### `mav flow lint` checks the screenshot steps it was passing through

The `sim.appearance` and `sim.statusbar.set|clear` flow actions shipped in v0.16.0
with their values checked only at run time. A localized App Store matrix is dozens
of captures per language, so `appearance: sepia`, `preset: marketing` or
`wifiBars: 9` failed after the captures before it had already been taken.

- `sim.appearance` requires `light` or `dark`.
- `sim.statusbar.set` is validated by the same parser the run uses — preset,
  enum fields and the 0-N ranges — so lint cannot drift from what a run accepts.
  A step with no fields at all is an error, as it is on the CLI.
- `sim.statusbar.clear` carrying status bar fields is a warning: the step resets
  the whole bar, so those fields were written expecting an override they will not
  get.
- Lint messages name the YAML key (`wifiBars`), not the CLI flag the step is
  translated into (`--wifi-bars`).

### `sim.appearance` waits for the screen to repaint

Running the documented matrix against a booted simulator caught the one failure
it cannot afford: `sim.appearance: { appearance: dark }` followed by `capture`
produced the *light* screenshot, and said `ok`. simctl accepts the new style
immediately and its own screenshot is current, but the axe path MAV prefers on a
simulator serves the pre-switch frame. `mav sim appearance` now returns only
after the repaint window (measured stale at 0s in every trial, correct from
0.5s; the wait is two seconds), so the capture after it is the appearance that
was asked for.

## v0.16.0

### App Store screenshots stop showing the real clock

Two simulator knobs MAV did not expose blocked using it for App Store screenshots:
the same screen in both appearances, and a status bar that is not the machine's
own `8:36` with half the signal dots.

- **`mav sim appearance light|dark`**, and the `sim.appearance` flow action. Both
  live under `mav sim` rather than `mav ui`, because appearance and the status bar
  are CoreSimulator-wide state that outlives the app, not an action on the screen
  in front.
- **`mav sim statusbar set|clear`**, and the `sim.statusbar.set` / `sim.statusbar.clear`
  flow actions. `--preset appstore` is Apple's own marketing status bar — 9:41, full
  battery, full signal — but it is a starting point, not a lock: every field stays
  individually settable and an explicit flag overrides the preset. The override is
  additive, matching `simctl`, so `--time` alone leaves the rest of the bar alone.
- **Values are validated before the call**, and before routing, so `--wifi-bars 9`
  answers `status_bar_value_invalid` naming the range, and a forgotten value that
  swallowed the next flag (`--time --preset appstore`) answers
  `status_bar_value_missing` — instead of `simctl`'s usage dump or an opaque POSIX
  error.
- Both are simulator-only, with the treatment `erase` and `hideKeyboard` already
  get: `appearance_unsupported_on_device` / `status_bar_unsupported_on_device`, plus
  `_unsupported_on_macos` variants so an agent branching on the code does not have to
  guess which platform it is standing on.

### A flow step param can no longer overwrite its own evidence record

`commands.jsonl` built each record from `time`/`step`/`action`/`status`/`elapsed` and
then let the step's fields overwrite them. Nothing had a param named after one until
now: `sim.statusbar.set: { time: "9:41" }` stamped the fake status bar clock as the
step's wall-clock time, in the log the evidence bundle ships verbatim. The record's own
keys are now written last, and the two steps that named one report it under a key
of their own instead: `time.status` as `timeStatus`, `sim.statusbar.set`'s clock as
`statusBarTime`. A test walks the flow executor and fails on any step field named
after a reserved key, so the next one fails in CI rather than in an evidence bundle.

## v0.15.0

### The macOS target answers about itself, not about a simulator

Validating a real Mac app from a VM showed that every dead end still spoke iOS. Now:

- **`ui tree` names the mac driver when it is missing.** The `tool_missing` answer on a
  macos target used to prescribe `mav setup --install axe idb`, two iOS tools that provide
  nothing there; it now names `cua-driver` and carries the router's own rejection detail,
  so "not installed" and "daemon down" stop looking identical.
- **A failed `capture` says it is a permission, not a display bug.** CGDisplay's
  `could not create image from display` means the capturing process has no Screen
  Recording grant; `capture_failed` (and the evidence-step captures) now report
  `cause=screen_recording_permission_missing` with the fix. The capture `tool_missing`
  also names the mac path instead of `axe|idb|xcrun`.
- **`doctor` diagnoses the selected target.** It reports `target_kind` and the active
  profile, prescribes `cua-driver` instead of axe/idb/baguette on macos, counts axcli for
  semantic taps (and only for them: it has no tree), reports `multitouch=unsupported` and
  `wall_clock=system` instead of prescribing simulator tools, and emits `mac_*` capability
  rows instead of `sim_*/device_*` ones. The correct macOS launch recipe — empty `launch`
  plus a bundle id, because `open` does not propagate environment — is no longer flagged
  incomplete.
- **`ui doubleTap` works on macOS.** Selector (`--id`/`--text`, or any rich selector,
  resolved against the tree) or `--x`/`--y`, routed to cua-driver's dedicated
  `double_click` tool — two single clicks can never form a double click, the event's
  clickCount never reaches 2. Coordinate intake validates both axes and rejects typos
  instead of clicking the menu bar at `(X, 0)`.
- **Coordinate taps route on macOS.** `ui tap --x --y` hard-preferred idb, so it died
  `tool_missing tool=idb` on a mac even with the mac driver healthy.

## v0.14.0

### mav can say which mav it is

- **`mav --version` and `mav version`.** Until now they answered
  `unknown_command`, which turned every bug report, and every run whose evidence is read
  weeks later, into a guess about which binary produced it. `mav doctor` reports
  `mav_version` for the same reason: a diagnostic that cannot say what produced it is
  half a diagnostic.
- Stamped at link time from the release tag. An unstamped build reports `dev` and never
  claims a release number it is not, so nobody goes chasing a bug in code that was never
  shipped. The Homebrew formula's own test now asserts the released binary reports the
  version the formula claims.

### The VM environment is checked, not assumed

- **The machine is verified when it is taken, once.** An image built before a driver
  existed, or one whose permission switches were never flipped, used to fail deep inside
  a run with an error about a window or an element. Measured on a real image with the
  driver hidden from the guest's PATH: the first symptom was `ui tree` reporting that the
  app was not running. `mav` now answers `vm_image_incomplete missing=cua-driver
  next=scripts/build-mav-vm-image.sh` before the run starts, and hands the unusable
  machine straight back rather than letting it hold one of the two available slots.
- **The driver's own permissions are part of that check**, and a daemon that has not
  answered yet is not read as granted. Asked with no daemon running, the driver says it
  does not know rather than guessing; treating that as "granted" would let through
  exactly the image this is meant to catch. The daemon is started here too, which is
  where it belongs: it is per-machine setup, so the first `ui tree` of every run stops
  paying for it.
- **A missing image no longer points at the installer.** `mav setup --install vm` does
  not build the image, so `vm_image=missing next=mav setup --install vm` sent the reader
  to run a command that reported the same line back at them. Image problems now name
  `scripts/build-mav-vm-image.sh`; tooling problems still name the install command.
- `mav doctor` reports `vm_guest` when a lease is held. Only then: checking the guest
  means talking to it, and a diagnostic that leases a machine to report that leasing
  works would take one of the two slots the run needs.
- **A run in a VM has processes on both machines, and `mav stop` now knows which is
  which.** The log stream and the recorder are the guest's; the run worker is this
  machine's, because what it watches -- the run's lease -- is here. Every stop was being
  sent to the guest, so the worker's pid went over there: it did not fail loudly, it
  looked for that number on the wrong machine while the real worker kept running here
  until its lease expired. Caught by a real run reporting `stop_failed failed=1`; the
  same code would have signalled a stranger's process in the guest had that number been
  in use. Process records now carry which machine they belong to.
- The check does **not** ask the guest for a mav. The image installs one, but mav runs on
  the host and reaches in for the drivers; it never invokes a mav over there. An earlier
  draft demanded it and would have refused a usable image over a binary nothing calls.

## v0.13.0

### macOS video

- **`mav evidence start` records video on macOS.** The drivers have been able to since
  v0.12.0 and nothing ever reached them: `startVideoRecording` went straight to `simctl`
  and refused anything that was not a simulator, so `evidence start` answered
  `video_unsupported target=device` on a Mac. It routes `CapVideo` now, and the failure
  it can still produce names the real target instead of `device`.
- **It records through the driver daemon, which is what makes it work over SSH.**
  `screencapture -v` needs mav to already be inside the graphical session; in a VM it is
  not, and it sees no display at all. The daemon is in that session and holds the Screen
  Recording grant, so recording through it works in both places. `screencapture` stays as
  the fallback for a local Mac.
- **The recording is held open by a session for the length of the run.** The daemon
  records only while a client stays connected, so mav keeps one, and `evidence stop` asks
  the daemon to finalize before anything signals it: only the daemon writes the mp4's
  index, and a file cut off without one is a plausible-looking video no player opens.
  Measured alternatives that do not work, in case anyone tries them again: the persistent
  `recording start` captures per-action stills and its video flag does nothing,
  `recording render` refuses without an mp4 only the other path produces, and the
  hypervisor's own desktop recording rejects macOS targets outright.
- No transcode when the recorder already produced H.264: on macOS the output is the mp4,
  not a `.mov` to convert.

### macOS in a disposable VM

- **`vm: true` next to `target_kind: macos` runs the app under test in a throwaway
  machine, and nothing about driving mav changes.** `open`, `ui tree`, `ui tap`,
  `capture`, `run`, `logs`, `crashes`, `evidence` and `network` take the same arguments
  and answer the same way; the only new field is `vm=true`, so an agent chaining loose
  commands can tell whether what it just drove was the VM's app or this machine's.
- **That one key is the whole config surface.** No host, no IP, no job name, no tool
  name, no key path. Which hypervisor provides the machine is mav's business, so the day
  it changes no `config.yaml` on anybody's disk has to. It replaces `runner:
  local|crabbox`, which shipped in v0.12.0, only declared an intent and did nothing.
- **Evidence lands in the local `.mav/runs/<id>/`**, which is the part that decides
  whether the feature is real. Captures, trees, logs, HAR and `report.json` are rsynced
  out of the guest after every command, not once at the end: an agent driving mav command
  by command never reaches anything that would be "the end", and evidence it cannot read
  until some later command happens to sync is evidence it reasons about stale. The
  upstream artifact mechanism was not an option: `crabbox run --artifact-glob` rejects
  native macOS targets ([crabbox#1393](https://github.com/openclaw/crabbox/issues/1393)).
- **The machine is handed back on `mav stop`, at the end of a flow, and on an idle
  timeout.** Not tidiness: Virtualization.framework *and* the macOS EULA cap you at two
  concurrent macOS VMs, so a leaked lease blocks the next run. The idle path rides the
  run worker's existing lease expiry, which is the one place mav already knows the run's
  owner is gone, so an agent that crashes releases the machine without anybody's help.
  Evidence always comes home before the release: the order has one safe direction and no
  second chance.
- **The remote project root is the same absolute path as the local one.** mav computes
  artifact paths everywhere, and any other choice would mean translating each one at each
  call site with a silent wrong-file bug waiting on every one it missed.
- **The launch recipe splits across the two machines.** `healthcheck`, `build` and
  `app_path` stay here, because a VM image carrying every project's build dependencies is
  not an image anybody can share; `install`, the fixture, `launch` and `cleanup` run
  there, because that is where the app runs. The checkout and the built bundle are shipped
  across in between.
- **Guest processes are stopped on the guest.** The PIDs a run records in VM mode belong
  to the other machine, and signalling them here does not fail loudly, it kills whatever
  local process happens to hold that number. `Runner` grew an optional `Stop`, which is
  the seam that makes the difference visible instead of catastrophic.
- **`mav setup --install vm` installs the VM tooling**, and every VM failure ends naming
  it. It is a tool in that list and not a command of its own on purpose: there should be
  one place to look for "mav is missing something I need", and everything else mav can
  install already lives behind that flag. Nobody writing `vm: true` is told which
  hypervisor to go and install, because that is exactly the detail the config surface
  exists to hide, and nothing in the output names it either, including on success.
  `mav setup` without `--install` stays interactive and now offers `vm` for a macOS
  project. `mav doctor` reports `vm_tooling`, `vm_image` and the current lease without
  ever leasing a machine to do it -- with a budget of two, a diagnostic that takes a slot
  is not a diagnostic.
- **The guest's copy of the bundle is re-signed ad-hoc when it would otherwise not
  launch**, and `open` says so with `resigned=adhoc`. A development-signed app carries
  entitlements tied to a team and a device list, and in a clean VM the kernel kills it on
  launch with no message; the symptom three commands later is "the app is not running",
  which points at everything except the signature. It is a real trade, iCloud and push go
  with it, so it is reported rather than done quietly, and only the guest's copy is
  touched.
- **An outdated hypervisor is caught before anything is leased.** Below 2.29 it dies while
  injecting the SSH key with a message about terminal sizes, and nothing in that message
  points at the version. `mav doctor` reports `vm_tooling=outdated` and
  `mav setup --install vm` upgrades it.
- `vm: true` is rejected on a simulator or device target instead of ignored. A simulator
  is reached from this machine and a phone is plugged into it; accepting the flag there
  would leave somebody believing they were isolated when nothing had changed.

## v0.12.0

### macOS

- **`mav` stops being iOS-only.** `target_kind: macos` is a first-class target kind. A macOS
  app has no UDID: its identity is its bundle id plus the `.app` path the launch recipe
  resolves at runtime. `ui tree`, `ui tap`, `ui type`, `ui erase`, `ui swipe`, `ui wait`,
  `capture`, `open`, `app list`, `openURL`, `clipboard`, `logs`, `crashes`, `evidence`,
  `run`, `network` and `time` all work there. Logs and crashes came almost for free: `log
  stream` on the host is the same line as on the simulator minus the `simctl spawn`, and the
  Mac's `.ips` files are the same JSON as iOS's, so `ParseIPS` works untouched.
- **The driver is [cua-driver](https://github.com/trycua/cua) (MIT).** The reason is
  structural rather than a preference: macOS grants Accessibility and Screen Recording only
  to interactive GUI processes, so a CLI cannot hold them however many times you grant them
  to your terminal. The only architecture that works is a broker, an app that owns the
  permissions plus a socket, and cua-driver ships one: the binary mav invokes lives inside
  `/Applications/CuaDriver.app`. It returns the tree with geometry, the window capture and
  background input from a single tool, and the tree and the capture come out of the same
  call, so both describe the same instant.
- Two alternatives were dropped on measurements, not taste. Peekaboo discards windows with
  `layer != 0`, so an app whose UI is a floating panel gets neither tree nor capture.
  axcli's capture returned **the desktop** cropped to the window's bounds, with no error, and
  activated the app on the way. A plausible PNG instead of an error is worse than failing.
- **[axcli](https://github.com/andelf/axcli) stays as an escape hatch**, input only.
  cua-driver resolves the window through `list_windows`, and an app whose entire UI lives in
  an accessory window has to be addressed by pid instead. When there is no window to resolve,
  the error says so in those words rather than looking like "the app isn't open".
- Input goes through cua-driver and axcli sits behind it, and not because it delivers worse:
  `cg-pid` synthesizes a mouse event and there are SwiftUI buttons that accept it **without
  reacting**. Measured against a real app's onboarding, the tap reported success and the
  screen did not advance. cua-driver's click goes through AXPress when the element exposes
  it, and on those same buttons it takes effect.
- **Network capture works end to end.** `mav network start` brings up mitmproxy and points
  the system at the proxy itself with `networksetup`, no sudo, on the service the default
  route leaves through, not the first in the list, which is usually a VM's virtual interface
  and gives no error, it just captures nothing. `network stop` and `mav stop` restore it, and
  the previous state goes in the run directory: start and stop are separate invocations, and
  a run that dies must not leave the machine pointing at a dead proxy. If the mitmproxy CA is
  not trusted, the command says so with the exact `security add-trusted-cert`; without it
  HTTPS comes out as CONNECT tunnels with no content.
- **Time control**, with what is possible and without faking what isn't. `mav time travel
  --to` and `mav time reset` move the machine's clock; `freeze` and `scale` fail saying a
  system clock runs and cannot be stopped or accelerated. Closed by default outside a VM
  (`kern.hv_vmm_present`), forced with `--system-clock`. On iOS simtime interposes the clock
  the app sees; on macOS the only per-process route is libfaketime through
  `DYLD_INSERT_LIBRARIES`, which the hardened runtime blocks in any app signed for
  distribution.
- **`mav location` explains why it can't**, instead of a bare "unsupported" that invites an
  afternoon of searching: Xcode's "Simulate Location" is not a debugger feature, it travels
  over the DVT channel, which serves iOS devices, and does nothing against a macOS app; lldb
  has no equivalent command; the tools that exist fake a connected iPhone, not the Mac.
- `mav doctor` reports permissions by asking **the daemon**, which is the one that holds
  them, and starts it if it isn't running. `mav` does that itself, with `open -g` so it
  doesn't steal focus: an agent that invents the incantation starts a bare `cua-driver
  serve`, which loses the permission attribution that is the whole point of the broker.
- Two things macOS does not get: elements do not expose AXIdentifier, only `element_token`,
  which is valid inside one snapshot, so `ui tap --id` has no cross-run stability there. And
  there is no menu-bar interaction or window management.

### Profiles and fixtures

- **Platform profiles** in `.mav/config.yaml`: one block per platform overriding
  `app_target`, the launch recipe, `process_name`, `target_command` and the log fields.
  Selected with `--profile`, `MAV_PROFILE` or `default_profile`, in that order; a profile
  that doesn't exist fails naming the valid ones instead of quietly falling back. They are an
  overlay over the flat fields: a single-platform repo writes none and nothing changes for
  it.
- **Fixtures**: named states, lists of commands, that leave the app in a known situation.
  They run between `install` and `launch`, the only window where the container already exists
  and nothing holds its database open, which is also why the app is closed before seeding.
  They compose with `--clear-state` (wipe the container, the fixture seeds on top), they are
  available as `fixture:` in a flow's `open` step, and the one applied is recorded in
  `report.json`: a run whose evidence doesn't say which state it started from is not
  reproducible.
- `--fixture` is rejected alongside `--no-relaunch`, like `--clear-state` and for the same
  reason: `--no-relaunch` skips the whole recipe, so the fixture would never run and the
  agent would validate against data nobody seeded.
- **An unknown key inside a profile is an error** (`profile_unknown_key`) instead of being
  ignored. Writing `fixture:` in a profile did nothing, and from outside that is
  indistinguishable from it applying with no effect. Scoped to profiles on purpose: they are
  new, so no existing configuration can break.

### Output contract

- **A failure now exits 1.** They all exited 0: the `fail code=...` line was written and the
  process said everything went fine, so `mav ui tap ... && next-step` kept going after a
  failure and every agent had to read stdout to find out whether its own command had worked.
  If you script against mav, this is the change to look at.
- And output is **always** written, on failure too: `mav ui` buffered it and discarded it
  when the command errored, leaving nothing to read exactly when it matters most.
- **`report.json` no longer passes a verdict on a screenshot that never existed.** Every flow
  carried `"screenshot_evidence":{"ok":false}`, indistinguishable from a broken capture for
  whoever reads the JSON. The field is absent when there is nothing to validate.

### Fixes

- An unknown `target_kind` no longer fails open. `targetKind()` sent everything that wasn't
  the literal `"device"` to `KindSim`, so a `target_kind: macos` written by hand behaved as a
  simulator from end to end: it resolved a simpool lease, booted a simulated iPhone, and with
  `--clear-state` uninstalled the app from it using a `bundle_id` that in a cross-platform
  app is the same on both. Validation now runs at the end of loading, once the profile and
  `MAV_TARGET_KIND` have been applied, because all three sources have to go through the same
  filter.
- A `clear_state` that doesn't manage to uninstall is no longer dropped in silence. It
  doesn't abort the `open`, the ordinary case is that the app wasn't installed yet, but it
  comes out as `clear_state_warn`. Without that, `--clear-state` lied: you believed you were
  starting from scratch and carried the previous run's state along.
- **`mav capture` ignored `--prefer-driver`.** The flag was accepted and thrown away: capture
  always routed by cost, so the only way to dodge a broken driver was to uninstall it.
- **`ui tree` on a physical device serves through idb again.** Replacing the per-tool gate
  with the router left the idb path behind a block that always returns, so it became dead
  code and the tree started failing on a device instead of falling back. idb declares
  `CapTreeAX` now and serves it, behind AXe on a simulator and alone on a device.
- `SaveConfig` moves to `yaml.Marshal` instead of a hand-rolled writer. The writer omitted
  empty values, so the file could not express "present and equal to the empty string", which
  is exactly what a profile needs to **annul** an inherited command. And it refuses to write
  a config with a profile applied: saving it would flatten the profile onto the base, and a
  `mav sim select` in a repo with `default_profile` would have left the macOS `app_target` as
  the base, silently and with no way back.

### Tooling

- `scripts/build-mav-vm-image.sh` builds a reproducible tart image with mav, cua-driver,
  axcli and mitmproxy. It builds cua-driver from the fork carrying
  [trycua/cua#3375](https://github.com/trycua/cua/pull/3375), since the released one cannot
  see an accessory window, and it refuses to call the image ready while the driver's
  permissions are off. Those cannot be granted from a script on macOS 26: seeding TCC.db does
  not work, PPPC profiles are only honored from an MDM, and AppleScript cannot reach System
  Settings over SSH. They are flipped once while the image is built, and every VM cloned from
  it starts with them on.

## v0.11.0

- El router de capacidades vuelve a decidir de verdad. La mayoria de las llamadas a
  `Route()` pedian una capacidad **y ademas** clavaban el driver (`prefer: "axe"`,
  `"idb"`, `"baguette"`, `"simctl"`), y otras trece se saltaban el router entero
  llamando a `axe`/`xcrun` a pelo. Eso convertia la tabla de `Cost` en decoracion:
  daba igual que un driver declarase la capacidad, nunca iba a ganar. Ahora los
  `prefer` redundantes desaparecen -- se han quitado solo donde la capacidad tiene
  un unico proveedor, comprobado capacidad por capacidad, asi que el driver que
  sirve cada comando no cambia -- y los que de verdad desempatan (`tap.coord`,
  `type`, `swipe`, `screenshot`) siguen ahi, con test de regresion que lo fija.
- `--prefer-driver` acepta cualquier driver registrado en vez de solo `auto|axe`,
  y el `usage` del error los lista de verdad en vez de repetir una cadena fija.
  Abrir el flag destapo un fallo que su propia validacion tapaba: `ui swipe`
  fijaba `axe` en cuanto estaba instalado y solo miraba `--prefer-driver` para el
  caso `axe`, asi que cualquier otro valor se habria aceptado y luego ignorado en
  silencio. Un prefer explicito manda ahora, y si el driver pedido no puede servir
  la capacidad se falla nombrandolo (`prefer_driver_unusable driver=<id>`) en vez
  de correr otro sin decirlo -- la misma correccion de rumbo que `target_command_ignored`
  en la v0.9.1.
- `isPhysicalDevice` (44 usos) y `normalizedTargetKind` desaparecen a favor de
  `targetKind()`, que devuelve el enum del router en vez de un booleano. Era un
  `if` binario que asumia "si no es device, es simulador" repartido por todo el
  CLI; ahora es un `switch`, y las guardas sim-only se escriben `!= KindSim` para
  que un tercer tipo de target falle cerrado en vez de colarse por la rama del
  simulador. La grafia publica no se mueve: `target_kind` sigue diciendo
  `simulator`/`device` en la salida, en `MAV_TARGET_KIND` y en los `config.yaml`
  ya escritos en disco.
- El rastro de evidencia del ciclo de vida mejora de paso: `commands.jsonl`
  registraba `driver=<el que se pidio>` y ahora registra `driver=<el que sirvio>`.
  Cuando el enrutado falla del todo no se inventa un driver: anota la capacidad.

## v0.10.1

- Un comando ya no falla cuando el simulador que `target_command` había
  resuelto se apaga por debajo dentro de la ventana del caché. Antes mav
  despachaba contra el dispositivo muerto y fallaba en 0 segundos, sin
  intentar nada, pese a que volver a preguntar habría bastado. Ahora, cuando
  un comando falla **y** una consulta de estado confirma que el simulador no
  está arrancado, mav invalida lo cacheado, reejecuta `target_command` y
  reintenta el comando **una vez** — nunca en bucle —, anunciándolo con
  `target_command_restale` para que un reintento no sea silencioso.
- El camino feliz no paga nada: la comprobación de estado sólo ocurre tras un
  fallo, y se decide por consulta real, no por adivinar el texto del error.
- Sin `target_command` configurado no hay a quién repreguntar, así que el
  error dice `reason=simulator_not_booted` en vez de dejar pasar el stderr
  crudo del driver.

## v0.10.0

- `mav run` ahora reinvoca `target_command` cada ~60s mientras el run está en marcha, como
  señal de vida pura -- no como una nueva resolución. El caché por run de `target_command`
  (2 min de TTL, pensado para una navegación en caliente de comandos sueltos) deja un hueco real
  en `mav run`: un solo paso largo -- un `open` con build, un `exec` que envuelve uno -- puede
  pasar minutos sin que mav despache ningún otro comando, así que nada volvía a tocar
  `target_command` en ese tiempo. Un gestor de pool que reserva su slot por TTL de reloj de pared
  (`simpool lease` es exactamente esto, y a partir de su propia v0.5.0 baja ese TTL por defecto a
  3 minutos) no tenía forma de saber que el run seguía vivo durante ese silencio, y reclamaba el
  slot -- justo la colisión que `target_command` existe para evitar.
- El run nunca cambia de simulador a media ejecución por culpa de una reinvocación: el UDID que
  usa para despachar quedó fijado al principio del run (la resolución que `bindFlowTarget` capturó
  antes del primer paso) y sigue siendo ese durante toda su vida. Si una reinvocación periódica
  resuelve a un UDID distinto, eso significa que algo ya se ha llevado el slot; el run se queda con
  el UDID original y añade un aviso accionable a `logs.txt` en vez de perseguir el nuevo -- cambiar
  de simulador a mitad de run reubicaría la colisión, no la evitaría. Un fallo a mitad de run se
  trata igual: avisado, nunca fatal, la misma forma de "avisa y sigue" que ya tenía la caída de
  emergencia de `target_command` para un solo comando.

## v0.9.2

- La caída de emergencia al simulador arrancado cuando `target_command` falla no llegaba a todos
  los comandos: `resolveConfigTarget` dejaba `simulator_udid` vacío en el `Config` que cada
  comando usa para despachar (axe, idb, xcrun...), y solo `withResolvedTarget` —el código que
  construye los campos de la salida de éxito, no el que decide a qué simulador hablar— aplicaba
  la caída real. `mav doctor` "funcionaba" porque solo necesitaba ese campo para reportar; `mav ui
  tree` y el resto de comandos que arrancan un driver directamente con el UDID de `cfg` recibían
  uno vacío y `axe` rechazaba la llamada (`Missing expected argument '--udid <udid>'`), pese a que
  el propio `target_command_warn` de esa misma respuesta afirmaba "falling back to the booted
  simulator". `resolveConfigTarget` ahora aplica esa caída sobre el propio `Config`, así que todo
  punto de entrada que pasa por ahí (o por `mustLoadConfig`) queda resuelto de verdad, no solo
  reportado. `mav time freeze|travel|scale|status|reset` no llamaba a la resolución en absoluto
  (bug aparte, mismo síntoma) y ahora sí.

## v0.9.1

- `target_command` dejaba de tener efecto en silencio cuando el repo también tenía un
  `simulator_udid` fijado en `.mav/config.yaml` (el pin gana, como debe ser), sin que nada lo
  dijera. Varios repos reales ya tenían un pin de una `mav sim select` anterior, así que añadir
  `target_command` ahí sería configuración muerta y nadie se enteraría. La precedencia no cambia
  —el pin sigue ganando— pero ahora ese conflicto es visible: `target_command_warn` avisa en cada
  comando afectado de que el pin está ganando y qué hacer (quitar el pin o quitar el comando).
  Nunca falla el comando ni lo cuelga: una config ambigua sigue siendo una config que funciona,
  solo que avisada.

## v0.9.0

- Nuevo campo `target_command` en `.mav/config.yaml`: un comando que mav ejecuta para obtener el
  UDID del simulador a usar. Resuelve el uso en caliente con varios simuladores arrancados a la vez
  —decenas de invocaciones sueltas (`mav tap`, `mav swipe`, `mav screenshot`...) sin un punto único
  donde envolver con un pool manager externo. mav no importa ni conoce simpool ni ningún otro pool
  manager: `target_command` solo ejecuta el comando configurado y lee un UDID de su stdout.
- Precedencia: un `--target` explícito en `mav run` (y los `MAV_TARGET_*` que fija en sus hijos de
  matrix) y los `MAV_TARGET_*` puestos directamente en el entorno ganan siempre; un `simulator_udid`
  fijado en config (`mav sim select`) también gana; `target_command` solo entra en juego donde antes
  se caía en silencio al simulador arrancado, y sigue cayendo ahí si falla.
- Se cachea por run igual que la resolución del simulador arrancado
  (`.mav/runs/<run-id>/target-command.json`, mismo TTL de 2 min), así que una navegación en caliente
  lo ejecuta una vez, no una vez por comando.
- Si `target_command` falla o no imprime nada, mav nunca cuelga ni hace panic: cae al comportamiento
  anterior (el simulador arrancado) y añade `target_command_warn=<motivo y siguiente paso>` a la
  salida del comando en vez de fallarlo.

## v0.8.0

- Toda respuesta de exito reporta ahora el objetivo sobre el que se actuo: `udid`, `target_kind`
  y `target_name`. En uso en caliente —un agente llamando al CLI comando a comando— eso permite
  fijar las llamadas siguientes al mismo simulador en vez de adivinarlo. Con varios agentes en la
  misma maquina, adivinar significa conducir la app de otro, y el fallo es silencioso porque los
  taps funcionan y las aserciones pasan.
- Cuando no hay objetivo fijado, el UDID reportado revela a que simulador se cayo por defecto:
  convierte un comportamiento implicito en algo observable.
- La resolucion del simulador arrancado se cachea por run (`booted-simulator.json`, TTL de 2 min).
  Sin ella cada comando pagaba ~0,78 s: 30 comandos pasaban de 23,2 s a 0,9 s.
- Los procesos auxiliares de grabacion de video ya no quedan huerfanos cuando `mav` muere de forma
  abrupta. El reaper de expiracion de lease los recoge, y `evidence start`, `network start` y la
  captura de logs garantizan que exista un vigilante aunque se invoquen sueltos, sin `mav open`.

## v0.7.0

- `mav run` ya no comparte el puntero global `.mav/current-run`: cada invocación crea y posee su propio run.
  Dos agentes trabajando en el mismo repo dejaban de pisarse los procesos y de escribir su evidencia en el
  directorio del otro, un fallo que pasaba en silencio porque las aserciones seguían en verde.
- El paso `open` recibe el run por parámetro en lugar de releer el puntero, así que ya no puede detener
  procesos pertenecientes a otro run.
- `current-run` queda como comodidad para los comandos manuales sueltos (`mav open`, `mav ui tap`, `mav logs`).
- Un flow mantiene un solo run de principio a fin. Antes cada paso `open` abría uno nuevo a mitad de flow,
  lo que dispersaba los artefactos: la evidencia aterriza ahora en un único sitio.
- `--run` con un identificador inexistente falla con `run_not_found` en vez de ignorarse en silencio.

## v0.6.0

- Transparent per-run worker over a private Unix socket, with direct fallback and persistent Baguette/DAP sessions.
- Renewable 15-minute inactivity lease with automatic worker, log, LLDB, time-control and simulator-lock cleanup.
- Single-call action, wait and observation fast path for tap, type, swipe, double tap, drag, drag path and toggle.
- Strict typed flow selectors, spatial/hierarchy predicates, boolean conditions, count assertions, stable waits and tree deltas.
- Flow parameters, target bindings, extraction outputs and configurable retry policies.
- New iOS primitives for gestures, app lifecycle, URLs, location and clipboard.
- Optional simulator wall-clock control through `simtime`.
- Idempotent setup verification for the simtime dylib and Xcode `lldb-dap`.
- Parallel multi-target flow runs with isolated artifacts and aggregate reports.
- Simulator LLDB debugging through `lldb-dap`.

Unknown YAML fields now fail linting. Existing flat `id`, `text`, `value` selectors and `optional: true` remain supported.
