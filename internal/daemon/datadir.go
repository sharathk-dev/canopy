package daemon

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

// DataDir returns the XDG data directory for canopy.
func DataDir() string {
	return filepath.Join(xdg.DataHome, "canopy")
}

// SocketPath returns the unix socket path for the daemon.
func SocketPath() string {
	return filepath.Join(DataDir(), "daemon.sock")
}

// DBPath returns the SQLite database path.
func DBPath() string {
	return filepath.Join(DataDir(), "canopy.db")
}

// PIDPath returns the daemon PID file path.
func PIDPath() string {
	return filepath.Join(DataDir(), "daemon.pid")
}
