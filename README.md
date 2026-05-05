# MAV

Mobile Agent Verifier (`mav`) is a deterministic agent-native CLI for
validating iOS apps from coding agents.

MAV gives agents a compact API to build, launch, observe, navigate, interact,
capture evidence, read logs, and check crashes. It is intentionally not an
autonomous testing agent: it runs concrete commands and returns small,
parseable results that another agent can act on. MAV output is agent-friendly by
default; there is no separate JSON mode to opt into.

## What MAV Is For

- Verifying iOS app changes from an agent without opening Xcode.
- Driving simulator UI through accessibility trees before screenshots.
- Building repeatable flows that combine UI actions, waits, screenshots, video,
  logs, crash checks, and HTML evidence.
- Maintaining an app map that lets `mav go <screen>` navigate from app launch to
  known screens.

MAV uses a project-local launch recipe to build, locate, install, and launch
the app. Bazel, Xcode, Tuist, Make, Just, and project scripts are setup-time
templates only; runtime executes the configured recipe.

## Status

MAV is early and evolving. The current stable pieces are:

- Configurable project launch recipes.
- Setup-time detection for common project launch commands.
- Simulator selection, boot, install, launch, screenshot, and video.
- Real iOS/iPadOS device selection, install, launch, screenshots, logs, and crashes where idb/CoreDevice support them.
- AXe-first simulator accessibility tree inspection and semantic interactions.
- idb coordinate taps and fallback capabilities.
- Appium-backed W3C Actions for optional multitouch gestures.
- Native MAV YAML flows through `mav run`.
- JSON app map storage in `.mav/map/**`.
- HTML evidence reports in `/tmp/mav/<run-id>/report.html`.
- Filtered unified log capture for explicit MAV probes.

## Requirements

- macOS.
- Xcode command line tools.
- Go, for development builds.
- AXe, for simulator accessibility tree and semantic UI actions.
- idb, for coordinate taps, real-device UI inspection, screenshots, logs, crashes, and fallback operations.
- Appium 2 with the XCUITest driver, optional, for true multitouch gestures
  such as pinch, rotate, and two-finger pan.

Check the local environment:

```bash
mav doctor
```

`mav doctor` reports capability availability. MAV routes commands by
capability: simulator accessibility and semantic actions use AXe, coordinate
taps and real-device fallback use idb, real-device install/launch uses
CoreDevice through `xcrun devicectl`, and multitouch uses Appium.

Configure the project or install supported helper tools:

```bash
mav setup
```

`mav setup` is idempotent. It scaffolds or refreshes `.mav/config.yaml` and the
initial app map by detecting app identity, simulator defaults, available real
iOS devices, UI tools, and an editable launch recipe. Existing explicit choices
in `.mav/config.yaml` are preserved.

```bash
mav setup --install axe idb appium
```

`mav setup --install idb` prefers pipx with Python 3.12/3.13 for `fb-idb` and
uses Homebrew for `idb-companion`. AXe uses Homebrew. For Appium it uses npm,
installs Appium globally, then installs and verifies the `xcuitest` driver. If Appium was
installed through a Node version manager, `mav doctor` also checks that the
active `node` matches the Node path used by the `appium` executable; put that
Node bin directory first in `PATH` if it reports a `multitouch_issue` related
to Node. If Appium reports that its home is not writable, MAV retries the driver
check with a temporary writable `APPIUM_HOME`; if that still fails, rerun MAV
outside the sandbox or set `APPIUM_HOME` to a writable directory.

## Install

With Homebrew:

```bash
brew install bitomule/tap/mav
```

Install the MAV skill globally with Vercel's Skills CLI:

```bash
mav install-skills
```

This runs:

```bash
npx skills add bitomule/mav --skill mav --global --yes
```

Build from source:

```bash
git clone https://github.com/bitomule/mav.git
cd mav
make build
```

Run the development binary:

```bash
.build/mav help
```

Or put it on your `PATH`:

```bash
ln -sf "$PWD/.build/mav" /usr/local/bin/mav
```

