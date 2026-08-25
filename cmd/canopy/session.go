package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/sharathk-dev/canopy/internal/daemon"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage agent sessions",
}

var (
	flagTool string
	flagCWD  string
)

var sessionNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new agent session",
	RunE:  runSessionNew,
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active sessions",
	RunE:  runSessionList,
}

var sessionAttachCmd = &cobra.Command{
	Use:   "attach <session-id>",
	Short: "Attach to a running session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionAttach,
}

var sessionRenameCmd = &cobra.Command{
	Use:   "rename <session-id> <title>",
	Short: "Rename a session (locks the title against auto-update)",
	Args:  cobra.ExactArgs(2),
	RunE:  runSessionRename,
}

var sessionKillCmd = &cobra.Command{
	Use:   "kill <session-id>",
	Short: "Kill a running session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionKill,
}

func init() {
	sessionNewCmd.Flags().StringVar(&flagTool, "tool", "claude", "Agent tool: claude, codex, or '' for shell")
	sessionNewCmd.Flags().StringVar(&flagCWD, "cwd", "", "Working directory (default: current directory)")

	sessionCmd.AddCommand(sessionNewCmd)
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionAttachCmd)
	sessionCmd.AddCommand(sessionRenameCmd)
	sessionCmd.AddCommand(sessionKillCmd)
}

func runSessionNew(_ *cobra.Command, _ []string) error {
	cwd := flagCWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	params := protocol.NewSessionParams{Tool: flagTool, CWD: cwd}
	raw, _ := json.Marshal(params)
	cmd := protocol.Cmd{Type: protocol.CmdNewSession, Payload: raw}

	resp, err := sendCmd(cmd)
	if err != nil {
		return err
	}

	var sess protocol.Session
	if err := json.Unmarshal(resp.Data, &sess); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("session created: %s\n", sess.ID)
	fmt.Printf("  tool: %s\n  cwd:  %s\n", sess.Tool, sess.CWD)
	return nil
}

func runSessionList(_ *cobra.Command, _ []string) error {
	cmd := protocol.Cmd{Type: protocol.CmdListSessions}
	resp, err := sendCmd(cmd)
	if err != nil {
		return err
	}

	var sessions []protocol.Session
	if err := json.Unmarshal(resp.Data, &sessions); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("no active sessions")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTOOL\tSTATE\tTITLE\tSTARTED")
	for _, s := range sessions {
		age := time.Since(s.StartedAt).Round(time.Second)
		title := s.Title
		if title == "" {
			title = s.CWD
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s ago\n", s.ID, s.Tool, s.State, title, age)
	}
	return w.Flush()
}

func runSessionAttach(_ *cobra.Command, args []string) error {
	sessionID := args[0]
	conn, err := dialDaemon()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send attach command.
	params := protocol.AttachParams{SessionID: sessionID}
	raw, _ := json.Marshal(params)
	cmd := protocol.Cmd{Type: protocol.CmdAttach, Payload: raw}
	cmdBytes, _ := json.Marshal(cmd)
	if err := protocol.WriteFrame(conn, protocol.FrameJSON, cmdBytes); err != nil {
		return err
	}

	// Read the first response (confirmation or error).
	typ, payload, err := protocol.ReadFrame(conn)
	if err != nil {
		return err
	}
	if typ == protocol.FrameJSON {
		var resp protocol.Response
		if err := json.Unmarshal(payload, &resp); err == nil && !resp.OK {
			return fmt.Errorf("attach failed: %s", resp.Error)
		}
		// First JSON frame was the OK confirmation; PTY bytes follow.
	}

	// Switch stdin to raw mode.
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	// Forward terminal resize signals to the daemon.
	resizeSig := make(chan os.Signal, 1)
	signal.Notify(resizeSig, syscall.SIGWINCH)
	go func() {
		for range resizeSig {
			cols, rows, _ := term.GetSize(fd)
			rz, _ := json.Marshal(protocol.ResizePayload{Rows: uint16(rows), Cols: uint16(cols)})
			_ = protocol.WriteFrame(conn, protocol.FrameResize, rz)
		}
	}()
	// Send initial size.
	cols, rows, _ := term.GetSize(fd)
	rz, _ := json.Marshal(protocol.ResizePayload{Rows: uint16(rows), Cols: uint16(cols)})
	_ = protocol.WriteFrame(conn, protocol.FrameResize, rz)

	// Forward stdin → daemon.
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				_ = protocol.WriteFrame(conn, protocol.FramePTY, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Daemon PTY output → stdout.
	for {
		typ, payload, err := protocol.ReadFrame(conn)
		if err != nil {
			break
		}
		if typ == protocol.FramePTY {
			os.Stdout.Write(payload) //nolint:errcheck
		}
	}

	signal.Stop(resizeSig)
	return nil
}

func runSessionRename(_ *cobra.Command, args []string) error {
	params := protocol.UpdateTitleParams{SessionID: args[0], Title: args[1]}
	raw, _ := json.Marshal(params)
	cmd := protocol.Cmd{Type: protocol.CmdUpdateTitle, Payload: raw}
	resp, err := sendCmd(cmd)
	if err != nil {
		return err
	}
	var sess protocol.Session
	if err := json.Unmarshal(resp.Data, &sess); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("session %s renamed to %q (locked)\n", sess.ID, sess.Title)
	return nil
}

func runSessionKill(_ *cobra.Command, args []string) error {
	params := protocol.KillSessionParams{SessionID: args[0]}
	raw, _ := json.Marshal(params)
	cmd := protocol.Cmd{Type: protocol.CmdKillSession, Payload: raw}
	resp, err := sendCmd(cmd)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("kill failed: %s", resp.Error)
	}
	fmt.Printf("session %s killed\n", args[0])
	return nil
}

// --- helpers ---

func dialDaemon() (net.Conn, error) {
	conn, err := net.Dial("unix", daemon.SocketPath())
	if err != nil {
		return nil, fmt.Errorf("cannot connect to daemon (is it running? try: canopy daemon start): %w", err)
	}
	return conn, nil
}

func sendCmd(cmd protocol.Cmd) (protocol.Response, error) {
	conn, err := dialDaemon()
	if err != nil {
		return protocol.Response{}, err
	}
	defer conn.Close()

	payload, _ := json.Marshal(cmd)
	if err := protocol.WriteFrame(conn, protocol.FrameJSON, payload); err != nil {
		return protocol.Response{}, err
	}

	typ, data, err := protocol.ReadFrame(conn)
	if err != nil {
		return protocol.Response{}, err
	}
	if typ != protocol.FrameJSON {
		return protocol.Response{}, fmt.Errorf("unexpected frame type %d", typ)
	}

	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return protocol.Response{}, fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}
