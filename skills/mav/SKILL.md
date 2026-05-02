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
   `logs.txt`. MAV captures a filtered unified log stream for MAV probes.
   Use `mav open --console` only when debugging non-MAV stdout/stderr issues.
5. Prefer `mav ui tree` to understand the current screen. It is cheaper and more
   structured than screenshots.
6. Use `mav capture` only when the tree is insufficient or visual evidence is
   needed.
7. Use `mav ui tap/type/swipe/wait/scrollUntil` for manual exploration. Prefer
   accessibility identifiers first (`--id`). Use coordinates only when the tree
   is insufficient and the screenshot makes the target unambiguous. Use text as
   the last option because labels change with localization and copy edits.
8. Use `mav go <screen-id>` only after `.mav/app-map.yaml` contains that screen
   and route. If MAV returns `screen_not_found` or `route_not_found`, explore
   manually and update the map yourself.
9. Use `mav preview init` to create a Bazel preview host, wire the view under
   test into the generated host, then use `mav preview <view-id>` and `mav capture`.
10. If `.mav/app-map.yaml` changes, review the git diff before continuing.
11. For ad-hoc sessions started with `mav open`, run `mav stop` when validation
    is done. `mav run` stops run-owned streams automatically.

## Internal Execution Validation

To prove code reached a point:

1. Add a temporary `OSLog.Logger` marker with a stable key. Use the
   `log_subsystem` and `log_category` from `.mav/config.yaml`, and make the
   message start with `MAV_LOG key=<StableKey>`.
2. Trigger the behavior with MAV.
3. Run `mav logs --key <StableKey>`.
4. Remove the temporary logger code before finishing unless it is intentionally
   becoming product logging.

Example Swift marker:

```swift
import OSLog

private let mavLog = Logger(
    subsystem: "mav.com.example.app",
    category: "probe"
)

mavLog.notice("MAV_LOG key=SettingsReached")
```

`mav logs` reads the run log captured from `mav open`; it does not start new log
streams. Do not use Swift `print` for MAV validation probes.

Native MAV flows may include project-local shell assertions when the repo has
`allow_shell: true` in `.mav/config.yaml`:

```yaml
- exec: { cmd: "grep -F 'MAV_LOG key=SettingsReached' $MAV_LOGS", contains: SettingsReached, timeout: 5s }
```

Use this for narrow checks against logs, generated files, or local test API
calls. MAV runs the command in the project root with `MAV_ROOT`, `MAV_RUN_ID`,
`MAV_RUN_DIR`, and `MAV_LOGS` set, writes stdout/stderr into the run directory,
and applies the requested timeout. Treat this as a trusted-project opt-in, not
as a hard sandbox for arbitrary untrusted commands.

## Evidence

Use evidence when the user needs proof of verification:

1. Prefer writing a temporary MAV YAML flow and running it with `mav run`.
2. The flow should start recording before navigation, use `delay` only for
   fixed launch/animation waits when tree-based waits are not possible, navigate
   with `go`, wait for the expected UI, capture named proof points, perform the
   tested action, capture the result, stop recording, check crashes, and
   generate a report.
3. Names should describe the assertion, for example `settings-before-toggle`
   and `settings-after-toggle`.
4. Share the generated `/tmp/mav/<run-id>/report.html`.

The video must cover the complete verification path from launch/navigation
through the tested behavior. The screenshots must prove the behavior itself, not
just that the app opened. For a notification toggle, record the navigation to
Settings, capture before toggling, toggle it, capture after toggling, then stop.

Use `mav go <screen-id>` for ad-hoc navigation. Use `mav run` for feature
verification evidence.

For off-screen elements, use `scrollUntil` in a MAV flow or `mav ui scrollUntil`
manually before tapping:

```yaml
- scrollUntil: { id: privacy_policy_button, direction: up, maxSwipes: 4 }
- tap: { id: privacy_policy_button }
```

```bash
mav ui scrollUntil --id privacy_policy_button --direction up --max-swipes 4
mav ui tap --id privacy_policy_button
```

If there is no stable id, use coordinates only after capturing/inspecting a
screenshot. Use `text` only when neither id nor coordinates are appropriate.

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
