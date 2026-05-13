---
name: mav
description: Use MAV, the Mobile Agent Verifier CLI, to validate iOS apps through deterministic simulator/device actions, configurable launch recipes, accessibility tree inspection, screenshots, logs, crashes, and evidence reports.
---

# MAV

Use `mav` when validating an iOS app locally. MAV is deterministic: it
does not explore or repair routes by itself. The agent decides the next action.

## Workflow

1. Run `mav doctor`.
   If it reports CoreSimulator, idb, or Appium sandbox/permission failures,
   rerun MAV outside the sandbox. For Appium home permission failures, MAV
   retries with a temporary writable `APPIUM_HOME`; if it still reports
   `appium_home_not_writable`, rerun outside the sandbox or set `APPIUM_HOME`
   to a writable directory.
2. If the project lacks `.mav/config.yaml`, run `mav setup` and review the
   prompts. Setup is idempotent and interactive by default: it detects app
   identity, simulator defaults, UI tools, and an editable `launch.commands`
   recipe, then lets the user accept or replace each value. Use
   `mav setup --non-interactive` for CI/scripts. Detection is conservative:
   explicit MAV Make/Just targets, explicit `scripts/mav-build` +
   `scripts/mav-app-path`, and standard Bazel/Xcode/Tuist shapes.
3. If the validation needs a specific simulator, runtime, or locale, use
   `mav sim list`, then `mav sim select --device ... --ios ... --locale ... --language ...`.
   You can also pass the same target flags to `mav open`.
4. Start the app with `mav open`. Use `mav open --clear-state` for a fresh
   install, and `mav open --warm-appium` when the session is likely to need
   Appium-backed tree or tap fallback. Appium/WDA warm-up can take about a
   minute on a cold start; tell the user it may take a while, then run it
   directly without asking for confirmation. This creates `/tmp/mav/<run-id>/`
   and starts `logs.txt`. MAV captures a filtered unified log stream for MAV
   probes.
5. Prefer `mav ui tree` to understand the current screen. It prints compact
   screen metadata followed by bounded `node ...` lines with ids, labels, roles,
   values, enabled state, subroles, titles, pids, focus state, and frames when
   available.
   Treat this as the primary structured UI source for agents; do not ask for
   `--json`. If the simulator accessibility service returns an empty
   `AXApplication` tree, MAV attempts recovery internally; do not work around it
   with screenshots unless `mav ui tree` fails after recovery. In the default
   `--prefer-driver auto` mode, MAV may fall back to Appium/WDA when AXe returns
   an empty tree or has no usable elements. Use `mav ui tree --include-system` when
   inspecting system UI, PHPicker, permission prompts, SpringBoard, or cross-app
   service processes; MAV asks Appium for the active foreground bundle and
   temporarily targets that bundle for the source tree. If the active app still
   reports the host bundle, MAV also probes known system UI bundles including
   SpringBoard, ATT (`com.apple.tccd`), PHPicker, SafariViewService, Mail
   composition, and remote alerts.
6. Use `mav capture --name <descriptive-name>` only when the tree is
   insufficient or visual evidence is needed. Captures are unique by default
   under `/tmp/mav/<run-id>/captures/`, and `--name` gives the client and report
   a stable, readable proof point such as `largest-videos-after-pinch`.
7. Use `mav ui tap/type/erase/hideKeyboard/swipe/wait/scrollUntil` for manual exploration. Prefer
   accessibility identifiers first (`--id`). Auto mode routes taps on wrappers
   that contain text fields/text views through Appium so the inner field gets
   keyboard focus. Use `mav ui tap --value VALUE` for placeholders exposed as
   AXValue, and `mav ui tap --prefer-driver appium` when `--text` must match a
   field value/placeholder. Use `mav ui type TEXT --prefer-driver appium` for
   emails, URLs, and text with shifted keyboard characters such as `@`. If
   `mav ui type` reports `type_no_focused_field`, tap the field with Appium and
   retry. Use `mav ui erase --focused --prefer-driver appium` to clear a focused
   field and `mav ui hideKeyboard` before tapping controls hidden by the
   keyboard. Use coordinates
   only when the tree is
   insufficient and the screenshot makes the target unambiguous. Use text as
   the last option because labels change with localization and copy edits. Use
   `mav ui wait --id`, `--text`, or `--value` for readiness checks.
8. `mav ui tree` may report a natural screen id when the AX root already has a
   `View`-suffix identifier, such as `SettingsView` → `settings-view`. This is
   a labelling/observability signal only. Selectors for tapping still work
   regardless.
