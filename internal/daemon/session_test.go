package daemon

import (
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
)

func TestRenderTermANSIProducesTextAndReset(t *testing.T) {
	term := vt10x.New(vt10x.WithSize(12, 2))
	if _, err := term.Write([]byte("hello\x1b[31m red")); err != nil {
		t.Fatal(err)
	}

	output := renderTermANSI(term)
	if !strings.Contains(output, "hello") || !strings.Contains(output, "red") {
		t.Fatalf("rendered output %q does not contain terminal text", output)
	}
	if !strings.HasSuffix(output, "\x1b[0m") {
		t.Fatalf("rendered output %q does not end with an ANSI reset", output)
	}
}

func TestTerminalResizeChangesSnapshotDimensions(t *testing.T) {
	term := vt10x.New(vt10x.WithSize(80, 24))
	term.Resize(40, 10)
	cols, rows := term.Size()
	if cols != 40 || rows != 10 {
		t.Fatalf("terminal size = %dx%d, want 40x10", cols, rows)
	}
}
