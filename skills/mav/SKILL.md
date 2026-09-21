---
name: mav
description: Use MAV, the Mobile Agent Verifier CLI, to validate iOS and macOS apps through deterministic simulator/device/mac actions, configurable launch recipes, accessibility tree inspection, screenshots, logs, crashes, and evidence reports.
---

# MAV

Use `mav` when validating an iOS app locally. MAV is deterministic: it
does not explore or repair routes by itself. The agent decides the next action.

`mav --version` reports the binary's version, and `mav doctor` carries it as
`mav_version`. Include it when you report a problem: behaviour differs between releases
and a report without it starts with a guess.

## Platforms

`mav` drives iOS simulators, physical iOS devices, and **macOS apps**. The platform comes
from `target_kind` in `.mav/config.yaml`: `simulator`, `device` or `macos`.

A repo whose app ships on more than one platform (same codebase, different build target)
uses **profiles** instead of two configs:

```yaml
app_target: "//App:MyAppiOS"
launch:
  commands:
    build: "bazelisk build '//App:MyAppiOS'"
profiles:
  mac:
    target_kind: macos
    app_target: "//App:MyAppMac"
    launch:
      commands:
        build: "bazelisk build '//App:MyAppMac'"
        install: ""        # explicitly none: macOS has no simctl install
```

Select one with `mav open --profile mac`, or set `default_profile`. An empty string in a
profile *annuls* the inherited value; an absent key inherits it. A profile that does not
exist fails with `profile_not_found` rather than silently using the base.

On macOS these work: `ui tree`, `ui tap`, `ui doubleTap` (by `--id`/`--text` selector or
`--x`/`--y`; inline-rename UIs that open on double click need this), `ui type`, `ui erase`, `ui swipe`, `ui wait`,
`capture`, `open`, `app list`, `openURL`, `clipboard`, `logs`, `crashes`, `evidence`
(including video), `run`, `network` and `time travel|reset`. `ui hideKeyboard` succeeds without doing
anything: there is no on-screen keyboard to hide, and failing would force a shared flow
to branch by platform.

