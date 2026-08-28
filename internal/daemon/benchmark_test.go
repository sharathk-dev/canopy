package daemon

import (
	"sync"
	"testing"

	"github.com/hinshun/vt10x"
)

func BenchmarkRenderTermANSI(b *testing.B) {
	term := vt10x.New(vt10x.WithSize(120, 40))
	for i := 0; i < 40; i++ {
		_, _ = term.Write([]byte("\x1b[38;5;110mCanopy session output with ANSI styling\x1b[0m\n"))
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = renderTermANSI(term)
	}
}

func benchmarkProc(cols, rows int) *sessionProc {
	term := vt10x.New(vt10x.WithSize(cols, rows))
	for i := 0; i < rows; i++ {
		_, _ = term.Write([]byte("\x1b[38;5;110mCanopy session output with ANSI styling\x1b[0m\n"))
	}
	return &sessionProc{
		term: term,
		subs: make(map[string]chan []byte),
		done: make(chan struct{}),
		mu:   sync.Mutex{},
	}
}

// BenchmarkSnapshotChanged measures the full snapshot path when the terminal
// has changed since the last poll (the common case while Claude is active).
func BenchmarkSnapshotChanged(b *testing.B) {
	proc := benchmarkProc(120, 40)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = proc.snapshot()
	}
}

// BenchmarkSnapshotUnchanged measures the snapshot path when the caller
// supplies the current revision. The render still runs; this documents the
// cost of the revision gate as it stands (full render, early discard).
func BenchmarkSnapshotUnchanged(b *testing.B) {
	proc := benchmarkProc(120, 40)
	_, since := proc.snapshot() // capture baseline revision
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, rev := proc.snapshot()
		_ = rev == since // mirrors the revision gate in handleSessionSnapshot
	}
}
