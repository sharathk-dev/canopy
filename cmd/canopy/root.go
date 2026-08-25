package main

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "canopy",
	Short: "Agent session manager — keep AI agent sessions alive across terminal disconnects",
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(worktreeCmd)
}
