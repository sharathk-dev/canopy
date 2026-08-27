package datadir

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

func DataDir() string {
	dir := filepath.Join(xdg.DataHome, "canopy")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func DBPath() string     { return filepath.Join(DataDir(), "canopy.db") }
func SocketPath() string { return filepath.Join(DataDir(), "daemon.sock") }
func PIDPath() string    { return filepath.Join(DataDir(), "daemon.pid") }
