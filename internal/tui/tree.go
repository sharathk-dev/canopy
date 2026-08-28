package tui

import (
	"fmt"
	"strings"

	"github.com/sharathk-dev/canopy/internal/protocol"
)

type treeItemKind int

const (
	kindProject treeItemKind = iota
	kindWorktree
	kindSession
	kindSchedule
	kindSettings
)

type treeItem struct {
	kind     treeItemKind
	project  *protocol.Project
	worktree *protocol.Worktree
	session  *protocol.Session
	schedule *protocol.Schedule
	expanded bool
}

// buildTree flattens the project→worktree→session hierarchy into a list
// of tree items for keyboard navigation.
func buildTree(
	schedules []protocol.Schedule,
	projects []protocol.Project,
	worktrees map[string][]protocol.Worktree,
	sessions map[string][]protocol.Session,
	expanded map[string]bool,
) []treeItem {
	var items []treeItem
	for i := range schedules {
		items = append(items, treeItem{kind: kindSchedule, schedule: &schedules[i]})
	}
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
	items = append(items, treeItem{kind: kindSettings})
	return items
}

// renderTree renders the visible tree items into a string of height lines.
func renderTree(items []treeItem, cursor, width, height int) string {
	if len(items) == 0 {
		return styleOutputEmpty.Render("No projects.\nRun: canopy project add")
	}

	type treeLine struct {
		heading   string
		itemIndex int
	}
	var lines []treeLine
	schedulesShown, projectsShown := false, false
	for i, item := range items {
		if item.kind == kindSettings {
			continue
		}
		if item.kind == kindSchedule && !schedulesShown {
			lines = append(lines, treeLine{heading: "SCHEDULES", itemIndex: -1})
			schedulesShown = true
		}
		if item.kind == kindProject && !projectsShown {
			lines = append(lines, treeLine{heading: "PROJECTS", itemIndex: -1})
			projectsShown = true
		}
		lines = append(lines, treeLine{itemIndex: i})
	}

	selectedLine := 0
	for i, line := range lines {
		if line.itemIndex == cursor {
			selectedLine = i
			break
		}
	}
	start := selectedLine - height + 1
	if start < 0 {
		start = 0
	}

	var sb strings.Builder
	for i := start; i < len(lines) && i < start+height; i++ {
		line := lines[i]
		if line.heading != "" {
			sb.WriteString(stylePanelTitle.Width(width).Render(line.heading))
		} else {
			sb.WriteString(renderTreeItem(items[line.itemIndex], line.itemIndex == cursor, width))
		}
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
		name := item.session.Title
		if name == "" {
			name = tool
		}

		age := timeAgo(item.session.StartedAt)
		ageW := len([]rune(age)) // age is always ASCII so rune len == display width
		// Layout inside the style padding: "    " (4) + dot (1) + "  " (2) + title + " " + age
		titleMaxW := w - 4 - 1 - 2 - 1 - ageW
		if titleMaxW < 4 {
			titleMaxW = 4
		}
		name = truncate(name, titleMaxW)
		titlePad := titleMaxW - len([]rune(name))
		if titlePad < 0 {
			titlePad = 0
		}
		ageStr := themed(colorDim).Render(age)

		if selected {
			// Use raw ANSI so inner sequences don't reset the blue background.
			// \x1b[1m = bold on, \x1b[22m = bold off (no full reset).
			boldName := fmt.Sprintf("\x1b[1m%s\x1b[22m", name)
			// Dim the age on the blue background without resetting bg.
			dimAge := fmt.Sprintf("\x1b[38;2;148;163;184m%s\x1b[38;2;255;255;255m", age)
			content = fmt.Sprintf("    %s  %s%s %s",
				selectedStateDot(item.session.State),
				boldName,
				strings.Repeat(" ", titlePad),
				dimAge,
			)
		} else {
			content = fmt.Sprintf("    %s  %s%s %s",
				stateDot(item.session.State),
				themed(colorText).Render(name),
				strings.Repeat(" ", titlePad),
				ageStr,
			)
		}

	case kindSchedule:
		marker := "○"
		if item.schedule.Enabled {
			marker = "◷"
		}
		content = fmt.Sprintf("%s %s", marker, item.schedule.Name)

	case kindSettings:
		content = "⚙ config"
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

func selectedSchedule(items []treeItem, cursor int) *protocol.Schedule {
	if cursor < 0 || cursor >= len(items) || items[cursor].kind != kindSchedule {
		return nil
	}
	return items[cursor].schedule
}

// filterTreeItems keeps only matching sessions with their parent project/worktree.
// Schedules are matched by name. Non-matching siblings are excluded.
func filterTreeItems(items []treeItem, query string) []treeItem {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}

	var filtered []treeItem
	for i := 0; i < len(items); {
		if items[i].kind == kindSchedule {
			if strings.Contains(strings.ToLower(items[i].schedule.Name), query) {
				filtered = append(filtered, items[i])
			}
			i++
			continue
		}

		if items[i].kind != kindProject {
			i++
			continue
		}

		projectItem := items[i]
		projectAdded := false
		i++

		for i < len(items) && items[i].kind != kindProject && items[i].kind != kindSchedule {
			if items[i].kind != kindWorktree {
				i++
				continue
			}
			worktreeItem := items[i]
			worktreeAdded := false
			i++

			for i < len(items) && items[i].kind == kindSession {
				if strings.Contains(strings.ToLower(items[i].session.Title), query) {
					if !projectAdded {
						filtered = append(filtered, projectItem)
						projectAdded = true
					}
					if !worktreeAdded {
						filtered = append(filtered, worktreeItem)
						worktreeAdded = true
					}
					filtered = append(filtered, items[i])
				}
				i++
			}
		}
	}
	return filtered
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
