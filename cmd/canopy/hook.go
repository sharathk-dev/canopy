package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/sharathk-dev/canopy/internal/datadir"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:    "_hook",
	Hidden: true,
	Short:  "Internal: handle a Claude lifecycle hook event",
	RunE:   runHook,
}

var (
	flagHookSession string
	flagHookToken   string
	flagHookEvent   string
)

func init() {
	hookCmd.Flags().StringVar(&flagHookSession, "session", "", "Session ID")
	hookCmd.Flags().StringVar(&flagHookToken, "token", "", "Hook token")
	hookCmd.Flags().StringVar(&flagHookEvent, "event", "", "Hook event type")
	rootCmd.AddCommand(hookCmd)
}

func runHook(_ *cobra.Command, _ []string) error {
	if flagHookSession == "" || flagHookEvent == "" {
		return nil
	}

	stdin, _ := io.ReadAll(os.Stdin)
	var data json.RawMessage
	if len(stdin) > 0 && json.Valid(stdin) {
		data = stdin
	}

	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return nil // best-effort, silent
	}
	defer db.Close()

	sess, err := db.GetSession(flagHookSession)
	if err != nil {
		return nil
	}

	changed := false

	if flagHookEvent == "UserPromptSubmit" && !sess.TitleLocked && sess.Title == "" {
		if title := extractHookTitle(data); title != "" {
			sess.Title = title
			changed = true
		}
	}

	if newState := hookStateTransition(flagHookEvent); newState != "" && sess.State != newState {
		sess.State = newState
		changed = true
	}

	if changed {
		_ = db.UpdateSession(sess)
	}
	return nil
}

func hookStateTransition(event string) string {
	switch event {
	case "PreToolUse", "PostToolUse", "UserPromptSubmit":
		return protocol.StateRunning
	case "Stop":
		return protocol.StateNeedsInput
	default:
		return ""
	}
}

func extractHookTitle(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var payload struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Prompt == "" {
		return ""
	}
	return hookTruncate(strings.TrimSpace(payload.Prompt), 60)
}

func hookTruncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
