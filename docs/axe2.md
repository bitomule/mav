# iOS automation with AXe 2

MAV requires AXe **2.0.4 or newer**. AXe 1 and older AXe 2 patch releases are
rejected with installation instructions. Homebrew installs the AXe formula as a
dependency. With another installation method, run `mav setup --install deps` or
`brew install cameroncooke/axe/axe`; upgrade an existing installation with
`brew update` followed by `brew upgrade cameroncooke/axe/axe`.

AXe 2 is an independent command-line client over native simulator and physical
device services. Commands carry an explicit target UDID; MAV does not use a
persistent current target inside AXe. AXe may reuse native connections through
its broker. MAV caches driver discovery and version health within one invocation,
not across processes. No external idb or idb_companion is required by AXe 2.
MAV uses Xcode CoreDevice for physical app lifecycle.

This guide covers ordinary automation introduced in MAV 0.34.0 and corrected
in 0.35.0. The [goal engine](design/goal-engine.md) uses AXe simulator
snapshot/ref admission, with MAV's own planner; it never invokes
`axe agent <objective>`. `ui tree --agent` is MAV's compact formatter.

## API and feature coverage

MAV's operation types live in `internal/mav/drivers`. They describe portable
operations; a driver declares support for a target and implements the interface.
The CLI and YAML flows call those interfaces, with no raw AXe passthrough.

| AXe 2 feature | MAV API | Physical-device restrictions |
| --- | --- | --- |
| Simulator listing | `sim list` | Simulator only |
| Physical listing and readiness | `device list`, `device discover`, `device doctor` | Trust, pairing, Developer Mode and developer services required |
| Full tree and point inspection | `ui tree`, `ui tree --point X,Y` | Supported |
| Semantic and coordinate taps | `ui tap` | Supported |
| Repeated or multi-touch tap, hold, semantic press | `ui tap --count`, `--touches`, `--duration`, `--semantic-press` | Native AXe validation applies |
| Slider percentage and endpoint | `ui slider --id ID --percent N` or `--endpoint min|max` | Simulator only |
| Keyboard text and ordered HID input | `ui type`, `ui erase`, `ui key`, `ui keySequence`, `ui keyCombo` | US keyboard layout; physical text submission is limited to 256 UTF-8 bytes |
| Touch down/up and contact token | `ui touch` | Supported |
| Straight swipe and drag | `ui swipe`, `ui drag` | Supported |
| Stroke path and shape | `ui draw`, existing `ui dragPath` | One-stroke draw supported; arbitrary per-point timing is driver-dependent |
| All gesture presets | `ui gesture NAME` | Right-edge swipe is unavailable on hardware |
| Home, lock, Siri and simulator buttons | `ui press --button NAME` | Hardware supports home, lock and siri; lock requires `--allow-lock` |
| Hinge read/set and device rotation | `sim hinge`, `ui turn --direction left|right` | Hinge is simulator only; turn supports physical devices |
| Settings read/set | `settings` | Hardware supported; simulator settings require Xcode 27+ |
| Simulated location, scenarios and clear | `location set`, `reset`, `scenarios`, `scenario NAME` | Uses AXe's supported device transport |
| Status-bar show, preset, clear and fields | `ui statusbar --spec JSON` and existing `sim statusbar` | Simulator only |
| Clipboard write and readback | `clipboard copy`, `ui` typed clipboard flow operation | Simulator only; existing `clipboard read` uses the system driver |
| Display identities and sizes | `sim displays` / `ui displays` | Simulator only |
| Confirmed shutdown | `sim shutdown` | Simulator only |
| Selected-display input | `--selected`, paired `--screen-id` / `--screen-uuid` | Simulator only; each operation enforces the native guarantees available to it |
| Ordered, prevalidated batches | `ui batch --spec @batch.json` | Supported subset validated before sending any step |
| PNG screenshot | `capture` | Supported |
| H.264 MP4 recording | `evidence start|stop`, `video record --out PATH` | Supported |
| Frame and native video streaming | `video stream --format FORMAT` | Native `h264` stream is physical only |
| Structured results and partial-input receipts | Standard `ok` / `fail` fields, typed flow evidence | Delivery uncertainty is preserved |
| AXe skill installation | `install-skills` installs MAV's own skill | No direct AXe skill required |

