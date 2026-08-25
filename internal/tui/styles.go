package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorBg        = lipgloss.Color("#1a1a1a")
	colorPanel     = lipgloss.Color("#111111")
	colorBorder    = lipgloss.Color("#333333")
	colorSelected  = lipgloss.Color("#2563eb") // blue
	colorText      = lipgloss.Color("#e2e8f0")
	colorDim       = lipgloss.Color("#64748b")
	colorRunning   = lipgloss.Color("#3b82f6") // blue
	colorWaiting   = lipgloss.Color("#f59e0b") // amber
	colorFinished  = lipgloss.Color("#64748b") // dim
	colorTerminated = lipgloss.Color("#ef4444") // red
	colorGreen     = lipgloss.Color("#22c55e")

	styleHeader = lipgloss.NewStyle().
			Background(colorPanel).
			Foreground(colorText).
			Padding(0, 1)

	styleHeaderBreadcrumb = lipgloss.NewStyle().
				Foreground(colorText).
				Bold(true)

	styleHeaderDim = lipgloss.NewStyle().
			Foreground(colorDim)

	styleFooter = lipgloss.NewStyle().
			Background(colorPanel).
			Foreground(colorDim).
			Padding(0, 1)

	styleFooterKey = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true)

	stylePanelTitle = lipgloss.NewStyle().
			Foreground(colorDim).
			Bold(true).
			PaddingLeft(1).
			PaddingBottom(0)

	styleTreeItem = lipgloss.NewStyle().
			PaddingLeft(1)

	styleTreeSelected = lipgloss.NewStyle().
				Background(colorSelected).
				Foreground(lipgloss.Color("#ffffff")).
				PaddingLeft(1)

	styleStateDot = map[string]lipgloss.Style{
		"running":      lipgloss.NewStyle().Foreground(colorRunning),
		"needs_input":  lipgloss.NewStyle().Foreground(colorWaiting),
		"fresh":        lipgloss.NewStyle().Foreground(colorDim),
		"finished":     lipgloss.NewStyle().Foreground(colorFinished),
		"terminated":   lipgloss.NewStyle().Foreground(colorTerminated),
		"disconnected": lipgloss.NewStyle().Foreground(colorTerminated),
	}

	styleStateLabel = map[string]lipgloss.Style{
		"running":      lipgloss.NewStyle().Foreground(colorRunning),
		"needs_input":  lipgloss.NewStyle().Foreground(colorWaiting),
		"fresh":        lipgloss.NewStyle().Foreground(colorDim),
		"finished":     lipgloss.NewStyle().Foreground(colorFinished),
		"terminated":   lipgloss.NewStyle().Foreground(colorTerminated),
		"disconnected": lipgloss.NewStyle().Foreground(colorTerminated),
	}

	styleDivider = lipgloss.NewStyle().
			Foreground(colorBorder)

	styleOutput = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(1)

	styleOutputEmpty = lipgloss.NewStyle().
				Foreground(colorDim).
				PaddingLeft(2).
				PaddingTop(1)

	styleStatusDot = lipgloss.NewStyle().Foreground(colorGreen)
)

func stateStyle(state string) lipgloss.Style {
	if s, ok := styleStateDot[state]; ok {
		return s
	}
	return lipgloss.NewStyle().Foreground(colorDim)
}

func stateDot(state string) string {
	return stateStyle(state).Render("●")
}

func stateLabel(state string) string {
	label := state
	if state == "needs_input" {
		label = "waiting"
	}
	return stateStyle(state).Render(label)
}
