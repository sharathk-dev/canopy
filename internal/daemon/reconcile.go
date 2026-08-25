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

// reconcile syncs the DB's worktree view against `git worktree list --porcelain`
// for every repo registered in the database.
func (d *Daemon) reconcile() {
	projects, err := d.db.ListProjects()
	if err != nil {
		log.Printf("reconcile: list projects: %v", err)
		return
	}

	for _, proj := range projects {
		liveWTs, err := git.ListWorktrees(proj.RepoPath)
		if err != nil {
			log.Printf("reconcile: list worktrees for %s: %v", proj.RepoPath, err)
			continue
		}

		// Build a set of live paths for quick lookup.
		livePaths := make(map[string]git.WorktreeInfo, len(liveWTs))
		for _, wt := range liveWTs {
			livePaths[wt.Path] = wt
		}

		// Upsert any live worktrees not yet in the DB.
		stored, err := d.db.ListWorktreesByRepo(proj.RepoPath)
		if err != nil {
			log.Printf("reconcile: db worktrees for %s: %v", proj.RepoPath, err)
			continue
		}
		storedPaths := make(map[string]protocol.Worktree, len(stored))
		for _, wt := range stored {
			storedPaths[wt.Path] = wt
		}

		for path, info := range livePaths {
			if _, exists := storedPaths[path]; !exists {
				wt := protocol.Worktree{
					ID:       protocol.NewID(),
					RepoPath: proj.RepoPath,
					Path:     info.Path,
					Branch:   info.Branch,
					IsMain:   info.IsMain,
				}
				if err := d.db.UpsertWorktree(wt); err != nil {
					log.Printf("reconcile: upsert worktree %s: %v", path, err)
				}
			}
		}

		// Mark DB entries whose directories no longer exist as missing.
		for path, wt := range storedPaths {
			_, live := livePaths[path]
			if err := d.db.MarkWorktreeMissing(wt.ID, !live); err != nil {
				log.Printf("reconcile: mark missing %s: %v", path, err)
			}
		}
	}
}
