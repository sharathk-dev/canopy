package daemon

import "testing"

func TestLooksLikeResumeFailure(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "missing conversation", text: "No conversation found for this session", want: true},
		{name: "invalid session", text: "Error: invalid session id; could not resume", want: true},
		{name: "ordinary error", text: "Error: unable to connect to the network", want: false},
		{name: "normal prompt", text: "Welcome back. Type a message to continue.", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeResumeFailure(tt.text); got != tt.want {
				t.Fatalf("looksLikeResumeFailure() = %v, want %v", got, tt.want)
			}
		})
	}
}
