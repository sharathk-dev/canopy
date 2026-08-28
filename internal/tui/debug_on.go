//go:build debug

package tui

import (
	"log"
	"os"
)

var dbg *log.Logger

func init() {
	f, err := os.OpenFile("/tmp/canopy-debug.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		dbg = log.New(os.Stderr, "DBG ", 0)
		return
	}
	dbg = log.New(f, "DBG ", log.Ltime|log.Lmicroseconds)
}
