package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"

	"github.com/sharathk-dev/canopy/internal/hooks"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
)

// Daemon owns all running PTY sessions and serves the unix socket.
type Daemon struct {
	db          *store.Store
	sessions    map[string]*sessionProc
	mu          sync.RWMutex
	sockPath    string
	injector    hooks.Injector
	binaryMtime int64 // mtime of os.Executable() at daemon start
}

func New(db *store.Store, sockPath string) *Daemon {
	var mtime int64
	if exe, err := os.Executable(); err == nil {
		if fi, err := os.Stat(exe); err == nil {
			mtime = fi.ModTime().Unix()
		}
	}
	return &Daemon{
		db:          db,
		sessions:    make(map[string]*sessionProc),
		sockPath:    sockPath,
		injector:    hooks.ClaudeInjector{},
		binaryMtime: mtime,
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	// Clean up any sessions whose processes died since the last daemon run
	// (e.g. after a binary update that forced the previous daemon to restart).
	d.reconcileDeadSessions()

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
		case protocol.CmdVersion:
			d.sendOK(conn, protocol.VersionResponse{BinaryMtime: d.binaryMtime})
		case protocol.CmdNewSession:
			d.handleNewSession(conn, cmd.Payload)
		case protocol.CmdAttach:
			d.handleAttach(conn, cmd.Payload, clientID)
			return
		case protocol.CmdKillSession:
			d.handleKillSession(conn, cmd.Payload)
		case protocol.CmdInput:
			d.handleInput(conn, cmd.Payload)
		case protocol.CmdSessionSnapshot:
			d.handleSessionSnapshot(conn, cmd.Payload)
		case protocol.CmdResizeSession:
			d.handleResizeSession(conn, cmd.Payload)
		default:
			d.sendErr(conn, fmt.Sprintf("unknown command: %s", cmd.Type))
		}
	}
}

func (d *Daemon) handleNewSession(conn net.Conn, raw json.RawMessage) {
	var params protocol.NewSessionParams
	if err := json.Unmarshal(raw, &params); err != nil {
		d.sendErr(conn, "invalid new_session params")
		return
	}

	// Auto-resolve WorktreeID from CWD.
	if params.WorktreeID == "" && params.CWD != "" {
		if wt, err := d.db.GetWorktreeByPath(params.CWD); err == nil {
			params.WorktreeID = wt.ID
		} else if wt, err := d.db.GetWorktreeByPathPrefix(params.CWD); err == nil {
			params.WorktreeID = wt.ID
		}
	}

	injector := hooks.Injector(hooks.NoopInjector{})
	if params.Tool == "claude" {
		injector = d.injector
	}

	proc, err := startSession(params, d.db, injector)
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

	if len(snap) > 0 {
		_ = protocol.WriteFrame(conn, protocol.FramePTY, snap)
	}
	d.sendOK(conn, map[string]string{"session_id": params.SessionID})

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

	if ok {
		proc.kill()
	}

	// Always archive in DB regardless of whether the daemon had it in memory.
	if sess, err := d.db.GetSession(params.SessionID); err == nil {
		sess.State = protocol.StateTerminated
		sess.Archived = true
		_ = d.db.UpdateSession(sess)
	}

	d.sendOK(conn, nil)
}

func (d *Daemon) handleSessionSnapshot(conn net.Conn, raw json.RawMessage) {
	var params protocol.SnapshotParams
	if err := json.Unmarshal(raw, &params); err != nil || params.SessionID == "" {
		d.sendErr(conn, "invalid session_snapshot params")
		return
	}

	d.mu.RLock()
	proc, ok := d.sessions[params.SessionID]
	d.mu.RUnlock()
	if !ok {
		d.sendErr(conn, "session not found")
		return
	}

	d.sendOK(conn, protocol.SnapshotResponse{Text: proc.snapshot()})
}

func (d *Daemon) handleInput(conn net.Conn, raw json.RawMessage) {
	var params protocol.InputParams
	if err := json.Unmarshal(raw, &params); err != nil || params.SessionID == "" {
		d.sendErr(conn, "invalid input params")
		return
	}
	d.mu.RLock()
	proc, ok := d.sessions[params.SessionID]
	d.mu.RUnlock()
	if !ok {
		d.sendErr(conn, "session not found")
		return
	}
	_ = proc.sendInput(params.Data)
	d.sendOK(conn, nil)
}

func (d *Daemon) handleResizeSession(conn net.Conn, raw json.RawMessage) {
	var params protocol.ResizeSessionParams
	if err := json.Unmarshal(raw, &params); err != nil || params.SessionID == "" {
		d.sendErr(conn, "invalid resize_session params")
		return
	}
	d.mu.RLock()
	proc, ok := d.sessions[params.SessionID]
	d.mu.RUnlock()
	if !ok {
		d.sendErr(conn, "session not found")
		return
	}
	_ = proc.resize(params.Rows, params.Cols)
	d.sendOK(conn, nil)
}

func (d *Daemon) sendOK(conn net.Conn, data any) {
	raw, _ := json.Marshal(data)
	resp, _ := json.Marshal(protocol.Response{OK: true, Data: raw})
	_ = protocol.WriteFrame(conn, protocol.FrameJSON, resp)
}

func (d *Daemon) sendErr(conn net.Conn, msg string) {
	resp, _ := json.Marshal(protocol.Response{OK: false, Error: msg})
	_ = protocol.WriteFrame(conn, protocol.FrameJSON, resp)
}

// reconcileDeadSessions marks any "running" session whose PID is no longer
// alive as terminated. This cleans up stale DB state after an unclean shutdown
// (e.g. when the embedded daemon was killed along with the TUI process).
func (d *Daemon) reconcileDeadSessions() {
	sessions, err := d.db.ListActiveSessions()
	if err != nil {
		return
	}
	for _, sess := range sessions {
		if sess.State != protocol.StateRunning && sess.State != protocol.StateNeedsInput {
			continue
		}
		if sess.PID <= 0 {
			continue
		}
		proc, err := os.FindProcess(sess.PID)
		if err != nil {
			// Process doesn't exist (OS-level).
			sess.State = protocol.StateTerminated
			sess.Archived = true
			_ = d.db.UpdateSession(sess)
			continue
		}
		// Signal 0 checks liveness without disturbing the process.
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			sess.State = protocol.StateTerminated
			sess.Archived = true
			_ = d.db.UpdateSession(sess)
		}
	}
}
