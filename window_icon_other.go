//go:build !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// setWindowIcon does nothing on platforms where the window manager takes the icon from
// the application bundle or the .desktop file.
func setWindowIcon(window *application.WebviewWindow) {}

// setWindowDarkMode does nothing on platforms where the title bar follows the system
// appearance.
func setWindowDarkMode(window *application.WebviewWindow, dark bool) {}
