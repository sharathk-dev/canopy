package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sharathk-dev/canopy/internal/daemon"
	"github.com/sharathk-dev/canopy/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "canopy",
	Short: "Agent session manager — keep AI agent sessions alive across terminal disconnects",
	// Launching with no subcommand opens the TUI.
	RunE: runUI,
}

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the interactive TUI",
	RunE:  runUI,
}

func runUI(_ *cobra.Command, _ []string) error {
	sockPath := daemon.SocketPath()
	m := tui.New(sockPath)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
		return err
	}
	return nil
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(worktreeCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(uiCmd)
}
