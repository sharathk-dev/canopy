package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const pickerHeight = 20

type pickerItem struct {
	path  string
	isGit bool
	label string
}

type picker struct {
	input   string
	items   []pickerItem
	matches []pickerItem
	cursor  int
	offset  int
	height  int
}

func newPicker(height int) picker {
	p := picker{height: height}
	p.items = scanPickerDirs()
	p.filter()
	return p
}

func scanPickerDirs() []pickerItem {
	seen := make(map[string]bool)
	var results []pickerItem

	add := func(path string) {
		abs, err := filepath.Abs(path)
		if err != nil || seen[abs] {
			return
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			return
		}
		seen[abs] = true
		isGit := false
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			isGit = true
		}
		results = append(results, pickerItem{
			path:  abs,
			isGit: isGit,
			label: shortenHome(abs),
		})
	}

	addChildren := func(dir string) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				add(filepath.Join(abs, e.Name()))
			}
		}
	}

	// 1. Current working directory
	if cwd, err := os.Getwd(); err == nil {
		add(cwd)
		// 2. Siblings of cwd
		addChildren(filepath.Dir(cwd))
	}

	// 3. Common locations — scan one level deep
	home, _ := os.UserHomeDir()
	commonDirs := []string{
		filepath.Join(home, "Projects"),
		filepath.Join(home, "Developer"),
		filepath.Join(home, "dev"),
		filepath.Join(home, "Code"),
		filepath.Join(home, "workspace"),
		filepath.Join(home, "src"),
	}
	for _, d := range commonDirs {
		addChildren(d)
	}

	// Sort: git repos first, then alphabetically by label
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].isGit != results[j].isGit {
			return results[i].isGit
		}
		return results[i].label < results[j].label
	})

	return results
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

func (p *picker) filter() {
	if p.input == "" {
		p.matches = make([]pickerItem, len(p.items))
		copy(p.matches, p.items)
		return
	}
	q := strings.ToLower(p.input)
	p.matches = p.matches[:0]
	for _, item := range p.items {
		if strings.Contains(strings.ToLower(item.label), q) {
			p.matches = append(p.matches, item)
		}
	}
}

func (p *picker) clampCursor() {
	if len(p.matches) == 0 {
		p.cursor = 0
		p.offset = 0
		return
	}
	if p.cursor >= len(p.matches) {
		p.cursor = len(p.matches) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	// Adjust scroll offset so cursor is visible
	visibleRows := p.height - 4 // input line + separator + hint line + border
	if visibleRows < 1 {
		visibleRows = 1
	}
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+visibleRows {
		p.offset = p.cursor - visibleRows + 1
	}
}

func (p picker) render(width, height int) string {
	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	textStyle := lipgloss.NewStyle().Foreground(colorText)

	innerW := width - 2 // subtract border
	if innerW < 10 {
		innerW = 10
	}

	// Input line
	inputLine := styleFooterKey.Render("> ") + textStyle.Render(p.input) + "█"
	inputLine = lipgloss.NewStyle().Width(innerW).Render(inputLine)

	// Separator
	sep := dimStyle.Render(strings.Repeat("─", innerW))

	// Hint line
	hint := styleFooterKey.Render("enter") + dimStyle.Render(" select   ") +
		styleFooterKey.Render("↑↓") + dimStyle.Render(" navigate   ") +
		styleFooterKey.Render("esc") + dimStyle.Render(" cancel")

	// Visible rows for the list
	// height = total inner height. We use: 1 input + 1 sep + list rows + 1 hint
	listRows := height - 4
	if listRows < 1 {
		listRows = 1
	}

	var listLines []string
	for i := p.offset; i < p.offset+listRows && i < len(p.matches); i++ {
		item := p.matches[i]
		marker := "○ "
		if item.isGit {
			marker = "● "
		}
		if i == p.cursor {
			label := truncate(marker+item.label, innerW-1)
			listLines = append(listLines, styleTreeSelected.Width(innerW).Render(label))
		} else {
			labelMaxW := innerW - len([]rune(marker))
			if labelMaxW < 1 {
				labelMaxW = 1
			}
			line := dimStyle.Render(marker) + textStyle.Render(truncate(item.label, labelMaxW))
			listLines = append(listLines, lipgloss.NewStyle().Width(innerW).Render(line))
		}
	}

	// Pad remaining rows
	for len(listLines) < listRows {
		listLines = append(listLines, strings.Repeat(" ", innerW))
	}

	if len(p.matches) == 0 {
		listLines = []string{}
		for i := 0; i < listRows; i++ {
			if i == 0 {
				listLines = append(listLines, lipgloss.NewStyle().Width(innerW).Foreground(colorDim).Render("  no matches"))
			} else {
				listLines = append(listLines, strings.Repeat(" ", innerW))
			}
		}
	}

	lines := []string{inputLine, sep}
	lines = append(lines, listLines...)
	lines = append(lines, hint)

	return strings.Join(lines, "\n")
}

func (m Model) renderPickerModal() string {
	width := m.width - 4
	if width > 70 {
		width = 70
	}
	if width < 30 {
		width = 30
	}
	height := m.height - 4
	if height > pickerHeight {
		height = pickerHeight
	}
	if height < 8 {
		height = 8
	}

	content := m.picker.render(width, height-2) // -2 for border top/bottom

	modal := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorSelected).
		Width(width-2).
		Height(height-2).
		Render(content)

	// Add title in border by prepending it
	// We'll use a simpler approach: put title above content
	titleStyle := lipgloss.NewStyle().Foreground(colorSelected).Bold(true)
	title := titleStyle.Render(" Add Project ")

	// Reconstruct with title line replacing part of the top border
	lines := strings.Split(modal, "\n")
	if len(lines) > 0 {
		topBorder := lines[0]
		// Insert title into the top border line
		borderRunes := []rune(topBorder)
		titleRunes := []rune(title)
		// Place title after the corner character
		if len(borderRunes) > len(titleRunes)+2 {
			// Build new top: corner + title + remaining border chars
			newTop := string(borderRunes[0]) + title + string(borderRunes[1+len(titleRunes):][:len(borderRunes)-1-len(titleRunes)-1]) + string(borderRunes[len(borderRunes)-1])
			lines[0] = newTop
		}
		modal = strings.Join(lines, "\n")
	}

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}
