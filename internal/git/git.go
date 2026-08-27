package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// WorktreeInfo holds the parsed output of one entry from `git worktree list --porcelain`.
type WorktreeInfo struct {
	Path   string
	Branch string
	IsMain bool
	IsBare bool
}

// RepoRoot returns the absolute path of the git repository root containing dir.
func RepoRoot(dir string) (string, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// ListWorktrees returns all worktrees registered in the repository at repoRoot.
func ListWorktrees(repoRoot string) ([]WorktreeInfo, error) {
	out, err := run(repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parsePorcelain(out), nil
}

// DefaultBranch returns the repository's preferred base branch without
// contacting the remote. Clones normally record origin/HEAD locally; for
// repositories without that symbolic ref, use common local branch names and
// finally the currently checked-out branch.
func DefaultBranch(repoRoot string) (string, error) {
	if out, err := run(repoRoot, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(out), "origin/"), nil
	}
	for _, branch := range []string{"main", "master", "development"} {
		if _, err := run(repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			return branch, nil
		}
	}
	if out, err := run(repoRoot, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(out), nil
	}
	return "", fmt.Errorf("could not determine default branch")
}

// AddWorktree creates a new branch and worktree at path, based on base.
func AddWorktree(repoRoot, path, branch, base string) error {
	args := []string{"worktree", "add", "-b", branch, path}
	if base != "" {
		args = append(args, base)
	}
	_, err := run(repoRoot, args...)
	if err != nil {
		return fmt.Errorf("git worktree add: %w", err)
	}
	return nil
}

// RemoveWorktree removes the worktree at path. Pass force=true to bypass the
// dirty-check (callers should have already confirmed with IsDirty).
func RemoveWorktree(repoRoot, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if _, err := run(repoRoot, args...); err != nil {
		return fmt.Errorf("git worktree remove: %w", err)
	}
	return nil
}

// IsDirty reports whether path has any uncommitted changes.
func IsDirty(path string) (bool, error) {
	out, err := run(path, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

// parsePorcelain parses `git worktree list --porcelain` output into WorktreeInfo
// entries. Each worktree stanza is separated by a blank line.
func parsePorcelain(output string) []WorktreeInfo {
	var results []WorktreeInfo
	stanzas := strings.Split(output, "\n\n")
	for _, stanza := range stanzas {
		stanza = strings.TrimSpace(stanza)
		if stanza == "" {
			continue
		}
		var info WorktreeInfo
		for _, line := range strings.Split(stanza, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "worktree ") {
				info.Path = strings.TrimPrefix(line, "worktree ")
				if len(results) == 0 {
					info.IsMain = true
				}
			} else if strings.HasPrefix(line, "branch ") {
				ref := strings.TrimPrefix(line, "branch ")
				// refs/heads/main → main
				info.Branch = strings.TrimPrefix(ref, "refs/heads/")
			} else if line == "bare" {
				info.IsBare = true
			}
		}
		if info.Path != "" {
			results = append(results, info)
		}
	}
	return results
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}
