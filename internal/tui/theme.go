package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// rendererProfile is forced rather than auto-detected: terminal color-profile
// detection (and lipgloss.HasDarkBackground's OSC 11 query in particular) is
// unreliable across common terminals — notably iTerm2 and Terminal.app on
// macOS — and can also race with Bubble Tea's own stdin reader once the
// program has taken over the terminal. ANSI256 is supported virtually
// everywhere, so forcing it trades true-color fidelity for colors that
// actually render.
var rendererProfile = termenv.ANSI256

func init() {
	lipgloss.SetColorProfile(rendererProfile)
}

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

var activeTheme = Dark

// ThemeNames is the cycle order shown in config.
var ThemeNames = []string{"dark", "light"}

// ThemeByName resolves a theme name to a palette. Unrecognized names
// (including the retired "system" auto-detect option) fall back to Dark.
func ThemeByName(name string) Theme {
	switch name {
	case "light":
		return Light
	default: // "dark", "system" (legacy), unknown
		return Dark
	}
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

// ansiSequence renders c through the forced rendererProfile, so manual
// escape injection always matches what the rest of the app emits — a
// terminal that can't render raw 24-bit truecolor codes (Terminal.app, most
// notably) still gets a downsampled color instead of nothing.
func ansiSequence(c lipgloss.Color, bg bool) string {
	value := string(c)
	if value == "" {
		return ""
	}
	return "\x1b[" + rendererProfile.Color(value).Sequence(bg) + "m"
}

// restorePanelBackground keeps the canvas stable across nested Lip Gloss
// styles. Lip Gloss closes nested styles with a terminal reset, which would
// otherwise expose the terminal default background between colored spans.
func restorePanelBackground(s string) string {
	background := ansiSequence(colorPanel, true)
	if background == "" {
		return s
	}
	restore := "\x1b[0m" + background
	s = strings.ReplaceAll(s, "\x1b[0m", restore)
	s = strings.ReplaceAll(s, "\x1b[49m", "\x1b[49m"+background)
	return background + s + "\x1b[0m"
}

// restoreBaseStyle is used for Canopy-owned text-only regions, such as the
// footer. It must not be applied to PTY output because that output owns its
// own foreground colors.
func restoreBaseStyle(s string) string {
	background := ansiSequence(colorPanel, true)
	foreground := ansiSequence(colorText, false)
	if background == "" || foreground == "" {
		return s
	}
	restore := "\x1b[0m" + background + foreground
	s = strings.ReplaceAll(s, "\x1b[0m", restore)
	s = strings.ReplaceAll(s, "\x1b[49m", "\x1b[49m"+background+foreground)
	return background + foreground + s + "\x1b[0m"
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