What does **not** exist there, with a structured error saying why: multitouch gestures
(`pinch`, `rotate`, `twoFingerPan`), hardware buttons, the simulator/device commands
(including `sim appearance` and `sim statusbar`),
`time freeze|scale` (a system clock runs, it cannot be stopped or accelerated) and
`location` (macOS has no supported way to feed CoreLocation a fake fix; Xcode's "Simulate
Location" is an iOS-device feature and does nothing against a Mac app).

`ui swipe` becomes a scroll with the direction inverted, so one flow means the same thing
on both platforms. `time travel --to` moves the **machine's** clock, so it is refused
outside a VM unless you pass `--system-clock`. `network start` also points the system at
the proxy and restores it on stop.

Selectors behave differently from iOS: the driver does not expose AXIdentifier, so
`--id` takes an `element_token` that is only valid inside the snapshot that produced it.
Re-read the tree before acting on it; a stale token is refused rather than applied to the
wrong element. `--text` is the selector that survives across snapshots.

### macOS in a disposable VM

`vm: true` next to `target_kind: macos` runs the app in a throwaway machine instead of
on the user's. That is the entire config surface; there is no host, key or tool to
name.

```yaml
target_kind: macos
vm: true
```

**Nothing about how you drive mav changes.** Same commands, same arguments, same
output, and evidence still lands in the local `.mav/runs/<id>/`. The one visible
difference is `vm=true` in the response fields, which is how you tell whether what you
just drove was the VM's app or the user's own machine.

What you do need to know:

- `mav doctor` reports `vm_tooling`, `vm_image` and `vm_lease`. Run it first when a VM
  project misbehaves.
- Every VM failure carries the command that fixes it in `next`. Tell the user to run
  that; do not go looking for the underlying hypervisor.
  - `vm_tooling_missing`, `vm_tooling_outdated` → `mav setup --install vm`
  - `vm_image_missing`, `vm_image_incomplete`, `vm_image_ungranted` →
    `scripts/build-mav-vm-image.sh`. The last one names which permission switch is off,
    and flipping it needs a human at the VM's screen: macOS has no scriptable way to
    grant those.
- **Call `mav stop` when you are done.** Only two macOS VMs can exist at once, so a
  machine you leave leased blocks the next run. An idle timeout catches the case where
  you crash, but it costs the user twenty minutes of a slot they could be using.
- The first `mav open` is slow: it boots a machine and ships the built bundle across.
  Later commands reuse it.
- `build` still runs on the user's machine; only the app runs in the VM. A build
  failure is a build failure, not a VM problem.
- `open` may answer `resigned=adhoc`. That means the bundle would not have launched in
  the VM and mav re-signed the guest's copy: iCloud, push and anything else tied to the
  provisioning profile are gone from what you are driving. Say so if you report on
  behaviour that could depend on them.
- `mav evidence start` records video here too. It goes through the driver daemon rather
  than `screencapture`, which over SSH sees no display, so it works the same in a VM as
  on the user's own Mac. Call `mav evidence stop` to finalize it: the mp4's index is
  written on stop, and a run killed without it leaves a file no player opens.

## Fixtures

`fixtures` are named states — lists of commands that leave the app in a known situation:

```yaml
fixtures:
  seeded:
    - "./scripts/seed-db.sh"
  empty:
    - "./scripts/wipe.sh"
```

Apply one with `mav open --fixture seeded`, or `fixture: seeded` in a flow's `open` step.
They run **after install and before launch** — the only window where the container exists
and nothing holds the app's database open — and the app is closed first for the same
reason. They compose with `--clear-state`: the container is wiped, then the fixture seeds
on top. The applied fixture is recorded in `report.json`.

`--fixture` cannot be combined with `--no-relaunch`, which skips the launch recipe
entirely.

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
   For App Store screenshots, `mav sim appearance light|dark` and
   `mav sim statusbar set --preset appstore` control the simulator's appearance
   and status bar; see **App Store Screenshots** below.
   You can also pass the same target flags to `mav open`.
   For a physical iOS device, use `mav device list`, then `mav device select
   --udid ...` or `mav device select --name ...`. Physical devices require idb
   for install, launch, logs, screenshots, and crashes. Multitouch, system UI,
   and `hideKeyboard` are simulator-only and return structured errors on
   device. Simulator crash checks use local DiagnosticReports directly.
4. Start the app with `mav open`. Use `mav open --clear-state` for a fresh
   install. Use `mav open --skip-build` when the app is already built and the
   launch recipe's `build` step would only rebuild the same artifact --
   `app_path`, `install` and `launch` still run. Use `mav open --no-relaunch`
   when the app was launched manually with custom `SIMCTL_CHILD_*` environment
   and MAV should only attach to the app already in front. This creates
   `.mav/runs/<run-id>/` and starts `logs.txt`.
   MAV captures a filtered unified log stream for MAV probes and app-process
   logs when `process_name` is configured. On physical devices, generated
   simulator install/launch recipes are mapped to idb when possible. MAV writes
   `/tmp/mav/sim-locks/<udid>.json` for simulator runs; if another worktree owns
   a fresh lock, pick a different simulator unless you are sure you own that run
   and pass `--force`.
5. **When you know what you want to tap, ask for it: `mav ui find "<your own
   words>"`.** It answers with one element or with nothing, and nothing is a
   real answer — it means read the tree yourself. Two reasons to reach for it
   before the tree:
   - `mav ui tree` prints **at most 80 nodes**. Real screens run past that (an
     iOS Settings screen measured at 213), and everything past 80 is announced
     by a single `node_more` line and otherwise not shown. `find` reads the
     whole extraction, so it can resolve elements the tree does not print.
   - It replaces `mav ui tree | grep`, **not your judgement**. It never returns
     an element it is unsure of, and never returns one that destroys something
     unless your own words asked for that. Read `resolved_by` first: `literal`
     and `model` carry an element; `none` means fall back to the tree, and
     `reason` says whether the screen was judged (`abstained`) or could not be
     (`no_key`, `no_network`, `ci_refused`).

   Every run also prints what it cost, in a `cost` block: `total_ms`,
   `tree_ms` (reading the screen, usually most of it), `model_ms` (the
   provider round trip, as jev measured it — `0` when no model was asked) and
   `local_ms` (what is left: candidates, rendering, the vetoes). No token
   counts: `find` asks jev, not a large model, so what it spends is not where
   the saving is — the saving is the tree you no longer paste into your own
   context.

   `find` needs a jev API key, and says on every run which of three places it
   read one from (`key_source=env|keychain|file`). Set one with
   **`mav jev set-key < key.txt`** — it reads the key from stdin, never from an
   argument, so the secret does not land in shell history. `mav jev doctor`
   says whether there is a key and where it came from, without printing it.
   With no key, `find` does not go quiet: it answers `resolved_by=none`
   `reason=no_key` and names the three places it looked. The order is
   `MAV_JEV_API_KEY`, then the system keychain (service `mav-jev`), then
   `~/.config/bitomule/mav/config.json`.

   Use `mav ui tree` to understand a screen you do not yet have a target on, or
   whenever `find` returns `none`. It prints compact screen metadata followed by
   bounded `node ...` lines with ids, labels, roles, values, enabled state,
   subroles, titles, pids, focus state, and frames when available.
   Treat this as the primary structured UI source for agents; do not ask for
   `--json`. Add `--agent` for a ranked 40-element view that puts focused and
   actionable elements first and drops frames. If the simulator accessibility service returns an empty
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
7. **Prefer a selector tap over a coordinate tap, and do not read `ok` as
   "it tapped".** `ui tap --x --y` and `ui swipe` report `delivered=unconfirmed`
   because the driver accepting a gesture has been measured to mean nothing:
   on iOS 26.3 simulator slots a coordinate tap prints success and the tree is
   identical either side, while a tap by `--text` on the same element in the
   same second works. Add `--verify` when the answer matters; it costs a tree
   read and returns `verified=changed|unchanged`.

   Use `mav ui tap/type/erase/hideKeyboard/swipe/longPress/wait/scrollUntil`
   for manual exploration. Prefer accessibility identifiers first (`--id`).
   `mav ui erase --focused` clears a focused field: baguette on simulator, and on
   macOS the driver sets the field to the empty value, which does not depend on
   the field holding focus. `mav ui hideKeyboard` dismisses the keyboard via
   baguette on simulator and is a successful no-op on macOS. Both
   return structured errors on a physical device (`erase_unsupported_on_device`,
   `hide_keyboard_unsupported_on_device`). Use `scrollUntil` before tapping
   targets that are present in the tree but may be off-screen. Use coordinates
   only when the tree is insufficient and the screenshot makes the target
   unambiguous. Use text as the last option because labels change with
   localization and copy edits. Use `mav ui wait --id`, `--text`, or `--value`
   for readiness checks.

   Coordinates are always in the space `mav ui tree` reports. On a rotated
   simulator every coordinate gesture — `tap`, `doubleTap`, `swipe`,
   `longPress`, `pinch`, `twoFingerPan`, `drag`, `dragPath` — rotates them
   into the touch surface's own space for you (90/270 only; an upside-down
   tree cannot be told apart from an app that never flipped, so a 180 window
   angle is ignored outright and the gesture goes out exactly as a portrait
   one — no `rotation_unavailable`, nothing for you to compensate). Do **not** pre-rotate
   them yourself, that compensates twice. The result line carries
   `rotation=` plus the dispatched `hid_x`/`hid_y`, or `hid_start`/`hid_end`
   for a swipe's two endpoints.

   Pass all four swipe coordinate flags or none: a partial set fails
   `swipe_coordinates_incomplete`. `ui swipe --direction` has no
   caller-supplied points, so its endpoints are re-derived as fractions of the
   rotated screen and reported with `direction_endpoints=derived` — "up" is up
   on the screen you are looking at. If no screen size can be resolved the
   fixed portrait defaults go out as written and `rotation_unavailable=` says
   the axis is uncompensated.

   **Rotate through `mav ui orientation`, not around it.** MAV can only
   compensate for a rotation it can see, and there are exactly two it can: one
   it applied itself, and one Simulator.app's window records. A simulator
   turned by `baguette orientation` or a raw GSEvent leaves no readable trace,
   and the tree alone cannot say which of the two landscapes is in effect —
   they differ by 180°, so a guess puts every tap in the opposite corner. On a
   headless boot (every `simpool` run) Simulator.app has no window at all, so
   this is the only source there is.

   ```
   mav ui orientation landscape-right
   mav ui tap --x 624 --y 330
     ok ... rotation=90 hid_x=72 hid_y=624 rotation_source=mav
   ```

   `rotation_unavailable=` means MAV saw a rotation it could not apply and
   dispatched raw; `unknown_landscape` specifically means run
   `mav ui orientation` and try again. `portrait-upside-down` is applied but
   never compensated. `rotation_rerouted=axe` means the gesture avoided AXe,
   which refuses coordinate gestures on a rotated simulator it cannot read the
   orientation of. `ui rotate` cannot dispatch at all: no driver provides that
   capability.
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

