package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:    "_hook",
	Hidden: true,
	Short:  "Internal: relay a Claude lifecycle hook event to the daemon",
	RunE:   runHook,
}

var (
	flagHookSession string
	flagHookToken   string
	flagHookEvent   string
)

func init() {
	hookCmd.Flags().StringVar(&flagHookSession, "session", "", "Session ID")
	hookCmd.Flags().StringVar(&flagHookToken, "token", "", "Hook bearer token")
	hookCmd.Flags().StringVar(&flagHookEvent, "event", "", "Hook event type")
	rootCmd.AddCommand(hookCmd)
}

func runHook(_ *cobra.Command, _ []string) error {
	if flagHookSession == "" || flagHookToken == "" || flagHookEvent == "" {
		return fmt.Errorf("--session, --token, and --event are required")
	}

	// Read hook payload from stdin (Claude passes event data as JSON).
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	var data json.RawMessage
	if len(stdin) > 0 && json.Valid(stdin) {
		data = json.RawMessage(stdin)
	}

	params := protocol.HookEventParams{
		SessionID: flagHookSession,
		Token:     flagHookToken,
		EventType: flagHookEvent,
		Data:      data,
	}
	raw, _ := json.Marshal(params)
	cmd := protocol.Cmd{Type: protocol.CmdHookEvent, Payload: raw}

	// Best-effort: if the daemon isn't running just exit cleanly.
	resp, err := sendCmd(cmd)
	if err != nil {
		// Don't surface errors to Claude — hook failures should be silent.
		return nil
	}
	_ = resp
	return nil
}
