package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/sharathk-dev/canopy/internal/daemon"
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
	Short: "Show whether the daemon is running",
	RunE:  runDaemonStatus,
}

// daemonRunCmd is an internal command invoked by the forked daemon process.
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
}

func runDaemonStart(cmd *cobra.Command, _ []string) error {
	if isDaemonRunning() {
		fmt.Println("daemon already running")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("executable path: %w", err)
	}

	if err := os.MkdirAll(daemon.DataDir(), 0700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	proc, err := os.StartProcess(exe, []string{exe, "daemon", "_run"}, &os.ProcAttr{
		Files: []*os.File{nil, nil, nil},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	})
	if err != nil {
		return fmt.Errorf("fork daemon: %w", err)
	}
	fmt.Printf("canopy daemon started (pid %d)\n", proc.Pid)
	proc.Release() //nolint:errcheck
	return nil
}

func runDaemonRun(_ *cobra.Command, _ []string) error {
	if err := os.MkdirAll(daemon.DataDir(), 0700); err != nil {
		return err
	}

	db, err := store.Open(daemon.DBPath())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	// Write PID file.
	pidData := []byte(strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(daemon.PIDPath(), pidData, 0600); err != nil {
		return fmt.Errorf("write pid: %w", err)
	}
	defer os.Remove(daemon.PIDPath())

	d := daemon.New(db, daemon.SocketPath())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		cancel()
	}()

	return d.Run(ctx)
}

func runDaemonStop(_ *cobra.Command, _ []string) error {
	data, err := os.ReadFile(daemon.PIDPath())
	if err != nil {
		return fmt.Errorf("daemon not running (no pid file)")
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("invalid pid file")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal %d: %w", pid, err)
	}
	fmt.Printf("sent SIGTERM to daemon (pid %d)\n", pid)
	return nil
}

func runDaemonStatus(_ *cobra.Command, _ []string) error {
	conn, err := net.Dial("unix", daemon.SocketPath())
	if err != nil {
		fmt.Println("daemon: not running")
		return nil
	}
	conn.Close()
	fmt.Println("daemon: running")
	return nil
}

func isDaemonRunning() bool {
	conn, err := net.Dial("unix", daemon.SocketPath())
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
