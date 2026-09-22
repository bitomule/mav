# Examples

Four flows. Every one of them was run against a real app on a real simulator
before it was written down, and the rate each one was measured at is in the
table below. None of them needs anything that is not in the published `mav`.

The app is Boxy, a real shipping iOS app, not a fixture built to make this
work — which is the point: the awkward things these examples deal with are
awkward because nobody designed the app around being automated.

| file | what it does | measured |
|---|---|---|
| [01-words-not-identifiers.yaml](01-words-not-identifiers.yaml) | Creates a category, naming no element by its identifier | 25/25, 10.2s |
| [02-take-me-there-then-do-this.yaml](02-take-me-there-then-do-this.yaml) | `goto` finds its own way to a screen, then declared steps do the work | 15/15, 11.9s |
| [03-app-store-screenshots.yaml](03-app-store-screenshots.yaml) | App Store screenshots, light and dark, clock at 9:41 | 10/10, 15.7s |
| [04-assertions-in-english.yaml](04-assertions-in-english.yaml) | An assertion no selector can express, and it can fail | 10/10 pass, 10/10 fail when it should |

Full numbers, the fixtures they were run against and the ablation behind the
waits: **[MEASURED.md](MEASURED.md)**.

Run one:

```
mav run examples/01-words-not-identifiers.yaml
```

## Why these four

**The identifiers on this screen are unusable, and that is normal.**
`createCategoryButton` is not one button. On the category grid it is already two
nodes — a group and the button inside it — so a tap by identifier refuses before
it starts:

```
$ mav ui tap --id createCategoryButton
fail code=ui_tap_failed stderr="Warning: Multiple (2) accessibility elements
matched --id 'createCategoryButton'. ... No tap performed."
```

Open the create-category form and it becomes **four**: the two on the toolbar
underneath, which *open* the form, and two more in the form's navigation bar,
which *save* it. Same identifier, opposite meanings, on screen together — and
one of them is disabled until the name field has text in it.

And the categories are identified by the UUID of their database row —
`category_D1FDDFD9-F146-4FF9-AA6A-2315CEED4DE6` — generated when the data is
created, so it is different on every machine, every install and every run.
There is no script you can write against that. There is a sentence you can
write: *the Moving Boxes category*.

**The model is never asked what to write.** Text comes from `inputs` in the
file, and reaches the app through `text.from`. The model is asked which
element, and only that. That is the difference between something you can use on
a real app and something that demos well.

**Arrival is decided in code, not by asking.** `goto` navigates on its own
judgement, but `arrivedWhen` is checked against the accessibility tree, and a
flow that did not arrive fails instead of carrying on into the wrong screen.
The same rule fences `verify` out of deciding whether anything progressed: a
judge whose mistakes correlate with the thing being judged is not a judge.

## Measured

Run against Boxy on an iPhone 17 Pro simulator, iOS 26.3, `mav 0.29.0` as
published, with the app relaunched from a clean database before every run.
Truth was read from the accessibility tree afterwards, never from what the
command claimed. See **[MEASURED.md](MEASURED.md)**.
