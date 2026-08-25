package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
)

// Daemon owns all running sessions and serves the unix socket.
type Daemon struct {
	db       *store.Store
	sessions map[string]*sessionProc
	mu       sync.RWMutex
	sockPath string
}

// New creates a Daemon. Call Run to start accepting connections.
func New(db *store.Store, sockPath string) *Daemon {
	return &Daemon{
		db:       db,
		sessions: make(map[string]*sessionProc),
		sockPath: sockPath,
	}
}

// Run starts the unix socket listener and blocks until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	// Remove stale socket file.
	os.Remove(d.sockPath)

	ln, err := net.Listen("unix", d.sockPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", d.sockPath, err)
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	// Kick off background reconciliation.
	go d.reconcileLoop(ctx)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("accept: %w", err)
			}
		}
		go d.handleConn(conn)
	}
}

// handleConn processes commands from a single client connection.
func (d *Daemon) handleConn(conn net.Conn) {
	defer conn.Close()
	clientID := protocol.NewID()

	for {
		typ, payload, err := protocol.ReadFrame(conn)
		if err != nil {
			return
		}
		if typ != protocol.FrameJSON {
			continue
		}

		var cmd protocol.Cmd
		if err := json.Unmarshal(payload, &cmd); err != nil {
			d.sendErr(conn, "invalid command JSON")
			continue
		}

		switch cmd.Type {
		case protocol.CmdNewSession:
			d.handleNewSession(conn, cmd.Payload)

		case protocol.CmdListSessions:
			d.handleListSessions(conn)

		case protocol.CmdAttach:
			// Streaming mode: blocks until client detaches or session ends.
			d.handleAttach(conn, cmd.Payload, clientID)
			return // connection is consumed by the attach loop

		case protocol.CmdKillSession:
			d.handleKillSession(conn, cmd.Payload)

		case protocol.CmdListWorktrees:
			d.handleListWorktrees(conn, cmd.Payload)

		default:
			d.sendErr(conn, fmt.Sprintf("unknown command: %s", cmd.Type))
		}
	}
}

// --- command handlers ---

func (d *Daemon) handleNewSession(conn net.Conn, raw json.RawMessage) {
	var params protocol.NewSessionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		d.sendErr(conn, "invalid new_session params")
		return
	}

	proc, err := startSession(params, d.db)
	if err != nil {
		d.sendErr(conn, fmt.Sprintf("start session: %v", err))
		return
	}

	d.mu.Lock()
	d.sessions[proc.id] = proc
	d.mu.Unlock()

	sess, _ := d.db.GetSession(proc.id)
	d.sendOK(conn, sess)
}

func (d *Daemon) handleListSessions(conn net.Conn) {
	sessions, err := d.db.ListActiveSessions()
	if err != nil {
		d.sendErr(conn, fmt.Sprintf("list sessions: %v", err))
		return
	}
	d.sendOK(conn, sessions)
}

func (d *Daemon) handleAttach(conn net.Conn, raw json.RawMessage, clientID string) {
	var params protocol.AttachParams
	if err := json.Unmarshal(raw, &params); err != nil {
		d.sendErr(conn, "invalid attach params")
		return
	}

	d.mu.RLock()
	proc, ok := d.sessions[params.SessionID]
	d.mu.RUnlock()
	if !ok {
		d.sendErr(conn, "session not found")
		return
	}

	outCh, snap := proc.attach(clientID)
	defer proc.detach(clientID)

	// Send scrollback snapshot so the client sees the current screen.
	if len(snap) > 0 {
		_ = protocol.WriteFrame(conn, protocol.FramePTY, snap)
	}
	d.sendOK(conn, map[string]string{"session_id": params.SessionID})

	// Read client input/resize in a goroutine; write PTY output in the main goroutine.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			typ, payload, err := protocol.ReadFrame(conn)
			if err != nil {
				return
			}
			switch typ {
			case protocol.FramePTY:
				_ = proc.sendInput(payload)
			case protocol.FrameResize:
				var rz protocol.ResizePayload
				if err := json.Unmarshal(payload, &rz); err == nil {
					_ = proc.resize(rz.Rows, rz.Cols)
				}
			}
		}
	}()

	for chunk := range outCh {
		if err := protocol.WriteFrame(conn, protocol.FramePTY, chunk); err != nil {
			break
		}
	}
	<-readDone
}

func (d *Daemon) handleKillSession(conn net.Conn, raw json.RawMessage) {
	var params protocol.KillSessionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		d.sendErr(conn, "invalid kill_session params")
		return
	}

	d.mu.Lock()
	proc, ok := d.sessions[params.SessionID]
	if ok {
		delete(d.sessions, params.SessionID)
	}
	d.mu.Unlock()

	if !ok {
		d.sendErr(conn, "session not found")
		return
	}
	proc.kill()
	d.sendOK(conn, nil)
}

func (d *Daemon) handleListWorktrees(conn net.Conn, raw json.RawMessage) {
	var params struct {
		RepoPath string `json:"repo_path"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || params.RepoPath == "" {
		d.sendErr(conn, "invalid list_worktrees params")
		return
	}
	worktrees, err := d.db.ListWorktreesByRepo(params.RepoPath)
	if err != nil {
		d.sendErr(conn, fmt.Sprintf("list worktrees: %v", err))
		return
	}
	d.sendOK(conn, worktrees)
}

// --- wire helpers ---

func (d *Daemon) sendOK(conn net.Conn, data any) {
	raw, _ := json.Marshal(data)
	resp, _ := json.Marshal(protocol.Response{OK: true, Data: raw})
	_ = protocol.WriteFrame(conn, protocol.FrameJSON, resp)
}

func (d *Daemon) sendErr(conn net.Conn, msg string) {
	resp, _ := json.Marshal(protocol.Response{OK: false, Error: msg})
	_ = protocol.WriteFrame(conn, protocol.FrameJSON, resp)
}
