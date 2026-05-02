---
name: mav
description: Use MAV, the Mobile Agent Verifier CLI, to validate iOS Bazel apps through deterministic simulator/device actions, accessibility tree inspection, screenshots, logs, crashes, and evidence reports.
---

# MAV

Use `mav` when validating an iOS Bazel app locally. MAV is deterministic: it
does not explore or repair routes by itself. The agent decides the next action.

## Workflow

1. Run `mav doctor`.
2. If the project lacks `.mav/config.yaml`, run `mav discover` and review the
   generated config.
3. If the validation needs a specific simulator, runtime, or locale, use
   `mav sim list`, then `mav sim select --device ... --ios ... --locale ... --language ...`.
   You can also pass the same target flags to `mav open`.
4. Start the app with `mav open`. This creates `/tmp/mav/<run-id>/` and starts
   `logs.txt`.
5. Prefer `mav ui tree` to understand the current screen. It is cheaper and more
   structured than screenshots.
6. Use `mav capture` only when the tree is insufficient or visual evidence is
   needed.
7. Use `mav ui tap/type/swipe/wait` for manual exploration. Prefer semantic
   AXe taps (`--id` or `--text`). If AXe cannot target the element and the
   screenshot makes the target unambiguous, use idb-backed coordinates with
   `mav ui tap --x ... --y ...`.
8. Use `mav go <screen-id>` only after `.mav/app-map.yaml` contains that screen
   and route. If MAV returns `screen_not_found` or `route_not_found`, explore
   manually and update the map yourself.
9. Use `mav preview init` to create a Bazel preview host, wire the view under
   test into the generated host, then use `mav preview <view-id>` and `mav capture`.
10. If `.mav/app-map.yaml` changes, review the git diff before continuing.

## Internal Execution Validation

To prove code reached a point:

1. Add a temporary `print`, `os_log`, or file-log marker with a unique string.
2. Trigger the behavior with MAV.
3. Run `mav logs --contains UniqueMarker`.
4. Remove the temporary marker unless it is intentionally becoming product
   logging.

`mav logs` reads the run log captured from `mav open`; it does not start new log
streams.

## Evidence

Use evidence when the user needs proof of verification:

1. Start recording before the user-visible flow with `mav evidence start`.
2. Navigate with `mav go <screen-id>` when the route is mapped, or use
   `mav ui ...` manually when it is not.
3. Capture named proof points with `mav evidence step --name ... --note ...`.
   Names should describe the assertion, for example `settings-before-toggle`
   and `settings-after-toggle`.
4. Stop recording after the tested behavior with `mav evidence stop`.
5. Confirm logs/crashes with `mav logs` and `mav crashes`.
6. Run `mav evidence report`.
7. Share the generated `/tmp/mav/<run-id>/report.html`.

The video must cover the complete verification path from launch/navigation
through the tested behavior. The screenshots must prove the behavior itself, not
just that the app opened. For a notification toggle, record the navigation to
Settings, capture before toggling, toggle it, capture after toggling, then stop.

`mav go --record` is acceptable only as a shortcut for a mapped navigation route.
It is not enough for feature verification unless the route itself is the feature
being tested.

## Previews

Use previews for isolated SwiftUI screens when launching the full app is too
slow or deep:

1. Run `mav preview init` if the repo does not have a preview host.
2. Add the real view and any lightweight mocks to `MAVPreview/PreviewHostApp.swift`
   or the generated host target.
3. Build and launch it with `mav preview <view-id>`.
4. Inspect with `mav ui tree`, then `mav capture` for visual proof.
5. Include preview screenshots in `mav evidence report` when they support the
   validation.

## Command Output

Default output is intentionally compact:

```text
ok cmd=open run=7fd logs=/tmp/mav/7fd/logs.txt
fail code=screen_not_found screen=settings
```

Use `--json` when parsing, `--raw` when the underlying tool output is needed,
and `--verbose` only for debugging MAV itself.
