package desktop

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	githubURL         = "https://github.com/oyzy/go-pw"
	githubReleasesURL = githubURL + "/releases"
)

var iconGitHub = gfx.Icon{ViewBox: 24, Commands: []gfx.PathCommand{
	gfx.MoveTo(12.0, 0.297),
	gfx.CubicTo(5.37, 0.297, 0.0, 5.67, 0.0, 12.297),
	gfx.CubicTo(0.0, 17.6, 3.438, 22.097, 8.205, 23.682),
	gfx.CubicTo(8.805, 23.795, 9.025, 23.424, 9.025, 23.105),
	gfx.CubicTo(9.025, 22.82, 9.015, 22.065, 9.01, 21.065),
	gfx.CubicTo(5.672, 21.789, 4.968, 19.455, 4.968, 19.455),
	gfx.CubicTo(4.422, 18.07, 3.633, 17.7, 3.633, 17.7),
	gfx.CubicTo(2.546, 16.956, 3.717, 16.971, 3.717, 16.971),
	gfx.CubicTo(4.922, 17.055, 5.555, 18.207, 5.555, 18.207),
	gfx.CubicTo(6.625, 20.042, 8.364, 19.512, 9.05, 19.205),
	gfx.CubicTo(9.158, 18.429, 9.467, 17.9, 9.812, 17.6),
	gfx.CubicTo(7.147, 17.3, 4.346, 16.268, 4.346, 11.67),
	gfx.CubicTo(4.346, 10.36, 4.811, 9.29, 5.581, 8.45),
	gfx.CubicTo(5.446, 8.147, 5.041, 6.927, 5.686, 5.274),
	gfx.CubicTo(5.686, 5.274, 6.691, 4.952, 8.986, 6.504),
	gfx.CubicTo(9.946, 6.237, 10.966, 6.105, 11.986, 6.099),
	gfx.CubicTo(13.006, 6.105, 14.026, 6.237, 14.986, 6.504),
	gfx.CubicTo(17.266, 4.952, 18.271, 5.274, 18.271, 5.274),
	gfx.CubicTo(18.916, 6.927, 18.511, 8.147, 18.391, 8.45),
	gfx.CubicTo(19.156, 9.29, 19.621, 10.36, 19.621, 11.67),
	gfx.CubicTo(19.621, 16.28, 16.816, 17.295, 14.146, 17.59),
	gfx.CubicTo(14.566, 17.95, 14.956, 18.686, 14.956, 19.81),
	gfx.CubicTo(14.956, 21.416, 14.941, 22.706, 14.941, 23.096),
	gfx.CubicTo(14.941, 23.411, 15.151, 23.786, 15.766, 23.666),
	gfx.CubicTo(20.565, 22.092, 24.0, 17.592, 24.0, 12.297),
	gfx.CubicTo(24.0, 5.67, 18.627, 0.297, 12.0, 0.297),
	gfx.ClosePath(),
}}

func (m *Model) githubButton(c ui.C) {
	if ui.Button(c, m.t("titlebar.github")).Icon(iconGitHub).IconOnly().Ghost().Clicked() {
		after(c, func() {
			if err := app.OpenURL(githubURL); err != nil {
				m.fail(err)
			}
		})
	}
}