## Typed options

New commands accept flags named after their typed fields in kebab-case, or a
JSON specification with `--spec '{...}'` / `--spec @file.json`. JSON field names
are case insensitive; the outer specification also accepts kebab-case names.
Unknown fields and wrong types are rejected. Numeric durations in specifications
are milliseconds, and `--duration 800ms` retains MAV's existing duration syntax.
No shell or driver command string is accepted by this API.

Examples:

```bash
mav ui key --code 42
mav ui keySequence --codes '[4,5,6,40]' --delay-ms 20
mav ui keyCombo --modifiers '[227]' --key 4
mav ui tap --id Continue --count 2 --touches 1 --duration 100ms
mav ui slider --id volume --percent 75
mav ui touch --x 100 --y 200 --down
mav ui touch --x 100 --y 200 --up --contact-token TOKEN
mav ui draw --points '[{"x":10,"y":10},{"x":80,"y":50}]' --duration 600ms
mav ui gesture swipe-from-left-edge
mav ui gesture two-finger-tap --spec '{"x":100,"y":200,"x2":150,"y2":200}'
mav settings --appearance dark --reduce-motion true
mav settings
mav location scenarios
mav location scenario city-run
mav ui statusbar --spec '{"action":"preset","preset":"appstore"}'
mav video record --out recording.mp4 --fps 15 --quality 70 --scale 0.5
mav video stream --format mjpeg > frames.mjpeg
```

Gesture preset names: `scroll-up`, `scroll-down`, `scroll-left`, `scroll-right`,
`swipe-from-left-edge`, `swipe-from-right-edge`, `swipe-from-top-edge`,
`swipe-from-bottom-edge`, `swipe-from-top-right-corner`, `pinch`, `rotate`,
`double-tap`, `two-finger-tap`, `two-finger-hold`, `two-finger-swipe`.

The `GestureSpec` fields also expose secondary points and endpoints, radius,
scale, degrees, distance, hold/interval, edge inset, sampling delta, pre/post
delays, until/inside selectors, maximum scrolls and explicit screen dimensions.
`SwipeSpec` exposes sampling delta, delays and until selectors; `DragSpec`
exposes the move count and delays. AXe cannot express a drifting pinch or a
terminal hold on two-finger pan; those requests are refused rather than silently
changing the gesture. Select an alternative supporting driver explicitly.

`ui type --stdin`, `ui type --file PATH` and `clipboard copy --stdin|--file PATH`
read literal content without interpreting it as flags. Choose one text source.

`SettingsSpec` exposes appearance, text size, Liquid Glass, contrast, reduced
motion/transparency, borders, VoiceOver, color filter and opacity/intensity.
Read without options. Values and platform availability are checked by AXe.

## Flows and batches

Every new ordinary operation is available through a typed `automation` step:

```yaml
name: keyboard-and-settings
steps:
  - automation:
      command: settings
      options:
        appearance: dark
        reduceMotion: true
  - automation:
      command: keySequence
      options:
        codes: [4, 5, 6, 40]
        delayMs: 20
```

The same operation names cover tree-independent primitives, settings, location,
statusbar, clipboard, displays, readiness, discovery, shutdown and batches.
Existing MAV actions, selectors, waits and assertions continue to work.
`flow lint` rejects unknown operation names, options and incompatible types.

A batch spec contains typed steps, not AXe command lines:

```json
{
  "steps": [
    {"tap": {"selector": {"id": "Search"}}},
    {"type": {"text": "sample"}},
    {"key": {"code": 40}}
  ],
  "continueOnError": false
}
```

For navigation between screens, set `axCache: perStep`; AXe's default
`perBatch` cache can still describe the screen from an earlier step.

All steps are serialized and validated before input. Native target restrictions
are checked locally. A failed later step preserves evidence that an earlier step
already sent input, even when the later step was rejected before input.

## Display and delivery guarantees

AXe coordinates are logical points in the **current orientation**. MAV does not
rotate those points into portrait or route an open foldable's input through idb.
AXe 2.0.4 selects the active display for screenshots.

