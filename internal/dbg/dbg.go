package dbg

import (
	"io"
	"log"
	"os"
	"sync"
)

const LogPath = "/tmp/canopy-debug.log"

var (
	mu      sync.Mutex
	logger  = log.New(io.Discard, "", 0)
	enabled bool
)

// Enable opens (or creates) the debug log file, truncating any prior run.
func Enable() {
	mu.Lock()
	defer mu.Unlock()
	f, err := os.OpenFile(LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		logger = log.New(os.Stderr, "DBG ", 0)
	} else {
		logger = log.New(f, "", log.Ltime|log.Lmicroseconds)
	}
	enabled = true
}

// Enabled reports whether debug logging is active.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled
}

// Log writes a formatted message to the debug log (no-op when disabled).
func Log(prefix, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if !enabled {
		return
	}
	logger.Printf("["+prefix+"] "+format, args...)
}
