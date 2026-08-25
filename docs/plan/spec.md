# Technical Design — Agent Session Manager (Go)

Companion doc to `SPEC.md`. Read that first for the product model; this covers how it's
built.

## 1. Architecture

```
┌─────────────┐     unix socket      ┌──────────────┐
│  TUI client  │ ◄──────────────────► │    Daemon     │
│ (bubbletea)  │                      │  (background) │
└─────────────┘                      └──────┬───────┘
                                             │
                    ┌────────────────────────┼────────────────────────┐
                    │                         │                        │
              ┌─────▼─────┐           ┌───────▼──────┐         ┌──────▼──────┐
              │  PTY pool  │           │   SQLite DB   │         │ HTTP status │
              │ (sessions) │           │ (persistence) │         │  endpoint   │
              └────────────┘           └───────────────┘         └─────────────┘
```

- The **daemon** is a long-running background process that owns every session's PTY. It
  keeps running independent of whether any client is attached, so closing a terminal or
  losing an SSH connection never kills a session.
- The **TUI client** is just a viewer/controller — it connects to the daemon over a local
  unix socket, renders state, and forwards input. Multiple clients can in principle
  attach to the same daemon.
- **Sessions persist across daemon restarts** where the underlying agent CLI supports
  resumption (e.g. `claude --resume <id>`), by storing the CLI-native session id and
  re-invoking it automatically on reattach.

## 2. Data model (Go types)

```go
type Project struct {
    ID       string
    RepoPath string // derived via `git rev-parse --show-toplevel`, cached
    Name     string // defaults to repo dir name, editable
}

type Worktree struct {
    ID       string
    RepoPath string // groups worktrees under the same repo across Projects, if ever needed
    Path     string
    Branch   string
    IsMain   bool // true for the original clone, not a real `git worktree add` entry
}

type Session struct {
    ID          string
    WorktreeID  string
    Kind        string // "agent" | "shell"
    Tool        string // "claude" | "codex" | "" for shell
    CWD         string // worktree.Path, or a module subpath chosen at creation time
    CLISessionID string // native resume id, e.g. claude's session uuid
    Title       string // auto-generated after first prompt, user-renameable
    TitleLocked bool   // true once user manually renames — agent won't overwrite
    State       string // fresh | running | finished | needs_input | terminated | disconnected
    Archived    bool
    TmuxOrPTYRef string // socket/pty handle managed by the daemon
    StartedAt   time.Time
}
```

Note: there is deliberately no `Module` type. A module-scoped effort is just a `Session`
whose `CWD` is a subpath of its worktree — see SPEC §3.

## 3. Stack

| Concern | Library / approach | Notes |
|---|---|---|
| TUI framework | `github.com/charmbracelet/bubbletea` | Elm-style model/update/view; handles the event loop |
| TUI styling | `github.com/charmbracelet/lipgloss` | Layout, colors, borders for the panel-based UI |
| TUI components | `github.com/charmbracelet/bubbles` | Pre-built lists, viewports, text inputs — avoids reinventing list navigation |
| PTY spawning | `github.com/creack/pty` | Standard, well-maintained Go PTY library |
| Terminal emulation (scrollback, rendering agent output) | `github.com/hinshun/vt10x` (or equivalent) | Needed to parse raw PTY output into a renderable buffer with correct scrollback. **This is the single hardest technical piece of the whole project** — alt-screen apps, resize handling, and scroll-region edge cases. Validate early with real agent CLI output, not synthetic test data. Check the library's maintenance status before committing; budget it as its own multi-day milestone. |
| Persistence | `modernc.org/sqlite` (pure Go, no cgo) | Preferred over `mattn/go-sqlite3` (cgo) — write volume here is low (session metadata, not high-frequency), so cgo's speed edge doesn't matter, and pure-Go keeps cross-compilation/static binaries trivial. |
| Concurrency | Goroutines + channels (stdlib) | No external async runtime needed — each session is a goroutine reading/writing its PTY and pushing bytes over a channel to attached clients |
| Daemon ↔ client IPC | `net.Listen("unix", ...)` (stdlib) | JSON or `encoding/gob` framed messages over the socket; no need for gRPC at this scale |
| Local status/hook HTTP endpoint | `net/http` (stdlib) | Loopback-only, authenticated with a per-session bearer token injected into that session's env |
| CLI command parsing | `github.com/spf13/cobra` | De facto standard for Go CLIs |
| XDG-correct config/data paths | `github.com/adrg/xdg` | Cross-platform `~/.local/share`, `~/.config` equivalents |
| Git worktree operations | Shell out via `os/exec` to `git worktree add/remove/list --porcelain` | No need for a git library — just wrap the CLI directly |
| Hook config merging (`.claude/settings.local.json` etc.) | `encoding/json` (stdlib) | Read-merge-write; tag managed entries so user-authored hooks are never clobbered |

