// GoKeep is a KeePass 2 compatible password manager built with Go and Wails 3.
package main

import (
	"embed"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"gokeep/internal/app"
)

// version is shown in the about dialog and in the window title.
const version = "1.0.0"

// The frontend is a plain HTML/CSS/JavaScript application: it needs no build step, so
// the binary can be built with a single "go build".
//
//go:embed all:frontend/dist
var assets embed.FS

//go:embed frontend/dist/icon.png
var appIcon []byte

func main() {
	manager, err := app.NewManager(version)
	if err != nil {
		slog.Error("cannot load the configuration", "error", err)
		os.Exit(1)
	}

	// The released build has no console window, so Wails system messages are written to
	// a log file next to the configuration; a console build prints them as well.
	loggers := app.NewLogger(manager.Config().LogPath())
	defer loggers.Close()
	logger := loggers.System
	slog.SetDefault(loggers.App)
	loggers.App.Info("GoKeep starting", "version", version, "config", manager.Config().Path())

	// Events the Go side sends to the frontend.
	application.RegisterEvent[string]("menu")
	application.RegisterEvent[string]("command")

	wailsApp := application.New(application.Options{
		Name:        "GoKeep",
		Description: "KeePass 2 compatible password manager",
		Services: []application.Service{
			application.NewService(app.NewVaultService(manager)),
			application.NewService(app.NewAppService(manager)),
			application.NewService(app.NewGeneratorService(manager)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Logger: logger,
		// Wails logs every asset request at info level; warnings and errors are the
		// interesting ones in the log file.
		LogLevel: slog.LevelWarn,
		// The about box and the platforms that take the window icon from the
		// application options use this; on Windows the executable resource carries the
		// icon as well (see rsrc_windows_amd64.syso).
		Icon: appIcon,
		// Wails' default marshaller turns Go errors into "{}", which the frontend can
		// only render as "[object Object]"; this one sends the message and a code.
		MarshalError: app.MarshalError,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	window := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "GoKeep",
		// The window frame and the menu bar are drawn by the frontend so that the whole
		// window follows the application theme.
		Frameless:        true,
		Width:            1180,
		Height:           760,
		MinWidth:         880,
		MinHeight:        560,
		URL:              "/",
		BackgroundColour: application.NewRGB(24, 26, 32),
		// The menu of the embedded browser offers "reload", "save as" and "inspect", none of
		// which belongs in a password manager; the frontend draws its own context menus.
		DefaultContextMenuDisabled: true,
		Windows: application.WindowsWindow{
			// The menu bar is drawn by the frontend so that it follows the application
			// theme instead of the Windows system theme.
			DisableMenu: true,
		},
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 40,
			Backdrop:                application.MacBackdropNormal,
			TitleBar:                application.MacTitleBarDefault,
		},
	})
	manager.Attach(wailsApp, window)
	setWindowIcon(window)

	// Closing the window (Alt+F4, taskbar, or the custom close button) asks the frontend
	// about unsaved changes before the application really quits.
	window.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if manager.IsQuitting() {
			return
		}
		event.Cancel()
		wailsApp.Event.Emit("command", "app:closing")
	})

	manager.SetThemeUpdater(func(theme string) {
		setWindowDarkMode(window, theme != "light")
	})

	if err := wailsApp.Run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}
