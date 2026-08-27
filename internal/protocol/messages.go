package protocol

import "encoding/json"

// Command type constants.
const (
	CmdVersion         = "version"
	CmdNewSession      = "new_session"
	CmdAttach          = "attach"
	CmdDetach          = "detach"
	CmdListSessions    = "list_sessions"
	CmdKillSession     = "kill_session"
	CmdInput           = "input"
	CmdRegisterProject = "register_project"
	CmdListProjects    = "list_projects"
	CmdHookEvent       = "hook_event"
	CmdUpdateTitle     = "update_title"
	CmdSessionSnapshot = "session_snapshot"
	CmdResizeSession   = "resize_session"
	CmdListWorktrees   = "list_worktrees"
	CmdAddWorktree     = "add_worktree"
	CmdRemoveWorktree  = "remove_worktree"
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
	// CLISessionID requests resuming a tool-native session (currently Claude).
	CLISessionID string `json:"cli_session_id,omitempty"`
	Rows         uint16 `json:"rows"` // 0 = use default
	Cols         uint16 `json:"cols"` // 0 = use default
}

type ResizeSessionParams struct {
	SessionID string `json:"session_id"`
	Rows      uint16 `json:"rows"`
	Cols      uint16 `json:"cols"`
}

type InputParams struct {
	SessionID string `json:"session_id"`
	Data      []byte `json:"data"`
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

// HookEventParams is sent by `canopy _hook` when Claude fires a lifecycle hook.
type HookEventParams struct {
	SessionID string          `json:"session_id"`
	Token     string          `json:"token"`
	EventType string          `json:"event_type"` // PreToolUse | PostToolUse | Stop | UserPromptSubmit
	Data      json.RawMessage `json:"data,omitempty"`
}

// Claude hook event types.
const (
	HookPreToolUse       = "PreToolUse"
	HookPostToolUse      = "PostToolUse"
	HookStop             = "Stop"
	HookUserPromptSubmit = "UserPromptSubmit"
)

type VersionResponse struct {
	BinaryMtime int64 `json:"binary_mtime"` // Unix timestamp of the daemon binary
}

type SnapshotParams struct {
	SessionID string `json:"session_id"`
}

type SnapshotResponse struct {
	Text string `json:"text"`
}

type UpdateTitleParams struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

type RegisterProjectParams struct {
	// RepoPath is the absolute path to the git repository root.
	// If empty the daemon resolves it via git rev-parse on Dir.
	RepoPath string `json:"repo_path"`
	Name     string `json:"name"` // optional; defaults to basename of RepoPath
}

type AddWorktreeParams struct {
	RepoPath string `json:"repo_path"` // resolved by daemon if empty
	Branch   string `json:"branch"`
	Path     string `json:"path"` // optional; daemon picks a sibling dir if empty
}

type RemoveWorktreeParams struct {
	RepoPath string `json:"repo_path"`
	Path     string `json:"path"`
	Force    bool   `json:"force"`
}
