# Changelog

All notable changes to Canopy are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [1.0.2] - 2026-09-01

### Fixed

- Schedule modal: the project and skill picker dropdowns hard-capped
  rendering at the first 5 items with no scroll offset, so moving the
  cursor past index 4 kept navigating correctly but the highlighted row
  went off-screen and became invisible. Both lists now scroll to follow
  the cursor.

## [1.0.1] - 2026-09-01

### Fixed

- Project picker: typing `j` or `k` while filtering moved the selection
  cursor instead of appending to the filter text, making paths containing
  those letters unfilterable.
- Theme rendering was broken regardless of the selected theme (`dark` or
  `light`) on terminals where Lip Gloss's auto-detected color profile
  produced no usable color output, notably iTerm2 and Terminal.app on
  macOS. The color profile is now forced to ANSI256 instead of relying on
  detection, and the canvas-background patching now emits codes for that
  same forced profile instead of raw 24-bit truecolor.
- Removed the `system` theme auto-detection option (`lipgloss.HasDarkBackground`
  via an OSC 11 terminal query), which was unreliable and could race with
  Bubble Tea's own stdin reader. The theme now defaults to `dark`; existing
  configs with a saved `system` value fall back to `dark`.

## [1.0.0] - 2026-08-29

### Changed

- First stable release. No functional changes since 0.1.0-beta.4 beyond a
  small project-row selection UX tweak and documentation cleanup.

## [0.1.0-beta.4] - 2026-08-28

### Added

- `canopy update` command to self-update to the latest published release.
- `canopy diagnostics` command to write a safe diagnostic snapshot for bug reports.
- Benchmark suite for the TUI and daemon with debug telemetry.

### Changed

- Scheduler execution safeguards to prevent duplicate or runaway runs.
- Refined workspace search header and session rendering/theming.
- Avoid redundant PTY snapshot refreshes when output is unchanged.

### Fixed

- Recover automatically from stale Claude native session IDs instead of failing restoration.

## [0.1.0-beta.3] - 2026-08-28

### Changed

- Improved schedule and form modals.

## [0.1.0-beta.2] - 2026-08-28

### Fixed

- Release workflow now checks out the repo before publishing, fixing a broken release job.

## [0.1.0-beta.1] - 2026-08-28

### Added

- Theme system with system, dark, and light options; persisted in settings table.
- Context-aware footer shortcuts that change based on selected item type.
- Telescope-style project picker modal with fuzzy directory filtering.
- Scheduler worker pool with bounded queue and per-schedule dedup via sync.Map.
- Shared debug logger (`--debug` flag) covering both TUI and daemon/scheduler events.
- Worktree management from the TUI.
- Uninstall script.

### Changed

- Renamed `max_concurrency` → `max_scheduler_concurrency` and `max_queue_size` → `max_scheduler_queue_size`.
- New projects and worktrees auto-expand in the tree on first appearance.
- `PROJECTS` and `SCHEDULES` section headings now appear exactly once.
- `make install` uses `go install` to always resolve the correct binary location.
- Added `make dev` target: build + restart daemon in one step.
- Switched license from GPL-3.0 to MIT.

### Fixed

- Contextual schedule actions and transient footer confirmations.
- Keep restored Claude sessions correctly sized after a rebuild or terminal resize.
- Copy schedule output from the selected schedule's latest run.
- Panel borders use lipgloss holistic rendering — no misalignment with raw PTY output.
- Selected row blue background preserved when applying bold (raw ANSI, no full reset).

## [0.1.0] - 2026-08-28

### Added

- Persistent Claude sessions restored across TUI and daemon restarts.
- Project and Git worktree management from the TUI and CLI.
- Session titles, state glyphs, contextual help, and confirmation prompts.
- Scheduled Claude skills and shell commands with cron expressions.
- Schedule run history with Markdown output and Claude token usage.
- macOS LaunchAgent and Linux systemd user-service installation.
- Cross-platform clipboard copying for schedule output.
- SQLite persistence with worktree reconciliation and concurrent access support.

### Documentation

- Added installation, build, licensing, and technical design documentation.

[Unreleased]: https://github.com/sharathk-dev/canopy/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/sharathk-dev/canopy/compare/v0.1.0-beta.4...v1.0.0
[0.1.0-beta.4]: https://github.com/sharathk-dev/canopy/compare/v0.1.0-beta.3...v0.1.0-beta.4
[0.1.0-beta.3]: https://github.com/sharathk-dev/canopy/compare/v0.1.0-beta.2...v0.1.0-beta.3
[0.1.0-beta.2]: https://github.com/sharathk-dev/canopy/compare/v0.1.0-beta.1...v0.1.0-beta.2
[0.1.0-beta.1]: https://github.com/sharathk-dev/canopy/compare/v0.1.0...v0.1.0-beta.1
[0.1.0]: https://github.com/sharathk-dev/canopy/releases/tag/v0.1.0
