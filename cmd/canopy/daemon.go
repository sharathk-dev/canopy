package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	internaldaemon "github.com/sharathk-dev/canopy/internal/daemon"
	"github.com/sharathk-dev/canopy/internal/datadir"
	"github.com/sharathk-dev/canopy/internal/store"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the canopy background daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon in the background",
	RunE:  runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running daemon",
	RunE:  runDaemonStop,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE:  runDaemonStatus,
}

// _run is the hidden command that actually runs the daemon process.
var daemonRunCmd = &cobra.Command{
	Use:    "_run",
	Hidden: true,
	RunE:   runDaemonRun,
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonRunCmd)
	rootCmd.AddCommand(daemonCmd)
}

func runDaemonStart(_ *cobra.Command, _ []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if daemonCurrent(exe) {
		fmt.Println("daemon already running")
		return nil
	}

	proc, err := os.StartProcess(exe, []string{exe, "daemon", "_run"},
		&os.ProcAttr{
			Files: []*os.File{nil, nil, nil},
			Sys:   &syscall.SysProcAttr{Setsid: true},
		},
	)
	if err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	fmt.Printf("daemon started (pid %d)\n", proc.Pid)
	return proc.Release()
}

func runDaemonStop(_ *cobra.Command, _ []string) error {
	pidFile := datadir.PIDPath()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("daemon not running (no pid file)")
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid pid file: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal daemon: %w", err)
	}

	fmt.Println("daemon stopped")
	return nil
}

func runDaemonStatus(_ *cobra.Command, _ []string) error {
	conn, err := net.Dial("unix", datadir.SocketPath())
	if err != nil {
		fmt.Println("daemon: not running")
		return nil
	}
	conn.Close()
	fmt.Println("daemon: running")
	return nil
}

func runDaemonRun(_ *cobra.Command, _ []string) error {
	pidFile := datadir.PIDPath()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(pidFile)

	db, err := store.Open(datadir.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	d := internaldaemon.New(db, datadir.SocketPath())
	return d.Run(ctx)
}
