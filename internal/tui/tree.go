package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sharathk-dev/canopy/internal/protocol"
)

type treeItemKind int

const (
	kindProject treeItemKind = iota
	kindWorktree
	kindSession
)

type treeItem struct {
	kind     treeItemKind
	project  *protocol.Project
	worktree *protocol.Worktree
	session  *protocol.Session
	expanded bool
}

// buildTree flattens the project→worktree→session hierarchy into a list
// of tree items for keyboard navigation.
func buildTree(
	projects []protocol.Project,
	worktrees map[string][]protocol.Worktree, // repoPath → []Worktree
	sessions map[string][]protocol.Session, // worktreeID → []Session
	expanded map[string]bool, // key → expanded
) []treeItem {
	var items []treeItem
	for i := range projects {
		p := &projects[i]
		projKey := "p:" + p.ID
		isExpanded := expanded[projKey]
		items = append(items, treeItem{
			kind:     kindProject,
			project:  p,
			expanded: isExpanded,
		})
		if !isExpanded {
			continue
		}
		for j := range worktrees[p.RepoPath] {
			wt := &worktrees[p.RepoPath][j]
			wtKey := "w:" + wt.ID
			wtExpanded := expanded[wtKey]
			items = append(items, treeItem{
				kind:     kindWorktree,
				project:  p,
				worktree: wt,
				expanded: wtExpanded,
			})
			if !wtExpanded {
				continue
			}
			for k := range sessions[wt.ID] {
				s := &sessions[wt.ID][k]
				items = append(items, treeItem{
					kind:     kindSession,
					project:  p,
					worktree: wt,
					session:  s,
				})
			}
		}
	}
	return items
}

// renderTree renders the visible tree items into a string of height lines.
func renderTree(items []treeItem, cursor, width, height int) string {
	if len(items) == 0 {
		return styleOutputEmpty.Render("No projects.\nRun: canopy project add")
	}

	// Compute scroll offset so cursor is always visible.
	offset := 0
	if cursor >= height {
		offset = cursor - height + 1
	}

	var sb strings.Builder
	for i := offset; i < len(items) && i < offset+height; i++ {
		item := items[i]
		selected := i == cursor
		line := renderTreeItem(item, selected, width)
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func renderTreeItem(item treeItem, selected bool, width int) string {
	var content string
	w := width - 2
	if w < 1 {
		w = 1
	}

	switch item.kind {
	case kindProject:
		arrow := "▶"
		if item.expanded {
			arrow = "▼"
		}
		name := item.project.Name
		content = fmt.Sprintf("%s %s", arrow, name)

	case kindWorktree:
		arrow := "▶"
		if item.expanded {
			arrow = "▼"
		}
		branch := item.worktree.Branch
		content = fmt.Sprintf("  %s %s", arrow, branch)

	case kindSession:
		tool := item.session.Tool
		if tool == "" {
			tool = "shell"
		}
		dot := stateDot(item.session.State)
		label := stateLabel(item.session.State)
		name := item.session.Title
		if name == "" {
			name = tool
		}
		// Use the session title when available, while keeping the state visible.
		if selected {
			// For selected, rebuild without style so we can apply bg
			content = fmt.Sprintf("    %s  %s", name, item.session.State)
			_ = dot
			_ = label
		} else {
			content = fmt.Sprintf("    %s · %s %s",
				lipgloss.NewStyle().Foreground(colorText).Render(name),
				label,
				dot,
			)
		}
	}

	if selected {
		return styleTreeSelected.Width(width).Render(content)
	}
	return styleTreeItem.Width(width).Render(content)
}

// selectedSession returns the session at cursor, or nil.
func selectedSession(items []treeItem, cursor int) *protocol.Session {
	if cursor < 0 || cursor >= len(items) {
		return nil
	}
	item := items[cursor]
	if item.kind == kindSession {
		return item.session
	}
	return nil
}

// firstSessionIndex returns the index of the first session item, or -1.
func firstSessionIndex(items []treeItem) int {
	for i, item := range items {
		if item.kind == kindSession {
			return i
		}
	}
	return -1
}

// breadcrumb returns the "project / worktree / tool" string for the header.
func breadcrumb(items []treeItem, cursor int) string {
	if cursor < 0 || cursor >= len(items) {
		return ""
	}
	item := items[cursor]
	parts := []string{}
	if item.project != nil {
		parts = append(parts, item.project.Name)
	}
	if item.worktree != nil {
		parts = append(parts, item.worktree.Branch)
	}
	if item.session != nil {
		tool := item.session.Tool
		if tool == "" {
			tool = "shell"
		}
		parts = append(parts, tool)
	}
	return strings.Join(parts, " / ")
}
