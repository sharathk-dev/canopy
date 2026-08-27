package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/sharathk-dev/canopy/internal/hooks"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
)

type sessionProc struct {
	id        string
	hookToken string
	ptmx      *os.File
	term      vt10x.Terminal
	mu        sync.Mutex
	subs      map[string]chan []byte
	done      chan struct{}
	exitErr   error
}

func startSession(params protocol.NewSessionParams, db *store.Store, injector hooks.Injector) (*sessionProc, error) {
	return startSessionRecord(params, db, injector, nil)
}

// restoreSession recreates a persisted session after the daemon has restarted.
// The Canopy session ID is retained so the TUI still refers to the same row.
func restoreSession(sess protocol.Session, db *store.Store, injector hooks.Injector) (*sessionProc, error) {
	params := protocol.NewSessionParams{
		WorktreeID:   sess.WorktreeID,
		Tool:         sess.Tool,
		CWD:          sess.CWD,
		CLISessionID: sess.CLISessionID,
	}
	return startSessionRecord(params, db, injector, &sess)
}

func startSessionRecord(params protocol.NewSessionParams, db *store.Store, injector hooks.Injector, existing *protocol.Session) (*sessionProc, error) {
	hookToken := protocol.NewID()
	procID := protocol.NewID()

	args := []string{}
	if params.Tool == "claude" && params.CLISessionID != "" {
		args = []string{"--resume", params.CLISessionID}
	}
	cmd := exec.Command(params.Tool, args...)
	cmd.Dir = params.CWD
	// Hook configuration is shared by all Claude processes in a worktree.
	// The environment identifies which Canopy session emitted the hook.
	cmd.Env = append(os.Environ(), "CANOPY_SESSION_ID="+procID)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	rows, cols := uint16(24), uint16(80)
	if params.Rows > 0 {
		rows = params.Rows
	}
	if params.Cols > 0 {
		cols = params.Cols
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})

	if existing != nil {
		procID = existing.ID
		cmd.Env = append(os.Environ(), "CANOPY_SESSION_ID="+procID)
	}
	proc := &sessionProc{
		id:        procID,
		hookToken: hookToken,
		ptmx:      ptmx,
		term:      vt10x.New(vt10x.WithSize(int(cols), int(rows))),
		subs:      make(map[string]chan []byte),
		done:      make(chan struct{}),
	}

	sess := protocol.Session{
		ID:           proc.id,
		WorktreeID:   params.WorktreeID,
		Tool:         params.Tool,
		CWD:          params.CWD,
		CLISessionID: params.CLISessionID,
		Title:        params.Title,
		State:        protocol.StateFresh,
		PID:          cmd.Process.Pid,
		StartedAt:    time.Now(),
	}
	if existing != nil {
		// Preserve user-visible metadata such as the title.
		sess = *existing
		sess.PID = cmd.Process.Pid
		if sess.State == protocol.StateRunning {
			sess.State = protocol.StateFresh
		}
		sess.Archived = false
	}
	var dbErr error
	if existing == nil {
		dbErr = db.CreateSession(sess)
	} else {
		dbErr = db.UpdateSession(sess)
	}
	if dbErr != nil {
		ptmx.Close()
		return nil, dbErr
	}

	_ = injector.Inject(proc.id, params.CWD, hookToken)

	go proc.readLoop()
	go proc.waitLoop(cmd, db, injector)

	return proc, nil
}

