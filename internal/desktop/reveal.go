package desktop

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The eye of a password field. A reveal button is the one control every password box is
// expected to have, and Viva puts it inside the field: Action paints it at the field's
// end and ActionClicked reports a press, so the eye travels with the text it hides
// instead of sitting in a row the layout has to keep in step with the field.
//
// The icons are drawn on Lucide's 24 by 24 grid with a 2 unit stroke and round caps,
// which is the geometry gfx.Icon expects.
var (
	// iconEye is the open eye of "show the password": the lens, and the pupil in it.
	iconEye = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(2, 12),
		gfx.CubicTo(5.5, 7, 8.5, 5, 12, 5),
		gfx.CubicTo(15.5, 5, 18.5, 7, 22, 12),
		gfx.CubicTo(18.5, 17, 15.5, 19, 12, 19),
		gfx.CubicTo(8.5, 19, 5.5, 17, 2, 12),
		gfx.ClosePath(),
		gfx.MoveTo(9, 12),
		gfx.CubicTo(9, 10.34, 10.34, 9, 12, 9),
		gfx.CubicTo(13.66, 9, 15, 10.34, 15, 12),
		gfx.CubicTo(15, 13.66, 13.66, 15, 12, 15),
		gfx.CubicTo(10.34, 15, 9, 13.66, 9, 12),
		gfx.ClosePath(),
	}}
	// iconEyeOff is the same eye crossed out, the way "hide the password" is drawn
	// everywhere else.
	iconEyeOff = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(2, 12),
		gfx.CubicTo(5.5, 7, 8.5, 5, 12, 5),
		gfx.CubicTo(15.5, 5, 18.5, 7, 22, 12),
		gfx.CubicTo(18.5, 17, 15.5, 19, 12, 19),
		gfx.CubicTo(8.5, 19, 5.5, 17, 2, 12),
		gfx.ClosePath(),
		gfx.MoveTo(4, 4),
		gfx.LineTo(20, 20),
	}}
)

// passwordField draws field with the eye that reveals and hides it. While *shown is
// false the field is Secure and paints one bullet per character; a press on the eye
// flips *shown. shown belongs to the caller's state, so the choice is what the user
// last made rather than something each paint decides again.
func (m *Model) passwordField(field ui.Box, shown *bool) ui.Box {
	icon, label := iconEye, m.t("field.showPassword")
	switch {
	case *shown:
		icon, label = iconEyeOff, m.t("field.hidePassword")
	default:
		field.Secure()
	}
	if field.Action(icon, label).ActionClicked() {
		*shown = !*shown
	}
	return field
}
