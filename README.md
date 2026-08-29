# Canopy

![Canopy logo](assets/logo.png)

Canopy is a local session manager for AI coding agents. It organizes Claude sessions by Git repository and worktree, keeps them running in a background daemon, and restores them after a restart.

![Canopy screenshot](assets/screenshot.png)

## Install

On macOS or Linux, for amd64 or arm64:

```bash
curl -fsSL https://raw.githubusercontent.com/sharathk-dev/canopy/master/install.sh | bash
```

The installer places `canopy` in `~/.local/bin`. Open a new terminal, or source the shell profile shown by the installer.

The installer uses published GitHub Release binaries. To install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/sharathk-dev/canopy/master/install.sh | VERSION=v0.1.0-beta.1 bash
```

## Quick start

Register a Git repository and open Canopy:

```bash
cd /path/to/project
canopy project add
canopy
```

Inside the TUI:

| Key | Action |
| --- | --- |
| `n` | Name and start a Claude session in the selected worktree; blank uses a random `session_` name |
| `Enter` | Expand a project/worktree or attach to a session |
| `Tab` | Switch between the project tree and output panel |
| `w` | Create a new branch and worktree |
| `x` | Remove the selected project/worktree or terminate a session |
| `Ctrl+Q` | Leave an attached session and return to the tree |
| `q` | Quit the TUI |

Quitting the TUI does not intentionally terminate sessions. Relaunching `canopy` restores non-archived sessions. Claude sessions are resumed with their native session ID when one has been captured.

## CLI commands

```bash
canopy update       # update canopy itself
canopy project      # register and list git repositories
canopy worktree     # list, add, and remove git worktrees
canopy session      # start, list, attach to, resume, and kill agent sessions
canopy daemon       # start, stop, and check the background daemon
canopy schedule     # manage recurring skills and commands
canopy diagnostics  # write a safe diagnostic snapshot for bug reports
```

Run `canopy <command> --help` for subcommands and flags.

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/sharathk-dev/canopy/master/uninstall.sh | bash
```

## Build from source

Requires Go 1.21+. See the [Makefile](Makefile) for build, test, and dev targets.

For implementation details, persistence, hooks, recovery behavior, and known limitations,
see the [technical design](docs/plan/spec.md). See the [changelog](CHANGELOG.md) for release notes.

## License

Canopy is licensed under the MIT License. See [LICENSE](LICENSE).