An optional step that fails is skipped, not done. The run still passes, but the
pass line carries `skipped=N` and `skipped_steps=<step>:<action>`, the trail
records that step as `status: skipped` with the reason in `error`, and
`run.json` lists it. Read those before believing a green run did everything the
flow says.

`skipped=N` always counts top-level steps. A `when` or `whileNotVisible` step
that skipped an optional child inside its `do:` block reports that separately,
as `skipped_children=N` on its own step record, and its `executed=N` counts
only the children that actually ran.

`mav run --prefer-driver axe flow.yaml` forces AXe for semantic UI steps. Use a
per-step `prefer-driver` override when a single interaction needs to pin the
driver:

```yaml
- tap: { text: "Deporte y ocio", prefer-driver: axe }
- wait: { text: "Continuar", prefer-driver: axe, timeout: 5s }
- hideKeyboard: {}
```

`open: { clearState: true }` and `open: { clear-state: true }` are both valid
flow spellings. `open: { skipBuild: true }` skips the launch recipe's `build`
step for that one step; `mav run flow.yaml --skip-build` skips it for every
`open` step in the flow. See **Reusing a build across runs** below.

`mav ui hideKeyboard` dispatches through baguette on simulator and returns
`hide_keyboard_unsupported_on_device` on a physical device.

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