9. For ad-hoc sessions started with `mav open`, run `mav stop` when validation
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

Use `out` when a trusted helper should feed later steps. MAV binds trimmed
stdout; binding names must use letters, numbers, `_`, or `-`, and cannot start
with a number or `-`. JSON stdout exposes fields through `${exec.NAME.field}`,
while plain text stdout is available as `${exec.NAME}`:

```yaml
- exec:
    cmd: "node utils/get_test_user.js sellersXp"
    out: credentials
    timeout: 10s
- type: "${exec.credentials.email}"
```

## Evidence

Use evidence when the user needs proof of verification:

1. Prefer writing a temporary MAV YAML flow and running it with `mav run`.
2. The flow should navigate to the relevant state first when that setup is not
   the behavior under test. Start recording as late as possible while still
   covering the verified action, use `wait` for a single `id`, `text`, or
   `value`, use `waitUntil` with `any` or `changedFrom` for alternate/visual
   outcomes, use `when` for optional UI that may already be dismissed or in a
   different state, and use `delay` only for fixed launch/animation waits when
   tree-based waits are not possible. Capture named proof points, perform the
   tested action, capture the result, stop recording immediately after the
   result is visible, check crashes, and generate a report.
3. Names should describe the assertion, for example `settings-before-toggle`
   and `settings-after-toggle`.
4. Share the generated report and evidence as clickable Markdown links using
   absolute paths, for example `[MAV evidence report](/tmp/mav/<run-id>/report.html)`.
   Include the video only when `mav evidence report` reports `video=<path>`;
   if it reports `video=missing`, the report has screenshots/logs but no video
   evidence. Include key captures when they are relevant, for example
   `[video](/tmp/mav/<run-id>/video.mov)` and
   `[after-toggle](/tmp/mav/<run-id>/captures/after-toggle.png)`.

The video should be limited to the relevant verification moment. Do not record
long setup, idle time, or repeated navigation unless the navigation itself is
being validated. The screenshots must prove the behavior itself, not just that
the app opened. For a notification toggle, navigate to Settings first if
Settings is not under test, start recording, capture before toggling, toggle it,
capture after toggling, then stop.

YAML flow steps `type`, `delay`, and `sleep` accept concise scalar forms as
aliases for their object forms:

```yaml
- type: "Search text"
- type: { text: "Search text" }
- delay: 500ms
- delay: { duration: 500ms }
- sleep: 500ms
- sleep: { duration: 500ms }
```

Use `when` to guard optional UI. It checks once and skips the `do` block without
failing when the condition is not visible. Keep `open` and `exec` as top-level
steps; they are not valid inside `do` blocks.

```yaml
- when: { visible: { text: Continue } }
  do:
    - tap: { text: Continue }
```

Use `whileNotVisible` for chained onboarding or permission prompts. MAV repeats
the `do` block until the target `id`, `text`, `value`, or `any` condition is
visible, or until `timeout` expires. In `--prefer-driver auto`, conditions retry
with Appium when AXe misses the target, which helps for tab bars and system-like
wrappers. Mark dismiss taps as `optional: true` when only some prompts appear:

```yaml
- whileNotVisible:
    text: "You"
    timeout: 30s
    do:
      - tap: { id: onboarding_dismiss, optional: true }
      - delay: 500ms
```

`mav run --prefer-driver appium flow.yaml` sets the default semantic UI driver
for flow steps. Use a per-step `prefer-driver` override when a flow mixes fast
AXe interactions with Appium-only rows, sheets, dropdowns, or system UI:

```yaml
- tap: { text: "Deporte y ocio", prefer-driver: appium }
- tap: { value: "Dirección de email", prefer-driver: appium }
- type: { text: "user@example.com", prefer-driver: appium }
- erase: { focused: true, prefer-driver: appium }
- hideKeyboard: {}
- swipe: { direction: up, prefer-driver: appium }
- wait: { text: "Continuar", prefer-driver: appium, timeout: 5s }
- scrollUntil: { text: "Estado*", direction: up, prefer-driver: appium }
```

`open: { clearState: true }` and `open: { clear-state: true }` are both valid
flow spellings. `mav ui hideKeyboard` verifies that the keyboard disappeared;
if WDA reports success but the keyboard remains visible, MAV retries alternate
Appium strategies and then fails with a clear `keyboard_still_visible` reason.

