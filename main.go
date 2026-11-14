// GoKeep is a password manager in the spirit of KeePass 2. Its database files use the
// application's own .gkdb container: they are written and read by internal/gkdb only.
//
// The window and every control in it are declared by internal/desktop with the Viva
// toolkit: Go draws the interface itself, so the application needs no web view, no HTML
// and no JavaScript at run time.
package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gokeep/internal/config"
	"gokeep/internal/desktop"
)

// version is shown in the about dialog and in the title strip.
const version = "1.0.0"

func main() {
	store, err := config.New()
	if err != nil {
		slog.Error("cannot load the configuration", "error", err)
		os.Exit(1)
	}
	closeLog := setupLogging(store.Path())
	defer closeLog()
	// A released build has no console, so the first line of the log is the record that
	// the program started at all: it is what a report of "it does not open" is read with.
	slog.Info("GoKeep starting", "version", version, "config", store.Path())

	if err := desktop.Run(version, store); err != nil {
		slog.Error("the application stopped", "error", err)
		os.Exit(1)
	}
}

// setupLogging writes the system log next to the configuration file: a released build
// has no console window, so the log file is where a failure can be read afterwards.
func setupLogging(configPath string) func() {
	path := filepath.Join(filepath.Dir(configPath), "GoKeep.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return func() {}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(file, os.Stderr), nil)))
	return func() { _ = file.Close() }
}