## Steps that say what they want in words

A flow declares the route; the model only ever resolves **one element on one
screen**, which is the part it does well. Three pieces, and they compose with
every action that already takes a selector (`tap`, `type`, `longPress`,
`toggle`, `assert`...).

**`find`: a selector that asks.** One field, read the same way from YAML and
from the command line:

```yaml
- tap: { where: { find: "la primera categoría" } }
- tap: { where: { find: "la primera caja", role: "cell" } }
```

```bash
mav ui tap --find "la primera categoría"
```

The other selector fields are structural and run **first**: they cut the
candidates down, and the words only choose among what survived. What cannot be
acted on — disabled, invisible — is never offered. If the model declines, the
step fails with `find_abstained` and stops: there is no second wording, no
second model, no ranking, and no confidence threshold anywhere. "Could not ask"
is a different code, `find_unavailable`, with `find_reason=no_key|no_network|
ci_refused`. Like `mav ui find`, this never runs in CI.

**Text the model does not write.** The flow declares its inputs; a step names
one, or lets the model name one:

```yaml
inputs:
  nombre: "Caja de herramientas"
  cantidad: "12"
steps:
  - type: { where: { find: "el campo del nombre" }, text: { from: nombre } }
  - type: { where: { find: "el campo de cantidad" }, text: { ask: "lo que toca escribir aquí" } }
```

`from` is a map lookup and costs nothing. `ask` puts the **keys** on the menu
and the code substitutes the value; anything that is not one of the declared
keys is an abstention (`text_abstained`), so nothing the model says is ever
typed. With one declared input nothing is asked.

**`verify`: a judgement about content.**

```yaml
- verify: { ask: "¿la caja que se ve abierta está vacía?" }
```

It records `verdict=yes|no|unclear` and nothing reads it back. Whether the
screen changed, and whether the flow arrived, are decided in code — never by
asking.

