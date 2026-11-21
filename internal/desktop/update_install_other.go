//go:build !windows

package desktop

import "github.com/vivaui/viva/update"

// installUpdate installs the downloaded package with Viva's platform helper.
func installUpdate(options update.InstallOptions, quit func()) error {
	return update.Install(options, quit)
}
