package daemon

import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/sharathk-dev/canopy/internal/hooks"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
)

const subChanSize = 256

// sessionProc is the daemon's live handle on one running PTY process.
type sessionProc struct {
	id        string
	hookToken string // bearer token for Claude hook authentication

	ptmx *os.File       // PTY master
	term vt10x.Terminal // terminal emulator for scrollback
	mu   sync.Mutex     // protects term + subs

	// subs maps client-id → outbound channel of raw PTY bytes.
	// Channels are non-blocking: frames are dropped when full rather than blocking the reader.
	subs map[string]chan []byte

	done    chan struct{} // closed when the process exits
	exitErr error
}

// startSession spawns the agent binary in a PTY, registers it in the store,
// and injects lifecycle hooks if the tool supports them.
func startSession(params protocol.NewSessionParams, db *store.Store, injector hooks.Injector) (*sessionProc, error) {
	var args []string
	switch params.Tool {
	case "claude":
		args = []string{"claude"}
	case "codex":
		args = []string{"codex"}
	default:
		args = []string{os.Getenv("SHELL")}
		if args[0] == "" {
			args = []string{"/bin/bash"}
		}
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = params.CWD
	if cmd.Dir == "" {
		cmd.Dir, _ = os.Getwd()
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	proc := &sessionProc{
		id:        protocol.NewID(),
		hookToken: protocol.NewID(), // unique bearer token for this session's hooks
		ptmx:      ptmx,
		subs:      make(map[string]chan []byte),
		done:      make(chan struct{}),
		// vt10x.New takes (cols, rows); default 80×24 until the client sends a resize.
		term: vt10x.New(vt10x.WithSize(80, 24)),
	}

	sess := protocol.Session{
		ID:         proc.id,
		WorktreeID: params.WorktreeID,
		Kind:       "agent",
		Tool:       params.Tool,
		CWD:        cmd.Dir,
		State:      protocol.StateRunning,
		StartedAt:  time.Now(),
	}
	if err := db.CreateSession(sess); err != nil {
		ptmx.Close()
		return nil, err
	}

	// Inject lifecycle hooks so Claude reports state transitions to the daemon.
	if injector != nil {
		if err := injector.Inject(proc.id, cmd.Dir, proc.hookToken); err != nil {
			// Non-fatal: hooks are best-effort; the session still runs.
			log.Printf("hook inject %s: %v", proc.id, err)
		}
	}

	go proc.readLoop(cmd, db)
	return proc, nil
}

// readLoop reads from the PTY master, feeds vt10x, and fans out to subscribers.
func (p *sessionProc) readLoop(cmd *exec.Cmd, db *store.Store) {
	defer close(p.done)

	// Feed the PTY through a bufio.Reader so vt10x.Terminal.Parse can use it.
	br := bufio.NewReader(p.ptmx)
	for {
		// Parse consumes one "frame" of terminal sequences from the reader.
		// It blocks until data is available and releases the lock when the buffer empties.
		if err := p.term.Parse(br); err != nil {
			break
		}

		// Snapshot the updated screen as text and fan out to subscribers.
		snap := []byte(p.term.String())
		p.mu.Lock()
		for _, ch := range p.subs {
			select {
			case ch <- snap:
			default: // drop rather than block
			}
		}
		p.mu.Unlock()
	}

	p.ptmx.Close()
	p.exitErr = cmd.Wait()

	state := protocol.StateFinished
	if p.exitErr != nil {
		state = protocol.StateTerminated
	}

	sess, err := db.GetSession(p.id)
	if err == nil {
		sess.State = state
		db.UpdateSession(sess) //nolint:errcheck
	}

	// Drain subscriber channels so clients see the connection close.
	p.mu.Lock()
	for _, ch := range p.subs {
		close(ch)
	}
	p.subs = make(map[string]chan []byte)
	p.mu.Unlock()
}

// attach registers a subscriber. Returns the output channel and a snapshot of
// the current terminal screen as raw bytes (for immediate display on attach).
func (p *sessionProc) attach(clientID string) (<-chan []byte, []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch := make(chan []byte, subChanSize)
	p.subs[clientID] = ch

	// Capture the current screen state so the client sees what's on screen now.
	snap := []byte(p.term.String())
	return ch, snap
}

// detach removes a subscriber.
func (p *sessionProc) detach(clientID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.subs, clientID)
}

// sendInput writes bytes to the PTY master (i.e., the stdin of the child process).
func (p *sessionProc) sendInput(data []byte) error {
	_, err := p.ptmx.Write(data)
	return err
}

// resize propagates a terminal resize to the PTY.
func (p *sessionProc) resize(rows, cols uint16) error {
	p.mu.Lock()
	// vt10x.Resize takes (cols, rows).
	p.term.Resize(int(cols), int(rows))
	p.mu.Unlock()
	return pty.Setsize(p.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// kill closes the PTY master, which sends SIGHUP to the child.
func (p *sessionProc) kill() {
	p.ptmx.Close()
}

// isDone reports whether the process has exited.
func (p *sessionProc) isDone() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}
