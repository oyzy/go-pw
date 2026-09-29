package app

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// maxLogSize is the size at which the log file is started over, so it cannot grow
// without bound in a long running installation.
const maxLogSize = 4 << 20

// LogPath returns the file the application writes its log to: "GoKeep.log" next to the
// configuration file.
func (c *ConfigStore) LogPath() string {
	return filepath.Join(filepath.Dir(c.Path()), "GoKeep.log")
}

// Loggers holds the two loggers the application uses: System receives the very verbose
// Wails messages and only shows warnings and errors, App carries the application's own
// messages.
type Loggers struct {
	System *slog.Logger
	App    *slog.Logger
	Close  func()
}

// NewLogger builds the loggers. The released build is a Windows GUI application, so
// there is no console to print to and the log is written to a file; when a console is
// attached (development builds started from a terminal) the output goes to both.
func NewLogger(path string) Loggers {
	var writers []io.Writer
	var file *os.File
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
			os.Remove(path)
		}
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			file = f
			writers = append(writers, f)
		}
	}
	if hasConsole() {
		writers = append(writers, os.Stdout)
	}
	if len(writers) == 0 {
		return Loggers{System: slog.Default(), App: slog.Default(), Close: func() {}}
	}
	writer := io.MultiWriter(writers...)
	system := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelWarn}))
	appLogger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	closeFn := func() {
		if file != nil {
			file.Close()
		}
	}
	return Loggers{System: system, App: appLogger, Close: closeFn}
}

// hasConsole reports whether standard output is attached to a console or terminal.
func hasConsole() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
