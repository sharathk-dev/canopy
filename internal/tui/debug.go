package tui

import (
	"io"
	"log"
	"os"
)

var dbg = log.New(io.Discard, "", 0)

// EnableDebug redirects the debug logger to /tmp/canopy-debug.log.
func EnableDebug() {
	f, err := os.OpenFile("/tmp/canopy-debug.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		dbg = log.New(os.Stderr, "DBG ", 0)
		return
	}
	dbg = log.New(f, "DBG ", log.Ltime|log.Lmicroseconds)
}
