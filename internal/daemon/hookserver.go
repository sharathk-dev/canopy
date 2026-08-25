package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/sharathk-dev/canopy/internal/protocol"
)

// handleHookEvent processes a hook event sent by `canopy _hook`.
func (d *Daemon) handleHookEvent(conn net.Conn, raw json.RawMessage) {
	var params protocol.HookEventParams
	if err := json.Unmarshal(raw, &params); err != nil {
		d.sendErr(conn, "invalid hook_event params")
		return
	}

	// Validate token → session.
	d.mu.RLock()
	sessionID, ok := d.tokens[params.Token]
	d.mu.RUnlock()
	if !ok || sessionID != params.SessionID {
		d.sendErr(conn, "invalid hook token")
		return
	}

	sess, err := d.db.GetSession(params.SessionID)
	if err != nil {
		d.sendErr(conn, fmt.Sprintf("session not found: %v", err))
		return
	}

	changed := false

	// Auto-title from first user prompt.
	if params.EventType == protocol.HookUserPromptSubmit && !sess.TitleLocked && sess.Title == "" {
		if title := extractTitle(params.Data); title != "" {
			sess.Title = title
			changed = true
		}
	}

	// State transition.
	if newState := hookStateTransition(params.EventType); newState != "" && sess.State != newState {
		sess.State = newState
		changed = true
	}

	if changed {
		if err := d.db.UpdateSession(sess); err != nil {
			log.Printf("hook: update session %s: %v", params.SessionID, err)
		}
	}

	d.sendOK(conn, map[string]string{"state": sess.State, "title": sess.Title})
}

// handleUpdateTitle handles a manual rename request (sets TitleLocked = true).
func (d *Daemon) handleUpdateTitle(conn net.Conn, raw json.RawMessage) {
	var params protocol.UpdateTitleParams
	if err := json.Unmarshal(raw, &params); err != nil || params.SessionID == "" {
		d.sendErr(conn, "invalid update_title params")
		return
	}

	sess, err := d.db.GetSession(params.SessionID)
	if err != nil {
		d.sendErr(conn, fmt.Sprintf("session not found: %v", err))
		return
	}

	sess.Title = strings.TrimSpace(params.Title)
	sess.TitleLocked = true
	if err := d.db.UpdateSession(sess); err != nil {
		d.sendErr(conn, fmt.Sprintf("update session: %v", err))
		return
	}
	d.sendOK(conn, sess)
}

// hookStateTransition maps a Claude hook event type to a session state.
// Returns "" if no state change is warranted.
func hookStateTransition(eventType string) string {
	switch eventType {
	case protocol.HookPreToolUse, protocol.HookPostToolUse, protocol.HookUserPromptSubmit:
		return protocol.StateRunning
	case protocol.HookStop:
		return protocol.StateNeedsInput
	default:
		return ""
	}
}

// extractTitle derives a short title from a UserPromptSubmit payload.
// Claude passes {"prompt": "..."} on stdin.
func extractTitle(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var payload struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Prompt == "" {
		return ""
	}
	return truncate(strings.TrimSpace(payload.Prompt), 60)
}

// truncate cuts s to at most n runes, appending "…" if truncated.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + "…"
}
