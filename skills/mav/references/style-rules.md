# Evidence Report Style Rules

These rules adapt visual-explainer discipline to MAV reports.

## Pick A Direction

Choose one aesthetic before writing HTML:

- Blueprint: precise grid, slate/blue palette, mono labels, crisp borders.
- Editorial: serif or high-character headings, generous whitespace, restrained
  gold/ink accents.
- Paper/ink: warm background, terracotta/sage/amber accents, document-like.
- Data-dense: compact tables, small labels, more rows visible, muted palette.
- IDE-inspired: use a named palette deliberately; do not approximate.

Do not default to generic dark blue dashboards. Avoid violet/indigo gradients,
neon cyan/magenta/pink, gradient text, glowing cards, and identical cards
everywhere.

## Typography

Typography carries hierarchy. Use a distinct pairing when possible:

- IBM Plex Sans + IBM Plex Mono
- Instrument Serif + JetBrains Mono
- DM Sans + Fira Code
- Bricolage Grotesque + Fragment Mono
- Plus Jakarta Sans + Azeret Mono

If external fonts are inappropriate for a local evidence artifact, use strong
system fallbacks but still define a deliberate type scale. Avoid `Inter`,
`Roboto`, `Arial`, `Helvetica`, or plain `system-ui` as the only visible design
choice.

## Layout

- First viewport must show verdict and primary visual proof.
- Use asymmetric layout: copy/metrics on one side, media on the other.
- Make media inspectable. Do not hide iPhone screenshots in tiny cards.
- Use a timeline for step evidence. A plain grid loses the proof sequence.
- Put audit material in compact sections after the visual proof.
- For long reports, add section navigation.
- Use real tables for structured comparisons or issue matrices.

## Evidence-Specific Visual Language

- Accepted evidence: calm green/teal status, not celebratory decoration.
- Warning: amber with precise text.
- Blocker/rejected: red status plus explanation of why it is not proof.
- Missing evidence: neutral empty state plus exact rerun guidance.

## Motion And Interaction

Use CSS-only entrance or hover transitions sparingly. Respect
`prefers-reduced-motion`. Do not add continuous pulse/glow animations. Video
controls are useful; custom JS is rarely necessary for MAV reports.

## Self-Containment

The final report must be one HTML file plus local media references from the MAV
run directory. Do not require a build step. Do not depend on remote app assets.
Remote font CDNs are optional only when acceptable for the environment; the page
must degrade cleanly without them.
