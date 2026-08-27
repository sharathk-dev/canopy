package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/sharathk-dev/canopy/internal/datadir"
	"github.com/sharathk-dev/canopy/internal/git"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
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
	repoPath, err := os.Getwd()
	if err != nil {
		return err
	}
	if len(args) > 0 {
		repoPath = args[0]
	}

	root, err := git.RepoRoot(repoPath)
	if err != nil {
		return fmt.Errorf("not a git repo: %w", err)
	}

	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	// Idempotent: skip if already registered.
	projects, _ := db.ListProjects()
	for _, p := range projects {
		if p.RepoPath == root {
			fmt.Printf("already registered: %s (%s)\n", p.Name, p.RepoPath)
			return nil
		}
	}

	proj := protocol.Project{
		ID:       protocol.NewID(),
		RepoPath: root,
		Name:     filepath.Base(root),
	}
	if err := db.UpsertProject(proj); err != nil {
		return fmt.Errorf("save project: %w", err)
	}

	seedWorktrees(db, root)
	fmt.Printf("project registered: %s (%s)\n", proj.Name, proj.RepoPath)
	return nil
}

func runProjectList(_ *cobra.Command, _ []string) error {
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	projects, err := db.ListProjects()
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
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

// seedWorktrees upserts all current git worktrees for repoPath into the DB.
func seedWorktrees(db *store.Store, repoPath string) {
	wts, err := git.ListWorktrees(repoPath)
	if err != nil {
		return
	}
	for _, wt := range wts {
		_ = db.UpsertWorktree(protocol.Worktree{
			ID:       protocol.NewID(),
			RepoPath: repoPath,
			Path:     wt.Path,
			Branch:   wt.Branch,
			IsMain:   wt.IsMain,
		})
	}
}