While a flow runs, the tree read for one step is reused by the next one as long
as nothing could have moved (any step that is not a read invalidates it). A
find that consulted the model re-checks the chosen element's identity against a
fresh read immediately before acting, and fails with `element_moved` rather
than tapping where the row used to be.

## Gestures

`mav ui longPress --x X --y Y [--duration 800ms]` holds one finger down. It is
not multitouch, so it works on a physical device as well as on a simulator: on
a device the hold goes out through idb.

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

## App Store Screenshots

Two simulator-wide knobs make the shots reproducible. Both are simulator-only and
return a structured error on a physical device (`appearance_unsupported_on_device`,
`status_bar_unsupported_on_device`) and on a macOS target (the same codes ending
in `_unsupported_on_macos`). The codes are emitted by the CLI; inside a flow the
step fails as `appearance_set_failed` / `status_bar_set_failed`, as the other
device actions already do.

```bash
mav sim appearance dark
mav sim appearance light
mav sim statusbar set --preset appstore          # 9:41, full battery, full signal
mav sim statusbar set --time 9:41 --battery-level 100 --cellular-bars 4 --wifi-bars 3
mav sim statusbar clear
mav sim language set --language en-US             # or --language en --locale en_US
mav sim language get
```

**`sim language` is not optional on iPad.** An iPad status bar shows the DATE,
and SpringBoard draws it in the SIMULATOR's language — `open: { language: en }`
is a launch argument and reaches the app process only. An English iPad capture
on a Spanish simulator reads `Viernes 18 de septiembre`, which is how Boxy
published English screenshots with a Spanish date for several versions. iPhone
captures never showed it: no date in that status bar.

A bare subtag (`--language fr`) is refused, because simctl takes it and iOS
falls back to English without saying so; `--language de --locale de_DE` is
enough, so a flow can pass the params it already has. The change restarts
SpringBoard (~5s) and waits for it, is a no-op when the language is already
set, and outlives the run — set it back on a pooled simulator.

`--preset appstore` is the status bar Apple uses in its own marketing shots. Every
field stays individually settable and an explicit flag overrides the preset, so a
screenshot that needs a different clock or a low battery is still one command.
The override is additive: `--time` alone changes the clock and leaves the rest of
the status bar as it is. Clear it when the run is done, or the next capture in the
same simulator inherits it.

Both are also flow actions, so the screenshot matrix is one flow, re-run per
language after `mav sim select --language de --locale de_DE` (the language is a
launch argument, not a flow param). Build once and pass `--skip-build` on every
run, or the same unchanged app is rebuilt once per language:

```bash
mav open
for locale in en_US de_DE es_ES; do
  # Name the device: `mav sim select` with no target selector re-picks one,
  # and a leftover booted simulator from another project can win it.
  mav sim select --device "iPhone 17 Pro Max" --ios 26 \
    --language "${locale%%_*}" --locale "$locale"
  mav run app_store_shots.yaml --skip-build
done
```

The same flow, unchanged:

```yaml
name: app_store_shots
steps:
  - sim.statusbar.set: { preset: appstore }
  - sim.appearance: { appearance: light }
  - open: { clearState: true }
  - capture: { name: home-light }
  - sim.appearance: { appearance: dark }
  - capture: { name: home-dark }
  - sim.statusbar.clear: {}
```

`sim.statusbar.set` accepts `preset`, `time`, `dataNetwork`, `wifiMode`, `wifiBars`,
`cellularMode`, `cellularBars`, `operatorName`, `batteryState`, `batteryLevel`.
Quote `time` in YAML.

`mav flow lint flow.yaml` validates those fields with the parser the run uses:
`appearance` must be `light` or `dark`, `preset` must be `appstore`, the enum and
0-N fields are range-checked, and a `sim.statusbar.set` with no fields at all is
an error, while a `${params.x}` binding is left for the run to resolve. Lint the
matrix before running it — a bad value found at step 12 costs
the eleven captures before it. `sim.statusbar.clear` takes no fields; passing any
is a warning, since it resets the whole bar.

Appearance and the status bar are simulator state, not app state: they survive a
relaunch, so set them once per matrix cell rather than per capture.

