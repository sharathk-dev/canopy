package daemon

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sharathk-dev/canopy/internal/git"
	"github.com/sharathk-dev/canopy/internal/protocol"
)

// registerProject resolves the repo root, upserts the project in the DB, and
// runs an initial worktree reconcile for that repo.
func (d *Daemon) registerProject(repoPath, name string) (protocol.Project, error) {
	root, err := git.RepoRoot(repoPath)
	if err != nil {
		return protocol.Project{}, fmt.Errorf("not a git repo: %w", err)
	}

	if name == "" {
		name = filepath.Base(root)
	}

	// Check if already registered by repo path.
	projects, err := d.db.ListProjects()
	if err != nil {
		return protocol.Project{}, err
	}
	for _, p := range projects {
		if p.RepoPath == root {
			// Already registered — update name if it changed.
			p.Name = name
			return p, d.db.UpsertProject(p)
		}
	}

	proj := protocol.Project{
		ID:       protocol.NewID(),
		RepoPath: root,
		Name:     name,
	}
	if err := d.db.UpsertProject(proj); err != nil {
		return protocol.Project{}, err
	}

	// Seed the worktrees for this repo immediately.
	d.reconcileRepo(proj.RepoPath)
	return proj, nil
}

// addWorktree creates a new git worktree and registers it in the DB.
// If path is empty, it picks a sibling directory named after the branch.
func (d *Daemon) addWorktree(repoPath, branch, path string) (protocol.Worktree, error) {
	root, err := git.RepoRoot(repoPath)
	if err != nil {
		return protocol.Worktree{}, fmt.Errorf("resolve repo root: %w", err)
	}

	if path == "" {
		// Place the worktree next to the repo root: <parent>/<reponame>-<sanitized-branch>
		safeBranch := strings.ReplaceAll(branch, "/", "-")
		path = filepath.Join(filepath.Dir(root), filepath.Base(root)+"-"+safeBranch)
	}

	if err := git.AddWorktree(root, path, branch); err != nil {
		return protocol.Worktree{}, err
	}

	wt := protocol.Worktree{
		ID:       protocol.NewID(),
		RepoPath: root,
		Path:     path,
		Branch:   branch,
		IsMain:   false,
	}
	if err := d.db.UpsertWorktree(wt); err != nil {
		return protocol.Worktree{}, err
	}
	return wt, nil
}

// removeWorktree removes a git worktree after a dirty check and unregisters it.
func (d *Daemon) removeWorktree(repoPath, path string, force bool) error {
	root, err := git.RepoRoot(repoPath)
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}

	if !force {
		dirty, err := git.IsDirty(path)
		if err != nil {
			return fmt.Errorf("check dirty status: %w", err)
		}
		if dirty {
			return fmt.Errorf("worktree has uncommitted changes; use --force to override")
		}
	}

	if err := git.RemoveWorktree(root, path, force); err != nil {
		return err
	}

	wt, err := d.db.GetWorktreeByPath(path)
	if err == nil {
		_ = d.db.DeleteWorktree(wt.ID)
	}
	return nil
}
