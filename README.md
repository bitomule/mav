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
mav capture
mav preview init
mav preview settings
mav go settings --evidence --video-seconds 12
mav logs --contains CheckoutView
mav evidence video record --seconds 3
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
- Evidence reports are generated explicitly. For user-facing verification,
  include screenshots for the checked steps and video for the full flow.