`mav sim appearance` waits two seconds after the switch, long enough for the
screen to repaint: the capture path otherwise serves the pre-switch frame and the
dark cell of the matrix comes out light. No `delay` step is needed between it and
the capture.

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

### Giving the app its own environment

Put `NAME=value` in front of the `launch` command and it reaches **the app**:

```yaml
    launch: BOXY_FORCE_PAID=1 xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"
```

MAV translates it per target (`SIMCTL_CHILD_*` on a simulator, `IDB_*` on a
device, the process environment on macOS), so relaunching by hand with
`SIMCTL_CHILD_*` is no longer needed for a flag the app reads at start. Values
can use the `MAV_*` variables (`OUT=$MAV_RUN_DIR/out`). The commands trail
records the names, never the values: `launch.launch driver=simctl
env=BOXY_FORCE_PAID`. Read that line to confirm the variable was passed — if it
has no `env=`, MAV did not pass one. On a physical device the names idb uses
itself (`UDID`, `COMPANION`, `COMPANION_TLS`) are refused with an error; the
match is exact, so a lowercase `udid` — which idb never reads — is allowed.
A prefix on `install` runs verbatim in the shell instead: those variables are
for the install tool, not for the app (its values are redacted in the trail).

Values follow shell rules: single quotes mean literal, and command substitution
(`$(...)`, backticks) is refused (`launch_env_command_substitution`) because the
driver path has no shell — compute it in `build`/`app_path` instead. A launch
line that reduces to assignments only, which one missing quote produces, fails
with `launch_command_only_env` rather than launching the app as though the
command had run.

The translation only fires for a launch line MAV recognizes: the canonical
`xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"` / `idb launch ...
"$MAV_BUNDLE_ID"` form, or an empty command with `bundle_id` set. A hardcoded
bundle id or a wrapper script instead runs in the shell verbatim, where the
prefix sets the variable on the launch tool, not the app; MAV emits a
`launch_env_not_translated` warning when it can tell that drop is certain
(not for a wrapper script, which may re-export on its own).

### Reusing a build across runs

The `build` step is the expensive one and the one that produces nothing new when
the checkout has not changed. `--skip-build` drops it and keeps `app_path`,
`install` and `launch`. It is applied to the recipe's `build` step, not to one
build system, so it works for every launch mode -- with the caveat that
`mode: already_installed` has no `build` and no `app_path` to begin with, so
there it is a no-op rather than a saving.

- `mav open --skip-build` covers that one launch.
- `mav run flow.yaml --skip-build` covers every `open` step in the flow,
  including the ones that do not mention it. Use this for a matrix that runs the
  same flow once per language: build once, then reuse.
- `open: { skipBuild: true }` marks a single flow step, for a flow that builds in
  its first `open` and reuses it in later ones.
- `mav run --target ... --target ...` already builds once and each target's child
  run carries `--skip-build`.
- `--skip-build` is rejected with `--no-relaunch`, which skips the whole recipe.

If nothing was built, `app_path` cannot resolve an artifact and `mav open` says
so itself instead of passing the build system's error through:

```text
fail code=build_skipped_app_missing logs=.mav/runs/439a2e85/logs.txt next="rerun without --skip-build" run=439a2e85 stderr="build was skipped (--skip-build) and no built app was found: app_path printed /repo/build/App.app, which does not exist" step=app_path
```

The same code comes back when `app_path` prints a path that is not on disk.

Inside a flow the step fails as `open_failed`, like every command wrapped into a
flow step, and carries that whole line in `detail`:

```text
fail code=open_failed action=open detail="fail code=build_skipped_app_missing ... next=\"rerun without --skip-build\" ..." step=1
```

Either way the run's `commands.jsonl` gets a `launch.skip_build_check` entry
naming the path MAV looked for. On that code, rerun the same command without
`--skip-build` once, then resume.

`video.start` / `evidence.start` video recording is simulator-only in this
release. On physical devices, use `capture` / `evidence.step` screenshots,
crash checks, logs, and reports for evidence.

## Environment variables MAV reads

