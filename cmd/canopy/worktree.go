package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Manage git worktrees (not yet implemented)",
}

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees for a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("worktree list: not yet implemented")
		return nil
	},
}

var worktreeAddCmd = &cobra.Command{
	Use:   "add <branch>",
	Short: "Add a new worktree",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("worktree add: not yet implemented")
		return nil
	},
}

var worktreeRemoveCmd = &cobra.Command{
	Use:   "remove <path>",
	Short: "Remove a worktree",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("worktree remove: not yet implemented")
		return nil
	},
}

func init() {
	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeAddCmd)
	worktreeCmd.AddCommand(worktreeRemoveCmd)
}
