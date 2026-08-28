package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sharathk-dev/canopy/internal/datadir"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/tui"
	"github.com/spf13/cobra"
)

var debugMode bool

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
	if debugMode {
		tui.EnableDebug()
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	// Ensure a current daemon is running as a background process.
	// The daemon survives TUI exit so sessions (and their PTYs) stay alive.
	if !daemonCurrent(exe) {
		stopExistingDaemon() // kill stale daemon if one is running
		if err := startDaemonProcess(exe); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		// Wait for the daemon to bind its socket.
		for i := 0; i < 20; i++ {
			time.Sleep(50 * time.Millisecond)
			if daemonCurrent(exe) {
				break
			}
		}
	}

	m := tui.New(datadir.SocketPath(), datadir.DBPath())
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
		return err
	}
	return nil
}

// startDaemonProcess forks the current binary as a detached daemon process.
func startDaemonProcess(exe string) error {
	args := []string{exe, "daemon", "_run"}
	if debugMode {
		args = append(args, "--debug")
	}
	proc, err := os.StartProcess(exe, args,
		&os.ProcAttr{
			Files: []*os.File{nil, nil, nil},
			Sys:   &syscall.SysProcAttr{Setsid: true},
		},
	)
	if err != nil {
		return err
	}
	return proc.Release()
}

// stopExistingDaemon sends SIGTERM to any daemon recorded in the PID file.
func stopExistingDaemon() {
	data, err := os.ReadFile(datadir.PIDPath())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Signal(syscall.SIGTERM)
		// Brief wait so the socket is released before we re-bind.
		time.Sleep(100 * time.Millisecond)
	}
}

// daemonCurrent returns true if a daemon is running AND was built from the
// same binary as the current process. Returns false if no daemon is reachable
// or if the daemon binary is older (stale install).
func daemonCurrent(exe string) bool {
	conn, err := net.Dial("unix", datadir.SocketPath())
	if err != nil {
		return false
	}
	defer conn.Close()

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
		return false
	}
	var ver protocol.VersionResponse
	if err := json.Unmarshal(resp.Data, &ver); err != nil {
		return false
	}

	fi, err := os.Stat(exe)
	if err != nil {
		return true
	}
	return fi.ModTime().UnixNano() == ver.BinaryMtime
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "log all events to /tmp/canopy-debug.log")
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(worktreeCmd)
	rootCmd.AddCommand(uiCmd)
}
