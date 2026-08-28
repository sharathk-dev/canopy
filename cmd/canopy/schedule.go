package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/sharathk-dev/canopy/internal/datadir"
	"github.com/sharathk-dev/canopy/internal/protocol"
	"github.com/sharathk-dev/canopy/internal/scheduler"
	"github.com/sharathk-dev/canopy/internal/store"
	"github.com/spf13/cobra"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage recurring skills and commands",
}

var (
	scheduleSkill   string
	scheduleCommand string
	scheduleCron    string
	scheduleCWD     string
)

var scheduleAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a schedule",
	Args:  cobra.ExactArgs(1),
	RunE:  runScheduleAdd,
}

var scheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List schedules",
	RunE:  runScheduleList,
}

var scheduleRunCmd = &cobra.Command{
	Use:   "run <name-or-id>",
	Short: "Run a schedule immediately",
	Args:  cobra.ExactArgs(1),
	RunE:  runScheduleRun,
}

var scheduleRunsCmd = &cobra.Command{
	Use:   "runs <name-or-id>",
	Short: "Show recent runs and output",
	Args:  cobra.ExactArgs(1),
	RunE:  runScheduleRuns,
}

var scheduleEnableCmd = &cobra.Command{
	Use:   "enable <name-or-id>",
	Short: "Enable a schedule",
	Args:  cobra.ExactArgs(1),
	RunE:  func(_ *cobra.Command, args []string) error { return setScheduleEnabled(args[0], true) },
}

var scheduleDisableCmd = &cobra.Command{
	Use:   "disable <name-or-id>",
	Short: "Disable a schedule",
	Args:  cobra.ExactArgs(1),
	RunE:  func(_ *cobra.Command, args []string) error { return setScheduleEnabled(args[0], false) },
}

func init() {
	scheduleAddCmd.Flags().StringVar(&scheduleSkill, "skill", "", "Claude skill name")
	scheduleAddCmd.Flags().StringVar(&scheduleCommand, "command", "", "shell command")
	scheduleAddCmd.Flags().StringVar(&scheduleCron, "cron", "", "five-field cron expression")
	scheduleAddCmd.Flags().StringVar(&scheduleCWD, "cwd", "", "working directory (default: current directory)")
	_ = scheduleAddCmd.MarkFlagRequired("cron")

	scheduleCmd.AddCommand(scheduleAddCmd, scheduleListCmd, scheduleRunCmd, scheduleRunsCmd, scheduleEnableCmd, scheduleDisableCmd)
	rootCmd.AddCommand(scheduleCmd)
}

func runScheduleAdd(_ *cobra.Command, args []string) error {
	if (scheduleSkill == "") == (scheduleCommand == "") {
		return fmt.Errorf("specify exactly one of --skill or --command")
	}
	if _, err := scheduler.ParseCron(scheduleCron); err != nil {
		return fmt.Errorf("invalid cron: %w", err)
	}
	cwd := scheduleCWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	actionType, action := "skill", scheduleSkill
	if scheduleCommand != "" {
		actionType, action = "command", scheduleCommand
	}
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()
	schedule := protocol.Schedule{
		ID: protocol.NewID(), Name: args[0], ActionType: actionType,
		Action: action, Cron: scheduleCron, CWD: cwd, Enabled: true,
	}
	if err := db.CreateSchedule(schedule); err != nil {
		return fmt.Errorf("save schedule: %w", err)
	}
	fmt.Printf("schedule created: %s\n", schedule.Name)
	return nil
}

func runScheduleList(_ *cobra.Command, _ []string) error {
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()
	schedules, err := db.ListSchedules()
	if err != nil {
		return fmt.Errorf("list schedules: %w", err)
	}
	if len(schedules) == 0 {
		fmt.Println("no schedules")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tCRON\tSTATE\tLAST RUN")
	for _, schedule := range schedules {
		state := "disabled"
		if schedule.Enabled {
			state = "enabled"
		}
		lastRun := "never"
		if !schedule.LastRunAt.IsZero() {
			lastRun = schedule.LastRunAt.Local().Format(time.DateTime)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", schedule.Name, schedule.ActionType, schedule.Cron, state, lastRun)
	}
	return w.Flush()
}

func findSchedule(db *store.Store, nameOrID string) (protocol.Schedule, error) {
	if schedule, err := db.GetSchedule(nameOrID); err == nil {
		return schedule, nil
	}
	return db.GetScheduleByName(nameOrID)
}

func runScheduleRun(_ *cobra.Command, args []string) error {
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	schedule, err := findSchedule(db, args[0])
	db.Close()
	if err != nil {
		return fmt.Errorf("schedule not found: %w", err)
	}
	payload, _ := json.Marshal(protocol.RunScheduleParams{ScheduleID: schedule.ID})
	if _, err := sendDaemonCmd(protocol.Cmd{Type: protocol.CmdRunSchedule, Payload: payload}); err != nil {
		return fmt.Errorf("run schedule: %w", err)
	}
	fmt.Printf("schedule started: %s\n", schedule.Name)
	return nil
}

func runScheduleRuns(_ *cobra.Command, args []string) error {
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()
	schedule, err := findSchedule(db, args[0])
	if err != nil {
		return fmt.Errorf("schedule not found: %w", err)
	}
	runs, err := db.ListScheduleRuns(schedule.ID, 20)
	if err != nil {
		return fmt.Errorf("list schedule runs: %w", err)
	}
	if len(runs) == 0 {
		fmt.Println("no runs")
		return nil
	}
	for _, run := range runs {
		when := run.StartedAt.Local().Format(time.DateTime)
		fmt.Printf("%s  %s  %d in / %d out\n", when, run.Status, run.InputTokens, run.OutputTokens)
		if run.Error != "" {
			fmt.Printf("error: %s\n", run.Error)
		}
		if run.Output != "" {
			fmt.Printf("%s\n", run.Output)
		}
	}
	return nil
}

func setScheduleEnabled(nameOrID string, enabled bool) error {
	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()
	schedule, err := findSchedule(db, nameOrID)
	if err != nil {
		return fmt.Errorf("schedule not found: %w", err)
	}
	if err := db.SetScheduleEnabled(schedule.ID, enabled); err != nil {
		return err
	}
	return nil
}
