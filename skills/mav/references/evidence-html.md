# MAV Evidence HTML

Use this reference when turning `<run-dir>/report.json` into
`<run-dir>/report.html`. Project runs normally live under
`.mav/runs/<run-id>/`; legacy or ad-hoc runs may live under
`/tmp/mav/<run-id>/`. Use the paths printed by MAV.

## Source Of Truth

The manifest is authoritative. Read it first and extract:

- `verdict`
- `video_status`, `video`, `video_mp4`, `video_duration`, `video_frames`,
  `video_issue`
- `valid_step_count`, `invalid_step_count`, `steps[*].image`
- `issues[*]`
- `crashes[*]`
- `commands[*]`
- `logs`

Do not infer that a file is valid because it exists. Use the manifest status.

## Required Narrative

The report must answer these questions in the page itself:

1. What behavior was verified?
2. What artifact is the primary proof?
3. Is the video accepted, missing, or rejected?
4. Which screenshots support the claim, and what does each one prove?
5. Which artifacts failed validation?
6. Were crashes captured?
7. Which commands and logs support the run?

Do not write a generic gallery. Every screenshot caption should state an
assertion. If the manifest note is weak, write the weakness explicitly.

## Page Structure

Use this order unless the evidence shape demands a better one:

1. Evidence stage: accepted video or strongest valid screenshot, visible in the
   first viewport and large enough to inspect.
2. Evidence actions: open, download, and copy path controls for the primary
   video and every named capture.
3. Verdict: one-sentence finding next to the evidence, not above it.
4. Metrics: video status, screenshot accepted/rejected counts, crash count,
   command count.
5. Capture strip: compact list of captures with thumbnails and direct actions.
6. What this proves: short explanation of the behavior and evidence chain.
7. Integrity checks: blockers and warnings from `issues`.
8. Video evidence: accepted video embedded with duration/frame facts, or a
   clear rejection/missing state.
9. Named evidence timeline: before/action/after captures with large images and
   per-step validation facts.
10. Crash evidence.
11. Command trail with copy button.
12. Log tail with copy button.

For 4+ sections, add a section nav. Keep technical audit sections compact;
the hero, video, and timeline are the visual core.

## Media Rules

- Accepted video: embed with `<video controls>`, show duration and frames.
  Prefer `video_mp4` for the source when it is present; `video` remains the
  original recording path and may be a QuickTime `.mov` that Chromium browsers
  cannot play inline.
- Accepted video should be the visual center of the page. Avoid burying it
  below overview copy.
- Invalid video: do not embed as proof. You may link/show the file for
  inspection, but label it rejected near the media.
- Missing video: say the report has no accepted video sequence.
- Accepted screenshot: show large enough to inspect the UI. Include dimensions
  and bytes.
- Rejected screenshot: show a rejected placeholder and the manifest issue.
- Current screenshot is secondary. Do not let it replace named evidence steps.
- Every important artifact should have at least one direct action:
  `Open`, `Download`, or `Copy path`. Logs and command trails should have
  `Copy logs` / `Copy commands` buttons.

## Completion Criteria

The report is complete only when a reader can understand the proof without
opening `report.json`, and can still audit every claim back to local artifacts.
