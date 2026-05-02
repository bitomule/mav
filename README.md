# MAV

Mobile Agent Verifier (`mav`) is a deterministic CLI for helping coding agents
validate iOS Bazel apps on simulators and devices.

MAV does not explore or decide what to test. It gives agents a compact,
scriptable API for building, launching, observing, navigating, collecting logs,
capturing screenshots/video, checking crashes, and producing evidence reports.

## Status

MAV is early. The current focus is iOS apps built with Bazel, simulator
validation, native MAV flows, accessibility-tree driven UI automation, and
evidence reports.

## Requirements

- macOS with Xcode command line tools.
- Bazel/Bazelisk project containing an iOS app target.
- `go` for local development builds.
- `xcrun` for simulator boot, install, launch, screenshots, video, and crashes.
- `axe` for accessibility tree inspection and semantic interactions.
- `idb` for coordinate taps and fallback simulator/device capabilities.

Check your environment:

```bash
mav doctor
```

Install missing tools that MAV knows how to install:

```bash
mav setup --install axe idb
```

## Install

Development build from this repo:

```bash
go build -o .build/mav ./cmd/mav
```

Then either run `.build/mav` directly or put it on your `PATH`.

## Quick Start

Run these commands from the root of an iOS Bazel app repo:

```bash
mav discover
mav sim list
mav sim select --device "iPhone 17 Pro Max" --ios 26
mav open
mav ui tree
```

`mav discover` writes `.mav/config.yaml`. It caches the Bazel app target,
bundle id, simulator defaults, and available tools.

`mav open` builds, installs, and launches the app. It creates a run directory:

```text
/tmp/mav/<run-id>/
```

Every run may contain:

```text
logs.txt
commands.jsonl
evidence.jsonl
steps/*.png
trees/*.json
video.mov
crashes/
report.html
```

Default output is intentionally compact:

```text
ok cmd=open run=7fd logs=/tmp/mav/7fd/logs.txt
ok cmd=ui.tree driver=axe nodes=42 screen=start
fail code=screen_not_found screen=settings
```

Use `--json` for parsing and `--raw` when you need the underlying tool output.

## App Map

MAV stores the app map in:

```text
.mav/map/index.json
.mav/map/screens/*.json
```

The map is updated by normal MAV usage:

1. `mav open` resets the current screen to the configured start screen.
2. `mav ui tree` records the current accessibility tree and screen elements.
3. `mav ui tap ...` records a pending action from the current screen.
4. The next `mav ui tree` observes the new screen and writes the route edge.

This means the basic mapping loop is:

```bash
mav open
mav ui tree
mav ui tap --id home_settings_button
mav ui tree
```

Prefer target selectors in this order:

1. Accessibility id: `mav ui tap --id home_settings_button`
2. Coordinates: `mav ui tap --x 398 --y 84`
3. Text: `mav ui tap --text Settings`

Text is the last fallback because copy and localization change. Coordinates are
acceptable only when the tree is insufficient and a screenshot makes the target
unambiguous.

Review `.mav/map/**` changes before relying on a route.

## Navigation

`mav go <screen-id>` is deterministic. It only works when the target screen and
a route from app launch already exist in `.mav/map/**`.

```bash
mav go settings
```

`mav go` will:

1. Build, install, and launch the app.
2. Wait for a usable accessibility tree.
3. Start video evidence.
4. Capture the start screen.
5. Follow the known route.
6. Validate that the screen changed and target assertions pass.
7. Capture the target screen.
8. Stop video, generate a report, and stop run-owned streams.

If the route does not exist, MAV fails clearly:

```text
fail code=screen_not_found screen=settings
fail code=route_not_found screen=settings
```

The agent should then explore manually with `mav ui tree`, `mav ui tap`,
`mav ui scrollUntil`, and `mav capture`. MAV itself does not invent routes.

## UI Commands

```bash
mav ui tree
mav ui tap --id element_id
mav ui tap --x 120 --y 400
mav ui tap --text "Daily Reminder"
mav ui type "hello"
mav ui swipe --direction up
mav ui wait --id element_id --timeout 5s
mav ui scrollUntil --id privacy_policy_button --direction up --max-swipes 4
```

AXe is preferred for accessibility tree, semantic taps, typing, swipes, waits,
and assertions. idb is used when it provides a concrete better capability, such
as coordinate taps or fallback device/simulator operations.

Tree first, screenshot second:

```bash
mav ui tree
mav capture
```

Screenshots are for visual layout, custom rendering, media/canvas UI, or
user-facing evidence. The tree is cheaper and better for agents.

If AXe/idb return a single empty `AXApplication` tree, MAV treats the simulator
accessibility service as unavailable. It attempts to reboot the simulator,
relaunch the app, and retry before failing.

