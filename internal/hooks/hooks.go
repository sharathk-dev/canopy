// Package hooks will inject and remove agent CLI hook configuration so the
// daemon can receive structured lifecycle events from running agents.
// Claude hook injection per spec §5.3 — not yet implemented.
package hooks

// Injector manages hook config for a session's working directory.
type Injector interface {
	Inject(sessionID, cwd string) error
	Remove(sessionID string) error
}

// NoopInjector is a placeholder that satisfies the interface without doing anything.
type NoopInjector struct{}

func (NoopInjector) Inject(_, _ string) error { return nil }
func (NoopInjector) Remove(_ string) error     { return nil }
