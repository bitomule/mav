# MAV Evidence Quality Checks

Run these checks before sharing a report.

## Content

- The hero states the verdict and the behavior under test.
- The primary visual proof dominates the first viewport. Evidence comes before
  explanatory copy.
- The accepted video or strongest valid screenshot has `Open`, `Download`, and
  `Copy path` controls.
- Each named capture has direct open/download access.
- Logs and commands have copy buttons.
- Accepted video is embedded only when `video_status` is `accepted`.
- Invalid or missing video is explicitly called out.
- Every named screenshot has a claim sentence, not just a filename.
- Every rejected artifact remains visible as rejected, with the manifest issue.
- Crash status is stated.
- Command trail and log tail are present or explicitly empty.

## Accuracy

- All counts match `report.json`.
- Every media path in HTML points to an existing local file.
- The report never upgrades `needs review` or `blocked` evidence to `verified`.
- Screenshots marked accepted in the HTML have `image.ok=true` in the manifest.
- Video duration/frame facts match the manifest.

## Visual Quality

- Squint test: verdict, primary media, timeline, and blockers are visually
  distinct.
- Evidence-first test: if the page is opened at desktop size, the first thing a
  reader notices is the media evidence, not a pale title block or dashboard.
- Swap test: replacing the palette/fonts with a generic dark dashboard would
  noticeably reduce the design. If not, strengthen the direction.
- No section uses the same identical card treatment as every other section.
- Timeline images are large enough to inspect UI state.
- Audit sections are compact and do not overpower visual proof.
- No emoji section headers, glowing cards, gradient text, or generic violet
  accent palette.

## Responsive

- At mobile width, text does not overlap or escape containers.
- Media scales without clipping important UI.
- Long file paths wrap.
- Section navigation, if present, remains usable.

## Browser Inspection

When possible, open the report in a browser or inspect the HTML:

- No broken video/image references.
- No console errors from optional script snippets.
- First viewport shows the intended hierarchy.
- Scroll through the full page and verify no overflow.
