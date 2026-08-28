package tui

import "github.com/charmbracelet/lipgloss"

type Theme struct {
	Panel      lipgloss.Color
	Border     lipgloss.Color
	Selected   lipgloss.Color
	Text       lipgloss.Color
	Dim        lipgloss.Color
	Key        lipgloss.Color // footer shortcut keys
	Running    lipgloss.Color
	Waiting    lipgloss.Color
	Finished   lipgloss.Color
	Terminated lipgloss.Color
}

var Dark = Theme{
	Panel:      "",        // transparent — inherits terminal background
	Border:     "#2d3348",
	Selected:   "#6366f1",
	Text:       "#e2e8f0",
	Dim:        "#4b5563",
	Key:        "#818cf8",
	Running:    "#fb923c",
	Waiting:    "#f87171",
	Finished:   "#4b5563",
	Terminated: "#ef4444",
}

var (
	Light = Theme{
		Panel:      "#f8fafc",
		Border:     "#cbd5e1",
		Selected:   "#2563eb",
		Text:       "#0f172a",
		Dim:        "#94a3b8",
		Key:        "#1d4ed8",
		Running:    "#ea580c",
		Waiting:    "#dc2626",
		Finished:   "#94a3b8",
		Terminated: "#dc2626",
	}
)

var activeTheme = systemTheme()

// ThemeNames is the cycle order shown in config.
var ThemeNames = []string{"system", "dark", "light"}

func ThemeByName(name string) Theme {
	switch name {
	case "dark":
		return Dark
	case "light":
		return Light
	default: // "system"
		return systemTheme()
	}
}

// System theme uses ANSI color numbers and no explicit background so it
// inherits whatever colors the terminal theme (e.g. Omarchy) defines.
var System = Theme{
	Panel:      "",   // transparent — terminal default background
	Border:     "8",  // ANSI bright-black (dark gray in most themes)
	Selected:   "4",  // ANSI blue
	Text:       "15", // ANSI bright-white
	Dim:        "8",  // ANSI bright-black
	Key:        "12", // ANSI bright-blue
	Running:    "3",  // ANSI yellow
	Waiting:    "1",  // ANSI red
	Finished:   "8",
	Terminated: "1",
}

// systemTheme returns the System theme (transparent bg, ANSI colors).
func systemTheme() Theme {
	return System
}

// SetTheme applies t as the active theme and rebuilds all package-level styles.
func SetTheme(t Theme) {
	activeTheme = t
	colorPanel = t.Panel
	colorBorder = t.Border
	colorSelected = t.Selected
	colorText = t.Text
	colorDim = t.Dim
	colorKey = t.Key
	colorRunning = t.Running
	colorWaiting = t.Waiting
	colorFinished = t.Finished
	colorTerminated = t.Terminated

	headerBase := lipgloss.NewStyle().Foreground(colorText).Padding(0, 1)
	footerBase := lipgloss.NewStyle().Foreground(colorDim).Padding(0, 1)
	if t.Panel != "" {
		headerBase = headerBase.Background(colorPanel)
		footerBase = footerBase.Background(colorPanel)
	}
	styleHeader = headerBase
	styleHeaderBreadcrumb = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	styleFooter = footerBase
	styleFooterKey = lipgloss.NewStyle().Foreground(colorKey).Bold(true)
	stylePanelTitle = lipgloss.NewStyle().Foreground(colorDim).Bold(true).PaddingLeft(1).PaddingBottom(0)
	styleTreeItem = lipgloss.NewStyle().PaddingLeft(1)
	styleTreeSelected = lipgloss.NewStyle().Background(colorSelected).Foreground(lipgloss.Color("#ffffff")).PaddingLeft(1)
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
	styleOutputEmpty = lipgloss.NewStyle().Foreground(colorDim).PaddingLeft(2).PaddingTop(1)
}

// NextThemeName returns the next theme name in the cycle.
func NextThemeName(current string) string {
	for i, n := range ThemeNames {
		if n == current {
			return ThemeNames[(i+1)%len(ThemeNames)]
		}
	}
	return ThemeNames[0]
}
