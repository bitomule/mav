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
   generated config. Setup is idempotent: it detects app identity, simulator
   defaults, UI tools, and an editable `launch.commands` recipe while
   preserving existing explicit choices. MAV uses that recipe to run the app;
   Bazel/Xcode/Tuist detection is setup-time scaffolding only.
3. If the validation needs a specific simulator, runtime, or locale, use
   `mav sim list`, then `mav sim select --device ... --ios ... --locale ... --language ...`.
   You can also pass the same target flags to `mav open`.
4. Start the app with `mav open`. This creates `/tmp/mav/<run-id>/` and starts
   `logs.txt`. MAV captures a filtered unified log stream for MAV probes.
5. Prefer `mav ui tree` to understand the current screen. It prints compact
   screen metadata followed by bounded `node ...` lines with ids, labels, roles,
   values, and frames when available. Treat this as the primary structured UI
   source for agents; do not ask for `--json`. If the simulator accessibility
   service returns an empty `AXApplication` tree, MAV attempts recovery
   internally; do not work around it with screenshots unless `mav ui tree` fails
   after recovery.
6. Use `mav capture --name <descriptive-name>` only when the tree is
   insufficient or visual evidence is needed. Captures are unique by default
   under `/tmp/mav/<run-id>/captures/`, and `--name` gives the client and report
   a stable, readable proof point such as `largest-videos-after-pinch`.
7. Use `mav ui tap/type/swipe/wait/scrollUntil` for manual exploration. Prefer
   accessibility identifiers first (`--id`). Use coordinates only when the tree
   is insufficient and the screenshot makes the target unambiguous. Use text as
   the last option because labels change with localization and copy edits.
8. Before navigating to a new screen, verify with `mav ui tree` that the target
   screen has stable accessibility ids or a visible title that MAV can observe.
   The mapping loop is `mav open`, `mav ui tree`, `mav ui tap --id ...`,
   `mav ui tree`, then inspect `.mav/map/**`. If the next tree reports
   `screen=unknown map_pending=true`, the tap was recorded but no reliable route
   was learned; add/expose accessibility ids or capture/inspect before mapping.
9. Use `mav go <screen-id>` only after `.mav/map/index.json` and
   `.mav/map/screens/*.json` contain that screen and a route from app launch.
   `mav go` opens the app, records evidence from the start screen to the target,
   validates screen change/assertions, writes a report, and stops run-owned
   streams. If MAV returns `screen_not_found` or `route_not_found`, explore
   manually with `mav ui tree/tap`; the map updates when the next screen is
   observed with `mav ui tree`.
10. If `.mav/map/**` changes, review the git diff before continuing.
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
2. The flow should navigate to the relevant state first when that setup is not
   the behavior under test. Start recording as late as possible while still
   covering the verified action, use `wait` for a single `id`, `text`, or
   `value`, use `waitUntil` with `any` or `changedFrom` for alternate/visual
   outcomes, and use `delay` only for fixed launch/animation waits when
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

The supported flow recording steps are `video.start` and `video.stop`;
`evidence.start` and `evidence.stop` remain supported aliases. Do not use or
invent `recordVideo: true`.

Use `mav go <screen-id>` for ad-hoc navigation evidence to a mapped screen; this
is appropriate when the route itself is the evidence. Use `mav run` for feature
verification evidence where the tested behavior needs extra taps, waits, logs,
crash checks, or assertions after navigation, and keep the recording window
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
ok cmd=ui.tree driver=axe nodes=42 screen=settings screen_source=recognized
node index=1 id=settings_button label=Settings role=button frame="{{20, 120}, {180, 44}}"
ok cmd=capture file=/tmp/mav/7fd/captures/largest-videos-after-pinch.png run=7fd
fail code=screen_not_found screen=settings
```

If `mav ui tree` reports `screen=unknown` with `screen_source=unmatched`, trust
the live node lines over stale map state and continue exploring with tree/tap or
capture named evidence before updating routes.

If it also reports `map_pending=true`, the previous tap has not produced a
mapped screen yet. Do not use `mav go` for that destination until `.mav/map/**`
contains the screen and route.

If simulator commands fail with a hint like `requires simulator/idb access;
rerun outside sandbox`, rerun MAV outside the sandbox instead of retrying the
same command inside the sandbox.

`mav evidence stop` rejects zero-duration or invalid videos. If it returns
`video_invalid`, rerun the evidence flow with enough interaction time so the
recording covers the behavior being verified, but avoid padding the video with
unrelated waiting or setup.

Use `--raw` only when the underlying tool output is needed, and `--verbose`
only for debugging MAV itself.