Use `sim displays` to observe screen identity, then pass both `--screen-id N`
and `--screen-uuid UUID` to compatible input, tree, screenshot and media commands.
The pair binds the operation to the observed active screen. `--selected` requests
selected input without an explicit screen pair. These options never enable agent
mode. `--expected-pid` is only accepted where AXe's non-agent operation actually
enforces it; unsupported combinations fail before input. Selected keyboard input
requires a focus ID, available through the typed key/text specification.

A failure can report `delivery=notSent`, `sent`, or `uncertain`. `retry=unsafe`
means MAV cannot safely replay the action. Flow retry policies and stale-target
recovery do not replay sent/uncertain input. Reobserve the UI before choosing the
next action. Structured flow evidence retains the native receipt/envelope in the
run directory. A successful HID receipt proves dispatch, not application-level
success; use an assertion or a verified observation for that.

Input commands print MAV's compact `ok` / `fail` line. Native JSON for input
and failures is printed only with `--raw`; reads such as `sim displays` still
print the requested data. Flow evidence retains the native envelope.

Without a receipt, only a `tap` error with `elementNotFound`, `ambiguousElement`,
`elementNotHittable` or `rejectedBeforeInput` proves `delivery=notSent`.
A native receipt always takes precedence: partial input, unknown cleanup or
quarantine cannot become safe to retry just because of an error code. A slider
can resolve its selector again **after** dragging, so its plain selector errors
remain `uncertain`, as do errors from scroll-until, batches and unknown commands.
See AXe 2.0.4's [tap admission](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Commands/Tap.swift#L502-L556),
[slider action and verification](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Commands/Slider.swift#L237-L258)
and [selected-input failure classification](https://github.com/cameroncooke/AXe/blob/v2.0.4/Sources/AXe/Utilities/SelectedInputOutput.swift#L63-L88).

After an ordinary tap selector is refused as ambiguous with no input sent, MAV
can resolve a repeated id on a structural ancestor and its innermost button.
The matches must form a real ancestor chain with nested frames (equal frames
count; tolerance is 0.5 pt), and no target ancestor may be marked hidden or
disabled. A new, uncached AXe tree supplies the point; type filters are retained.
MAV reports `selector_via=tree nested=true matches=N`. Distinct actionable
controls, siblings and non-containing overlaps remain ambiguous. Selected
display/process input keeps AXe's original refusal, since ordinary coordinates
cannot preserve that binding. This read-before-tap fallback is not atomic;
it does not provide snapshot/ref admission or prove application-level success.

Evidence recording captures the display active at startup. Foreground
`video record --follow-active-display` can follow display changes. Background
evidence recording does not use follow mode: AXe keeps that mode's output in an
undisclosed temporary file until stop, which prevents a reliable startup check.

Recording startup waits for a live recorder and an MP4 header, rather than only
a forked PID. Stop uses the driver and target recorded at startup, even if config
changes. Foreground recording handles cancellation gracefully so the MP4 can
finish. Stream stdout contains media only; diagnostics use stderr. Native H.264
streaming does not accept frame-rate, quality or scaling overrides.

## Upstream references

The integration was checked against the [AXe v2.0.4 source](https://github.com/cameroncooke/AXe/tree/v2.0.4),
its CLI help and the [physical-device documentation](https://www.axe-cli.com/docs/physical-devices).
The local behavior tests cover command construction, JSON envelopes, version
rejection, capability selection, lifecycle, delivery, selected displays and media.

## Verification for this release

We ran the integration tests with the real AXe 2.0.4 binary on an iOS 27.0
simulator: tree and point queries, a verified navigation tap, selected-display
input, settings readback, screenshots, an ordered batch, foreground recording,
stream cancellation and an 11-step evidence flow. The flow produced an accepted
34.34-second video with 301 frames. Cancellation left no AXe child processes.
These are smoke-test results, not a performance benchmark.

No iPhone or iPad was connected for UI/lifecycle verification. Discovery and
readiness were checked against a known disconnected iPhone; AXe returned
`developerDiskImageUnavailable`. Physical command construction and lifecycle
behavior are covered by synthetic tests and the upstream implementation.

One upstream limitation was observed: setting appearance to dark and reading it
back returned dark, while both AXe and simctl screenshots of the running Settings
app stayed light. MAV reports the native result; check the rendered UI before
using a setting change as screenshot evidence. The test restored light afterward.
