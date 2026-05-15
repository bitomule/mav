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

MAV uses a project-local launch recipe to build, locate, install, and launch
the app. Bazel, Xcode, Tuist, Make, Just, and project scripts are setup-time
templates only; runtime executes the configured recipe.

## Status

MAV is early and evolving. The current stable pieces are:

- Configurable project launch recipes.
- Setup-time detection for common project launch commands.
- Simulator selection, boot, install, launch, screenshot, and video.
- Physical device selection, install, launch, logs, screenshots, UI actions,
  crashes, and evidence screenshots.
- AXe-first accessibility tree inspection and semantic interactions.
- idb coordinate taps and fallback capabilities.
- Appium-backed WDA fallback for system-process trees, form wrappers, and
  optional multitouch gestures.
- Native MAV YAML flows through `mav run`.
- HTML evidence reports in `/tmp/mav/<run-id>/report.html`.
- Filtered unified log capture for explicit MAV probes.

## Requirements

- macOS.
- Xcode command line tools.
- Go, for development builds.
- AXe, for accessibility tree and semantic UI actions.
- idb, for coordinate taps and device/simulator fallback operations.
- Appium 2 with the XCUITest driver, optional, for WDA-backed tree/tap
  fallback, system UI such as PHPicker and permission prompts, and true
  multitouch gestures such as pinch, rotate, and two-finger pan.

Check the local environment:

```bash
mav doctor
```

`mav doctor` reports capability availability. MAV routes commands by
capability: accessibility and semantic actions use AXe, coordinate taps and
device fallback use idb, and WDA-backed fallback or multitouch uses Appium.
Physical iOS devices require idb for install, launch, logs, screenshots, and
crashes. Appium/WDA can also target a selected physical device, but the
Appium/XCUITest signing setup for WDA must be configured outside MAV when your
device requires it.

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
mav setup --install axe idb appium
```

`mav setup --install idb` prefers pipx with Python 3.12/3.13 for `fb-idb` and
uses Homebrew for `idb-companion`. AXe uses Homebrew. For Appium it uses npm,
installs Appium globally, then installs and verifies the `xcuitest` driver. If
the default `xcuitest` driver requires a newer Appium server than the installed
one, MAV retries with `xcuitest@8`, which is compatible with Appium 2.x. If
Appium was installed through a Node version manager, `mav doctor` also checks
that the active `node` matches the Node path used by the `appium` executable;
put that Node bin directory first in `PATH` if it reports a `multitouch_issue`
related to Node. If Appium reports that its home is not writable, MAV retries
the driver check with a temporary writable `APPIUM_HOME`; if that still fails,
rerun MAV outside the sandbox or set `APPIUM_HOME` to a writable directory.

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

Use `mav open --no-relaunch --warm-appium` when the app was launched manually
with custom environment such as `SIMCTL_CHILD_*` and MAV should only attach run
logging/Appium to the app already in front.

Example compact output:

```text
ok cmd=setup bundle=com.example.app config=/repo/.mav/config.yaml launch_recipe=ok multitouch=missing multitouch_next="mav setup --install appium"
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
mav ui tree --prefer-driver appium
mav ui tree --include-system
mav ui tap --id element_id
mav ui tap --x 120 --y 400
mav ui tap --text "Daily Reminder"
mav ui tap --value "Email"
mav ui type "hello"
mav ui type "user@example.com" --prefer-driver appium
mav ui erase --focused --prefer-driver appium
mav ui hideKeyboard
mav ui swipe --direction up
mav ui swipe --start-x 220 --start-y 760 --end-x 220 --end-y 260
mav ui longPress --x 200 --y 450 --duration 800ms
mav ui pinch --x 200 --y 450 --scale 0.5
mav ui pinch --x 200 --y 450 --scale 0.5 --pan-x 80 --pan-y -40
mav ui rotate --x 200 --y 450 --degrees 30
mav ui twoFingerPan --x 200 --y 450 --pan-x 80 --pan-y -40
mav ui actions --file .mav/actions/map-zoom.json
mav ui wait --id element_id --timeout 5s
mav ui wait --text "Privacy Policy" --timeout 5s
mav ui wait --value "Email" --timeout 5s
mav ui scrollUntil --id privacy_policy_button --direction up --max-swipes 4
```

MAV chooses drivers by capability. AXe is the default fast path for
accessibility tree inspection, semantic taps, typing, swipes, waits, and
assertions. idb is used for coordinate taps and device/simulator fallback
operations.

For `mav ui tree` and semantic `mav ui tap`, `--prefer-driver auto` is the
default. In auto mode MAV tries AXe first, then falls back to Appium/WDA when
the AXe tree is empty, has no usable elements, or when an AXe tap by id/text
fails. Use `--prefer-driver axe` to debug AXe-only behavior and
`--prefer-driver appium` for WDA-only inspection. Use `mav ui tree
--include-system` when a system process or cross-app surface is in front, such
as PHPicker, App Tracking Transparency, permission prompts, SpringBoard, or an
iOS 26 service process. MAV asks Appium for the active foreground bundle and
temporarily targets that bundle for the source tree.

If `mav ui tap --text X` fails because AXe sees `X` as a value/placeholder but
not as a label, MAV reports `ui_tap_text_no_label_match` with `matched_value`
and suggests Appium text matching. Prefer stable ids when possible. With
`appium-xcuitest-driver@8`, MAV automatically retries text matching with
`-ios class chain` when the session rejects `predicate string` selectors.
Use `mav ui tap --value X` for text fields whose placeholder is exposed as
`AXValue`; MAV routes that selector through Appium.

When `mav ui tap --id X` detects that `X` is a wrapper containing a descendant
text field or text view, auto mode routes the tap through Appium so the inner
field receives keyboard focus. `mav ui type TEXT` also checks Appium focus
metadata when available: if no text input is focused it fails with
`type_no_focused_field`; when it can compare before/after values it reports
`chars_sent` and `chars_received` in addition to the legacy `chars` field.
Use `mav ui type TEXT --prefer-driver appium` for text that must be entered
through XCUITest, such as emails, URLs, and other strings with shifted keyboard
characters. `mav ui erase` clears a focused or selected text field through
Appium, and `mav ui hideKeyboard` dismisses the keyboard through WDA.

When you expect to need Appium, run `mav open --warm-appium` to create the WDA
session after the launch recipe finishes so the first Appium-backed tree or tap
does not pay the full cold-start cost. Cold starts can take about a minute, so
MAV prints a progress note to stderr before it waits for the Appium/WDA session.

Appium is also used for true multitouch. AXe and idb do not expose a real pinch
or two-finger gesture primitive. MAV sends Appium W3C Actions: multiple touch
sources execute step-by-step in concurrent ticks, which lets a flow move two
fingers at the same time for pinch+pan, rotate, and two-finger drag.

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
  - video.start: {}
  - evidence.step: { name: before-toggle, note: Daily Reminder before tap }
  - tap: { text: Daily Reminder }
  - type: "Search text"
  - type: { text: "user@example.com", prefer-driver: appium }
  - erase: { focused: true, prefer-driver: appium }
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
  - logs: { key: SettingsReached }
  - crashes: {}
  - video.stop: {}
  - report: {}
```

