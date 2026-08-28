package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func discoverSkills(projectCWD string) []string {
	seen := make(map[string]bool)
	var skills []string
	addRoot := func(root string) {
		entries, err := os.ReadDir(root)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || seen[entry.Name()] {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, entry.Name(), "SKILL.md")); err == nil {
				seen[entry.Name()] = true
				skills = append(skills, entry.Name())
			}
		}
	}
	if projectCWD != "" {
		addRoot(filepath.Join(projectCWD, ".claude", "skills"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		addRoot(filepath.Join(home, ".claude", "skills"))
	}
	sort.Strings(skills)
	return skills
}

func (m Model) filteredSkillOptions() []string {
	query := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(m.scheduleSkill), "/"))
	var options []string
	for _, skill := range m.skillOptions {
		if query == "" || strings.Contains(strings.ToLower(skill), query) {
			options = append(options, skill)
		}
	}
	if m.skillCursor >= len(options) {
		m.skillCursor = 0
	}
	return options
}

func (m Model) modal(title, body string, width int) string {
	if width > m.width-4 {
		width = m.width - 4
	}
	if width < 30 {
		width = 30
	}
	content := themed(colorText).Width(width-2).Padding(1, 2).Render(body)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(colorFocus).Background(colorPanel).Width(width).
		Render(lipgloss.JoinVertical(lipgloss.Left,
			themed(colorFocus).Bold(true).Render(title), content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(lipgloss.NoColor{}))
}

func (m Model) renderTitleModal() string {
	value := m.titleInput + "█"
	if m.titleInput == "" {
		value = "█" + themed(colorDim).Render("session title")
	}
	body := "Title: " + value + "\n\n" +
		styleFooterKey.Render("enter") + " save    " + styleFooterKey.Render("esc") + " cancel"
	return m.modal("Rename session", body, 52)
}

func (m Model) renderNewSessionModal() string {
	value := m.titleInput + "█"
	if m.titleInput == "" {
		value = "█" + themed(colorDim).Render("session title")
	}
	body := "Title (optional): " + value + "\n\n" +
		themed(colorDim).Render("Leave blank to generate a session name.") + "\n\n" +
		styleFooterKey.Render("enter") + " start    " + styleFooterKey.Render("esc") + " cancel"
	return m.modal("New session", body, 60)
}

func (m Model) renderKillModal() string {
	return m.modal("Kill session", "Stop the selected session?\n\n"+
		styleFooterKey.Render("y/enter")+" confirm    "+styleFooterKey.Render("n/esc")+" cancel", 52)
}

func (m Model) renderDeleteModal() string {
	enterStyle := themed(colorDim)
	if m.projectDeleteInput == "DELETE" || m.worktreeDeleteInput == "DELETE" || m.scheduleDeleteInput == "DELETE" {
		enterStyle = styleFooterKey
	}
	if m.projectDeleteID != "" {
		body := fmt.Sprintf("Remove project %q and its registered worktrees?\n\nType DELETE: %s█\n\n%s confirm    %s cancel",
			m.projectDeleteName, m.projectDeleteInput, enterStyle.Render("enter"), styleFooterKey.Render("esc"))
		return m.modal("Remove project", body, 64)
	}
	if m.scheduleDeleteID != "" {
		body := fmt.Sprintf("Remove schedule %q?\n\nType DELETE: %s█\n\n%s confirm    %s cancel",
			m.scheduleDeleteName, m.scheduleDeleteInput, enterStyle.Render("enter"), styleFooterKey.Render("esc"))
		return m.modal("Remove schedule", body, 64)
	}
	body := fmt.Sprintf("Remove worktree at %q?\n\nType DELETE: %s█\n\n%s confirm    %s cancel",
		m.worktreeDeletePath, m.worktreeDeleteInput, enterStyle.Render("enter"), styleFooterKey.Render("esc"))
	return m.modal("Remove worktree", body, 64)
}

func (m Model) renderWorktreeModal() string {
	fields := []struct {
		label string
		value string
		hint  string
	}{
		{"Branch", m.worktreeBranch, "feature/my-task"},
		{"Path (optional)", m.worktreePath, "defaults to a sibling directory"},
	}
	var lines []string
	labelWidth := lipgloss.Width("Path (optional)")
	for i, field := range fields {
		label := fmt.Sprintf("%-*s:", labelWidth, field.label)
		value := field.value
		if i == m.worktreeField {
			if value == "" {
				value = "█" + themed(colorDim).Render(field.hint)
			} else {
				value += "█"
			}
			lines = append(lines, styleFooterKey.Render(label)+" "+value)
		} else {
			if value == "" {
				value = themed(colorDim).Render(field.hint)
			}
			lines = append(lines, themed(colorDim).Render(label+" ")+value)
		}
	}
	enterStyle := styleFooterKey
	if strings.TrimSpace(m.worktreeBranch) == "" {
		enterStyle = themed(colorDim)
	}
	lines = append(lines, "", styleFooterKey.Render("tab")+" next    "+
		enterStyle.Render("enter")+" create    "+styleFooterKey.Render("esc")+" cancel")
	return m.modal("Add worktree", strings.Join(lines, "\n"), 78)
}
