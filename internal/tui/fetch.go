package tui

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
)

// daemonData holds a full snapshot of state for one render cycle.
type daemonData struct {
	projects  []protocol.Project
	worktrees map[string][]protocol.Worktree // repoPath → []Worktree
	sessions  map[string][]protocol.Session  // worktreeID → []Session
	schedules []protocol.Schedule
	runs      map[string][]protocol.ScheduleRun // scheduleID → recent runs
	config    protocol.Config
}

// fetchAll reads projects, worktrees, and sessions directly from SQLite.
func fetchAll(dbPath string) (daemonData, error) {
	db, err := store.Open(dbPath)
	if err != nil {
		return daemonData{}, err
	}
	defer db.Close()

	projects, err := db.ListProjects()
	if err != nil {
		return daemonData{}, err
	}

	worktrees := make(map[string][]protocol.Worktree)
	for _, p := range projects {
		wts, _ := db.ListWorktreesByRepo(p.RepoPath)
		worktrees[p.RepoPath] = wts
	}

	allSessions, err := db.ListActiveSessions()
	if err != nil {
		return daemonData{}, err
	}

	sessionsByWT := make(map[string][]protocol.Session)
	for _, s := range allSessions {
		sessionsByWT[s.WorktreeID] = append(sessionsByWT[s.WorktreeID], s)
	}

	schedules, err := db.ListSchedules()
	if err != nil {
		return daemonData{}, err
	}
	runsBySchedule := make(map[string][]protocol.ScheduleRun)
	for _, schedule := range schedules {
		runs, _ := db.ListScheduleRuns(schedule.ID, 1)
		runsBySchedule[schedule.ID] = runs
	}

	config, _ := db.LoadConfig()

	return daemonData{
		projects:  projects,
		worktrees: worktrees,
		sessions:  sessionsByWT,
		schedules: schedules,
		runs:      runsBySchedule,
		config:    config,
	}, nil
}

// fetchSnapshot gets the current PTY snapshot for a session from the daemon.
func fetchSnapshot(sockPath, sessionID string) (string, error) {
	p, _ := json.Marshal(protocol.SnapshotParams{SessionID: sessionID})
	raw, err := rpc(sockPath, protocol.Cmd{Type: protocol.CmdSessionSnapshot, Payload: p})
	if err != nil {
		return "", err
	}
	var resp protocol.SnapshotResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	return resp.Text, nil
}

// rpc sends one command to the daemon over a fresh connection and returns Data.
func rpc(sockPath string, cmd protocol.Cmd) (json.RawMessage, error) {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("dial daemon: %w", err)
	}
	defer conn.Close()

	payload, _ := json.Marshal(cmd)
	if err := protocol.WriteFrame(conn, protocol.FrameJSON, payload); err != nil {
		return nil, err
	}

	_, data, err := protocol.ReadFrame(conn)
	if err != nil {
		return nil, err
	}
	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Data, nil
}

func isDaemonDown(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "no such file") ||
		strings.Contains(s, "connect:")
}
