# Changelog

All notable changes to Canopy are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and releases use [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Contextual schedule actions and transient footer confirmations.

### Fixed

- Keep restored Claude sessions correctly sized after a rebuild or terminal resize.
- Copy schedule output from the selected schedule’s latest run.
- Avoid repeating the `PROJECTS` section heading for each worktree.

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

[Unreleased]: https://github.com/sharathk-dev/canopy/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/sharathk-dev/canopy/releases/tag/v0.1.0
