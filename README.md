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
mav capture
mav preview init
mav preview settings
mav evidence start
mav go settings
mav evidence step --name settings-before-toggle --note "Notifications toggle is off"
mav ui tap --id notifications_toggle
mav evidence step --name settings-after-toggle --note "Notifications toggle is on"
mav evidence stop
mav logs --contains CheckoutView
mav evidence report
```

Project state lives in `.mav/`.
Run artifacts live in `/tmp/mav/<run-id>/`.

## Design Rules

- `mav go` only follows known routes in `.mav/app-map.yaml`.
- `mav sim` selects and boots explicit simulator devices, runtimes and locales.
- `mav capture` never navigates.
- `mav logs` only reads logs captured when MAV launched the app.
- Accessibility tree inspection is preferred before screenshots.
- AXe is the primary driver for tree and semantic taps by id/text. idb remains
  available for screenshots, crashes, and coordinate taps when AXe cannot target
  an element.
- Evidence reports are generated explicitly. For user-facing verification,
  use named evidence steps for the checked behavior and video for the full flow.
- `mav go --record` is only a shortcut for recording a mapped navigation route.
  Rich feature evidence should use `mav evidence start/step/stop` around the
  exact interactions being tested.
