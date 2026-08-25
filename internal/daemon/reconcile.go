package daemon

import (
	"context"
	"log"
	"time"

	"github.com/sharathk-dev/canopy/internal/git"
	"github.com/sharathk-dev/canopy/internal/protocol"
)

const reconcileInterval = 30 * time.Second

// reconcileLoop calls reconcile on startup and every reconcileInterval.
func (d *Daemon) reconcileLoop(ctx context.Context) {
	d.reconcile()
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reconcile()
		}
	}
}

// reconcile syncs the DB's worktree view for all registered repos.
func (d *Daemon) reconcile() {
	projects, err := d.db.ListProjects()
	if err != nil {
		log.Printf("reconcile: list projects: %v", err)
		return
	}
	for _, proj := range projects {
		d.reconcileRepo(proj.RepoPath)
	}
}

// reconcileRepo syncs one repo's worktrees against `git worktree list --porcelain`.
func (d *Daemon) reconcileRepo(repoPath string) {
	liveWTs, err := git.ListWorktrees(repoPath)
	if err != nil {
		log.Printf("reconcile %s: %v", repoPath, err)
		return
	}

	livePaths := make(map[string]git.WorktreeInfo, len(liveWTs))
	for _, wt := range liveWTs {
		livePaths[wt.Path] = wt
	}

	stored, err := d.db.ListWorktreesByRepo(repoPath)
	if err != nil {
		log.Printf("reconcile db %s: %v", repoPath, err)
		return
	}
	storedPaths := make(map[string]protocol.Worktree, len(stored))
	for _, wt := range stored {
		storedPaths[wt.Path] = wt
	}

	for path, info := range livePaths {
		if _, exists := storedPaths[path]; !exists {
			wt := protocol.Worktree{
				ID:       protocol.NewID(),
				RepoPath: repoPath,
				Path:     info.Path,
				Branch:   info.Branch,
				IsMain:   info.IsMain,
			}
			if err := d.db.UpsertWorktree(wt); err != nil {
				log.Printf("reconcile upsert %s: %v", path, err)
			}
		}
	}

	for path, wt := range storedPaths {
		_, live := livePaths[path]
		if err := d.db.MarkWorktreeMissing(wt.ID, !live); err != nil {
			log.Printf("reconcile mark-missing %s: %v", path, err)
		}
	}
}
