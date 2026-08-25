package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage projects (git repositories)",
}

var projectAddCmd = &cobra.Command{
	Use:   "add [path]",
	Short: "Register a git repository with canopy (default: current directory)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runProjectAdd,
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered projects",
	RunE:  runProjectList,
}

func init() {
	projectCmd.AddCommand(projectAddCmd)
	projectCmd.AddCommand(projectListCmd)
	rootCmd.AddCommand(projectCmd)
}

func runProjectAdd(_ *cobra.Command, args []string) error {
	repoPath := "."
	if len(args) > 0 {
		repoPath = args[0]
	}

	abs, err := os.Getwd()
	if err != nil {
		return err
	}
	if repoPath == "." {
		repoPath = abs
	}

	params := protocol.RegisterProjectParams{RepoPath: repoPath}
	raw, _ := json.Marshal(params)
	cmd := protocol.Cmd{Type: protocol.CmdRegisterProject, Payload: raw}

	resp, err := sendCmd(cmd)
	if err != nil {
		return err
	}

	var proj protocol.Project
	if err := json.Unmarshal(resp.Data, &proj); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	fmt.Printf("project registered: %s (%s)\n", proj.Name, proj.RepoPath)
	return nil
}

func runProjectList(_ *cobra.Command, _ []string) error {
	cmd := protocol.Cmd{Type: protocol.CmdListProjects}
	resp, err := sendCmd(cmd)
	if err != nil {
		return err
	}

	var projects []protocol.Project
	if err := json.Unmarshal(resp.Data, &projects); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(projects) == 0 {
		fmt.Println("no projects registered (run: canopy project add)")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPATH")
	for _, p := range projects {
		fmt.Fprintf(w, "%s\t%s\t%s\n", p.ID, p.Name, p.RepoPath)
	}
	return w.Flush()
}
