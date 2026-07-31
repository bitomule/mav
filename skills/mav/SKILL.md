---
name: mav
description: Use MAV, the Mobile Agent Verifier CLI, to validate iOS apps through deterministic simulator/device actions, configurable launch recipes, accessibility tree inspection, screenshots, logs, crashes, and evidence reports.
---

# MAV

Use `mav` when validating an iOS app locally. MAV is deterministic: it
does not explore or repair routes by itself. The agent decides the next action.

## Workflow

1. Run `mav doctor`.
   If it reports CoreSimulator or idb sandbox/permission failures, rerun MAV
   outside the sandbox.
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
   For a physical iOS device, use `mav device list`, then `mav device select
   --udid ...` or `mav device select --name ...`. Physical devices require idb
   for install, launch, logs, screenshots, and crashes. Multitouch, system UI,
   and `hideKeyboard` are simulator-only and return structured errors on
   device. Simulator crash checks use local DiagnosticReports directly.
4. Start the app with `mav open`. Use `mav open --clear-state` for a fresh
   install. Use `mav open --no-relaunch` when the app was launched manually with
   custom `SIMCTL_CHILD_*` environment and MAV should only attach to the app
   already in front. This creates `.mav/runs/<run-id>/` and starts `logs.txt`.
   MAV captures a filtered unified log stream for MAV probes and app-process
   logs when `process_name` is configured. On physical devices, generated
   simulator install/launch recipes are mapped to idb when possible. MAV writes
   `/tmp/mav/sim-locks/<udid>.json` for simulator runs; if another worktree owns
   a fresh lock, pick a different simulator unless you are sure you own that run
   and pass `--force`.
5. Prefer `mav ui tree` to understand the current screen. It prints compact
   screen metadata followed by bounded `node ...` lines with ids, labels, roles,
   values, enabled state, subroles, titles, pids, focus state, and frames when
   available.
   Treat this as the primary structured UI source for agents; do not ask for
   `--json`. If the simulator accessibility service returns an empty
   `AXApplication` tree, MAV attempts recovery internally; do not work around it
   with screenshots unless `mav ui tree` fails after recovery. Use
   `mav ui tree --include-system` when inspecting system UI, PHPicker,
   permission prompts, SpringBoard, or cross-app service processes; this asks
   baguette for the SpringBoard/system tree on simulator. The
   `--include-system` flag is sim-only — on a physical device it returns
   `tree_system_unsupported_on_device`.
6. Use `mav capture --name <descriptive-name>` only when the tree is
   insufficient or visual evidence is needed. Captures are unique by default
   under `.mav/runs/<run-id>/captures/`, and `--name` gives the client and report
   a stable, readable proof point such as `largest-videos-after-pinch`.
7. Use `mav ui tap/type/erase/hideKeyboard/swipe/longPress/wait/scrollUntil`
   for manual exploration. Prefer accessibility identifiers first (`--id`).
   `mav ui erase --focused` clears a focused field via baguette on simulator;
   `mav ui hideKeyboard` dismisses the keyboard via baguette on simulator. Both
   return structured errors on a physical device (`erase_unsupported_on_device`,
   `hide_keyboard_unsupported_on_device`). Use `scrollUntil` before tapping
   targets that are present in the tree but may be off-screen. Use coordinates
   only when the tree is insufficient and the screenshot makes the target
   unambiguous. Use text as the last option because labels change with
   localization and copy edits. Use `mav ui wait --id`, `--text`, or `--value`
   for readiness checks.
8. `mav ui tree` may report a natural screen id when the AX root already has a
   `View`-suffix identifier, such as `SettingsView` → `settings-view`. This is
   a labelling/observability signal only. Selectors for tapping still work
   regardless.
9. Sessions started with `mav open` have a renewable 15-minute inactivity
   lease. Each MAV command keeps the lease alive, including heartbeats during
   long commands. Expiration automatically stops run-owned streams, resets
   non-preserved time control, and releases the simulator lock. Use `mav stop`
   only for immediate cleanup. `mav run` stops run-owned streams
   deterministically.
10. `mav run flow.yaml` always creates its own run and never adopts or kills
    whatever `.mav/current-run` names -- safe to run concurrently against the
    same repo from separate agents. It still publishes `.mav/current-run` for
    manual follow-up (`mav logs` / `mav stop` / `mav evidence report` without
    `--run`), but never by stealing the pointer from a different run that's
    still alive. Pass `--run RUN_ID` to continue an existing run (e.g. a
    second flow appending evidence to a run already opened); an id that
    doesn't name a real run fails with `run_not_found`.