These are read *from* your environment. The launch recipe's own variables
(`MAV_ROOT`, `MAV_UDID`, `MAV_APP_PATH`, ...) are the other direction --
MAV sets those for the commands it runs; see "Custom launch recipes" above.

| Variable | Status | What it does |
| --- | --- | --- |
| `MAV_TARGET_KIND` / `MAV_TARGET_UDID` | supported | Pin the target, beating both a config pin and `target_command`. Either works on its own: `MAV_TARGET_UDID=<udid> mav ui tree` pins the simulator with no kind beside it, and the `ok` line reads `target_source=env`. (Through v0.19.2 the whole overlay hung off `MAV_TARGET_KIND`, so a UDID on its own was ignored in silence.) `mav run --target ...` sets them on each matrix child. |
| `MAV_TARGET_NAME` / `MAV_TARGET_RUNTIME` | supported, partial | Narrow the reported target, but do not select one: nothing in mav resolves a name or a runtime to a UDID. On their own the target still comes from a pin, `target_command`, or the booted simulator. |
| `MAV_PROFILE` | supported | Selects a platform profile, below `--profile` and above `default_profile`. |
| `MAV_EXACT_RUN_DIR` | supported, internal | Pins run state to this exact directory instead of allocating one under `.mav/runs/`. `mav run --target ... --target ...` sets it per matrix child so each target gets an unambiguous run dir. Set it yourself only to place a run's state somewhere specific. |
| `MAV_DRIVERS_DISABLE` | supported, internal | Comma-separated driver ids to suppress. Changes routing, so a stale export makes `mav doctor` disagree with reality. |
| `MAV_MATRIX_CHILD` | internal, do not set | Marks a matrix child. Exporting it makes `mav run --target a --target b` stop fanning out, silently. |
| `MAV_SKIP_BUILD` | **gone** | Was the private channel `mav run --target` used to tell its children not to rebuild. Removed in v0.16.2 and now silently ignored. The supported spelling is the `--skip-build` flag (`mav open --skip-build`, `mav run flow.yaml --skip-build`, `open: { skipBuild: true }`). |

## When `target_command` cannot pick a simulator

If `.mav/config.yaml` sets `target_command`, that is the source of the
simulator -- unless `MAV_TARGET_KIND` / `MAV_TARGET_UDID` or a pinned
`simulator_udid` overrides it. A pin wins outright and reports
`target_command_ignored` on the `ok` line; it is a warning, not a failure.

`--target` is a `mav run` flag and only `mav run` reads it. On any other
command it fails with `code=flag_unsupported` instead of being accepted and
ignored; to point a single command at one simulator, export
`MAV_TARGET_UDID`.

Where `target_command` is what should answer and cannot -- it exits
non-zero, prints nothing, or exceeds `target_command_timeout` (3 minutes by
default) -- the command **fails**. MAV does not fall back to whatever
simulator is booted:

```text
fail code=target_command_timeout detail="no UDID after 3m0s" fallback=none remediation="Raise target_command_timeout in .mav/config.yaml, or set target_command_required: false to allow the booted-simulator fallback" target_command="simpool lease --device \"iPhone 17 Pro\" --os 26.3" target_command_timeout=3m0s title="Configured target_command timed out; no fallback"
```

Codes: `target_command_failed` (non-zero exit -- the pool said no),
`target_command_timeout` (raise `target_command_timeout`, or make the
command faster), `target_command_empty` (the command printed no UDID),
`target_command_timeout_invalid` (`target_command_timeout` is not a Go
duration). All carry `fallback=none` and exit non-zero.

Two commands are exempt and still work through the failure, on purpose:
`mav doctor` reports it as `target_command_warn` and still gives you the
diagnosis, and `mav sim select` does not consult `target_command` at all, so
pinning a simulator remains available as the escape from a broken pool
manager.

Do not work around a failure by unsetting `target_command` -- on a machine
with several simulators booted that is exactly how a capture ends up taken
on a device nobody chose. Fix the command, or raise the timeout. The one
deliberate escape hatch is `target_command_required: false` in
`.mav/config.yaml`, which restores the warn-and-fall-back behaviour and
reports `target_command_warn=...` on the command's success output.

