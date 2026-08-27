# Canopy Technical Design

Canopy is a local session manager for Claude Code. It organizes sessions by Git project
and worktree, keeps agent PTYs in a background daemon, and restores sessions after the
daemon or TUI is restarted.

## Architecture

```text
┌─────────────┐       Unix socket       ┌──────────────┐
│     TUI     │ ◄─────────────────────► │    Daemon    │
│  Bubble Tea │                          │  background  │
└─────────────┘                          └──────┬───────┘
                                               │
                         ┌─────────────────────┼──────────────────┐
                         │                     │                  │
                   ┌─────▼─────┐       ┌───────▼──────┐    ┌──────▼──────┐
                   │    PTYs   │       │    SQLite     │    │ Claude Code │
                   │  + vt10x  │       │   metadata    │    │    hooks     │
                   └───────────┘       └───────────────┘    └─────────────┘
```

- The daemon owns session processes and PTY streams. It continues running after the TUI
  exits, and accepts multiple local clients over a Unix socket.
- The TUI reads project, worktree, and session metadata from SQLite and uses the daemon
  for attach, input, resize, snapshots, creation, and termination.
- On daemon startup, every non-archived session is recreated. Claude sessions with a
  saved native ID are started with `claude --resume <id>`; sessions without one start
  fresh.
- On daemon startup, registered projects are reconciled against Git's current worktree
  list. Missing worktrees are retained as historical rows but hidden from the active
  tree; reappearing paths reuse their original IDs.
- Canopy session IDs remain stable across restoration, so the TUI and database continue
  referring to the same session row.
- Worktree reconciliation is authoritative: Git determines which worktrees exist, while
  SQLite retains missing rows for session history and possible reappearance.

## Data model

```go
type Project struct {
    ID       string
    RepoPath string
    Name     string
}

type Worktree struct {
    ID        string
    ProjectID string
    RepoPath  string
    Path      string
    Branch    string
    IsMain    bool
}

type Session struct {
    ID           string
    WorktreeID   string
    Kind         string
    Tool         string
    CWD          string
    CLISessionID string // native resume ID, such as Claude's UUID
    Title        string
    State        string
    Archived     bool
    PID          int
    StartedAt    time.Time
}
```

SQLite tables are `projects`, `worktrees`, and `sessions`. Session metadata is persisted
at `~/.local/share/canopy/canopy.db`. Project removal is a soft-unregister; worktrees and
sessions are retained. A Git worktree removed outside Canopy is marked missing on the next
daemon startup. Explicit `canopy worktree remove` also marks it missing rather than deleting
the historical row.

The session state values are:

| Stored state | UI meaning | Typical transition |
| --- | --- | --- |
| `fresh` | Idle | New or restored session |
| `running` | Working | Prompt/tool hook received |
| `needs_input` | Waiting | Claude `Stop` hook received |
| `finished` | Finished | Process exits successfully |
| `terminated` | Terminated | Process exits with an error or is killed |
| `disconnected` | Disconnected | Worktree or session directory is no longer available |

Titles are requested when a session is created in the TUI. A typed title is saved
immediately; an empty title receives a short random `session_` name. Titles can later be
edited with `e`. Claude's first prompt only supplies a title when the title is still empty.

## Claude hook design

Claude's project-local `.claude/settings.local.json` is runtime state and is ignored by
Git. Canopy installs one shared hook per lifecycle event (`PreToolUse`, `PostToolUse`,
`Stop`, and `UserPromptSubmit`).

All Claude processes in one worktree share that settings file, so session identity is
passed through the child process environment as `CANOPY_SESSION_ID`. The hook command
expands that environment variable before invoking `canopy _hook`. This prevents an event
from one Claude process from updating every session in the worktree and prevents the
settings file from growing one entry per session.

Hook payloads also provide Claude's native `session_id`, which Canopy stores as
`CLISessionID` for restoration.

## TUI behavior

- Session rows display the title and a state glyph, without redundant state text.
- Idle sessions use a dim gray dot.
- Working sessions use an orange animated Claude-style star (`✳`, `✽`, `✻`, `✺`).
- Waiting sessions use a coral/red dot.
- Finished and terminated sessions use muted status colors.
- The animation is driven by one shared 250ms timer, so all working rows update together.
- Selected rows retain the blue selection background while preserving the colored glyph.
- Pressing `x` asks for confirmation before killing a session or removing a project/worktree.
- Pressing `w` on a project or worktree prompts for a new branch and optional path. The
  worktree is created from the repository's detected default branch, which can be overridden
  with `--base` on the CLI. The branch name is the primary worktree label in the tree.

## Commands

```text
canopy
canopy project add [path]
canopy project list
canopy worktree list
canopy worktree add <branch> [--base branch] [--path path]
canopy worktree remove <path>
canopy session new [--tool claude] [--cwd path]
canopy session list
canopy session attach <session-id>
canopy session resume <session-id>
canopy session kill <session-id>
canopy daemon start|stop|status
```

## Implementation stack

| Concern | Implementation |
| --- | --- |
| TUI | Bubble Tea and Lip Gloss |
| PTY | `github.com/creack/pty` |
| Terminal rendering | `github.com/hinshun/vt10x` |
| Persistence | `modernc.org/sqlite` |
| IPC | Framed JSON over a Unix socket |
| CLI | Cobra |
| Git integration | Git CLI wrappers |
| Claude integration | Local lifecycle hooks and subprocess environment |

## Repository layout

```text
cmd/canopy/          CLI commands and entry point
internal/daemon/     PTY management and Unix socket server
internal/tui/        Bubble Tea model, tree, rendering, and commands
internal/store/      SQLite persistence
internal/git/        Git project and worktree operations
internal/hooks/      Claude settings merge and cleanup
internal/protocol/   Shared IPC and domain types
docs/plan/           Design notes
```

## Known limitations and future work

- Only Claude Code has lifecycle-hook integration; other tools currently run without
  native status or resume support.

- If Claude has not emitted a hook yet, its native session ID is unknown and restoration
  starts a fresh process.
- A stale or deleted Claude native session ID should eventually fall back automatically
  to a fresh session instead of marking restoration as failed.
- Project removal is a reversible soft-unregister. It hides the project without deleting
  its repository, worktrees, or sessions; registering the same repository restores it.
