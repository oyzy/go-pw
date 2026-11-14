//go:build darwin

package desktop

import (
	"os"
	"path/filepath"
	"strings"
)

// selfUpdatable accepts a normal .app bundle and rejects a translocated one,
// which macOS mounts read-only and the updater cannot swap.
func selfUpdatable() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	path := filepath.ToSlash(executable)
	if strings.Contains(path, "/AppTranslocation/") {
		return false
	}
	return strings.Contains(path, ".app/Contents/MacOS/")
}
