<p align="left">
  <img src="assets/logo.png" alt="mav logo" width="120">
</p>

# MAV

[![CI](https://github.com/bitomule/mav/actions/workflows/ci.yml/badge.svg)](https://github.com/bitomule/mav/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/bitomule/mav?display_name=tag)](https://github.com/bitomule/mav/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The iOS control plane for AI coding agents: one command surface, native drivers underneath, and evidence your agent can hand back to a human.

<p align="center">
  <img src="assets/hero.png" alt="A split screen showing an iOS simulator on the left and a terminal on the right running mav tap, mav axtree, and mav screenshot, with compact agent-readable output" width="820">
</p>

Mobile Agent Verifier (`mav`) is the interface between an agent and iOS. The
agent asks for intent-level operations like `ui tree`, `tap`, `pinch`,
`network start`, or `evidence report`; MAV routes each operation to the best
native backend available on that target, records what happened, and returns a
compact result the next turn can act on.

MAV is intentionally not an autonomous testing agent. It runs the command. The agent decides what to run next.

## What MAV Does Best

MAV has two jobs:

1. Route agent intent to the right iOS driver.
2. Produce evidence that is useful after the command finishes.

The router is the core value. Agents should not need to know when to use AXe,
Baguette, idb, simctl, or mitmproxy. They should ask for a capability and get a
deterministic answer:

- Accessibility tree, semantic taps, waits, and screenshots go through AXe when
  it is healthy.
- Simulator multitouch, system UI, hardware buttons, erase, and hideKeyboard go
  through Baguette.
- Physical device install, launch, coordinate input, logs, screenshots, and
  crashes go through idb.
- Simulator lifecycle, video, and logs go through simctl.
- Simulator network evidence goes through mitmproxy HAR capture.

The evidence layer makes the result inspectable. A run can include accepted
video, named screenshots, accessibility tree snapshots, log tails, crash
reports, command trails, and network HAR summaries. The CLI writes the verified
manifest; the MAV skill turns it into a visual HTML report for humans.

## What MAV Is For

- Giving agents one stable iOS API instead of a pile of tool-specific CLIs.
- Verifying iOS app changes without opening Xcode or writing a one-off test.
- Driving simulator and device UI through accessibility trees before falling
  back to screenshots or coordinates.
- Capturing proof windows with video, screenshots, logs, crashes, commands, and
  optional network traffic.
- Building repeatable flows that are still agent-readable and debuggable.

MAV uses a project-local launch recipe to build, locate, install, and launch
the app. Bazel, Xcode, Tuist, Make, Just, and project scripts are setup-time
templates only; runtime executes the configured recipe.

## Why MAV?

### vs Maestro

Maestro is for humans: a fluent YAML DSL, a recording mode, a flow file you read top-to-bottom. MAV is for agents: single-shot commands, one compact line per result, native iOS drivers, and evidence artifacts that survive the session. If you already write Maestro flows for human testing, MAV does not replace them — it gives your agent a different kind of access.

### vs XCUITest

XCUITest knows what the test file says. It does not know what the screen actually looks like right now. MAV reads the live accessibility tree and can attach video, screenshots, logs, crashes, and HAR evidence to the run, so the agent can react to the state the app actually reached.

### vs Appium

Appium covers Android, iOS, Windows, web, and a long tail of platforms — at the cost of a heavy stack and a slower path to "did the tap happen?". MAV is macOS-only and iOS-only on purpose: it leans directly on `simctl`, AXe, `idb`, Baguette, and mitmproxy. The May 2026 driver overhaul removed Appium/WDA from the pipeline entirely; everything runs through native host-side drivers now.

### vs Detox

Detox is the right answer if you are shipping React Native and want gray-box testing tied to the JS runtime. MAV is app-stack-agnostic — Swift, RN, Flutter, anything that ends up in a `.app` bundle. The price is that MAV does not know about your framework; it only knows what the simulator shows.

## How an agent uses MAV

<p align="center">
  <img src="assets/loop.png" alt="The mav loop: agent decides next action, mav executes a deterministic command, agent reads the compact output, loops" width="720">
</p>

Each call is one verb. The agent picks the next verb based on the previous output. The full vocabulary lives below in the Command Reference, but the four verbs that cover most flows are `tap`, `tree` (accessibility tree), `screenshot`, and `logs`.

## Used at

`mav` runs in development on these production iOS apps:

- [Undolly](https://undolly.app) — finding duplicate photos
- [Boxy](https://boxy-app.com/) — organising physical items
- [HiddenFace](https://hiddenface.app) — privacy-first face blur

## Status

MAV is early and evolving. The current stable pieces are:

- Configurable project launch recipes.
- Setup-time detection for common project launch commands.
- Simulator selection, boot, install, launch, screenshot, and video.
- Physical device selection, install, launch, logs, screenshots, UI actions,
  crashes, and evidence screenshots.
- AXe-first accessibility tree inspection and semantic interactions.
- idb coordinate taps and device/simulator fallback capabilities.
- Baguette-backed multitouch gestures, system UI tree, hardware buttons, and
  keyboard helpers on simulator.
- Native MAV YAML flows through `mav run`.
- Verified evidence manifests in `.mav/runs/<run-id>/report.json`; the MAV
  skill authors the visual HTML report from that data.
- Filtered unified log capture for explicit MAV probes.

![MAV driver router](assets/router.svg)

## Requirements

- macOS.
- Xcode command line tools.
- Go, for development builds.
- AXe, for accessibility tree and semantic UI actions.
- idb, for coordinate taps and device/simulator fallback operations.
- Baguette, for simulator multitouch (pinch, two-finger pan), the
  SpringBoard / system UI tree, hardware buttons, keyboard erase, and
  hideKeyboard. Sim-only — device multitouch is intentionally unsupported.
- mitmproxy, optional, for `mav network start|stop` HAR capture on the
  simulator. Install with `mav setup --install mitmproxy`.

Check the local environment:

```bash
mav doctor
```

`mav doctor` reports capability availability. MAV routes commands by
capability: accessibility and semantic actions use AXe, coordinate taps and
device fallback use idb, multitouch and system UI use baguette on simulator.
Physical iOS devices require idb for install, launch, logs, screenshots, and
crashes. Multitouch gestures, system-UI trees, and hideKeyboard return
structured errors on device — use a simulator for those flows.

Configure the project or install supported helper tools:

```bash
mav setup
```

`mav setup` is idempotent and interactive by default. It scaffolds or refreshes
`.mav/config.yaml` by detecting app identity, simulator defaults, UI tools, and
an editable launch recipe, then asks you to accept or replace each value.
Existing explicit choices in `.mav/config.yaml` are preserved. Use
`mav setup --non-interactive` for CI/scripts.

```bash
mav setup --install axe idb baguette
```

`mav setup --install idb` prefers pipx with Python 3.12/3.13 for `fb-idb` and
uses Homebrew for `idb-companion`. AXe and Baguette are installed via Homebrew
(`cameroncooke/axe/axe` and `tddworks/baguette/baguette`). MAV does not require
Node, npm, Java, or any Appium component (those were dropped in the May 2026
driver overhaul).

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

`mav setup` scaffolds `.mav/config.yaml`. By default it is interactive: MAV detects a bundle id, selected simulator, locale/language,
available tools, and a launch recipe when it can infer one, then lets you accept
or replace each value. Use `mav setup --non-interactive` for CI/scripts.
Launch recipe detection is intentionally conservative: MAV recognizes explicit
`Makefile`/`justfile` MAV targets, `scripts/mav-build` plus
`scripts/mav-app-path`, and standard Bazel/Tuist/Xcode project shapes.

`mav open` executes the configured launch recipe. It creates a persistent run
directory under `.mav/runs/<run-id>/` and starts `logs.txt` for MAV probes. Use
`mav open --clear-state` to uninstall the configured bundle before install and
launch. If a Bazel app bundle from `bazel-out` fails simulator install with a
permission error, MAV copies the `.app` into the run directory with writable
permissions and retries the install.

Use `mav open --no-relaunch` when the app was launched manually with custom
environment such as `SIMCTL_CHILD_*` and MAV should only attach run logging to
the app already in front.

Example compact output:

```text
ok cmd=setup bundle=com.example.app config=/repo/.mav/config.yaml launch_recipe=ok multitouch=missing multitouch_next="mav setup --install baguette"
ok cmd=open run=7fd logs=/repo/.mav/runs/7fd/logs.txt target="iPhone 17 Pro Max"
ok cmd=ui.tree driver=axe nodes=42 screen=unknown recognized_screen=settings screen_source=recognized
node index=1 id=settings_button label=Settings role=button enabled=true frame="{{20, 120}, {180, 44}}"
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
logs
network
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
.mav/runs/<run-id>/logs.txt
.mav/runs/<run-id>/commands.jsonl
.mav/runs/<run-id>/evidence.jsonl
.mav/runs/<run-id>/steps/*.png
.mav/runs/<run-id>/trees/*.json
.mav/runs/<run-id>/video.mov
.mav/runs/<run-id>/crashes/
.mav/runs/<run-id>/report.json
```

`/tmp` may resolve to a macOS per-user temporary directory such as
`/var/folders/.../T`.

Prefer target selectors in this order:

1. Accessibility id: `mav ui tap --id home_settings_button`
2. Coordinates: `mav ui tap --x 398 --y 84`
3. Text: `mav ui tap --text Settings`

Coordinates should be used only when the accessibility tree is insufficient and
a screenshot makes the target unambiguous. Text is the last fallback because
labels change with localization and copy edits.

## UI Commands

```bash
mav ui tree
mav ui tree --prefer-driver axe
mav ui tree --include-system
mav ui tap --id element_id
mav ui tap --x 120 --y 400
mav ui tap --text "Daily Reminder"
mav ui type "hello"
mav ui erase --focused
mav ui hideKeyboard
mav ui swipe --direction up
mav ui swipe --start-x 220 --start-y 760 --end-x 220 --end-y 260
mav ui longPress --x 200 --y 450 --duration 800ms
mav ui pinch --x 200 --y 450 --scale 0.5
mav ui pinch --x 200 --y 450 --scale 0.5 --pan-x 80 --pan-y -40
mav ui twoFingerPan --x 200 --y 450 --pan-x 80 --pan-y -40
mav ui wait --id element_id --timeout 5s
mav ui wait --text "Privacy Policy" --timeout 5s
mav ui wait --value "Email" --timeout 5s
mav ui scrollUntil --id privacy_policy_button --direction up --max-swipes 4
```

MAV chooses drivers by capability. AXe is the default fast path for
accessibility tree inspection, semantic taps, typing, swipes, waits, and
assertions. idb is used for coordinate taps and device/simulator fallback
operations. Baguette provides multitouch, system UI, hardware buttons, erase,
and hideKeyboard on simulator.

For `mav ui tree` and semantic `mav ui tap`, `--prefer-driver auto` is the
default. Use `--prefer-driver axe` to debug AXe-only behavior. `mav ui tree
--include-system` asks baguette for the SpringBoard/system tree when a system
process or cross-app surface is in front (PHPicker, App Tracking Transparency,
permission prompts, SpringBoard, iOS 26 service processes). System-tree
inspection is simulator-only.

If `mav ui tap --text X` fails because AXe sees `X` as a value/placeholder but
not as a label, MAV reports `ui_tap_text_no_label_match` with `matched_value`.
Prefer stable accessibility ids when possible.

`mav ui erase` and `mav ui hideKeyboard` dispatch through baguette on
simulator. On a physical device they return `erase_unsupported_on_device` and
`hide_keyboard_unsupported_on_device` respectively. Tap and retype the field,
or tap outside the input area to dismiss the keyboard.

True multitouch gestures that Baguette currently exposes (pinch and
two-finger pan) go through baguette on simulator. On device they return
`gesture_unsupported_on_device` with a remediation hint — use a simulator for
multitouch flows. Rotate and W3C Actions remain reserved flow/CLI surfaces
until MAV adds a reliable Baguette translation for them.

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
  - open: { clearState: true }      # clear-state is also accepted
  - go: { screen: settings }
  - wait: { text: Daily Reminder, timeout: 5s }
  - evidence.start: { network: true }
  - evidence.step: { name: before-toggle, note: Daily Reminder before tap }
  - tap: { text: Daily Reminder }
  - type: "Search text"
  - type: { text: "user@example.com" }
  - erase: { focused: true }
  - hideKeyboard: {}
  - delay: 500ms
  - when: { visible: { text: Continue } }
    do:
      - tap: { text: Continue }
  - whileNotVisible:
      text: "You"
      timeout: 30s
      do:
        - tap: { id: onboarding_dismiss, optional: true }
        - delay: 500ms
  - waitUntil:
      any:
        - text: "Don't Allow"
        - text: "Allow"
        - changedFrom: before-toggle
      timeout: 5s
  - evidence.step: { name: after-toggle, note: Result after tapping reminder }
  - pinch: { x: 200, y: 450, scale: 0.5, panX: 80, panY: -40, duration: 800ms }
  - twoFingerPan: { x: 200, y: 450, panX: 80, panY: -40, duration: 800ms }
  - logs: { key: SettingsReached }
  - crashes: {}
  - evidence.stop: {}
  - report: {}
```

Semantic flow steps inherit the process-level `--prefer-driver auto|axe`
setting from `mav run`. A step can override it with `prefer-driver` when one
interaction needs a specific backend:

```yaml
- tap: { text: "Deporte y ocio", prefer-driver: axe }
- wait: { text: "Continuar", prefer-driver: axe, timeout: 5s }
```

This applies to `tree`, `tap`, `swipe`, `wait`, `assert`, `waitUntil`, and
`scrollUntil`.

Supported step types:

```text
open
go
tree
tap
type
erase
hideKeyboard
swipe
pinch
twoFingerPan
wait
waitUntil
when
whileNotVisible
include
assert
capture
scrollUntil
delay
sleep
logs
exec
crashes
network.start
network.stop
network.status
evidence.start
evidence.step
evidence.stop
video.start
video.stop
report
```

`hideKeyboard` dispatches through baguette on simulator. On device it returns
`hide_keyboard_unsupported_on_device`.

`type`, `delay`, and `sleep` accept both scalar and object forms. These are
equivalent:

```yaml
- type: "Search text"
- type: { text: "Search text" }
- delay: 500ms
- delay: { duration: 500ms }
- sleep: 500ms
- sleep: { duration: 500ms }
```

On failure, MAV stops run-owned processes, tries to capture failure evidence,
writes report data, and returns a compact failure line.

Use `wait` for a single `id`, `text`, or `value`. Use `waitUntil` with `any`
when more than one result is acceptable, and use `changedFrom` after a named
evidence step when the UI change is visual rather than semantic.

Use `when` for optional UI. MAV evaluates the condition once; if it is visible,
it runs the `do` block, otherwise it skips the block without failing. `do`
blocks are for UI/evidence steps and cannot contain `open` or `exec`:

```yaml
- when: { visible: { id: ToggleX } }
  do:
    - tap: { id: ToggleX }
```

Use `whileNotVisible` for chained onboarding or permission surfaces. MAV repeats
the `do` block until the target `id`, `text`, `value`, or `any` condition is
visible, or until `timeout` expires:

```yaml
- whileNotVisible:
    text: "You"
    timeout: 30s
    do:
      - tap: { id: dismiss_button, optional: true }
      - delay: 500ms
```

Use `include` to compose reusable sub-flows. The included file path is resolved
relative to the file that declares it, and `env` values are available to the
included flow as `${env.NAME}`. The `file` field may also reference values from
the same `env` block:

```yaml
- include:
    file: "components/auth/${env.USER}.mav.yaml"
    env:
      USER: sellersXp
      FRESH_INSTALL: true
```

## Evidence

Evidence is explicit. Use it when a user needs proof of verification.

For feature behavior, use a flow with named evidence points:

```yaml
- open: {}
- tap: { id: HomeView.settingsButton }
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
prove the behavior itself, not only that the app opened. The supported video
recording flow steps are `video.start` and `video.stop`; `evidence.start` and
`evidence.stop` remain supported aliases. Add `network: true` to
`evidence.start` when the proof window should also capture a simulator HAR via
mitmproxy:

```yaml
- evidence.start: { network: true }
- tap: { id: refresh_button }
- wait: { id: loaded_state, timeout: 10s }
- evidence.stop: {}
- report: {}
```

Flows can also control network capture explicitly:

```yaml
- network.start: {}
- tap: { id: refresh_button }
- network.status: {}
- network.stop: {}
```

`mav evidence report` writes `.mav/runs/<run-id>/report.json` for project runs
and prints
`video=<path>` only when a valid video exists. It prints `video=missing` when
the run has no recording, and `video=invalid` with `video_issue=...` when the
file exists but is not acceptable evidence. When `network.har` exists, the
manifest includes request, response, status, and domain counts so the HTML
report can prove which network traffic happened inside the evidence window. A
report without an accepted video does not prove video evidence was captured.

The CLI owns the evidence data. The MAV skill owns the visual HTML report: it
reads the manifest, uses `skills/mav/templates/evidence-report.html` as a
reference, and writes a self-contained `.mav/runs/<run-id>/report.html`
tailored to the run. MAV does not open HTML automatically; inspect the reported
HTML file after the skill writes it.

## Logs

`mav open` and `mav run` capture a filtered unified log stream into `logs.txt`.
The predicate includes the configured MAV probe subsystem/category, `MAV_LOG`
messages, the app process when `process_name` is configured, and the app bundle
subsystem when `bundle_id` is configured.

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

Prefer `OSLog.Logger` for validation probes. `NSLog` from the configured app
process is also captured when `process_name` is set.

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

Use `out` to bind trimmed stdout for later steps. The binding name must use
letters, numbers, `_`, or `-`, and cannot start with a number or `-`. JSON
stdout exposes nested fields; plain text stdout is available as the binding
itself:

```yaml
- exec:
    cmd: "node utils/get_test_user.js sellersXp"
    out: credentials
    timeout: 10s
- tap: { id: EmailField }
- type: "${exec.credentials.email}"
```

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

## Physical Devices

List and select connected iOS devices:

```bash
mav device list
mav device select --udid <device-udid>
mav device select --name "David iPhone"
```

`mav device select` switches the active target to `target_kind: device` in
`.mav/config.yaml`. `mav sim select` switches it back to `target_kind:
simulator`. For physical devices, MAV uses idb for install, launch, log
capture, screenshots, and crash listing:

```yaml
launch:
  mode: custom
  commands:
    build: ./scripts/mav-build-device.sh
    app_path: ./scripts/mav-app-path-device.sh
    install: idb install --udid "$MAV_UDID" "$MAV_APP_PATH"
    launch: idb launch --udid "$MAV_UDID" -f "$MAV_BUNDLE_ID"
```

The generated simulator install/launch recipe is automatically mapped to idb
when the active target is a physical device. Video recording is simulator-only
in this release; use `capture` / `evidence.step` screenshots for device
evidence.

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
`MAV_ROOT`, `MAV_RUN_DIR`, `MAV_TARGET_KIND`, `MAV_IS_DEVICE`, `MAV_UDID`,
`MAV_BUNDLE_ID`, `MAV_APP_PATH`, `MAV_DEVICE_NAME`, `MAV_RUNTIME`, and
`MAV_PLATFORM`. `app_path` must print one `.app` path. If the app is already
installed, configure only `launch`.

`mav open --clear-state` runs `xcrun simctl uninstall "$MAV_UDID"
"$MAV_BUNDLE_ID" || true` before the launch recipe. When the configured install
step fails with a permission error for a `bazel-out` `.app`, MAV retries with a
writable copy at `/tmp/mav/<run-id>/app.tmp/<App>.app`.

## Cleanup

Ad-hoc `mav open` sessions keep log capture running for the current run. Stop
them when done:

```bash
mav stop
```

`mav run` stops run-owned streams automatically.

## Command Reference

```text
mav doctor
mav setup [--non-interactive]
mav setup --install axe idb baguette
mav install-skills
mav sim list
mav sim select --device NAME --ios VERSION [--locale LOCALE] [--language LANG]
mav sim select --udid UDID
mav sim boot
mav device list
mav device select --udid UDID
mav device select --name NAME
mav open [--device NAME] [--ios VERSION] [--udid UDID] [--locale LOCALE] [--language LANG] [--clear-state] [--no-relaunch]
mav ui tree [--prefer-driver auto|axe] [--include-system]
mav ui tap --id ID [--prefer-driver auto|axe]
mav ui tap --x X --y Y
mav ui tap --text TEXT [--prefer-driver auto|axe]
mav ui type TEXT [--prefer-driver auto|axe]
mav ui erase [--id ID | --text TEXT | --value VALUE | --focused true]
mav ui hideKeyboard
mav ui swipe [--direction up|down|left|right]
mav ui longPress --x X --y Y [--duration 800ms]
mav ui pinch --x X --y Y --scale SCALE [--pan-x DX] [--pan-y DY] [--distance D] [--angle DEG] [--rotate DEG] [--duration 800ms] [--hold DURATION]
mav ui rotate --x X --y Y --degrees DEG [--distance D] [--duration 800ms] [--hold DURATION]
mav ui twoFingerPan --x X --y Y --pan-x DX --pan-y DY [--distance D] [--angle DEG] [--duration 800ms] [--hold DURATION]
mav ui actions --file actions.json
mav ui wait --id ID [--timeout 5s]
mav ui scrollUntil --id ID [--direction up] [--max-swipes 5]
mav capture [--name NAME] [--run RUN_ID]
mav run flow.yaml
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

`fail code=ui_tap_failed` after a screen transition

The target element is not in the current AX tree. Inspect what mav sees:

```bash
mav open
mav ui tree --include-system
```

Then refine the selector based on what shows up. Prefer accessibility ids over
text.

`fail code=ui_tree_empty`

The simulator accessibility service did not recover after MAV retried. Re-run
`mav open` or select another simulator with `mav sim select`.

`CoreSimulator` or `idb` permission failures

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
