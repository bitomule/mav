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
7. Use `mav ui tap/type/swipe/wait` for manual exploration.
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

1. Evidence must show the relevant checked steps or feature state. Prefer
   `mav go <screen-id> --evidence` for mapped flows so Maestro step screenshots
   are collected automatically.
2. Record video when validating a user-visible flow. The video should cover the
   full sequence from launch/open through reaching and testing the feature. Use
   `mav go <screen-id> --video-seconds N` for mapped flows, or
   `mav evidence video record --seconds N` when manually driving the app.
3. Confirm logs/crashes with `mav logs` and `mav crashes`.
4. Run `mav evidence report`.
5. Share the generated `/tmp/mav/<run-id>/report.html`.

The skill decides how long to record and which screenshots are useful, but
user-facing verification should include enough visual evidence to reconstruct
what was tested.

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
