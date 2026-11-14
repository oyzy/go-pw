package desktop

import (
	"bytes"
	_ "embed"
	"image/color"
	"image/png"
	"log/slog"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"gokeep/internal/config"
)

// appTitle is the name the desktop shows for the window, and the first word of every
// message about the application.
const appTitle = "GoKeep"

// titlebarHeight is the height of the strip the page paints under the window buttons.
const titlebarHeight = 34

// iconSize is the edge of the picture the taskbar, Alt+Tab and the title bar ask for.
const iconSize = 512

// appicon.png is the icon tools/makeicon draws; it is embedded so the binary needs no
// files beside it.
//
//go:embed assets/appicon.png
var iconPNG []byte

// Run opens the application window and blocks until it closes.
func Run(version string, store *config.Store) error {
	session := NewSession(version, store)
	return app.RunApplication(func(application *app.Application) error {
		model := newModel(session)
		page := ui.NewPage(ui.PageConfig{View: model.page, Theme: &model.theme})
		windowConfig := page.Config(app.Config{
			Title: appTitle,
			// Class is the name a Linux desktop matches to a launcher entry, so the
			// taskbar can show the installed icon.
			Class:          appTitle,
			Width:          1180,
			Height:         760,
			MinWidth:       900,
			MinHeight:      600,
			WindowMode:     app.WindowCustomTitlebar,
			TitlebarHeight: titlebarHeight,
			WindowControls: model.windowControls,
			Icon:           windowIcon(),
			ClearColor:     appColor(model.theme.Background),
			OnCloseRequest: model.handleCloseRequest,
			OnWake:         model.wake,
			OnActivate:     model.windowActivate,
			OnMinimize:     model.windowMinimize,
			// A divider keeps its arrow keys in its own state, so the window hands it the
			// key events the page's shortcuts do not take.
			OnKey: model.splitKey,
		})
		// The page's routes handle most input themselves, so activity is recorded
		// around the configured handlers rather than in a fallback behind them.
		windowConfig.OnKey = model.trackKey(windowConfig.OnKey)
		windowConfig.OnPointer = model.trackPointer(windowConfig.OnPointer)
		windowConfig.OnShortcut = model.trackShortcut(windowConfig.OnShortcut)
		windowConfig.OnScroll = model.trackScroll(windowConfig.OnScroll)
		window, err := application.OpenWindow(windowConfig, nil)
		if err != nil {
			return err
		}
		model.window = window
		model.application = application
		// The clipboard countdown and anything else off the window thread wake the
		// page through this.
		session.SetInvalidator(windowConfig.Invalidator)
		return nil
	})
}

// windowControls styles the minimize, maximize and close buttons Viva draws for a
// window without a title bar, so they follow the application theme.
func (m *Model) windowControls() app.WindowControlsStyle {
	theme := m.theme
	return app.WindowControlsStyle{
		Foreground:      theme.Foreground,
		Hover:           theme.Hover,
		Pressed:         theme.Accent,
		Close:           theme.Danger,
		CloseForeground: theme.DangerForeground,
		Minimize:        m.t("titlebar.minimize"),
		Maximize:        m.t("titlebar.maximize"),
		Restore:         m.t("titlebar.restore"),
		CloseName:       m.t("titlebar.close"),
	}
}

// appColor converts a theme color for the window's own clear color.
func appColor(value gfx.Color) app.Color {
	return app.Color{
		Red:   float64(value.Red),
		Green: float64(value.Green),
		Blue:  float64(value.Blue),
		Alpha: float64(value.Alpha),
	}
}

// windowIcon decodes the embedded icon and shrinks it to the size the desktop asks
// for. The pixels are straight RGBA, un-premultiplied, as app.WindowIcon wants.
func windowIcon() app.WindowIcon {
	source, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		slog.Warn("cannot decode the application icon", "error", err)
		return app.WindowIcon{}
	}
	bounds := source.Bounds()
	step := bounds.Dx() / iconSize
	if step < 1 {
		step = 1
	}
	if bounds.Dx() < iconSize || bounds.Dy() < iconSize {
		return app.WindowIcon{}
	}
	pixels := make([]byte, iconSize*iconSize*4)
	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			var red, green, blue, alpha float64
			for dy := 0; dy < step; dy++ {
				for dx := 0; dx < step; dx++ {
					r, g, b, a := straightRGBA(source.At(bounds.Min.X+x*step+dx, bounds.Min.Y+y*step+dy))
					red += r
					green += g
					blue += b
					alpha += a
				}
			}
			samples := float64(step * step)
			at := (y*iconSize + x) * 4
			pixels[at+0] = byte(red / samples)
			pixels[at+1] = byte(green / samples)
			pixels[at+2] = byte(blue / samples)
			pixels[at+3] = byte(alpha / samples)
		}
	}
	return app.WindowIcon{Size: iconSize, RGBA: pixels}
}

// straightRGBA is one pixel in 0..255 straight (not premultiplied) components.
func straightRGBA(value color.Color) (float64, float64, float64, float64) {
	r, g, b, a := value.RGBA()
	if a == 0 {
		return 0, 0, 0, 0
	}
	scale := 255.0 / float64(a)
	return float64(r) * scale, float64(g) * scale, float64(b) * scale, float64(a) / 257
}