## Semantic taps need AXe 1.8.0

MAV passes `axe tap --tap-style physical` on every semantic tap, so AXe
1.8.0 or newer is the floor. AXe's own default (`automatic`) routes anything
that is not a switch through FBSimulator's `tapAt`, which **drops the touch
under load and still exits 0** printing `✓ Tap ... completed successfully`.
The symptom is a flow reporting `ok` step after step while the screen never
changes -- and a stale tree afterwards, so the next `--id` from that tree
looks like it does not exist.

Measured 2026-09-19 (iPhone 17 Pro / iOS 26.3, same Settings row, clean
launch each time, counting tree nodes 135 → 188): `automatic` 6 of 12 then
0 of 8; `simulator` 2 of 10; `physical` 10 of 10. Through `mav ui tap
--text`: 0 of 10 before, 10 of 10 after.

If a tap ever reports `ok` and nothing moves, **count nodes before and
after** (`mav ui tree` reports `nodes=`). An `ok` from a tap is not evidence
that anything happened.

## Which simulator did I just drive?

Every success line carries `target_source=` beside `udid=`, because a UDID
only answers that question for someone willing to compare identifiers by
hand:

| `target_source` | Means |
| --- | --- |
| `env` | `MAV_TARGET_UDID` -- a `mav run --matrix` child, or a `simpool with`/`acquire` wrapper |
| `config` | `simulator_udid` / `device_udid` in `.mav/config.yaml` |
| `target_command` | the UDID a pool manager printed |
| `booted` | **MAV chose this one itself**: nothing named a target and exactly one simulator was booted |
| `localhost` | a macOS target |

`target_source=booted` is the value to check for in a script: it is the only
one that means nobody said which simulator this was.

With **several** simulators booted and nothing naming one, MAV refuses
instead of choosing:

```text
fail code=ambiguous_booted_simulator booted="AAAA-1111 (iPhone 17 Pro), BBBB-2222 (iPhone Duo)" booted_count=2 fallback=none remediation="Pick one: `mav sim select <udid>`, or set target_command in .mav/config.yaml (a pool manager such as `simpool lease`), or export MAV_TARGET_KIND=simulator with MAV_TARGET_UDID"
```

It used to pick one at random and report `ok`, which on a shared machine
means measuring somebody else's device. `mav doctor` reports the ambiguity
in `target_command_warn` rather than failing.

## An unrecognised key in `.mav/config.yaml` fails the load

`config_unknown_key key=<key> ... known=<every valid key>`. A misspelt key
used to be dropped in silence, so a config that looked applied did nothing.
The legacy `tools:` section is the one key this removes from older configs:
tool detection is a run-time probe, so delete that section.

Each reason reports its own code — `config_unknown_key`,
`profile_unknown_key`, `profile_not_found`, `target_kind_invalid`,
`vm_unsupported_target` — and `config_not_found` means the file really is
absent. **Do not answer any of the others with `mav setup`**: it rewrites
the file, and the fix is the one key the code already names.

`mav doctor` is the exception, in one direction only: with no config file
it still reports `ok`, because that is how you check tools before
`mav setup`. With a config it cannot load it prints the whole diagnosis
under `fail code=<the load error>` — an `ok` next to `launch_recipe=missing`
is the contradiction this closes.

## Command Output

Output is intentionally compact and agent-friendly by default:

```text
ok cmd=open run=7fd logs=/path/to/repo/.mav/runs/7fd/logs.txt
ok cmd=ui.tree driver=axe nodes=42 screen=settings screen_source=identity
node index=1 id=settings_button label=Settings role=button enabled=true frame="{{20, 120}, {180, 44}}"
ok cmd=capture file=/tmp/mav/7fd/captures/largest-videos-after-pinch.png run=7fd
fail code=ui_tap_failed stderr="…"
```

**A failure exits 1 and success exits 0**, so `mav ... && next-step` stops where it
should. Read the `code=` when you need to branch on why; the exit status is enough to
know whether to continue. The `fail` line is always written, including when the command
errors, so there is never a failure with nothing to read.

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
