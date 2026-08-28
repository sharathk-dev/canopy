package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/scheduler"
)

func (d *Daemon) scheduleLoop(ctx context.Context) {
	d.runDueSchedules(ctx, time.Now())
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			d.runDueSchedules(ctx, now)
		}
	}
}

func (d *Daemon) runDueSchedules(ctx context.Context, now time.Time) {
	schedules, err := d.db.ListSchedules()
	if err != nil {
		return
	}
	minute := now.Truncate(time.Minute)
	for _, schedule := range schedules {
		if !schedule.Enabled {
			continue
		}
		cron, err := scheduler.ParseCron(schedule.Cron)
		if err != nil || !cron.Matches(minute) {
			continue
		}
		claimed, err := d.db.ClaimSchedule(schedule.ID, minute)
		if err == nil && claimed {
			go d.executeSchedule(ctx, schedule)
		}
	}
}

func (d *Daemon) executeSchedule(ctx context.Context, schedule protocol.Schedule) {
	started := time.Now()
	run := protocol.ScheduleRun{
		ID:         protocol.NewID(),
		ScheduleID: schedule.ID,
		StartedAt:  started,
		Status:     "running",
	}
	if err := d.db.CreateScheduleRun(run); err != nil {
		return
	}

	var cmd *exec.Cmd
	switch schedule.ActionType {
	case "skill":
		prompt := "/" + strings.TrimPrefix(strings.TrimSpace(schedule.Action), "/")
		cmd = exec.CommandContext(ctx, "claude", "-p", prompt, "--output-format", "json")
	case "command":
		cmd = exec.CommandContext(ctx, "sh", "-c", schedule.Action)
	default:
		run.Status = "failed"
		run.Error = fmt.Sprintf("unsupported action type: %s", schedule.ActionType)
		run.FinishedAt = time.Now()
		_ = d.db.FinishScheduleRun(run)
		return
	}
	if schedule.CWD != "" {
		cmd.Dir = schedule.CWD
	}

	output, err := cmd.CombinedOutput()
	run.FinishedAt = time.Now()
	run.Output = string(output)
	if schedule.ActionType == "skill" {
		parseClaudeScheduleOutput(&run, output)
	}
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
	} else {
		run.Status = "success"
	}
	_ = d.db.FinishScheduleRun(run)
}

type claudeScheduleOutput struct {
	Result string `json:"result"`
	Usage  struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

func parseClaudeScheduleOutput(run *protocol.ScheduleRun, output []byte) {
	var response claudeScheduleOutput
	if err := json.Unmarshal(output, &response); err != nil {
		return
	}
	run.Output = response.Result
	run.InputTokens = response.Usage.InputTokens
	run.OutputTokens = response.Usage.OutputTokens
	run.CacheRead = response.Usage.CacheReadInputTokens
	run.CacheWrite = response.Usage.CacheCreationInputTokens
}

func (d *Daemon) handleRunSchedule(conn net.Conn, raw []byte) {
	var params protocol.RunScheduleParams
	if err := json.Unmarshal(raw, &params); err != nil || params.ScheduleID == "" {
		d.sendErr(conn, "invalid run_schedule params")
		return
	}
	schedule, err := d.db.GetSchedule(params.ScheduleID)
	if err != nil {
		d.sendErr(conn, "schedule not found")
		return
	}
	go d.executeSchedule(context.Background(), schedule)
	d.sendOK(conn, map[string]string{"schedule_id": schedule.ID})
}
