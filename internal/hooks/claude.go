package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const settingsFile = ".claude/settings.local.json"

// hookMarker is a prefix embedded in every canopy-managed hook command so they
// can be identified and replaced without touching user-authored hooks.
const hookMarker = "canopy _hook "

// ClaudeInjector injects lifecycle hooks into .claude/settings.local.json so
// Claude Code reports session state transitions to the canopy daemon.
type ClaudeInjector struct {
	SocketPath string // daemon unix socket path (for canopy _hook to dial)
}

func (ci ClaudeInjector) Inject(sessionID, cwd string) error {
	path := filepath.Join(cwd, settingsFile)
	settings, err := readSettings(path)
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}

	if settings["hooks"] == nil {
		settings["hooks"] = map[string]any{}
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		// Unexpected type — don't clobber.
		return fmt.Errorf("unexpected hooks type in %s", path)
	}

	for _, event := range []string{"PreToolUse", "PostToolUse", "Stop", "UserPromptSubmit"} {
		cmd := fmt.Sprintf(
			"canopy _hook --session=$CANOPY_SESSION_ID --event=%s",
			event,
		)
		hooks[event] = mergeHookEntry(hooks[event], cmd)
	}

	settings["hooks"] = hooks
	return writeSettings(path, settings)
}

// RemoveFromCWD removes canopy-managed hooks for sessionID from cwd's settings file.
func (ci ClaudeInjector) RemoveFromCWD(sessionID, cwd string) error {
	path := filepath.Join(cwd, settingsFile)
	settings, err := readSettings(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return nil
	}

	changed := false
	for event, val := range hooks {
		var eventChanged bool
		hooks[event], eventChanged = removeHookEntry(val, sessionID)
		changed = changed || eventChanged
	}
	if !changed {
		return nil
	}
	settings["hooks"] = hooks
	return writeSettings(path, settings)
}

// --- settings.local.json helpers ---

func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// Claude settings hook entry shape:
// [ { "hooks": [ { "type": "command", "command": "..." } ] }, ... ]
//
// mergeHookEntry ensures exactly one shared canopy entry exists in the list.
// The session identity is supplied by the Claude process environment.
func mergeHookEntry(existing any, cmd string) any {
	entries := toSlice(existing)

	// Remove old/shared canopy entries before adding the current one.
	var kept []any
	for _, e := range entries {
		if isCanopyEntry(e) {
			continue
		}
		kept = append(kept, e)
	}

	// Append the new canopy entry.
	kept = append(kept, map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": cmd},
		},
	})
	return kept
}

func removeHookEntry(existing any, sessionID string) (any, bool) {
	entries := toSlice(existing)
	var kept []any
	changed := false
	for _, e := range entries {
		if isCanopyEntryForSession(e, sessionID) {
			changed = true
			continue
		}
		kept = append(kept, e)
	}
	return kept, changed
}

func isCanopyEntry(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooks, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range hooks {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := hm["command"].(string)
		if strings.HasPrefix(cmd, hookMarker) {
			return true
		}
	}
	return false
}

// isCanopyEntryForSession identifies legacy per-session entries. Shared
// entries intentionally remain when one session exits because other sessions
// in the same worktree still use them.
func isCanopyEntryForSession(entry any, sessionID string) bool {
	if !isCanopyEntry(entry) {
		return false
	}
	m := entry.(map[string]any)
	for _, h := range m["hooks"].([]any) {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := hm["command"].(string)
		if strings.Contains(cmd, "--session="+sessionID) {
			return true
		}
	}
	return false
}

func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	s, ok := v.([]any)
	if !ok {
		return nil
	}
	return s
}
