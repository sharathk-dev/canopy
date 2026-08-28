package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var animationFrame uint64

var (
	colorPanel      = activeTheme.Panel
	colorBorder     = activeTheme.Border
	colorSelected   = activeTheme.Selected
	colorText       = activeTheme.Text
	colorDim        = activeTheme.Dim
	colorKey        = activeTheme.Key
	colorRunning    = activeTheme.Running
	colorWaiting    = activeTheme.Waiting
	colorFinished   = activeTheme.Finished
	colorTerminated = activeTheme.Terminated

	styleHeader = lipgloss.NewStyle().
			Background(colorPanel).
			Foreground(colorText).
			Padding(0, 1)

	styleFooter = lipgloss.NewStyle().
			Background(colorPanel).
			Foreground(colorDim).
			Padding(0, 1)

	styleFooterKey = lipgloss.NewStyle().
			Foreground(colorKey).
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
	r, g, b := 148, 163, 184
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
