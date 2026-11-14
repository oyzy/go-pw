//go:build linux

package desktop

import (
	"os"
	"path/filepath"
)

// selfUpdatable accepts the portable GoKeep executable in a writable folder.
// A .deb installs into a root-owned directory and is left to the package
// manager.
func selfUpdatable() bool {
	if os.Getenv("APPIMAGE") != "" {
		return false
	}
	executable, err := os.Executable()
	if err != nil || filepath.Base(executable) != "GoKeep" {
		return false
	}
	probe, err := os.CreateTemp(filepath.Dir(executable), ".gokeep-update-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	return closeErr == nil && removeErr == nil
}
