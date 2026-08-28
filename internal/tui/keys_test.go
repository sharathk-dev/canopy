package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyToBytes(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{name: "runes", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")}, want: "abc"},
		{name: "enter", msg: tea.KeyMsg{Type: tea.KeyEnter}, want: "\r"},
		{name: "backspace", msg: tea.KeyMsg{Type: tea.KeyBackspace}, want: "\x7f"},
		{name: "up", msg: tea.KeyMsg{Type: tea.KeyUp}, want: "\x1b[A"},
		{name: "shift tab", msg: tea.KeyMsg{Type: tea.KeyShiftTab}, want: "\x1b[Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(keyToBytes(tt.msg)); got != tt.want {
				t.Fatalf("keyToBytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKeyToBytesDoesNotForwardUnknownKeys(t *testing.T) {
	if got := keyToBytes(tea.KeyMsg{Type: tea.KeyCtrlQ}); got != nil {
		t.Fatalf("ctrl+q produced %q, want nil", got)
	}
}
