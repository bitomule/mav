# What these examples were measured at

Every number here comes from running the file in this folder, unchanged, against
Boxy on an iPhone 17 Pro simulator (iOS 26.3) with `mav 0.29.0` as published.
The app was relaunched from a clean database before every single run.

**Truth was read from the accessibility tree after the run, never from what the
command claimed.** A run counts as a pass only if the thing it was supposed to
do is actually there afterwards.

## Rates

Two fixtures, because the demo videos show one and the harder measurement used
the other. The three-category fixture is the harder one: it puts two more
plausible candidates in front of the model on the first screen.

### Three categories on screen

| example | runs | passed | median |
|---|---|---|---|
| `01-words-not-identifiers.yaml` | 15 | **15** created, 0 false writes | 10.3s |
| `02-take-me-there-then-do-this.yaml` | 15 | **15** created, 0 false writes | 11.9s |

### One category on screen (what the videos show)

| example | runs | passed | median |
|---|---|---|---|
| `01-words-not-identifiers.yaml` | 10 | **10** created | 10.1s |
| `03-app-store-screenshots.yaml` | 10 | **10** — three PNGs, light ≠ dark byte for byte | 15.7s |
| `04-assertions-in-english.yaml` | 10 | **10** passed | 4.7s |
| `04`, with the question changed to one whose honest answer is *no* | 10 | **10 failed**, `code=verify_rejected` | 5.0s |

That last row is the point of the fourth example. A check that cannot fail is
not a check. The only thing changed between the two `04` rows is the sentence
being asked — *"a box that would hold kitchen crockery"* against *"a box that
would hold live animals"* — over the same screen, and it flips 10/10 in both
directions.

## The waits are not decoration, and here is the ablation

Both `01` and `02` carry `after: { wait: { stable: true } }` on two steps. They
were added because the flows failed without them, and the rates above are with
them.

| `02-take-me-there-then-do-this.yaml` | runs | passed |
|---|---|---|
| no waits | 25 | 22 |
| no waits, step 3 reworded (see below) | 25 | 23 |
| wait after `type` | 15 | **15** |
| wait after `goto` **and** after `type` — the file as it stands | 15 | **15** |

| `01-words-not-identifiers.yaml` | runs | passed |
|---|---|---|
| no waits | 7 | 7 |
| wait after `type` only | 15 | 14 |
| both waits — the file as it stands | 25 | **25** |

Two different failures, both races, both caught with a screenshot:

- **The save button is disabled until the name field has text.** One run in ten
  read the screen while it was still disabled. `find` refused to offer a
  disabled button — which is correct — so the step failed with nothing to tap.
  Note what did *not* happen: it did not tap something else and report success.
- **The form arrives as a sheet that slides up.** Typing into a screen that is
  still moving loses the focus: `type` failed with the field empty and no
  keyboard.

`stable` is not a sleep. It hashes the screen's `(id, label, role)` identity,
waits 200ms, hashes it again, and continues when the two match — so it costs
what the app takes and no more. The screen-changed decision is made in code,
against the tree. Nothing asks a model whether anything moved.

## One thing that was tried and did not work

The `02` failure looked like a wording problem first: its step said *"the button
that confirms creating the new category"*, and the button that **opens** the
form is labelled `Create Category`, so the description read like a description
of the wrong button. Rewording it to *"confirms and saves"* measured **23/25**
against **22/25** — no better. The wording was not the cause; the race was. The
rewording was kept anyway, because it is clearer, but it is not what fixed it.