Semantic flow steps inherit the process-level `--prefer-driver auto|axe|appium`
setting from `mav run`. A step can override it with `prefer-driver` when one
interaction needs a specific backend:

```yaml
- tap: { text: "Deporte y ocio", prefer-driver: appium }
- wait: { text: "Continuar", prefer-driver: appium, timeout: 5s }
- scrollUntil: { text: "Estado*", direction: up, prefer-driver: appium }
```

This applies to `tree`, `tap`, `swipe`, `wait`, `assert`, `waitUntil`, and
`scrollUntil`; `type` and `erase` can also force Appium for form fields. In
`auto` mode, MAV also routes taps inside table, collection, sheet, and tab bar
containers through Appium/WDA when AXe can match the text but may not activate
the row or tab. `wait`, `assert`, `waitUntil`, and `whileNotVisible` also retry
their condition with Appium when AXe misses the target.

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
rotate
twoFingerPan
actions
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
evidence.start
evidence.step
evidence.stop
video.start
video.stop
report
```

`hideKeyboard` verifies that the keyboard disappeared. If WDA reports success
but the keyboard is still present, MAV retries with alternate Appium strategies
and then fails with `ui_hide_keyboard_failed reason=keyboard_still_visible`
instead of returning a false positive.

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
prove the behavior itself, not only that the app opened. The supported recording
flow steps are `video.start` and `video.stop`; `evidence.start` and
`evidence.stop` remain supported aliases. Flows do not have a `recordVideo`
option.

`mav evidence report` prints `video=<path>` only when a valid video exists, and
`video=missing` when the report has no recording. A report without `video.mov`
does not prove video evidence was captured.

MAV does not open HTML automatically. Inspect the reported file:

```text
.mav/runs/<run-id>/report.html
```

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
mav setup --install axe idb appium
mav install-skills
mav sim list
mav sim select --device NAME --ios VERSION [--locale LOCALE] [--language LANG]
mav sim select --udid UDID
mav sim boot
mav device list
mav device select --udid UDID
mav device select --name NAME
mav open [--device NAME] [--ios VERSION] [--udid UDID] [--locale LOCALE] [--language LANG] [--clear-state] [--warm-appium] [--no-relaunch]
mav ui tree [--prefer-driver auto|axe|appium] [--include-system]
mav ui tap --id ID [--prefer-driver auto|axe|appium]
mav ui tap --x X --y Y
mav ui tap --text TEXT [--prefer-driver auto|axe|appium]
mav ui tap --value VALUE [--prefer-driver auto|appium]
mav ui type TEXT [--prefer-driver auto|axe|appium]
mav ui erase [--id ID | --text TEXT | --value VALUE | --focused true] [--prefer-driver appium]
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
mav ui tree --prefer-driver appium
```

Then refine the selector based on what shows up. Prefer accessibility ids over
text.

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
