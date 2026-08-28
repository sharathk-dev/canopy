package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var animationFrame uint64

var (
	colorPanel      = lipgloss.Color("#111111")
	colorBorder     = lipgloss.Color("#4a5568") // visible but not distracting
	colorSelected   = lipgloss.Color("#3b82f6") // blue
	colorText       = lipgloss.Color("#e2e8f0")
	colorDim        = lipgloss.Color("#64748b")
	colorRunning    = lipgloss.Color("#f97316") // orange
	colorWaiting    = lipgloss.Color("#f87171") // coral red
	colorFinished   = lipgloss.Color("#64748b") // dim
	colorTerminated = lipgloss.Color("#ef4444") // red

	styleHeader = lipgloss.NewStyle().
			Background(colorPanel).
			Foreground(colorText).
			Padding(0, 1)

	styleHeaderBreadcrumb = lipgloss.NewStyle().
				Foreground(colorText).
				Bold(true)

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

	styleOutputEmpty = lipgloss.NewStyle().
				Foreground(colorDim).
				PaddingLeft(2).
				PaddingTop(1)
)

func stateStyle(state string) lipgloss.Style {
	if s, ok := styleStateDot[state]; ok {
		return s
	}
	return lipgloss.NewStyle().Foreground(colorDim)
}

func stateDot(state string) string {
	return stateStyle(state).Render(stateGlyph(state))
}

func selectedStateDot(state string) string {
	r, g, b := 148, 163, 184 // dim gray
	switch state {
	case "running":
		r, g, b = 249, 115, 22
	case "needs_input":
		r, g, b = 248, 113, 113
	case "finished":
		r, g, b = 100, 116, 139
	case "terminated", "disconnected":
		r, g, b = 239, 68, 68
	}
	// Restore only the foreground after the glyph; resetting all attributes
	// would also clear the selected row's blue background.
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[38;2;255;255;255m", r, g, b, stateGlyph(state))
}

func stateGlyph(state string) string {
	if state == "running" {
		frames := []string{"✳", "✽", "✻", "✺"}
		return frames[animationFrame%uint64(len(frames))]
	}
	return "●"
}

func stateLabel(state string) string {
	return stateStyle(state).Render(stateText(state))
}

func stateText(state string) string {
	switch state {
	case "fresh":
		return "idle"
	case "running":
		return "working"
	case "needs_input":
		return "waiting"
	default:
		return state
	}
}

// truncate shortens s to maxW runes, appending … if cut.
func truncate(s string, maxW int) string {
	runes := []rune(s)
	if len(runes) <= maxW {
		return s
	}
	if maxW <= 1 {
		return "…"
	}
	return string(runes[:maxW-1]) + "…"
}

// timeAgo returns a compact human-readable duration since t.
func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
