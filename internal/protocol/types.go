package protocol

import "time"

// Project maps to a git repository.
type Project struct {
	ID       string
	RepoPath string
	Name     string
}

// Worktree is a git worktree under a project.
type Worktree struct {
	ID       string
	RepoPath string
	Path     string
	Branch   string
	IsMain   bool
}

// Session is one agent or shell process managed by the daemon.
type Session struct {
	ID           string
	WorktreeID   string
	Kind         string // "agent" | "shell"
	Tool         string // "claude" | "codex" | ""
	CWD          string
	CLISessionID string
	Title        string
	TitleLocked  bool
	State        string
	Archived     bool
	TmuxOrPTYRef string
	StartedAt    time.Time
}

// Session state constants in descending priority order.
const (
	StateNeedsInput   = "needs_input"
	StateRunning      = "running"
	StateTerminated   = "terminated"
	StateDisconnected = "disconnected"
	StateFinished     = "finished"
	StateFresh        = "fresh"
)

// StatusPriority returns a number for rollup logic; higher = more urgent.
func StatusPriority(state string) int {
	switch state {
	case StateNeedsInput:
		return 5
	case StateRunning:
		return 4
	case StateTerminated:
		return 3
	case StateDisconnected:
		return 2
	case StateFinished:
		return 1
	default:
		return 0
	}
}
