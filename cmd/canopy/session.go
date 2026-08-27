package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/sharathk-dev/canopy/internal/datadir"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
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
	Short: "Start a new agent session",
	RunE:  runSessionNew,
}

var sessionAttachCmd = &cobra.Command{
	Use:   "attach <session-id>",
	Short: "Attach to a running session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionAttach,
}

var sessionResumeCmd = &cobra.Command{
	Use:   "resume <session-id>",
	Short: "Resume a saved Claude session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionResume,
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active sessions",
	RunE:  runSessionList,
}

var sessionKillCmd = &cobra.Command{
	Use:   "kill <session-id>",
	Short: "Kill a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionKill,
}

func init() {
	sessionNewCmd.Flags().StringVar(&flagTool, "tool", "claude", "Agent tool to launch")
	sessionNewCmd.Flags().StringVar(&flagCWD, "cwd", "", "Working directory (default: current directory)")

	sessionCmd.AddCommand(sessionNewCmd)
	sessionCmd.AddCommand(sessionAttachCmd)
	sessionCmd.AddCommand(sessionResumeCmd)
	sessionCmd.AddCommand(sessionListCmd)
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
	resp, err := sendDaemonCmd(protocol.Cmd{Type: protocol.CmdNewSession, Payload: raw})
	if err != nil {
		return fmt.Errorf("daemon not running — start it with: canopy daemon start\n%w", err)
	}

	var sess protocol.Session
	if err := json.Unmarshal(resp.Data, &sess); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	fmt.Printf("session %s started\n", sess.ID)
	return runSessionAttach(nil, []string{sess.ID})
}

func runSessionResume(_ *cobra.Command, args []string) error {
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	sess, err := db.GetSession(args[0])
	db.Close()
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}
	if sess.Tool != "claude" || sess.CLISessionID == "" {
		return fmt.Errorf("session has no saved Claude session ID")
	}

	params := protocol.NewSessionParams{
		WorktreeID:   sess.WorktreeID,
		Tool:         sess.Tool,
		CWD:          sess.CWD,
		CLISessionID: sess.CLISessionID,
	}
	raw, _ := json.Marshal(params)
	resp, err := sendDaemonCmd(protocol.Cmd{Type: protocol.CmdNewSession, Payload: raw})
	if err != nil {
		return fmt.Errorf("daemon not running — start it with: canopy daemon start\n%w", err)
	}
	var resumed protocol.Session
	if err := json.Unmarshal(resp.Data, &resumed); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("session %s resumed from %s\n", resumed.ID, sess.CLISessionID)
	return runSessionAttach(nil, []string{resumed.ID})
}

func runSessionAttach(_ *cobra.Command, args []string) error {
	sessionID := args[0]

	conn, err := dialDaemon()
	if err != nil {
		return err
	}
	defer conn.Close()

	params := protocol.AttachParams{SessionID: sessionID}
	raw, _ := json.Marshal(params)
	cmdBytes, _ := json.Marshal(protocol.Cmd{Type: protocol.CmdAttach, Payload: raw})
	if err := protocol.WriteFrame(conn, protocol.FrameJSON, cmdBytes); err != nil {
		return err
	}

	// First frame may be a PTY snapshot (scrollback), second is the OK response.
	typ, payload, err := protocol.ReadFrame(conn)
	if err != nil {
		return err
	}
	if typ == protocol.FramePTY {
		os.Stdout.Write(payload) //nolint:errcheck
		// Read the OK frame.
		typ, payload, err = protocol.ReadFrame(conn)
		if err != nil {
			return err
		}
	}
	if typ == protocol.FrameJSON {
		var resp protocol.Response
		if err := json.Unmarshal(payload, &resp); err == nil && !resp.OK {
			return fmt.Errorf("attach failed: %s", resp.Error)
		}
	}

	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	defer term.Restore(fd, oldState) //nolint:errcheck

	// Forward resize signals.
	resizeSig := make(chan os.Signal, 1)
	signal.Notify(resizeSig, syscall.SIGWINCH)
	go func() {
		for range resizeSig {
			cols, rows, _ := term.GetSize(fd)
			rz, _ := json.Marshal(protocol.ResizePayload{Rows: uint16(rows), Cols: uint16(cols)})
			_ = protocol.WriteFrame(conn, protocol.FrameResize, rz)
		}
	}()
	cols, rows, _ := term.GetSize(fd)
	rz, _ := json.Marshal(protocol.ResizePayload{Rows: uint16(rows), Cols: uint16(cols)})
	_ = protocol.WriteFrame(conn, protocol.FrameResize, rz)

	// stdin → daemon.
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

	// daemon PTY → stdout.
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

func runSessionList(_ *cobra.Command, _ []string) error {
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	sessions, err := db.ListActiveSessions()
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
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

func runSessionKill(_ *cobra.Command, args []string) error {
	fmt.Printf("Kill session %s? [y/N] ", args[0])
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(answer) == 0 {
		return err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("cancelled")
		return nil
	}

	// Try daemon first so it can cleanly tear down the PTY.
	params := protocol.KillSessionParams{SessionID: args[0]}
	raw, _ := json.Marshal(params)
	if _, err := sendDaemonCmd(protocol.Cmd{Type: protocol.CmdKillSession, Payload: raw}); err == nil {
		fmt.Printf("session %s killed\n", args[0])
		return nil
	}

	// Daemon not running — fall back to direct DB + SIGTERM.
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	sess, err := db.GetSession(args[0])
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}
	if sess.PID > 0 {
		if proc, err := os.FindProcess(sess.PID); err == nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
	}
	sess.State = protocol.StateTerminated
	sess.Archived = true
	_ = db.UpdateSession(sess)
	fmt.Printf("session %s killed\n", sess.ID)
	return nil
}

// --- helpers ---

func dialDaemon() (net.Conn, error) {
	conn, err := net.Dial("unix", datadir.SocketPath())
	if err != nil {
		return nil, fmt.Errorf("cannot connect to daemon (run: canopy daemon start): %w", err)
	}
	return conn, nil
}

func sendDaemonCmd(cmd protocol.Cmd) (protocol.Response, error) {
	conn, err := dialDaemon()
	if err != nil {
		return protocol.Response{}, err
	}
	defer conn.Close()

	payload, _ := json.Marshal(cmd)
	if err := protocol.WriteFrame(conn, protocol.FrameJSON, payload); err != nil {
		return protocol.Response{}, err
	}

	_, data, err := protocol.ReadFrame(conn)
	if err != nil {
		return protocol.Response{}, err
	}

	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return protocol.Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}
