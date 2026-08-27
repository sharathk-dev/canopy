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
VERSION=v0.1.0 curl -fsSL https://raw.githubusercontent.com/sharathk-dev/canopy/master/install.sh | bash
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
| `n` | Start a Claude session in the selected worktree |
| `Enter` | Expand a project/worktree or attach to a session |
| `Tab` | Switch between the project tree and output panel |
| `x` | Request session termination; press `y`/Enter to confirm |
| `Ctrl+Q` | Leave an attached session and return to the tree |
| `q` | Quit the TUI |

Quitting the TUI does not intentionally terminate sessions. Relaunching `canopy` restores non-archived sessions. Claude sessions are resumed with their native session ID when one has been captured.

## CLI commands

```bash
canopy project add [path]
canopy project list
canopy worktree list
canopy worktree add <branch>
canopy worktree remove <path>
canopy session new [--tool claude] [--cwd path]
canopy session list
canopy session attach <session-id>
canopy session resume <session-id>
canopy session kill <session-id>
canopy daemon status
canopy daemon start
canopy daemon stop
```

`session kill` asks for confirmation before terminating a session.

## Persistence

Canopy stores its SQLite database at:

```text
~/.local/share/canopy/canopy.db
```

The database contains `projects`, `worktrees`, and `sessions` tables. You can inspect it with:

```bash
sqlite3 ~/.local/share/canopy/canopy.db
```

Claude hook configuration is written to `.claude/settings.local.json` in each session's working directory. This file is local runtime state and should not be committed.

## Build from source

Requires Go:

```bash
go test ./...
go build -o canopy ./cmd/canopy
```

To install a locally built binary:

```bash
INSTALL_DIR="$HOME/.local/bin" go build -o "$INSTALL_DIR/canopy" ./cmd/canopy
```

## Architecture

The TUI is a client. A background daemon owns the PTYs and communicates with clients over a Unix socket. SQLite stores session metadata, including Claude's native resume ID. This lets the daemon recreate sessions after a daemon or TUI restart.

## License

Canopy is licensed under the GNU General Public License v3.0 or later. See [LICENSE](LICENSE).
