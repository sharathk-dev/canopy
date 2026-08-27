package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParsePorcelain(t *testing.T) {
	output := "worktree /tmp/repo\nHEAD abc\nbranch refs/heads/main\n\n" +
		"worktree /tmp/repo-feature\nHEAD def\nbranch refs/heads/feature\n\n" +
		"worktree /tmp/repo.git\nHEAD 123\nbare\n"

	got := parsePorcelain(output)
	if len(got) != 3 {
		t.Fatalf("got %d worktrees, want 3", len(got))
	}
	if got[0].Path != "/tmp/repo" || !got[0].IsMain || got[0].Branch != "main" {
		t.Fatalf("unexpected main worktree: %+v", got[0])
	}
	if got[1].Branch != "feature" || got[1].IsMain {
		t.Fatalf("unexpected feature worktree: %+v", got[1])
	}
	if !got[2].IsBare {
		t.Fatalf("bare worktree was not identified: %+v", got[2])
	}
}

func TestDefaultBranchUsesOriginHead(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", dir},
		{"-C", dir, "config", "user.email", "test@example.com"},
		{"-C", dir, "config", "user.name", "Test"},
		{"-C", dir, "checkout", "-q", "-b", "development"},
		{"-C", dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/development"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	branch, err := DefaultBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "development" {
		t.Fatalf("got default branch %q, want development", branch)
	}
}
