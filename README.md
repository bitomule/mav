# MAV

Mobile Agent Verifier (`mav`) is a deterministic CLI for helping coding agents
validate iOS Bazel apps.

MAV does not plan or explore by itself. It runs concrete commands, captures logs
and evidence, and returns compact output that an agent can act on.

## Install

Development build:

```bash
go build -o .build/mav ./cmd/mav
```

## Core Commands

```bash
mav doctor
mav discover
mav sim list
mav sim select --device "iPhone 16 Pro" --ios 26 --locale es_ES --language es
mav open --device "iPhone 16 Pro" --ios 26
mav ui tree
mav ui tap --id settings_button
mav ui tap --x 350 --y 480
mav ui scrollUntil --id privacy_policy_button --direction up --max-swipes 4
mav capture
mav preview init
mav preview settings
mav run /tmp/verify_daily_reminder.mav.yaml
mav logs --key SettingsReached
mav stop
mav evidence report
```

Project state lives in `.mav/`.
Run artifacts live in `/tmp/mav/<run-id>/`.

## Design Rules

- `mav go` only follows known routes in `.mav/app-map.yaml`.
- `mav sim` selects and boots explicit simulator devices, runtimes and locales.
- `mav capture` never navigates.
- `mav logs` only reads logs captured when MAV launched the app. MAV captures
  a filtered unified log stream for temporary MAV probes.
  Validation probes should use `OSLog.Logger` with the configured
  `log_subsystem` and `log_category`, and messages should start with
  `MAV_LOG key=<StableKey>`.
- `mav open` starts only the filtered MAV probe log stream.
- `mav open` can keep log stream processes running for the current run. Use
  `mav stop` when an ad-hoc verification is done. `mav run` stops run-owned
  log streams automatically on success or failure.
- Accessibility tree inspection is preferred before screenshots.
- AXe is the primary driver for tree and semantic taps. Prefer accessibility ids
  first, coordinates only when the visual target is unambiguous, and text as the
  final fallback because labels change with localization and copy. idb remains
  available for screenshots, crashes, and coordinate taps when AXe cannot target
  an element.
- Project-local shell assertions can be enabled with `allow_shell: true` in
  `.mav/config.yaml` and used from native flows with `exec`. MAV constrains the
  command to the project cwd, fixed MAV env vars and a timeout; this is an
  opt-in guard, not a security sandbox.
- Evidence reports are generated explicitly. For user-facing verification,
  use named evidence steps for the checked behavior and video for the full flow.
- `mav run` executes native MAV YAML flows. Use it for repeatable validation
  that combines navigation, AXe/idb UI actions, waits, evidence, logs, crashes,
  and reports.

Example flow:

```yaml
version: 1
name: verify_daily_reminder
steps:
  - evidence.start: {}
  - delay: { duration: 2s }
  - go: { screen: settings }
  - wait: { text: Daily Reminder, timeout: 5s }
  - evidence.step: { name: before-toggle, note: Daily Reminder before tap }
  - tap: { text: Daily Reminder }
  - scrollUntil: { text: Privacy Policy, direction: up, maxSwipes: 4 }
  - waitUntil:
      any:
        - text: "Don’t Allow"
        - text: "Allow"
        - changedFrom: before-toggle
      timeout: 5s
  - evidence.step: { name: after-toggle, note: Result after tapping reminder }
  - logs: { key: SettingsReached }
  - evidence.stop: {}
  - crashes: {}
  - report: {}
```
