package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/sharathk-dev/canopy/internal/datadir"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
	"github.com/spf13/cobra"
)

var diagnosticsOutput string

type diagnosticsReport struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Version     string                `json:"version"`
	OS          string                `json:"os"`
	Arch        string                `json:"arch"`
	Database    diagnosticsDatabase   `json:"database"`
	Daemon      diagnosticsDaemon     `json:"daemon"`
	Sessions    []diagnosticsSession  `json:"sessions"`
	Schedules   []diagnosticsSchedule `json:"schedules"`
}

type diagnosticsDatabase struct {
	Path      string `json:"path"`
	Projects  int    `json:"projects"`
	Worktrees int    `json:"worktrees"`
}

type diagnosticsDaemon struct {
	Connected   bool  `json:"connected"`
	BinaryMtime int64 `json:"binary_mtime,omitempty"`
}

type diagnosticsSession struct {
	ID          string    `json:"id"`
	WorktreeID  string    `json:"worktree_id"`
	Tool        string    `json:"tool"`
	CWD         string    `json:"cwd"`
	HasNativeID bool      `json:"has_native_id"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	Archived    bool      `json:"archived"`
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"started_at"`
}

type diagnosticsSchedule struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ActionType string `json:"action_type"`
	Cron       string `json:"cron"`
	CWD        string `json:"cwd"`
	Enabled    bool   `json:"enabled"`
}

var diagnosticsCmd = &cobra.Command{
	Use:   "diagnostics",
	Short: "Write a safe diagnostic snapshot",
	RunE:  runDiagnostics,
}

func init() {
	diagnosticsCmd.Flags().StringVarP(&diagnosticsOutput, "output", "o", "", "write JSON to a file instead of stdout")
	rootCmd.AddCommand(diagnosticsCmd)
}

func runDiagnostics(_ *cobra.Command, _ []string) error {
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	projects, err := db.ListProjects()
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	worktreeCount := 0
	for _, project := range projects {
		worktrees, listErr := db.ListWorktreesByRepo(project.RepoPath)
		if listErr == nil {
			worktreeCount += len(worktrees)
		}
	}
	sessions, err := db.ListSessions()
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	schedules, err := db.ListSchedules()
	if err != nil {
		return fmt.Errorf("list schedules: %w", err)
	}

	report := diagnosticsReport{
		GeneratedAt: time.Now().UTC(),
		Version:     version,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Database: diagnosticsDatabase{
			Path: datadir.DBPath(), Projects: len(projects), Worktrees: worktreeCount,
		},
		Daemon:    diagnosticsDaemon{Connected: false},
		Sessions:  make([]diagnosticsSession, 0, len(sessions)),
		Schedules: make([]diagnosticsSchedule, 0, len(schedules)),
	}

	if raw, daemonErr := sendDaemonCmd(protocol.Cmd{Type: protocol.CmdVersion}); daemonErr == nil {
		var response protocol.VersionResponse
		if json.Unmarshal(raw.Data, &response) == nil {
			report.Daemon = diagnosticsDaemon{Connected: true, BinaryMtime: response.BinaryMtime}
		}
	}
	for _, session := range sessions {
		report.Sessions = append(report.Sessions, diagnosticsSession{
			ID: session.ID, WorktreeID: session.WorktreeID, Tool: session.Tool,
			CWD: session.CWD, HasNativeID: session.CLISessionID != "", Title: session.Title,
			State: session.State, Archived: session.Archived, PID: session.PID,
			StartedAt: session.StartedAt,
		})
	}
	for _, schedule := range schedules {
		report.Schedules = append(report.Schedules, diagnosticsSchedule{
			ID: schedule.ID, Name: schedule.Name, ActionType: schedule.ActionType,
			Cron: schedule.Cron, CWD: schedule.CWD, Enabled: schedule.Enabled,
		})
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode diagnostics: %w", err)
	}
	data = append(data, '\n')
	if diagnosticsOutput == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(diagnosticsOutput, data, 0600); err != nil {
		return fmt.Errorf("write diagnostics: %w", err)
	}
	fmt.Printf("diagnostics written to %s\n", diagnosticsOutput)
	return nil
}
