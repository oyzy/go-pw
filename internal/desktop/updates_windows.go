//go:build windows

package desktop

import (
	"os"
	"path/filepath"
	"strings"
)

// selfUpdatable reports whether this copy came from the per-user NSIS
// installer. That installer always writes Uninstall.exe beside the program; a
// portable zip does not, and the Windows update helper needs the installer's
// install layout.
func selfUpdatable() bool {
	executable, err := os.Executable()
	if err != nil || !strings.EqualFold(filepath.Base(executable), "GoKeep.exe") {
		return false
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(executable), "Uninstall.exe"))
	return err == nil && !info.IsDir()
}