Use `include` to compose reusable flow fragments. Resolve paths relative to the
including YAML file and pass values through `env`; included steps can reference
them with `${env.NAME}`. The `file` field can reference values from the same
`env` block:

```yaml
- include:
    file: "components/auth/${env.USER}.mav.yaml"
    env:
      USER: sellersXp
      FRESH_INSTALL: true
```

The supported flow recording steps are `video.start` and `video.stop`;
`evidence.start` and `evidence.stop` remain supported aliases. Do not use or
invent `recordVideo: true`.

Use `mav run` for feature verification evidence where the tested behavior
needs taps, waits, logs, crash checks, or assertions. Keep the recording window
around the behavior rather than the whole setup path.

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
screenshot. Coordinate taps can be useful for manual visual fallback, but they
are not the preferred basis for reliable routes. Use `text` only when neither
id nor coordinates are appropriate.

## Gestures

Use Appium-backed gestures when the app needs multi-touch behavior:

```bash
mav ui pinch --x 200 --y 450 --scale 0.5 --duration 800ms
mav ui pinch --x 200 --y 450 --scale 0.5 --pan-x 80 --pan-y -40 --duration 800ms --hold 2s
mav ui rotate --x 200 --y 450 --degrees 30 --hold 1s
mav ui twoFingerPan --x 200 --y 450 --pan-x 80 --pan-y -40 --hold 1s
```

`--hold DURATION` keeps both fingers down at the final positions before
releasing. This also applies to simultaneous pinch+pan via `mav ui pinch
--pan-x/--pan-y`. MAV waits for `duration + hold` before returning or advancing
to the next flow step, so a following `mav capture --name ...` or evidence step
does not race ahead of the gesture.

In YAML flows, gesture steps accept the same `hold` key:

```yaml
- pinch: { x: 200, y: 450, scale: 0.5, panX: 80, panY: -40, duration: 800ms, hold: 2s }
- capture: { name: zoom-held }
```

## Launch Recipes

MAV does not own the project build system. `.mav/config.yaml` should define
the commands needed to run the app:

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

Each command runs from `MAV_ROOT` with `MAV_RUN_DIR`, `MAV_UDID`,
`MAV_BUNDLE_ID`, `MAV_APP_PATH`, `MAV_DEVICE_NAME`, `MAV_RUNTIME`, and
`MAV_PLATFORM`. `app_path` must print exactly one `.app` path. If the app is
already installed, configure only `launch`.

## Command Output

Output is intentionally compact and agent-friendly by default:

```text
ok cmd=open run=7fd logs=/tmp/mav/7fd/logs.txt
ok cmd=ui.tree driver=axe nodes=42 screen=settings screen_source=identity
node index=1 id=settings_button label=Settings role=button enabled=true frame="{{20, 120}, {180, 44}}"
ok cmd=capture file=/tmp/mav/7fd/captures/largest-videos-after-pinch.png run=7fd
fail code=ui_tap_failed stderr="…"
```

If `mav ui tree` reports `screen=unknown` with
`screen_source=identity_missing`, mav could not infer a natural screen name
from the AX tree. UI commands still work; the natural id is a labelling signal,
not a gate.

AXe is the fast semantic driver, but it can miss system-process UI, PHPicker,
permission alerts, non-accessibility wrapper views, and text-field placeholders.
Use `mav ui tree --include-system` for system-process UI and
`--prefer-driver appium` to force WDA/XCUITest for a specific tree or tap, or
leave the default `auto` mode to let MAV fall back when AXe cannot provide a
usable tree or tap target.

If `mav ui tap --text X` returns `ui_tap_text_no_label_match`, AXe found `X` as
a value/placeholder but not as a label. Prefer a stable id, retry with
`--prefer-driver appium`, or use coordinates only after capture inspection.
With `appium-xcuitest-driver@8`, MAV automatically retries text matching with
`-ios class chain` when the session rejects `predicate string` selectors.

When `mav doctor` reports an Appium-required tool gap, keep Appium installed and prefer
`mav open --warm-appium` for related manual exploration.

If simulator commands fail with a hint like `requires simulator/idb access;
rerun outside sandbox`, rerun MAV outside the sandbox instead of retrying the
same command inside the sandbox.

`mav evidence stop` rejects zero-duration or invalid videos. If it returns
`video_invalid`, rerun the evidence flow with enough interaction time so the
recording covers the behavior being verified, but avoid padding the video with
unrelated waiting or setup.

Use `--raw` only when the underlying tool output is needed, and `--verbose`
only for debugging MAV itself.
