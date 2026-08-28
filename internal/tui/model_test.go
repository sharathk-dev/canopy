package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputModePriority(t *testing.T) {
	tests := []struct {
		name  string
		model Model
		want  inputMode
	}{
		{name: "navigation", model: Model{}, want: modeNavigation},
		{name: "modal", model: Model{scheduleAdding: true}, want: modeModal},
		{name: "search takes precedence over modal", model: Model{searching: true, scheduleAdding: true}, want: modeSearch},
		{name: "help takes precedence over search", model: Model{showHelp: true, searching: true}, want: modeHelp},
		{name: "attached session takes precedence over everything", model: Model{sessionLocked: true, showHelp: true, searching: true}, want: modeAttached},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.model.inputMode(); got != tt.want {
				t.Fatalf("inputMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleKeyRoutesHelpToHelpMode(t *testing.T) {
	m := Model{showHelp: true}
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEscape}, nil)
	got := updated.(Model)
	if got.showHelp {
		t.Fatal("expected escape to close help")
	}
}

func TestHandleKeyRoutesCtrlQToAttachedSessionMode(t *testing.T) {
	m := Model{sessionLocked: true, lockedSessionID: "session-1", showHelp: true}
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ}, nil)
	got := updated.(Model)
	if got.sessionLocked || got.lockedSessionID != "" {
		t.Fatalf("expected ctrl+q to leave attached mode, got locked=%v id=%q", got.sessionLocked, got.lockedSessionID)
	}
	if !got.showHelp {
		t.Fatal("expected attached mode to consume ctrl+q without changing help state")
	}
}

func TestHandleKeyKeepsGlobalShortcutsOutOfModal(t *testing.T) {
	m := Model{scheduleAdding: true}
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}, nil)
	got := updated.(Model)
	if cmd != nil {
		t.Fatal("expected modal input not to produce a global command")
	}
	if got.scheduleName != "q" {
		t.Fatalf("expected q to be handled by the modal, got schedule name %q", got.scheduleName)
	}

	_, cmd = got.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC}, nil)
	if cmd != nil {
		t.Fatal("expected ctrl+c not to quit from a modal")
	}
}

func TestSnapshotMsgIgnoresStaleSession(t *testing.T) {
	m := Model{sessionLocked: true, lockedSessionID: "session-current", output: "current"}
	m.viewport.SetContent(m.output)
	updated, _ := m.Update(snapshotMsg{
		sessionID: "session-old", text: "stale", revision: 4, changed: true,
	})
	got := updated.(Model)
	if got.output != "current" {
		t.Fatalf("stale snapshot replaced current output with %q", got.output)
	}
}
