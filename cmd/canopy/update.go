package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const installScriptURL = "https://raw.githubusercontent.com/sharathk-dev/canopy/master/install.sh"

var updateYes bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Canopy to the latest published release",
	RunE:  runUpdate,
}

func init() {
	updateCmd.Flags().BoolVarP(&updateYes, "yes", "y", false, "update without confirmation")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(_ *cobra.Command, _ []string) error {
	if !updateYes {
		fmt.Print("Update Canopy to the latest release? [y/N] ")
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return err
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Update cancelled.")
			return nil
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	installDir := filepath.Dir(exe)

	fmt.Println("Downloading the Canopy installer...")
	curl := exec.Command("curl", "--fail", "--location", "--silent", "--show-error", "--retry", "3", installScriptURL)
	script, err := curl.Output()
	if err != nil {
		return fmt.Errorf("download installer: %w", err)
	}

	bash := exec.Command("bash")
	bash.Stdin = bytes.NewReader(script)
	bash.Stdout = os.Stdout
	bash.Stderr = os.Stderr
	bash.Env = append(os.Environ(), "INSTALL_DIR="+installDir, "VERSION=latest")
	if err := bash.Run(); err != nil {
		return fmt.Errorf("run installer: %w", err)
	}
	return nil
}