For evidence flows that need mitmproxy on macOS 26+, remember that mitmproxy's
local Network Extension may take several seconds to become ready after launch.
If traffic is not appearing, check `mav network start` output and the run log
before assuming the app made no requests.

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
4. `mav evidence report` writes a verified evidence manifest at
   `<run-dir>/report.json`. MAV owns the facts: video duration/frames,
   screenshot decodability, network HAR status/counts, issue severity,
   crashes, commands, and log tail.
   Current project runs normally live under `.mav/runs/<run-id>/`; legacy or
   ad-hoc runs may live under `/tmp/mav/<run-id>/`. Use the paths printed by
   MAV instead of guessing.
5. **Author the HTML report. This is mandatory, not optional.** `mav evidence
   report` only writes the JSON manifest; the skill owns the HTML. After
   `mav evidence report` succeeds, read `report.json` and author a
   self-contained `<run-dir>/report.html` for that specific run. Use
   `./templates/evidence-report.html` as a reference, not as a fixed renderer:
   rewrite the copy, metrics, media, and sections so the report explains the
   actual evidence.

   Do not treat any of the following as a valid evidence deliverable:
   - opening the run folder in Finder
   - opening `video.mov` in QuickTime
   - linking only to `report.json`
   - saying "see the artifacts in `<run-dir>`"

   The HTML is the deliverable. Share it and the manifest as clickable Markdown
   links using absolute paths, for example
   `[MAV evidence report](/path/to/repo/.mav/runs/<run-id>/report.html)` and
   `[evidence data](/path/to/repo/.mav/runs/<run-id>/report.json)`. Embed the
   MP4 video when `video_status=accepted` and `video_mp4` is present; otherwise
   fall back to `video` only when the browser can play it. The HTML must include
   a visible video download button whenever `video_status=accepted`; if
   `video_mp4` exists, download that browser-friendly file, otherwise download
   `video`. If the manifest reports `video_status=missing` or `invalid`, say
   that video evidence was not accepted. If the manifest includes `network.har`, link it directly and
   include the manifest's request, response, error-status, domain, active, and
   issue fields in the HTML. Embed key captures when they are relevant, for example
   `[video](/path/to/repo/.mav/runs/<run-id>/video.mov)` and
   `[after-toggle](/path/to/repo/.mav/runs/<run-id>/steps/02_after-toggle.png)`.

## Evidence Report Standard

MAV reports must be built like visual explainers: dense, visual, and explicit
about what each artifact proves. Do not treat a media file as proof just because
it exists.

- Before authoring HTML, read:
  - `./references/evidence-html.md` for the MAV evidence report structure.
  - `./references/style-rules.md` for visual-explainer-grade design rules.
  - `./references/quality-checks.md` before delivery.
  - `./templates/evidence-report.html` as a starting shape, not as a fixed
    renderer.
- Think briefly before writing. Decide the audience, claim under test, evidence
  shape, and aesthetic. Evidence reports are usually timeline + dashboard +
  media review: CSS timeline for named captures, dashboard metrics for manifest
  health, and full-width video/image sections for the primary proof.
- Use the CLI manifest as the source of truth. Do not hand-wave around manifest
  blockers: invalid video, zero/too-short duration, low frame count, missing
  screenshots, undecodable images, missing assertion notes, active network
  captures, empty HAR files, or HAR parse failures must be surfaced in the
  report and in the final answer.
- The HTML should start with evidence, not prose: a large accepted video or
  strongest valid screenshot must dominate the first viewport. Put verdict,
  video status, valid/invalid step counts, network request count when present,
  crash count, and command count next to that evidence.
- Every evidence step needs a human explanation. The note should state the
  observed claim, not merely repeat the file name. Weak notes such as "after"
  or "screen" should be treated as low-quality evidence and clarified in the
  narrative.
- Screenshots must be verified by the manifest as decodable images with real
  dimensions. If the image is invalid, too small, missing, or unrelated to the
  claim, mark it rejected and rerun the flow when possible.
- Videos of zero seconds, missing duration, too-short duration, or too few
  frames are not accepted as video evidence. Rerun the flow with a recording
  window that covers the behavior itself, not unrelated setup or idle padding.
- The report must be self-contained in styling and narrative. Use local
  evidence files only; do not use remote CSS, JS, fonts, or image assets.
  Do not use a generic renderer that blindly maps JSON to cards. The agent must
  compose the page like visual-explainer does: choose an aesthetic, establish a
  visual hierarchy, write the explanatory text, and place video/images where
  they carry the claim.
