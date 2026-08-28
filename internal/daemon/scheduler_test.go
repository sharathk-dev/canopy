package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/store"
)

func TestExecuteScheduleRecordsTimeout(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "canopy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	d := New(db, "")
	schedule := protocol.Schedule{
		ID: "schedule-timeout", Name: "timeout", ActionType: "command",
		Action: "sleep 1", Enabled: true,
	}

	d.executeScheduleWithTimeout(context.Background(), schedule, 10*time.Millisecond)

	runs, err := db.ListScheduleRuns(schedule.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	if runs[0].Status != "failed" {
		t.Fatalf("got status %q, want failed", runs[0].Status)
	}
	if !strings.Contains(runs[0].Error, "timed out") {
		t.Fatalf("got error %q, want timeout error", runs[0].Error)
	}
}
