package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	internaldaemon "github.com/sharathk-dev/canopy/internal/daemon"
	"github.com/sharathk-dev/canopy/internal/datadir"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
	"github.com/sharathk-dev/canopy/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "canopy",
	Short: "Agent session manager — keep AI agent sessions organised across worktrees",
	RunE:  runUI,
}

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the interactive TUI",
	RunE:  runUI,
}

func runUI(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start an embedded daemon if one isn't running or if the existing one
	// was built from an older binary (e.g. user just installed a new version).
	if !daemonCurrent() {
		db, err := store.Open(datadir.DBPath())
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		d := internaldaemon.New(db, datadir.SocketPath())
		go func() {
			_ = d.Run(ctx)
			db.Close()
		}()
		// Give the embedded daemon a moment to bind the socket.
		time.Sleep(80 * time.Millisecond)
	}

	m := tui.New(datadir.SocketPath(), datadir.DBPath())
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
		return err
	}
	return nil
}

// daemonCurrent returns true if a daemon is running AND was built from the
// same binary as the current process. Returns false if no daemon is reachable
// or if the daemon binary is older (stale install).
func daemonCurrent() bool {
	conn, err := net.Dial("unix", datadir.SocketPath())
	if err != nil {
		return false // not running
	}
	defer conn.Close()

	// Ask the daemon for its binary mtime.
	payload, _ := json.Marshal(protocol.Cmd{Type: protocol.CmdVersion})
	if err := protocol.WriteFrame(conn, protocol.FrameJSON, payload); err != nil {
		return false
	}
	_, data, err := protocol.ReadFrame(conn)
	if err != nil {
		return false
	}
	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil || !resp.OK {
		return false // old daemon without CmdVersion support
	}
	var ver protocol.VersionResponse
	if err := json.Unmarshal(resp.Data, &ver); err != nil {
		return false
	}

	// Compare with current binary mtime.
	exe, err := os.Executable()
	if err != nil {
		return true // can't tell; assume current
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return true
	}
	return fi.ModTime().Unix() == ver.BinaryMtime
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(worktreeCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(uiCmd)
}
