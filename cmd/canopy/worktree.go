package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/sharathk-dev/canopy/internal/datadir"
	"github.com/sharathk-dev/canopy/internal/git"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Manage git worktrees",
}

var (
	flagWorktreePath  string
	flagWorktreeForce bool
	flagWorktreeRepo  string
	flagWorktreeBase  string
)

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees for a project",
	RunE:  runWorktreeList,
}

var worktreeAddCmd = &cobra.Command{
	Use:   "add <branch>",
	Short: "Create a new branch and worktree",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorktreeAdd,
}

var worktreeRemoveCmd = &cobra.Command{
	Use:   "remove <path>",
	Short: "Remove a worktree",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorktreeRemove,
}

func init() {
	worktreeListCmd.Flags().StringVar(&flagWorktreeRepo, "repo", "", "Repo path (default: current directory)")
	worktreeAddCmd.Flags().StringVar(&flagWorktreeRepo, "repo", "", "Repo path (default: current directory)")
	worktreeAddCmd.Flags().StringVar(&flagWorktreePath, "path", "", "Worktree path (default: sibling dir named after branch)")
	worktreeAddCmd.Flags().StringVar(&flagWorktreeBase, "base", "", "Base branch (default: detected repository default)")
	worktreeRemoveCmd.Flags().StringVar(&flagWorktreeRepo, "repo", "", "Repo path (default: current directory)")
	worktreeRemoveCmd.Flags().BoolVar(&flagWorktreeForce, "force", false, "Remove even with uncommitted changes")

	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeAddCmd)
	worktreeCmd.AddCommand(worktreeRemoveCmd)
}

func resolveRepo(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	return os.Getwd()
}

func runWorktreeList(_ *cobra.Command, _ []string) error {
	repoPath, err := resolveRepo(flagWorktreeRepo)
	if err != nil {
		return err
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

	worktrees, err := db.ListWorktreesByRepo(root)
	if err != nil {
		return fmt.Errorf("list worktrees: %w", err)
	}
	if len(worktrees) == 0 {
		fmt.Println("no worktrees found (register first: canopy project add)")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "BRANCH\tPATH\tMAIN")
	for _, wt := range worktrees {
		main := ""
		if wt.IsMain {
			main = "✓"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", wt.Branch, wt.Path, main)
	}
	return w.Flush()
}

func runWorktreeAdd(_ *cobra.Command, args []string) error {
	branch := args[0]
	repoPath, err := resolveRepo(flagWorktreeRepo)
	if err != nil {
		return err
	}
	root, err := git.RepoRoot(repoPath)
	if err != nil {
		return fmt.Errorf("not a git repo: %w", err)
	}

	path := flagWorktreePath
	if path == "" {
		safeBranch := strings.ReplaceAll(branch, "/", "-")
		path = filepath.Join(filepath.Dir(root), filepath.Base(root)+"-"+safeBranch)
	}

	base := flagWorktreeBase
	if base == "" {
		base, err = git.DefaultBranch(root)
		if err != nil {
			return fmt.Errorf("determine base branch: %w", err)
		}
	}

	if err := git.AddWorktree(root, path, branch, base); err != nil {
		return fmt.Errorf("git add worktree: %w", err)
	}

	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	wt := protocol.Worktree{
		ID:       protocol.NewID(),
		RepoPath: root,
		Path:     path,
		Branch:   branch,
	}
	if project, err := db.GetProjectByRepoPath(root); err == nil {
		wt.ProjectID = project.ID
	}
	if err := db.UpsertWorktree(wt); err != nil {
		return fmt.Errorf("save worktree: %w", err)
	}

	fmt.Printf("worktree created: %s\n  branch: %s\n  base:   %s\n  path:   %s\n", wt.ID, wt.Branch, base, wt.Path)
	return nil
}

func runWorktreeRemove(_ *cobra.Command, args []string) error {
	path := args[0]
	repoPath, err := resolveRepo(flagWorktreeRepo)
	if err != nil {
		return err
	}
	root, err := git.RepoRoot(repoPath)
	if err != nil {
		return fmt.Errorf("not a git repo: %w", err)
	}

	if !flagWorktreeForce {
		dirty, err := git.IsDirty(path)
		if err != nil {
			return fmt.Errorf("check dirty status: %w", err)
		}
		if dirty {
			return fmt.Errorf("worktree has uncommitted changes; use --force to override")
		}
	}

	if err := git.RemoveWorktree(root, path, flagWorktreeForce); err != nil {
		return fmt.Errorf("git remove worktree: %w", err)
	}

	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	if wt, err := db.GetWorktreeByRepoAndPath(root, path); err == nil {
		_ = db.MarkWorktreeMissing(wt.ID, true)
	}

	fmt.Printf("worktree removed: %s\n", path)
	return nil
}
