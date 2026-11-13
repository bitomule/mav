# MAV 0.34.1

Fixes found while driving a real app with AXe 2.0.4 on an iOS 26.4 simulator.

- `ui tap --id` works again when an app repeats an id on a container and on the
  button inside it, as design-system navigation bars do. AXe 2 rejects any id
  that matches more than once. When every match is nested in the previous one,
  MAV taps the centre of the innermost and reports `selector_via=tree
  nested=true matches=N`. Matches in different places still fail with
  `ambiguousElement`, and no input is sent.
- Stdout carries only MAV's own `ok` or `fail` line. The AXe result envelope no
  longer follows it; pass `--raw` to see it. Flow evidence still keeps it in the
  run directory.
- A tap or slider that AXe refuses before sending input (`elementNotFound`,
  `ambiguousElement`, `elementNotHittable`) reports `delivery=notSent`, so it is
  safe to retry. The same codes from gestures, swipes and batches stay
  `uncertain`, because AXe can return them after it has already scrolled.
- `ui tree`'s `nodes=` counts the nodes it prints.
- The worker prepares its socket's permissions before it announces it is ready
  (#142).