Release binaries are built by the GitHub release workflow for tagged releases.
Homebrew packaging lives in `packaging/homebrew/mav.rb` and is published to
`bitomule/tap`.

The release workflow can also update `bitomule/homebrew-tap` automatically. The
`bitomule/mav` repo must define a `COMMITTER_TOKEN` secret with permission to
push to `bitomule/homebrew-tap`; this is the same pattern used by Koubou.

## Quick Start

Run from the root of an iOS app repo:

```bash
mav setup
mav sim list
mav sim select --device "iPhone 17 Pro Max" --ios 26
mav open
mav ui tree
```

`mav setup` scaffolds `.mav/config.yaml` and an initial app map. It detects
a bundle id, selected simulator or device, locale/language, available tools,
and a launch recipe when it can infer one. It is useful for non-interactive
setup and for refreshing the generated MAV config after project structure
changes.

`mav open` executes the configured launch recipe. It creates a run directory
under `/tmp/mav/<run-id>/` and starts `logs.txt` for MAV probes.

Example compact output:

```text
ok cmd=setup bundle=com.example.app config=/repo/.mav/config.yaml launch_recipe=ok
ok cmd=open run=7fd logs=/tmp/mav/7fd/logs.txt target="iPhone 17 Pro Max"
ok cmd=ui.tree driver=axe nodes=42 screen=start screen_source=recognized
node index=1 id=settings_button label=Settings role=button frame="{{20, 120}, {180, 44}}"
```

Use `--raw` only when the underlying tool output is needed:

```bash
mav --raw ui tree
```

## Help

```bash
mav help
mav help ui
mav ui --help
```

The top-level commands are:

```text
doctor
setup
install-skills
sim
open
ui
capture
run
go
logs
stop
crashes
evidence
```

## Output Contract

Default output starts with one compact status line. Commands that inspect
structured state, such as `mav ui tree`, may add bounded detail lines after it:

```text
ok cmd=<command> key=value key=value
fail code=<error_code> key=value key=value
```

Examples:

```text
ok cmd=capture file=/tmp/mav/7fd/captures/20260503T120000.000.png run=7fd
ok cmd=logs file=/tmp/mav/7fd/logs.txt matches=1 run=7fd
fail code=screen_not_found next="explore with mav ui tree/tap; map updates when the next screen is observed" screen=settings
fail code=ui_tree_empty driver=axe reason=simulator_accessibility_unavailable recovered=false
```

The goal is to give agents the minimum useful fields: what happened, where the
artifact is, and what to do next when the command failed.

## Project And Run State

Project state:

```text
.mav/config.yaml
.mav/map/index.json
.mav/map/screens/*.json
.mav/map/current.json
.mav/map/pending.json
```

Run state:

```text
/tmp/mav/<run-id>/logs.txt
/tmp/mav/<run-id>/commands.jsonl
/tmp/mav/<run-id>/evidence.jsonl
/tmp/mav/<run-id>/steps/*.png
/tmp/mav/<run-id>/trees/*.json
/tmp/mav/<run-id>/video.mov
/tmp/mav/<run-id>/crashes/
/tmp/mav/<run-id>/report.html
```

`/tmp` may resolve to a macOS per-user temporary directory such as
`/var/folders/.../T`.

## App Map

The app map is JSON. It is updated by normal MAV commands:

1. `mav open` resets the current screen to the configured start screen.
2. `mav ui tree` records the current accessibility tree and screen elements.
3. `mav ui tap ...` records a pending action from the current screen.
4. The next `mav ui tree` observes the next screen and writes the route edge.

Basic mapping loop:

```bash
mav open
mav ui tree
mav ui tap --id home_settings_button
mav ui tree
```

When navigating to a new screen, first make sure the destination exposes stable
accessibility identifiers or a visible title in `mav ui tree`. If the next tree
reports `screen=unknown map_pending=true`, the tap was recorded but the map did
not learn a route yet; add or expose accessibility ids, capture/inspect the
screen, then run `mav ui tree` again before relying on `mav go`.

Prefer target selectors in this order:

1. Accessibility id: `mav ui tap --id home_settings_button`
2. Coordinates: `mav ui tap --x 398 --y 84`
3. Text: `mav ui tap --text Settings`

