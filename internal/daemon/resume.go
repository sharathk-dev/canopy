package daemon

import (
	"strings"
	"time"

	"github.com/sharathk-dev/canopy/internal/protocol"
)

const resumeFailureCheckWindow = 2 * time.Second

// looksLikeResumeFailure recognizes the stable error wording Claude uses when
// a native conversation cannot be resumed. It intentionally requires resume-
// related wording so an unrelated Claude error does not start a fresh session.
func looksLikeResumeFailure(snapshot string) bool {
	text := strings.ToLower(snapshot)
	if !strings.Contains(text, "resume") && !strings.Contains(text, "conversation") {
		return false
	}
	for _, phrase := range []string{
		"no conversation found",
		"conversation not found",
		"session not found",
		"invalid session",
		"could not resume",
		"failed to resume",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// watchResumeFailure waits briefly for a restored Claude process to report an
// invalid native session. It does not inspect or alter ordinary new sessions.
func (d *Daemon) watchResumeFailure(proc *sessionProc) {
	timer := time.NewTimer(resumeFailureCheckWindow)
	defer timer.Stop()
	select {
	case <-timer.C:
		return
	case <-proc.done:
	}

	snapshot, _ := proc.snapshot()
	if !looksLikeResumeFailure(snapshot) {
		return
	}

	sess, err := d.db.GetSession(proc.id)
	if err != nil || sess.CLISessionID == "" {
		return
	}
	// Clear the unusable provider ID before restarting so the next daemon
	// restart does not retry the same invalid conversation forever.
	sess.CLISessionID = ""
	sess.State = protocol.StateFresh
	sess.Archived = false
	newProc, err := restoreSession(sess, d.db, d.injector)
	if err != nil {
		return
	}

	d.mu.Lock()
	if current, ok := d.sessions[proc.id]; ok && current == proc {
		d.sessions[proc.id] = newProc
	}
	d.mu.Unlock()
}
