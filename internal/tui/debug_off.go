//go:build !debug

package tui

import (
	"io"
	"log"
)

var dbg = log.New(io.Discard, "", 0)
