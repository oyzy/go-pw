package desktop

import "github.com/vivaui/viva/ui"

// captioned declares a control under the sentence that names it. Viva reads the label of
// a text field as its accessible name and paints nothing for it, so a form that only
// passes one shows a column of anonymous boxes: this is the caption a user reads, above
// the control, the way the lock screen labels the file box.
//
//	captioned(c, m.t("field.title"), func() {
//		ui.TextField(c, &state.entry.Title, m.t("field.title"), "").AutoFocus()
//	})
func captioned(c ui.C, label string, control func()) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Text(c, label).Muted().FontSize(12)
		control()
	})
}

// captionedRow is captioned with a button beside the control, such as the one that
// generates the password the field holds. The button shares the control's row rather
// than standing in the caption's, so the field starts at the same top edge as the field
// in the other column of a captionedPair, whose row carries no button.
func captionedRow(c ui.C, label string, aside func(), control func()) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Text(c, label).Muted().FontSize(12)
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			control()
			aside()
		})
	})
}

// captionedPair puts two captioned controls side by side, each half the width. A form
// whose fields each take a row of its own grows past the bottom of its dialog, and a
// field below the fold is a field the user never fills in: the entry editor holds eight
// of them, so it pairs them up.
//
//	captionedPair(c,
//		func() { captioned(c, m.t("field.title"), func() { titleField() }) },
//		func() { captioned(c, m.t("field.userName"), func() { userField() }) })
func captionedPair(c ui.C, left, right func()) {
	ui.Row(c).FillWidth().Gap(12).Items(ui.Start).Children(func() {
		// Both halves start from nothing and grow alike, so the fields of a row are as
		// wide as each other whatever their captions happen to say.
		ui.Column(c).Basis(0).Grow(1).Children(left)
		ui.Column(c).Basis(0).Grow(1).Children(right)
	})
}
