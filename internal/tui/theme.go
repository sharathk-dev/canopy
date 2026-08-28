package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	Panel         lipgloss.Color
	Border        lipgloss.Color
	Selected      lipgloss.Color
	Focus         lipgloss.Color
	SelectionText lipgloss.Color
	Text          lipgloss.Color
	Dim           lipgloss.Color
	Key           lipgloss.Color // footer shortcut keys
	Running       lipgloss.Color
	Waiting       lipgloss.Color
	Finished      lipgloss.Color
	Terminated    lipgloss.Color
}

var Dark = Theme{
	// Rosé Pine default dark palette.
	Panel:         "#191724",
	Border:        "#26233a",
	Selected:      "#31748f",
	Focus:         "#9ccfd8",
	SelectionText: "#191724",
	Text:          "#e0def4",
	Dim:           "#908caa",
	Key:           "#9ccfd8",
	Running:       "#f6c177",
	Waiting:       "#eb6f92",
	Finished:      "#6e6a86",
	Terminated:    "#eb6f92",
}

var (
	Light = Theme{
		// Rosé Pine Dawn palette.
		Panel:         "#faf4ed",
		Border:        "#dfdad9",
		Selected:      "#56949f",
		Focus:         "#286983",
		SelectionText: "#fffaf3",
		Text:          "#575279",
		Dim:           "#797593",
		Key:           "#286983",
		Running:       "#ea9d34",
		Waiting:       "#b4637a",
		Finished:      "#9893a5",
		Terminated:    "#b4637a",
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

// systemTheme resolves the terminal preference to one of the two real visual
// themes. System is only an automatic selector; it is not a third palette.
func systemTheme() Theme {
	if lipgloss.HasDarkBackground() {
		return Dark
	}
	return Light
}

// SetTheme applies t as the active theme and rebuilds all package-level styles.
func SetTheme(t Theme) {
	activeTheme = t
	colorPanel = t.Panel
	colorBorder = t.Border
	colorSelected = t.Selected
	colorFocus = t.Focus
	colorSelectionText = t.SelectionText
	colorText = t.Text
	colorDim = t.Dim
	colorKey = t.Key
	colorRunning = t.Running
	colorWaiting = t.Waiting
	colorFinished = t.Finished
	colorTerminated = t.Terminated

	headerBase := themed(colorText).Padding(0, 1)
	footerBase := themed(colorText).Padding(0, 1)
	if t.Panel != "" {
		headerBase = headerBase.Background(colorPanel)
		footerBase = footerBase.Background(colorPanel)
	}
	styleHeader = headerBase
	styleFooter = footerBase
	styleFooterKey = themed(colorKey).Bold(true)
	stylePanelTitle = themed(colorDim).Bold(true).PaddingLeft(1).PaddingBottom(0)
	styleTreeItem = themed(colorText).PaddingLeft(1)
	styleTreeSelected = lipgloss.NewStyle().Background(colorSelected).Foreground(colorSelectionText).PaddingLeft(1)
	styleStateDot = map[string]lipgloss.Style{
		"running":      lipgloss.NewStyle().Foreground(colorRunning).Background(colorPanel),
		"needs_input":  lipgloss.NewStyle().Foreground(colorWaiting).Background(colorPanel),
		"fresh":        lipgloss.NewStyle().Foreground(colorDim).Background(colorPanel),
		"finished":     lipgloss.NewStyle().Foreground(colorFinished).Background(colorPanel),
		"terminated":   lipgloss.NewStyle().Foreground(colorTerminated).Background(colorPanel),
		"disconnected": lipgloss.NewStyle().Foreground(colorTerminated).Background(colorPanel),
	}
	styleOutputEmpty = themed(colorDim).PaddingLeft(2).PaddingTop(1)
}

// themed preserves the active panel canvas for nested foreground styles.
// Without an explicit background, Lip Gloss resets spans to the terminal
// default, which produces black rectangles in Light mode.
func themed(fg lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(fg).Background(colorPanel)
}

// restorePanelBackground keeps the canvas stable across nested Lip Gloss
// styles. Lip Gloss closes nested styles with a terminal reset, which would
// otherwise expose the terminal default background between colored spans.
func restorePanelBackground(s string) string {
	panel := colorRGB(colorPanel)
	if panel == [3]uint64{} {
		return s
	}
	background := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", panel[0], panel[1], panel[2])
	restore := "\x1b[0m" + background
	s = strings.ReplaceAll(s, "\x1b[0m", restore)
	s = strings.ReplaceAll(s, "\x1b[49m", "\x1b[49m"+background)
	return background + s + "\x1b[0m"
}

// restoreBaseStyle is used for Canopy-owned text-only regions, such as the
// footer. It must not be applied to PTY output because that output owns its
// own foreground colors.
func restoreBaseStyle(s string) string {
	panel := colorRGB(colorPanel)
	text := colorRGB(colorText)
	if panel == [3]uint64{} || text == [3]uint64{} {
		return s
	}
	background := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", panel[0], panel[1], panel[2])
	foreground := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", text[0], text[1], text[2])
	restore := "\x1b[0m" + background + foreground
	s = strings.ReplaceAll(s, "\x1b[0m", restore)
	s = strings.ReplaceAll(s, "\x1b[49m", "\x1b[49m"+background+foreground)
	return background + foreground + s + "\x1b[0m"
}

func colorRGB(color lipgloss.Color) [3]uint64 {
	var rgb [3]uint64
	value := string(color)
	if !strings.HasPrefix(value, "#") || len(value) != 7 {
		return rgb
	}
	var err error
	for i, part := range []string{value[1:3], value[3:5], value[5:7]} {
		rgb[i], err = strconv.ParseUint(part, 16, 8)
		if err != nil {
			return [3]uint64{}
		}
	}
	return rgb
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