Coordinates should be used only when the accessibility tree is insufficient and
a screenshot makes the target unambiguous. Coordinate taps are visual fallbacks,
not the primary way to create reliable routes. Text is the last fallback because
labels change with localization and copy edits.

Review `.mav/map/**` diffs before relying on a route. The map is source-level
project state, not temporary run output.

## Navigation

`mav go <screen-id>` starts from app launch and follows a known route in the app
map.

```bash
mav go settings
```

It will:

1. Build, install, and launch the app.
2. Wait for a usable accessibility tree.
3. Start video recording.
4. Capture the start screen.
5. Execute each mapped route edge with MAV UI primitives.
6. Validate that each edge changes the tree.
7. Validate target screen assertions when the map has them.
8. Capture the target screen.
9. Stop video.
10. Generate `report.html`.
11. Stop run-owned streams.

If the screen or route is unknown, MAV fails and does not explore:

```text
fail code=screen_not_found screen=settings
fail code=route_not_found screen=settings
```

The caller should then explore manually with `mav ui tree`, `mav ui tap`,
`mav ui scrollUntil`, and `mav capture`.

## UI Commands

```bash
mav ui tree
mav ui tap --id element_id
mav ui tap --x 120 --y 400
mav ui tap --text "Daily Reminder"
mav ui type "hello"
mav ui swipe --direction up
mav ui swipe --start-x 220 --start-y 760 --end-x 220 --end-y 260
mav ui pinch --x 200 --y 450 --scale 0.5
mav ui pinch --x 200 --y 450 --scale 0.5 --pan-x 80 --pan-y -40
mav ui rotate --x 200 --y 450 --degrees 30
mav ui twoFingerPan --x 200 --y 450 --pan-x 80 --pan-y -40
mav ui actions --file .mav/actions/map-zoom.json
mav ui wait --id element_id --timeout 5s
mav ui scrollUntil --id privacy_policy_button --direction up --max-swipes 4
```

MAV chooses drivers by capability. AXe is the default for simulator
accessibility tree inspection, semantic taps, typing, swipes, waits, and
assertions. AXe is simulator-only. On real devices, use idb-backed tree,
coordinate actions, screenshots, logs, and crashes; semantic AXe actions fail
with a `tool_missing` hint that points back to coordinates or a simulator.

Appium is only used for true multitouch. AXe and idb do not expose a real
pinch or two-finger gesture primitive. MAV sends Appium W3C Actions: multiple
touch sources execute step-by-step in concurrent ticks, which lets a flow move
two fingers at the same time for pinch+pan, rotate, and two-finger drag.

Observation priority:

1. `mav ui tree`
2. `mav capture`
3. Video through `mav evidence start/stop` or flows

Screenshots are for visual layout, custom rendering, media/canvas UI, or
user-facing proof. The accessibility tree is cheaper and more useful for most
agent decisions.

If AXe/idb return a single empty `AXApplication` tree, MAV treats simulator
accessibility as unavailable. It attempts a simulator reboot, app relaunch, and
tree retry before returning `ui_tree_empty`.

## Native MAV Flows

`mav run <flow.yaml>` executes a native MAV YAML flow.

Use flows for repeatable feature validation:

```yaml
version: 1
name: verify_daily_reminder
steps:
  - open: {}
  - go: { screen: settings }
  - wait: { text: Daily Reminder, timeout: 5s }
  - video.start: {}
  - evidence.step: { name: before-toggle, note: Daily Reminder before tap }
  - tap: { text: Daily Reminder }
  - waitUntil:
      any:
        - text: "Don't Allow"
        - text: "Allow"
        - changedFrom: before-toggle
      timeout: 5s
  - evidence.step: { name: after-toggle, note: Result after tapping reminder }
  - pinch: { x: 200, y: 450, scale: 0.5, panX: 80, panY: -40, duration: 800ms }
  - logs: { key: SettingsReached }
  - crashes: {}
  - video.stop: {}
  - report: {}
```

Supported step types:

