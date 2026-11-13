# MAV 0.34.0

MAV now requires AXe 2.0.4 or newer for iOS automation. Homebrew installs AXe as
a dependency. AXe 1 is rejected with instructions to upgrade.

Real iPhones and iPads use AXe 2 for UI, gestures, screenshots and video, and
CoreDevice for app installation and launch. The normal workflow no longer needs
idb or an idb companion. Existing launch recipes retain app arguments and
environment variables on hardware.

MAV adds its own typed API for keyboard sequences/combos, low-level touch,
drawing, slider values, all gesture presets, settings, location scenarios,
status-bar operations, displays, readiness, shutdown and batches. These operations
also work in YAML flows. See [the API and coverage matrix](axe2.md).

AXe coordinates stay in the current orientation. Open-foldable capture and input
use AXe's native display routing. Sent or uncertain input is not automatically
replayed after errors. Video capture supports physical devices; streaming keeps
binary stdout separate from diagnostics.

Agent modes and the replacement of `goto` by `goal` are separate follow-up work.
This release preserves the existing `goto` command and MAV's compact tree mode.

We ran an 11-step simulator flow against AXe 2.0.4 and obtained accepted
MP4 evidence, selected input and recording/stream cancellation without leftover
children. Physical-device discovery and readiness were checked; connected-device
UI/lifecycle testing was unavailable. An observed upstream appearance-refresh
limitation is documented in the API guide.
