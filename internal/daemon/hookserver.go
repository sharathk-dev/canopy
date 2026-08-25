package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"net"

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

	newState := hookStateTransition(params.EventType, params.Data)
	if newState == "" {
		d.sendOK(conn, nil)
		return
	}

	sess, err := d.db.GetSession(params.SessionID)
	if err != nil {
		d.sendErr(conn, fmt.Sprintf("session not found: %v", err))
		return
	}
	if sess.State == newState {
		d.sendOK(conn, nil)
		return
	}

	sess.State = newState
	if err := d.db.UpdateSession(sess); err != nil {
		log.Printf("hook: update session %s state: %v", params.SessionID, err)
	}

	d.sendOK(conn, map[string]string{"state": newState})
}

// hookStateTransition maps a Claude hook event type to a session state.
// Returns "" if no state change is warranted.
func hookStateTransition(eventType string, data json.RawMessage) string {
	switch eventType {
	case protocol.HookPreToolUse, protocol.HookUserPromptSubmit:
		return protocol.StateRunning

	case protocol.HookPostToolUse:
		// Agent finished a tool call but may issue more; stay running.
		return protocol.StateRunning

	case protocol.HookStop:
		// Parse stop reason if available.
		var payload struct {
			StopReason string `json:"stop_reason"`
		}
		if len(data) > 0 {
			json.Unmarshal(data, &payload) //nolint:errcheck
		}
		switch payload.StopReason {
		case "end_turn", "max_tokens", "":
			// Agent finished its turn — waiting for the user's next prompt.
			return protocol.StateNeedsInput
		default:
			return protocol.StateNeedsInput
		}

	default:
		return ""
	}
}