```text
open
go
tree
tap
type
swipe
pinch
rotate
twoFingerPan
actions
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
video.start
video.stop
report
```

On failure, MAV stops run-owned processes, tries to capture failure evidence,
writes report data, and returns a compact failure line.

Use `wait` for a single `id`, `text`, or `value`. Use `waitUntil` with `any`
when more than one result is acceptable, and use `changedFrom` after a named
evidence step when the UI change is visual rather than semantic.

## Evidence

Evidence is explicit. Use it when a user needs proof of verification.

For ad-hoc navigation to a mapped screen:

```bash
mav go settings
```

For feature behavior, use a flow with named evidence points:

```yaml
- open: {}
- go: { screen: settings }
- wait: { id: daily_reminder_button, timeout: 5s }
- video.start: {}
- evidence.step: { name: before-toggle, note: Before tapping Daily Reminder }
- tap: { id: daily_reminder_button }
- waitUntil:
    any:
      - id: notification_permission_alert
      - changedFrom: before-toggle
    timeout: 5s
- evidence.step: { name: after-toggle, note: After tapping Daily Reminder }
- video.stop: {}
- report: {}
```

Start recording as late as possible: navigate and wait for the state first when
navigation is setup, then record the behavior under test. Screenshots should
prove the behavior itself, not only that the app opened. The supported recording
flow steps are `video.start` and `video.stop`; `evidence.start` and
`evidence.stop` remain supported aliases. Flows do not have a `recordVideo`
option.

`mav evidence report` prints `video=<path>` only when a valid video exists, and
`video=missing` when the report has no recording. A report without `video.mov`
does not prove video evidence was captured.

MAV does not open HTML automatically. Inspect the reported file:

```text
/tmp/mav/<run-id>/report.html
```

## Logs

`mav open`, `mav go`, and `mav run` capture a filtered unified log stream for
MAV probes into `logs.txt`.

Use `OSLog.Logger` probes to prove code execution:

```swift
import OSLog

private let mavLog = Logger(
    subsystem: "mav.com.example.app",
    category: "probe"
)

mavLog.notice("MAV_LOG key=SettingsReached")
```

Then read logs from the current run:

```bash
mav logs --key SettingsReached
mav logs --contains SettingsReached
mav --raw logs --key SettingsReached
```

Do not use Swift `print` for MAV validation probes. MAV is designed around
filtered unified logs so the same probe pattern applies to simulator and device.

For trusted project-local shell assertions, opt in through `.mav/config.yaml`:

```yaml
allow_shell: true
```

Then use an `exec` step:

```yaml
- exec: { cmd: "grep -F 'MAV_LOG key=SettingsReached' $MAV_LOGS", contains: SettingsReached, timeout: 5s }
```

`exec` runs in the project root with `MAV_ROOT`, `MAV_RUN_ID`, `MAV_RUN_DIR`,
and `MAV_LOGS` set. This is an opt-in guard for trusted project checks, not a
security sandbox for untrusted commands.

## Simulators

```bash
mav sim list
mav sim select --device "iPhone 17 Pro Max" --ios 26 --locale es_ES --language es
mav sim select --udid <simulator-udid>
mav sim boot
```

You can also pass simulator selection flags to `mav open`:

```bash
mav open --device "iPhone 17 Pro Max" --ios 26 --locale es_ES --language es
```

## Real Devices

MAV can target paired physical iOS/iPadOS devices through CoreDevice and idb:

```bash
mav device list
mav device select --id <coredevice-id>
mav device select --name "David iPhone"
mav open --target device --device-id <coredevice-id>
```

Device selection stores `target_type: device`, the CoreDevice identifier, the
hardware UDID used by idb/Appium, the display name, model, and OS version in
`.mav/config.yaml`. Device launch recipes use:

```bash
xcrun devicectl device install app --device "$MAV_DEVICE_ID" "$MAV_APP_PATH"
xcrun devicectl device process launch --device "$MAV_DEVICE_ID" --terminate-existing "$MAV_BUNDLE_ID"
```

Real-device screenshots and screenshot evidence use idb. Video evidence remains
simulator-only and returns `video_unsupported target=device` on physical
devices.

