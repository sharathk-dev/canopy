package protocol

import "encoding/json"

// Command type constants.
const (
	CmdNewSession     = "new_session"
	CmdAttach         = "attach"
	CmdDetach         = "detach"
	CmdListSessions   = "list_sessions"
	CmdKillSession    = "kill_session"
	CmdListWorktrees  = "list_worktrees"
	CmdAddWorktree    = "add_worktree"
	CmdRemoveWorktree = "remove_worktree"
)

// Cmd is a client→daemon command envelope.
type Cmd struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Response is a daemon→client response envelope.
type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// ResizePayload is carried in FrameResize frames.
type ResizePayload struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// --- per-command payload types ---

type NewSessionParams struct {
	WorktreeID string `json:"worktree_id"` // may be empty
	Tool       string `json:"tool"`        // "claude" | "codex" | ""
	CWD        string `json:"cwd"`
}

type AttachParams struct {
	SessionID string `json:"session_id"`
}

type DetachParams struct {
	SessionID string `json:"session_id"`
}

type KillSessionParams struct {
	SessionID string `json:"session_id"`
}

type AddWorktreeParams struct {
	RepoPath string `json:"repo_path"`
	Path     string `json:"path"`
	Branch   string `json:"branch"`
}

type RemoveWorktreeParams struct {
	RepoPath string `json:"repo_path"`
	Path     string `json:"path"`
	Force    bool   `json:"force"`
}
