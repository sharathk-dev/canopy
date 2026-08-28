package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sharathk-dev/canopy/internal/protocol"
)

func TestStorePersistsOwnershipAndSession(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "canopy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	project := protocol.Project{ID: "project-1", RepoPath: "/tmp/repo", Name: "repo"}
	if err := db.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	worktree := protocol.Worktree{
		ID: "worktree-1", ProjectID: project.ID, RepoPath: project.RepoPath,
		Path: "/tmp/repo", Branch: "main", IsMain: true,
	}
	if err := db.UpsertWorktree(worktree); err != nil {
		t.Fatal(err)
	}
	gotWorktree, err := db.GetWorktree(worktree.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotWorktree.ProjectID != project.ID {
		t.Fatalf("got project ID %q, want %q", gotWorktree.ProjectID, project.ID)
	}

	sess := protocol.Session{
		ID: "session-1", WorktreeID: worktree.ID, Kind: "agent", Tool: "claude",
		CWD: worktree.Path, Title: "test", State: protocol.StateFresh,
		StartedAt: time.Now().UTC(),
	}
	if err := db.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	active, err := db.ListActiveSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != sess.ID {
		t.Fatalf("unexpected active sessions: %+v", active)
	}
}

func TestWorktreePathPrefixRequiresBoundary(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "canopy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	worktree := protocol.Worktree{ID: "worktree-1", RepoPath: "/tmp/repo", Path: "/tmp/repo", Branch: "main"}
	if err := db.UpsertWorktree(worktree); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetWorktreeByPathPrefix("/tmp/repository"); err == nil {
		t.Fatal("matched a path without a directory boundary")
	}
	got, err := db.GetWorktreeByPathPrefix("/tmp/repo/subdir")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != worktree.ID {
		t.Fatalf("got worktree %q, want %q", got.ID, worktree.ID)
	}
}

func TestScheduleClaimIsIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "canopy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	schedule := protocol.Schedule{
		ID: "schedule-1", Name: "test", ActionType: "command",
		Action: "echo test", Cron: "* * * * *", Enabled: true,
	}
	if err := db.CreateSchedule(schedule); err != nil {
		t.Fatal(err)
	}
	minute := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	claimed, err := db.ClaimSchedule(schedule.ID, minute)
	if err != nil || !claimed {
		t.Fatalf("first claim: claimed=%v err=%v", claimed, err)
	}
	claimed, err = db.ClaimSchedule(schedule.ID, minute)
	if err != nil || claimed {
		t.Fatalf("second claim: claimed=%v err=%v", claimed, err)
	}
}
