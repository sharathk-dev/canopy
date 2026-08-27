// Package hooks manages agent CLI hook configuration so the daemon receives
// structured lifecycle events from running agents.
package hooks

// Injector manages hook config for a session's working directory.
type Injector interface {
	// Inject writes canopy-managed hooks into the agent's settings file.
	Inject(sessionID, cwd string) error
}

// NoopInjector satisfies the interface without doing anything.
// Used for non-Claude tools or when hook injection is disabled.
type NoopInjector struct{}

func (NoopInjector) Inject(_, _ string) error { return nil }
