package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const daemonServiceName = "dev.canopy.daemon"

func installDaemonService() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var path string
	var content []byte
	switch runtime.GOOS {
	case "linux":
		configDir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		path = filepath.Join(configDir, "systemd", "user", daemonServiceName+".service")
		content = []byte(fmt.Sprintf(`[Unit]
Description=Canopy background daemon

[Service]
ExecStart=%s daemon _run
Restart=on-failure

[Install]
WantedBy=default.target
`, systemdQuote(exe)))
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, "Library", "LaunchAgents", daemonServiceName+".plist")
		content = []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>daemon</string><string>_run</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
</dict>
</plist>
`, daemonServiceName, xmlEscape(exe)))
	default:
		return fmt.Errorf("daemon services are unsupported on %s", runtime.GOOS)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}
	if err := activateDaemonService(path); err != nil {
		return err
	}
	fmt.Printf("daemon service installed: %s\n", path)
	return nil
}

func uninstallDaemonService() error {
	var path string
	switch runtime.GOOS {
	case "linux":
		configDir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		path = filepath.Join(configDir, "systemd", "user", daemonServiceName+".service")
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, "Library", "LaunchAgents", daemonServiceName+".plist")
	default:
		return fmt.Errorf("daemon services are unsupported on %s", runtime.GOOS)
	}
	_ = deactivateDaemonService(path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("daemon service removed")
	return nil
}

func activateDaemonService(path string) error {
	if runtime.GOOS == "linux" {
		if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
			return fmt.Errorf("reload systemd user units: %w", err)
		}
		if err := exec.Command("systemctl", "--user", "enable", "--now", daemonServiceName+".service").Run(); err != nil {
			return fmt.Errorf("enable systemd user service: %w", err)
		}
		return nil
	}
	uid := strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", "gui/"+uid, path).Run()
	if err := exec.Command("launchctl", "bootstrap", "gui/"+uid, path).Run(); err != nil {
		return fmt.Errorf("start LaunchAgent: %w", err)
	}
	return nil
}

func deactivateDaemonService(path string) error {
	if runtime.GOOS == "linux" {
		return exec.Command("systemctl", "--user", "disable", "--now", daemonServiceName+".service").Run()
	}
	return exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid()), path).Run()
}

func systemdQuote(value string) string {
	return strconv.Quote(value)
}

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, "\"", "&quot;")
	return value
}
