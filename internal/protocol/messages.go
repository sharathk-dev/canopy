package protocol

import "encoding/json"

// Command type constants.
const (
	CmdVersion         = "version"
	CmdNewSession      = "new_session"
	CmdAttach          = "attach"
	CmdKillSession     = "kill_session"
	CmdInput           = "input"
	CmdUpdateTitle     = "update_title"
	CmdSessionSnapshot = "session_snapshot"
	CmdResizeSession   = "resize_session"
	CmdRunSchedule     = "run_schedule"
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
	Title        string `json:"title,omitempty"`
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

type KillSessionParams struct {
	SessionID string `json:"session_id"`
}

// Claude hook event types.
const (
	HookPreToolUse       = "PreToolUse"
	HookPostToolUse      = "PostToolUse"
	HookStop             = "Stop"
	HookUserPromptSubmit = "UserPromptSubmit"
)

type VersionResponse struct {
	BinaryMtime int64 `json:"binary_mtime"` // Nanosecond mtime of the daemon binary
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

type RunScheduleParams struct {
	ScheduleID string `json:"schedule_id"`
}