- Reports are evidence workspaces. Include direct `Open`, `Download`, and
  `Copy path` affordances for the primary video and named captures. The primary
  video download button is mandatory whenever the manifest has accepted video.
  Include direct `Open`, `Download`, and `Copy path` affordances for
  `network.har` when present. Include `Copy logs` and `Copy commands` controls
  for audit sections.
- Prefer a before/action/after sequence. If a behavior is visual, pair the
  screenshots with a wait or `changedFrom` assertion so the report explains why
  the captured state is meaningful.
- If the HTML has 4 or more major sections, include a compact sticky table of
  contents on desktop and a horizontal section nav on mobile. Evidence readers
  should be able to jump from verdict to video, timeline, integrity, logs, and
  command trail without hunting.
- After writing the HTML, inspect it when possible. Check that local videos and
  images load, that rejected artifacts are visibly rejected, that no text
  overflows at mobile width, and that the first viewport immediately shows the
  verdict plus primary visual proof.

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
visible, or until `timeout` expires. Mark dismiss taps as `optional: true` when
only some prompts appear:

```yaml
- whileNotVisible:
    text: "You"
    timeout: 30s
    do:
      - tap: { id: onboarding_dismiss, optional: true }
      - delay: 500ms
```

`mav run --prefer-driver axe flow.yaml` forces AXe for semantic UI steps. Use a
per-step `prefer-driver` override when a single interaction needs to pin the
driver:

```yaml
- tap: { text: "Deporte y ocio", prefer-driver: axe }
- wait: { text: "Continuar", prefer-driver: axe, timeout: 5s }
- hideKeyboard: {}
```

`open: { clearState: true }` and `open: { clear-state: true }` are both valid
flow spellings. `mav ui hideKeyboard` dispatches through baguette on simulator
and returns `hide_keyboard_unsupported_on_device` on a physical device.

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

Multitouch gestures (pinch, rotate, two-finger pan) dispatch through baguette on
simulator. They return `gesture_unsupported_on_device` on a physical device:

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

Physical device launch recipes should use idb:

```yaml
launch:
  mode: custom
  commands:
    build: ./scripts/mav-build-device.sh
    app_path: ./scripts/mav-app-path-device.sh
    install: idb install --udid "$MAV_UDID" "$MAV_APP_PATH"
    launch: idb launch --udid "$MAV_UDID" -f "$MAV_BUNDLE_ID"
```

Each command runs from `MAV_ROOT` with `MAV_RUN_DIR`, `MAV_TARGET_KIND`,
`MAV_IS_DEVICE`, `MAV_UDID`, `MAV_BUNDLE_ID`, `MAV_APP_PATH`,
`MAV_DEVICE_NAME`, `MAV_RUNTIME`, and `MAV_PLATFORM`. `app_path` must print
exactly one `.app` path. If the app is already installed, configure only
`launch`.

`video.start` / `evidence.start` video recording is simulator-only in this
release. On physical devices, use `capture` / `evidence.step` screenshots,
crash checks, logs, and reports for evidence.

## Command Output

Output is intentionally compact and agent-friendly by default:

```text
ok cmd=open run=7fd logs=/path/to/repo/.mav/runs/7fd/logs.txt
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
Use `mav ui tree --include-system` (simulator-only) for system-process UI.

If `mav ui tap --text X` returns `ui_tap_text_no_label_match`, AXe found `X` as
a value/placeholder but not as a label. Prefer a stable id or use coordinates
only after capture inspection.

If simulator commands fail with a hint like `requires simulator/idb access;
rerun outside sandbox`, rerun MAV outside the sandbox instead of retrying the
same command inside the sandbox.

`mav evidence stop` rejects zero-duration or invalid videos. If it returns
`video_invalid`, rerun the evidence flow with enough interaction time so the
recording covers the behavior being verified, but avoid padding the video with
unrelated waiting or setup.

Use `--raw` only when the underlying tool output is needed, and `--verbose`
only for debugging MAV itself.
# MAV v0.6 additions

- Prefer an action fast path when the next observation is known:
  `mav ui tap --id save --wait-id detailView --wait-timeout 5s --observe delta`.
- Use typed `where` selectors and combine predicates to make actions unique.
- Pass flow inputs with `mav run flow.yaml --param name=value`.
- Run a flow on exact targets with repeated `--target` and limit concurrency
  with `--jobs`.
- Enable optional simulator wall-clock control with
  `mav setup --install simtime`, then `mav open --time-control`; use
  `mav time ...` only after injection.
- Run `mav setup --install lldb-dap` to verify the selected Xcode debugger.
  Use `mav debug ...` only for simulator debug builds with dSYM; unresolved
  source breakpoints fail with `debug_symbols_missing`.
