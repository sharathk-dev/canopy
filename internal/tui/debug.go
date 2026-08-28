package tui

import "github.com/sharathk-dev/canopy/internal/dbg"

// EnableDebug turns on debug logging for the TUI and daemon (delegates to shared dbg package).
func EnableDebug() {
	dbg.Enable()
}

func tuiLog(format string, args ...any) {
	dbg.Log("TUI", format, args...)
}