## Launch Recipes

MAV does not own the build system. Configure project commands in
`.mav/config.yaml`:

```yaml
app:
  bundle_id: com.example.app
  process_name: Example

launch:
  mode: custom
  commands:
    build: ./scripts/mav-build.sh
    app_path: ./scripts/mav-app-path.sh
    install: xcrun simctl install "$MAV_UDID" "$MAV_APP_PATH"
    launch: xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"
```

Each command runs from `MAV_ROOT` with stable environment variables:
`MAV_ROOT`, `MAV_RUN_DIR`, `MAV_TARGET_TYPE`, `MAV_UDID`, `MAV_DEVICE_ID`,
`MAV_DEVICE_UDID`, `MAV_BUNDLE_ID`, `MAV_APP_PATH`, `MAV_DEVICE_NAME`,
`MAV_RUNTIME`, and `MAV_PLATFORM`. `MAV_UDID` is preserved for compatibility:
it is the simulator UDID for simulator targets and the hardware UDID for device
targets. `app_path` must print one `.app` path. If the app is already
installed, configure only `launch`.

## Cleanup

Ad-hoc `mav open` sessions keep log capture running for the current run. Stop
them when done:

```bash
mav stop
```

`mav go` and `mav run` stop run-owned streams automatically.

## Command Reference

```text
mav doctor
mav setup
mav setup --install axe idb appium
mav install-skills
mav sim list
mav sim select --device NAME --ios VERSION [--locale LOCALE] [--language LANG]
mav sim select --udid UDID
mav sim boot
mav device list
mav device select --id DEVICE_ID
mav device select --name DEVICE_NAME
mav open [--target simulator|device] [--device NAME] [--ios VERSION] [--udid UDID] [--device-id DEVICE_ID] [--locale LOCALE] [--language LANG]
mav ui tree
mav ui tap --id ID
mav ui tap --x X --y Y
mav ui tap --text TEXT
mav ui type TEXT
mav ui swipe [--direction up|down|left|right]
mav ui pinch --x X --y Y --scale SCALE [--pan-x DX] [--pan-y DY] [--distance D] [--angle DEG] [--rotate DEG] [--duration 800ms] [--hold DURATION]
mav ui rotate --x X --y Y --degrees DEG [--distance D] [--duration 800ms] [--hold DURATION]
mav ui twoFingerPan --x X --y Y --pan-x DX --pan-y DY [--distance D] [--angle DEG] [--duration 800ms] [--hold DURATION]
mav ui actions --file actions.json
mav ui wait --id ID [--timeout 5s]
mav ui scrollUntil --id ID [--direction up] [--max-swipes 5]
mav capture [--name NAME] [--run RUN_ID]
mav run flow.yaml
mav go <screen-id>
mav logs [--run RUN_ID] [--key KEY] [--contains TEXT] [--level LEVEL]
mav stop [--run RUN_ID]
mav crashes [--raw]
mav evidence start [--run RUN_ID]
mav evidence step --name NAME [--note NOTE] [--run RUN_ID]
mav evidence stop [--note NOTE] [--no-capture] [--run RUN_ID]
mav evidence report [--run RUN_ID]
```

## Troubleshooting

`fail code=config_not_found`

Run:

```bash
mav setup
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
`mav open` or select another simulator with `mav sim select`.

`CoreSimulator`, `idb`, or Appium permission failures

MAV needs direct simulator/device access for launch, accessibility, coordinate
taps, screenshots, video, and multitouch. If output says to rerun outside the
sandbox, do that instead of retrying the same command in the sandbox.

`mav logs --key ...` returns no matches

Make sure the app logs with `OSLog.Logger` using the configured MAV subsystem
and category, and make sure the behavior happened after MAV started the run.

## Development

```bash
make test
make build
make check
```

`make check` runs `gofmt`, tests, and a local build.

## Contributing

Issues and pull requests are welcome. Keep changes deterministic and preserve
compact output: commands should report the minimum information an agent needs to
continue, parse, or present evidence.

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. See [LICENSE](LICENSE).
