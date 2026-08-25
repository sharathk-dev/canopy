// Package tui will contain the bubbletea TUI for canopy.
// The full TUI implementation is planned for a later milestone.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Model is the top-level bubbletea model.
type Model struct {
	sockPath string
}

// New creates a new TUI model connected to the given daemon socket.
func New(sockPath string) Model {
	return Model{sockPath: sockPath}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() string {
	return "Canopy TUI — not yet implemented. Press q to quit.\n"
}