func (s *sessionProc) readLoop() {
	defer func() {
		s.mu.Lock()
		for _, ch := range s.subs {
			close(ch)
		}
		s.subs = make(map[string]chan []byte)
		s.mu.Unlock()
		close(s.done)
	}()

	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.mu.Lock()
			_, _ = s.term.Write(chunk)
			for _, ch := range s.subs {
				select {
				case ch <- chunk:
				default:
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
}

func (s *sessionProc) waitLoop(cmd *exec.Cmd, db *store.Store, injector hooks.Injector) {
	err := cmd.Wait()
	s.exitErr = err
	s.ptmx.Close() // causes readLoop to exit

	<-s.done // wait for readLoop to finish and close all subs

	sess, dbErr := db.GetSession(s.id)
	if dbErr == nil {
		if err != nil {
			sess.State = protocol.StateTerminated
		} else {
			sess.State = protocol.StateFinished
		}
		sess.Archived = true
		_ = db.UpdateSession(sess)
		if ci, ok := injector.(hooks.ClaudeInjector); ok {
			_ = ci.RemoveFromCWD(s.id, sess.CWD)
		}
	}
}

func (s *sessionProc) attach(clientID string) (<-chan []byte, []byte) {
	ch := make(chan []byte, 256)
	s.mu.Lock()
	s.subs[clientID] = ch
	snap := []byte(s.term.String())
	s.mu.Unlock()
	return ch, snap
}

func (s *sessionProc) detach(clientID string) {
	s.mu.Lock()
	delete(s.subs, clientID)
	s.mu.Unlock()
}

func (s *sessionProc) sendInput(data []byte) error {
	_, err := s.ptmx.Write(data)
	return err
}

func (s *sessionProc) resize(rows, cols uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

func (s *sessionProc) kill() {
	s.ptmx.Close()
}

func (s *sessionProc) snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return renderTermANSI(s.term)
}

// renderTermANSI converts the vt10x cell grid back to ANSI-colored text so
// that styled content (status bars, colors) is visible in the right panel.
func renderTermANSI(term vt10x.Terminal) string {
	cols, rows := term.Size()
	var sb strings.Builder

	for y := 0; y < rows; y++ {
		if y > 0 {
			sb.WriteString("\x1b[0m\n")
		}

		var curFG, curBG vt10x.Color = vt10x.DefaultFG, vt10x.DefaultBG
		var curMode int16 = 0

		for x := 0; x < cols; x++ {
			g := term.Cell(x, y)

			fg, bg := g.FG, g.BG
			// Honour reverse-video: swap fg/bg for rendering.
			if g.Mode&attrReverse != 0 {
				fg, bg = bg, fg
			}

			if fg != curFG || bg != curBG || g.Mode != curMode {
				sb.WriteString("\x1b[0m")
				if g.Mode&attrBold != 0 {
					sb.WriteString("\x1b[1m")
				}
				if g.Mode&attrItalic != 0 {
					sb.WriteString("\x1b[3m")
				}
				if g.Mode&attrUnderline != 0 {
					sb.WriteString("\x1b[4m")
				}
				sb.WriteString(ansiColor(fg, true))
				sb.WriteString(ansiColor(bg, false))
				curFG, curBG, curMode = fg, bg, g.Mode
			}

			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

// Glyph mode bit positions (matches vt10x internal constants).
const (
	attrReverse   int16 = 1 << 0
	attrUnderline int16 = 1 << 1
	attrBold      int16 = 1 << 2
	attrItalic    int16 = 1 << 4
)

func ansiColor(c vt10x.Color, fg bool) string {
	base, hi, reset := 30, 90, 39
	if !fg {
		base, hi, reset = 40, 100, 49
	}
	switch {
	case c == vt10x.DefaultFG && fg, c == vt10x.DefaultBG && !fg:
		return fmt.Sprintf("\x1b[%dm", reset)
	case uint32(c) < 8:
		return fmt.Sprintf("\x1b[%dm", base+int(c))
	case uint32(c) < 16:
		return fmt.Sprintf("\x1b[%dm", hi+int(c)-8)
	case uint32(c) < 256:
		code := 38
		if !fg {
			code = 48
		}
		return fmt.Sprintf("\x1b[%d;5;%dm", code, uint32(c))
	default:
		// 24-bit truecolor encoded as r<<16|g<<8|b.
		v := uint32(c)
		code := 38
		if !fg {
			code = 48
		}
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", code, (v>>16)&0xFF, (v>>8)&0xFF, v&0xFF)
	}
}