## 4. Project layout

```
cmd/tool/              # main binary entrypoint, cobra command definitions
internal/daemon/        # PTY management, session registry, unix socket server, status engine
internal/tui/            # bubbletea model/update/view
internal/store/          # SQLite persistence layer
internal/git/             # worktree add/remove/list wrappers via os/exec, repo-path resolution
internal/hooks/           # hook config merge logic per agent CLI
internal/protocol/        # shared types/messages between daemon and client
```

## 5. Key behaviors to implement correctly

- **Repo path resolution.** `RepoPath` on Project is derived once via
  `git rev-parse --show-toplevel` and cached — never asked of the user, never
  recomputed on every render. It exists purely so the daemon can group worktree lookups
  per repo instead of shelling out per-Project on every refresh.
- **Reconciliation on startup (and periodically).** The daemon's view of worktrees can
  drift from reality if the user runs `git worktree add/remove` by hand outside the
  tool. On daemon start, and on a timer, reconcile the DB against
  `git worktree list --porcelain` per registered repo — add missing entries, flag
  entries whose directory no longer exists.
- **Destructive worktree deletion.** Before `git worktree remove`, run
  `git -C <path> status --porcelain`; if non-empty, block with a warning and require
  `--force` or typed confirmation rather than silently discarding uncommitted work.
- **Status rollup.** Worktree and Project level status is computed, not stored — take
  the most-urgent state across child Sessions (`needs_input` > `running` > `terminated` >
  `disconnected` > `finished` > `fresh`) each time it's rendered.
- **Session resumption fallback.** If a stored `CLISessionID` is no longer valid (expired
  or deleted upstream), fall back to spawning a fresh session rather than erroring the
  whole reattach flow.

## 6. Build-order recommendation

1. **Daemon + PTY management** for a single agent CLI, with basic attach/detach over the
   unix socket. Prove persistence survives client disconnect before anything else.
2. **Worktree layer** — wrap `git worktree add/remove/list`, list worktrees per project,
   spawn sessions scoped to a worktree's directory (or module subpath).
3. **Status tracking via CLI hooks** — start with Claude Code (richest hook support),
   wire up the loopback HTTP endpoint and hook config injection.
4. **Self-titling** — session auto-renames itself from its first prompt via a one-shot
   hook-injected instruction; respects `TitleLocked`.
5. **Multi-CLI support** — extend to Codex/Cursor, handling each one's quirks (e.g.
   Cursor's lack of context-injecting hooks needs the project-rule fallback from SPEC
   §4.2).
6. **TUI polish** — status roll-up coloring, session archiving, remote SSH support,
   workspaces/grouping.

## 7. Known hard parts (plan real time for these)

- **Terminal emulation correctness** — scrollback, alt-screen switching, resize
  propagation. Hardest piece regardless of stack.
- **Hook injection without clobbering user config** — careful read-merge-write logic,
  clear tagging of tool-managed vs. user-authored entries.
- **Session resumption fallback** — handle a stale/invalid CLI session id gracefully.
- **Reconciliation correctness** — the DB must never be treated as more authoritative
  than `git worktree list` itself.
