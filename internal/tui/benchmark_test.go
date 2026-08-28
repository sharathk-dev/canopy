package tui

import (
	"fmt"
	"testing"
	"time"

	"github.com/sharathk-dev/canopy/internal/protocol"
)

func benchmarkTreeFixture() ([]protocol.Schedule, []protocol.Project, map[string][]protocol.Worktree, map[string][]protocol.Session, map[string]bool) {
	schedules := make([]protocol.Schedule, 20)
	projects := make([]protocol.Project, 10)
	worktrees := make(map[string][]protocol.Worktree)
	sessions := make(map[string][]protocol.Session)
	expanded := make(map[string]bool)
	for i := range schedules {
		schedules[i] = protocol.Schedule{Name: fmt.Sprintf("schedule-%02d", i), Cron: "*/5 * * * *"}
	}
	for i := range projects {
		project := protocol.Project{ID: fmt.Sprintf("project-%02d", i), Name: fmt.Sprintf("project-%02d", i), RepoPath: fmt.Sprintf("/tmp/project-%02d", i)}
		projects[i] = project
		expanded["p:"+project.ID] = true
		for j := 0; j < 3; j++ {
			wt := protocol.Worktree{ID: fmt.Sprintf("worktree-%02d-%d", i, j), ProjectID: project.ID, RepoPath: project.RepoPath, Branch: fmt.Sprintf("feature/%d/%d", i, j), Path: fmt.Sprintf("/tmp/project-%02d-%d", i, j)}
			worktrees[project.RepoPath] = append(worktrees[project.RepoPath], wt)
			expanded["w:"+wt.ID] = true
			for k := 0; k < 5; k++ {
				sessions[wt.ID] = append(sessions[wt.ID], protocol.Session{ID: fmt.Sprintf("session-%d-%d-%d", i, j, k), Title: fmt.Sprintf("task %d project %d", k, i), Tool: "claude", State: protocol.StateFresh, StartedAt: time.Now()})
			}
		}
	}
	return schedules, projects, worktrees, sessions, expanded
}

func BenchmarkBuildTree(b *testing.B) {
	schedules, projects, worktrees, sessions, expanded := benchmarkTreeFixture()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = buildTree(schedules, projects, worktrees, sessions, expanded)
	}
}

func BenchmarkFilterTreeItems(b *testing.B) {
	schedules, projects, worktrees, sessions, expanded := benchmarkTreeFixture()
	items := buildTree(schedules, projects, worktrees, sessions, expanded)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = filterTreeItems(items, "project 7")
	}
}

// BenchmarkModelView measures a full TUI render pass with a realistic tree
// (10 projects × 3 worktrees × 5 sessions) and PTY content in the right panel.
func BenchmarkModelView(b *testing.B) {
	schedules, projects, worktrees, sessions, expanded := benchmarkTreeFixture()
	SetTheme(ThemeByName("dark"))
	m := Model{
		ready:     true,
		width:     220,
		height:    50,
		projects:  projects,
		worktrees: worktrees,
		sessions:  sessions,
		schedules: schedules,
		runs:      make(map[string][]protocol.ScheduleRun),
		expanded:  expanded,
	}
	m.items = buildTree(schedules, projects, worktrees, sessions, expanded)
	m.rebuildViewport()
	m.output = "mock PTY snapshot — status bar line at full width\x1b[0m"
	m.viewport.SetContent(m.output)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
