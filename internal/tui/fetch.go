package tui

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/sharathk-dev/canopy/internal/protocol"
)

// daemonData holds a full snapshot of daemon state for one render cycle.
type daemonData struct {
	projects  []protocol.Project
	worktrees map[string][]protocol.Worktree // repoPath → []Worktree
	sessions  map[string][]protocol.Session  // worktreeID → []Session
}

// fetchAll pulls projects, worktrees, and sessions from the daemon in one connection burst.
func fetchAll(sockPath string) (daemonData, error) {
	projects, err := fetchProjects(sockPath)
	if err != nil {
		return daemonData{}, fmt.Errorf("projects: %w", err)
	}

	worktrees := make(map[string][]protocol.Worktree)
	for _, p := range projects {
		wts, err := fetchWorktrees(sockPath, p.RepoPath)
		if err != nil {
			continue
		}
		worktrees[p.RepoPath] = wts
	}

	allSessions, err := fetchSessions(sockPath)
	if err != nil {
		return daemonData{}, fmt.Errorf("sessions: %w", err)
	}

	// Index sessions by worktree ID.
	sessionsByWT := make(map[string][]protocol.Session)
	for _, s := range allSessions {
		sessionsByWT[s.WorktreeID] = append(sessionsByWT[s.WorktreeID], s)
	}

	return daemonData{
		projects:  projects,
		worktrees: worktrees,
		sessions:  sessionsByWT,
	}, nil
}

func fetchProjects(sockPath string) ([]protocol.Project, error) {
	raw, err := rpc(sockPath, protocol.Cmd{Type: protocol.CmdListProjects})
	if err != nil {
		return nil, err
	}
	var out []protocol.Project
	return out, json.Unmarshal(raw, &out)
}

func fetchWorktrees(sockPath, repoPath string) ([]protocol.Worktree, error) {
	p, _ := json.Marshal(map[string]string{"repo_path": repoPath})
	raw, err := rpc(sockPath, protocol.Cmd{Type: protocol.CmdListWorktrees, Payload: p})
	if err != nil {
		return nil, err
	}
	var out []protocol.Worktree
	return out, json.Unmarshal(raw, &out)
}

func fetchSessions(sockPath string) ([]protocol.Session, error) {
	raw, err := rpc(sockPath, protocol.Cmd{Type: protocol.CmdListSessions})
	if err != nil {
		return nil, err
	}
	var out []protocol.Session
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []protocol.Session{}
	}
	return out, nil
}

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

// rpc sends one JSON command over a fresh unix socket connection and returns the response Data.
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
