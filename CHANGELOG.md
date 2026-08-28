# Changelog

All notable changes to Canopy are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

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

[Unreleased]: https://github.com/sharathk-dev/canopy/compare/v0.1.0-beta.1...HEAD
[0.1.0-beta.1]: https://github.com/sharathk-dev/canopy/compare/v0.1.0...v0.1.0-beta.1
[0.1.0]: https://github.com/sharathk-dev/canopy/releases/tag/v0.1.0
