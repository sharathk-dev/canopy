package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/sharathk-dev/canopy/internal/git"
	"github.com/sharathk-dev/canopy/internal/hooks"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
)

// Daemon owns all running PTY sessions and serves the unix socket.
type Daemon struct {
	db            *store.Store
	sessions      map[string]*sessionProc
	mu            sync.RWMutex
	sockPath      string
	injector      hooks.Injector
	binaryMtime   int64 // mtime of os.Executable() at daemon start
	scheduleQueue chan protocol.Schedule
	inFlight      sync.Map // scheduleID → struct{}: currently queued or running
}

func New(db *store.Store, sockPath string) *Daemon {
	var mtime int64
	if exe, err := os.Executable(); err == nil {
		if fi, err := os.Stat(exe); err == nil {
			mtime = fi.ModTime().UnixNano()
		}
	}
	cfg, _ := db.LoadConfig()
	return &Daemon{
		db:            db,
		sessions:      make(map[string]*sessionProc),
		sockPath:      sockPath,
		injector:      hooks.ClaudeInjector{},
		binaryMtime:   mtime,
		scheduleQueue: make(chan protocol.Schedule, cfg.MaxSchedulerQueueSize),
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	d.reconcileWorktrees()
	// Recreate persisted sessions before accepting clients. This gives Canopy
	// browser-like restore semantics when the daemon or TUI is relaunched.
	d.restoreSessions()

	cfg, _ := d.db.LoadConfig()
	for i := 0; i < cfg.MaxSchedulerConcurrency; i++ {
		go d.scheduleWorker(ctx)
	}
	go d.scheduleLoop(ctx)

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

// reconcileWorktrees makes the database follow Git's authoritative worktree
// list without deleting historical rows. Missing worktrees are hidden by the
// store and become visible again if their path returns.
func (d *Daemon) reconcileWorktrees() {
	projects, err := d.db.ListProjects()
	if err != nil {
		return
	}
	for _, project := range projects {
		gitWorktrees, err := git.ListWorktrees(project.RepoPath)
		if err != nil {
			continue
		}
		existing, _ := d.db.ListWorktreesByRepo(project.RepoPath)
		seen := make(map[string]bool)
		for _, info := range gitWorktrees {
			if info.IsBare {
				continue
			}
			seen[info.Path] = true
			wt, lookupErr := d.db.GetWorktreeByRepoAndPath(project.RepoPath, info.Path)
			if lookupErr != nil {
				wt.ID = protocol.NewID()
			}
			wt.RepoPath = project.RepoPath
			wt.ProjectID = project.ID
			wt.Path = info.Path
			wt.Branch = info.Branch
			wt.IsMain = info.IsMain
			_ = d.db.UpsertWorktree(wt)
		}
		for _, wt := range existing {
			if !seen[wt.Path] {
				_ = d.db.MarkWorktreeMissing(wt.ID, true)
			}
		}
	}
}

func (d *Daemon) restoreSessions() {
	sessions, err := d.db.ListActiveSessions()
	if err != nil {
		return
	}

	for _, sess := range sessions {
		missingWorktree, worktreeErr := d.db.IsWorktreeMissing(sess.WorktreeID)
		if _, err := os.Stat(sess.CWD); err != nil || (worktreeErr == nil && missingWorktree) {
			sess.State = protocol.StateDisconnected
			_ = d.db.UpdateSession(sess)
			continue
		}
		// A previous daemon may have left the child process alive. Stop it before
		// replacing it, otherwise restoring would create duplicate Claude tabs.
		if sess.PID > 0 {
			if old, err := os.FindProcess(sess.PID); err == nil {
				_ = old.Signal(syscall.SIGTERM)
			}
		}

		injector := hooks.Injector(hooks.NoopInjector{})
		if sess.Tool == "claude" {
			injector = d.injector
		}
		proc, err := restoreSession(sess, d.db, injector)
		if err != nil {
			sess.State = protocol.StateTerminated
			sess.Archived = true
			_ = d.db.UpdateSession(sess)
			continue
		}
		d.mu.Lock()
		d.sessions[proc.id] = proc
		d.mu.Unlock()
		if sess.Tool == "claude" && sess.CLISessionID != "" {
			go d.watchResumeFailure(proc)
		}
	}

	// Also clean hooks for sessions archived by an earlier daemon run.
	d.reconcileDeadSessions()
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
		case protocol.CmdUpdateTitle:
			d.handleUpdateTitle(conn, cmd.Payload)
		case protocol.CmdRunSchedule:
			d.handleRunSchedule(conn, cmd.Payload)
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
		if ci, ok := d.injector.(hooks.ClaudeInjector); ok {
			_ = ci.RemoveFromCWD(sess.ID, sess.CWD)
		}
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

	text, revision := proc.snapshot()
	if params.SinceRevision != 0 && params.SinceRevision == revision {
		d.sendOK(conn, protocol.SnapshotResponse{Revision: revision, Changed: false})
		return
	}
	d.sendOK(conn, protocol.SnapshotResponse{Text: text, Revision: revision, Changed: true})
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

func (d *Daemon) handleUpdateTitle(conn net.Conn, raw json.RawMessage) {
	var params protocol.UpdateTitleParams
	if err := json.Unmarshal(raw, &params); err != nil || params.SessionID == "" {
		d.sendErr(conn, "invalid update_title params")
		return
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		d.sendErr(conn, "title cannot be empty")
		return
	}
	sess, err := d.db.GetSession(params.SessionID)
	if err != nil {
		d.sendErr(conn, "session not found")
		return
	}
	sess.Title = title
	if err := d.db.UpdateSession(sess); err != nil {
		d.sendErr(conn, fmt.Sprintf("save title: %v", err))
		return
	}
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
	// Archived sessions can still have hooks left behind if the daemon was
	// stopped before waitLoop performed its cleanup. Remove those first.
	if sessions, err := d.db.ListSessions(); err == nil {
		if ci, ok := d.injector.(hooks.ClaudeInjector); ok {
			for _, sess := range sessions {
				if sess.Archived {
					_ = ci.RemoveFromCWD(sess.ID, sess.CWD)
				}
			}
		}
	}

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
			if ci, ok := d.injector.(hooks.ClaudeInjector); ok {
				_ = ci.RemoveFromCWD(sess.ID, sess.CWD)
			}
			continue
		}
		// Signal 0 checks liveness without disturbing the process.
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			sess.State = protocol.StateTerminated
			sess.Archived = true
			_ = d.db.UpdateSession(sess)
			if ci, ok := d.injector.(hooks.ClaudeInjector); ok {
				_ = ci.RemoveFromCWD(sess.ID, sess.CWD)
			}
		}
	}
}