## Native Flows

`mav run <flow.yaml>` executes a native MAV YAML flow. Use flows for repeatable
feature validation that needs navigation, waits, screenshots, logs, crashes, or
reports.

Example:

```yaml
version: 1
name: verify_daily_reminder
steps:
  - open: {}
  - evidence.start: {}
  - go: { screen: settings }
  - wait: { text: Daily Reminder, timeout: 5s }
  - evidence.step: { name: before-toggle, note: Daily Reminder before tap }
  - tap: { text: Daily Reminder }
  - waitUntil:
      any:
        - text: "Don't Allow"
        - text: "Allow"
        - changedFrom: before-toggle
      timeout: 5s
  - evidence.step: { name: after-toggle, note: Result after tapping reminder }
  - logs: { key: SettingsReached }
  - crashes: {}
  - evidence.stop: {}
  - report: {}
```

Supported step types include:

```text
open
go
tree
tap
type
swipe
wait
waitUntil
assert
capture
scrollUntil
delay
logs
exec
crashes
evidence.start
evidence.step
evidence.stop
report
```

On failure, MAV captures failure evidence when possible, stops recording, writes
report data, and returns a compact failure line.

## Evidence

Evidence is explicit. Use it when the user needs proof of verification.

For ad-hoc navigation to a mapped screen:

```bash
mav go settings
```

For a feature behavior, write a flow with named evidence steps:

```yaml
- evidence.start: {}
- go: { screen: settings }
- evidence.step: { name: before-toggle, note: Before tapping Daily Reminder }
- tap: { text: Daily Reminder }
- evidence.step: { name: after-toggle, note: After tapping Daily Reminder }
- evidence.stop: {}
- report: {}
```

The video should cover the path from launch/navigation through the tested
behavior. Screenshots should prove the specific behavior, not just that the app
opened.

MAV does not open the HTML report automatically. Open the reported file when you
want to inspect or share evidence:

```text
/tmp/mav/<run-id>/report.html
```

## Logs

`mav open`, `mav go`, and `mav run` capture a filtered unified log stream for
MAV probes into the run's `logs.txt`.

Use `OSLog.Logger` probes in app code when validating that code executed:

```swift
import OSLog

private let mavLog = Logger(
    subsystem: "mav.com.example.app",
    category: "probe"
)

mavLog.notice("MAV_LOG key=SettingsReached")
```

Then read the captured logs:

```bash
mav logs --key SettingsReached
mav logs --contains SettingsReached
mav logs --raw --key SettingsReached
```

Do not use Swift `print` for MAV validation probes. MAV is built around
filtered unified logs so the same pattern applies to simulator and device.

For flow-level shell checks, enable trusted project-local exec:

```yaml
allow_shell: true
```

Then use:

```yaml
- exec: { cmd: "grep -F 'MAV_LOG key=SettingsReached' $MAV_LOGS", contains: SettingsReached, timeout: 5s }
```

`exec` runs in the project root with `MAV_ROOT`, `MAV_RUN_ID`, `MAV_RUN_DIR`,
and `MAV_LOGS` set. This is an opt-in guard for trusted project checks, not a
security sandbox for untrusted commands.

## Simulators

List and select simulators:

```bash
mav sim list
mav sim select --device "iPhone 17 Pro Max" --ios 26 --locale es_ES --language es
```

You can also pass simulator selection flags to `mav open`:

```bash
mav open --device "iPhone 17 Pro Max" --ios 26 --locale es_ES --language es
```

## Previews

Use previews for isolated SwiftUI screens when launching the full app is too
slow or the screen is deep in a flow:

```bash
mav preview init
mav preview settings
mav ui tree
mav capture
```

`mav preview init` creates a Bazel preview host. Wire the real view and any
lightweight mocks into the generated host, then launch the preview by id.

## Cleanup

Ad-hoc `mav open` sessions keep log capture running for the current run. Stop
them when done:

```bash
mav stop
```

`mav go` and `mav run` stop run-owned streams automatically.

## Troubleshooting

`fail code=config_not_found`

Run:

```bash
mav discover
```

`fail code=screen_not_found` or `fail code=route_not_found`

The map does not know that screen or route yet. Explore manually:

```bash
mav open
mav ui tree
mav ui tap --id some_button
mav ui tree
```

Then inspect `.mav/map/**`.

`fail code=ui_tree_empty`

The simulator accessibility service did not recover after MAV retried. Re-run
`mav open` or select/boot another simulator with `mav sim select`.

`mav logs --key ...` returns no matches

Make sure the app uses `OSLog.Logger` with the configured MAV subsystem/category
and that the behavior was triggered after `mav open` started the run.
