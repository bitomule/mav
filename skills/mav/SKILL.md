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
3. Start the app with `mav open`. This creates `/tmp/mav/<run-id>/` and starts
   `logs.txt`.
4. Prefer `mav ui tree` to understand the current screen. It is cheaper and more
   structured than screenshots.
5. Use `mav capture` only when the tree is insufficient or visual evidence is
   needed.
6. Use `mav ui tap/type/swipe/wait` for manual exploration.
7. Use `mav go <screen-id>` only after `.mav/app-map.yaml` contains that screen
   and route. If MAV returns `screen_not_found` or `route_not_found`, explore
   manually and update the map yourself.
8. Use `mav preview <view-id>` only when `.mav/config.yaml` has a preview host
   configured. If MAV returns `preview_not_configured`, create or configure a
   Bazel preview host first.
9. If `.mav/app-map.yaml` changes, review the git diff before continuing.

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

1. Capture relevant state with `mav capture`; record video only if sequence or
   animation matters with `mav evidence video record --seconds N`.
2. Confirm logs/crashes with `mav logs` and `mav crashes`.
3. Run `mav evidence report`.
4. Share the generated `/tmp/mav/<run-id>/report.html`.

Do not generate reports for every tiny check; use them when the result is worth
showing.

## Command Output

Default output is intentionally compact:

```text
ok cmd=open run=7fd logs=/tmp/mav/7fd/logs.txt
fail code=screen_not_found screen=settings
```

Use `--json` when parsing, `--raw` when the underlying tool output is needed,
and `--verbose` only for debugging MAV itself.
