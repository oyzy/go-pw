package desktop

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// page declares the whole application: the lock screen while no database is open, and
// otherwise the title bar, menu bar, toolbar, the three panes and the status bar.
func (m *Model) page(c ui.C) {
	m.pageC = c
	ui.Column(c).Fill().Background(m.theme.Background).Children(func() {
		if !m.sess.Unlocked() {
			m.lockPage(c)
			return
		}
		m.titlebar(c)
		m.menubar(c)
		m.toolbar(c)
		// The panes are as wide as the two dividers say, so the group tree, the entry
		// list and the details can be sized to what the reader is working on.
		ui.Row(c).FillWidth().Grow(1).Children(func() {
			m.treePane(c, m.splitTree.Pos)
			ui.Splitter(c, &m.splitTree).Mins(minTreePane, minListPane+minDetailPane).Label(m.t("split.groups"))
			ui.Row(c).FillHeight().Grow(1).Children(func() {
				m.listPane(c, m.splitList.Pos)
				ui.Splitter(c, &m.splitList).Mins(minListPane, minDetailPane).Label(m.t("split.entries"))
				m.detailPane(c)
			})
		})
		m.statusbar(c)
	})
	m.overlays(c)
	m.shortcuts(c)
	ui.Toasts(c, &m.toaster)
}

// shortcuts declares the keystrokes the menus label. A MenuItem.Keys only prints the
// shortcut beside the label, so the action itself is declared here, once per paint.
func (m *Model) shortcuts(c ui.C) {
	open := m.sess.Unlocked()
	switch {
	case ui.Shortcut(c, ui.Primary, "n"):
		after(c, m.cmdNewDatabase)
	case ui.Shortcut(c, ui.Primary, "o"):
		after(c, m.cmdOpenDatabase)
	case ui.Shortcut(c, ui.Primary|gfx.ModifierShift, "n"):
		if open {
			m.cmdNewEntry()
		}
	case ui.Shortcut(c, ui.Primary|gfx.ModifierShift, "g"):
		if open {
			m.cmdNewGroup()
		}
	case ui.Shortcut(c, ui.Primary, "s"):
		if open {
			m.cmdSave()
		}
	case ui.Shortcut(c, ui.Primary, "g"):
		if open {
			m.openGenerator("")
		}
	case ui.Shortcut(c, ui.Primary, "l"):
		if open {
			m.cmdLock()
		}
	case ui.Shortcut(c, ui.Primary, "f"):
		if open {
			m.focus = focusSearch
		}
	case ui.Shortcut(c, gfx.ModifierControl, "r"):
		if open {
			m.reload()
		}
	case ui.Shortcut(c, gfx.ModifierShift, "delete"):
		if m.hasDetail {
			m.cmdDeleteEntry(true)
		}
	case ui.Shortcut(c, gfx.ModifierControl, "d"):
		if open && m.hasDetail {
			m.cmdDuplicate()
		}
	}
}

// overlays declares every dialog at the page level, where Viva paints them over the
// rest of the window.
func (m *Model) overlays(c ui.C) {
	m.confirmDialog(c)
	m.editorDialog(c)
	m.groupDialog(c)
	m.generatorDialog(c)
	m.optionsDialog(c)
	m.settingsDialog(c)
	m.keyDialog(c)
	m.moveDialog(c)
	m.searchDialog(c)
	m.aboutDialog(c)
	m.shortcutsDialog(c)
	m.duplicatesDialog(c)
	m.expiredDialog(c)
	m.quitDialog(c)
}

// titlebar draws the strip that moves the window; Viva paints the window buttons at
// its end on Windows, so the row keeps clear of them.
func (m *Model) titlebar(c ui.C) {
	leading, trailing := app.TitlebarInsets()
	// The strip carries the file's own name, the way KeePass names its window: the name
	// and the version the application tells about itself belong in the About dialog, not
	// beside the database name on every window.
	title := appTitle
	if v := m.vault(); v != nil {
		title = v.FileName()
		if v.Modified() {
			title += " *"
		}
	}
	ui.Row(c).FillWidth().Height(34).Items(ui.Center).
		Background(m.theme.Card).Border(1, m.theme.Border).
		PadSides(0, trailing+12, 0, leading+12).MovesWindow().
		Children(func() {
			ui.Text(c, title).Weight(gfx.WeightSemibold)
			ui.Spacer(c)
			m.githubButton(c)
		})
}

// menubar is the application's own menu bar, drawn by the page so that it follows the
// theme, like the title bar.
func (m *Model) menubar(c ui.C) {
	labels := []string{
		m.t("menu.file"), m.t("menu.edit"), m.t("menu.view"), m.t("menu.tools"), m.t("menu.help"),
	}
	ui.Row(c).FillWidth().Gap(2).PadSides(4, 6, 4, 6).
		Background(m.theme.Card).Border(1, m.theme.Border).Children(func() {
		for index, label := range labels {
			entry := ui.Button(c, label).Ghost().Small()
			if entry.Clicked() {
				for other := range m.menus {
					m.menus[other] = other == index && !m.menus[index]
				}
			}
			m.menuFor(c, index, entry)
		}
	})
}

// menuFor declares one drop-down menu under its button.
func (m *Model) menuFor(c ui.C, which int, anchor ui.Box) {
	ui.Menu(c, anchor, &m.menus[which], func(items *ui.MenuItems) {
		switch which {
		case menuFile:
			m.fileMenu(c, items)
		case menuEdit:
			m.editMenu(c, items)
		case menuView:
			m.viewMenu(c, items)
		case menuTools:
			m.toolsMenu(c, items)
		case menuHelp:
			m.helpMenu(c, items)
		}
	})
}

// after runs a command that may block or that changes what the page declares, after
// the paint has finished.
func after(c ui.C, action func()) { c.After(action) }

func (m *Model) fileMenu(c ui.C, items *ui.MenuItems) {
	if items.Item(m.t("menu.newDatabase")).Clicked() {
		after(c, m.cmdNewDatabase)
	}
	if items.Item(m.t("menu.openDatabase")).Clicked() {
		after(c, m.cmdOpenDatabase)
	}
	recents := m.sess.Config().Get().RecentFiles
	items.Sub(m.t("menu.openRecent"), func(sub *ui.MenuItems) {
		if len(recents) == 0 {
			sub.Item(m.t("menu.recentNone")).Disabled(true)
		}
		for _, path := range recents {
			path := path
			if sub.Item(fileLabel(path)).Description(path).Clicked() {
				after(c, func() { m.openRecent(path) })
			}
		}
		sub.Separator()
		if sub.Item(m.t("menu.clearList")).Clicked() {
			if err := m.sess.Config().ClearRecent(); err != nil {
				m.fail(err)
			}
		}
	})
	items.Separator()
	items.Item(m.t("menu.save")).Keys(ui.Primary, "s").Disabled(!m.sess.Unlocked())
	if items.Item(m.t("menu.saveAs")).Disabled(!m.sess.Unlocked()).Clicked() {
		after(c, m.cmdSaveAs)
	}
	if items.Item(m.t("menu.closeDatabase")).Disabled(!m.sess.Unlocked()).Clicked() {
		after(c, m.cmdCloseDatabase)
	}
	items.Separator()
	if items.Item(m.t("menu.importCsv")).Disabled(!m.sess.Unlocked()).Clicked() {
		after(c, m.cmdImportCSV)
	}
	if items.Item(m.t("menu.exportCsv")).Disabled(!m.sess.Unlocked()).Clicked() {
		after(c, m.cmdExportCSV)
	}
	if items.Item(m.t("menu.backup")).Disabled(!m.sess.Unlocked()).Clicked() {
		after(c, m.cmdBackup)
	}
	items.Separator()
	if items.Item(m.t("menu.lockWorkspace")).Keys(ui.Primary, "l").Disabled(!m.sess.Unlocked()).Clicked() {
		m.cmdLock()
	}
	if items.Item(m.t("menu.exit")).Clicked() {
		after(c, m.cmdQuit)
	}
}

func (m *Model) editMenu(c ui.C, items *ui.MenuItems) {
	if items.Item(m.t("menu.newEntry")).Keys(ui.Primary|gfx.ModifierShift, "n").Disabled(!m.sess.Unlocked()).Clicked() {
		m.cmdNewEntry()
	}
	if items.Item(m.t("menu.newGroup")).Keys(ui.Primary|gfx.ModifierShift, "g").Disabled(!m.sess.Unlocked()).Clicked() {
		m.cmdNewGroup()
	}
	if items.Item(m.t("menu.duplicateEntry")).Disabled(!m.hasDetail).Clicked() {
		m.cmdDuplicate()
	}
	if items.Item(m.t("menu.delete")).Disabled(!m.hasDetail).Clicked() {
		after(c, func() { m.cmdDeleteEntry(false) })
	}
	items.Separator()
	has := m.hasDetail
	if items.Item(m.t("menu.copyUser")).Disabled(!has).Clicked() {
		m.cmdCopyField("UserName")
	}
	if items.Item(m.t("menu.copyPassword")).Disabled(!has).Clicked() {
		m.cmdCopyField("Password")
	}
	if items.Item(m.t("menu.copyUrl")).Disabled(!has).Clicked() {
		m.cmdCopyField("URL")
	}
	if items.Item(m.t("menu.openUrl")).Disabled(!has).Clicked() {
		m.cmdOpenURL()
	}
	items.Separator()
	if items.Item(m.t("menu.find")).Keys(ui.Primary, "f").Disabled(!m.sess.Unlocked()).Clicked() {
		m.focus = focusSearch
	}
	if items.Item(m.t("menu.findAll")).Disabled(!m.sess.Unlocked()).Clicked() {
		m.searchOpen = true
	}
}

func (m *Model) viewMenu(c ui.C, items *ui.MenuItems) {
	if items.Item(m.t("menu.refresh")).Keys(gfx.ModifierControl, "r").Disabled(!m.sess.Unlocked()).Clicked() {
		m.reload()
	}
	showBin := m.sess.Config().Get().ShowRecycleBin
	label := m.t("menu.showRecycleBin")
	if showBin {
		label = m.t("menu.hideRecycleBin")
	}
	if items.Item(label).Disabled(!m.sess.Unlocked()).Clicked() {
		cfg := m.sess.Config().Get()
		cfg.ShowRecycleBin = !cfg.ShowRecycleBin
		_ = m.sess.Config().Set(cfg)
		m.reload()
	}
	items.Separator()
	theme := m.t("menu.toggleTheme")
	if m.dark() {
		theme = m.t("options.themeLight")
	} else {
		theme = m.t("options.themeDark")
	}
	if items.Item(theme).Clicked() {
		m.toggleTheme()
	}
}

func (m *Model) toolsMenu(c ui.C, items *ui.MenuItems) {
	if items.Item(m.t("menu.generator")).Disabled(!m.sess.Unlocked()).Clicked() {
		m.openGenerator("")
	}
	if items.Item(m.t("menu.dbSettings")).Disabled(!m.sess.Unlocked()).Clicked() {
		m.openSettings()
	}
	if items.Item(m.t("menu.changeKey")).Disabled(!m.sess.Unlocked()).Clicked() {
		m.key = keyState{open: true}
	}
	if items.Item(m.t("menu.createKeyFile")).Clicked() {
		after(c, m.cmdCreateKeyFile)
	}
	items.Separator()
	if items.Item(m.t("menu.findDuplicates")).Disabled(!m.sess.Unlocked()).Clicked() {
		m.cmdFindDuplicates()
	}
	if items.Item(m.t("menu.checkExpired")).Disabled(!m.sess.Unlocked()).Clicked() {
		m.cmdCheckExpired()
	}
	if items.Item(m.t("menu.emptyBin")).Disabled(!m.sess.Unlocked()).Clicked() {
		after(c, func() { m.cmdEmptyRecycleBin(false) })
	}
	items.Separator()
	if items.Item(m.t("menu.clearClipboard")).Clicked() {
		m.sess.ClearClipboard()
		m.info(m.t("toast.clipboardCleared"))
	}
	if items.Item(m.t("menu.options")).Clicked() {
		m.opts = optionsState{open: true, cfg: m.sess.Config().Get()}
	}
}

func (m *Model) helpMenu(c ui.C, items *ui.MenuItems) {
	if items.Item(m.t("menu.shortcuts")).Clicked() {
		m.shortcutsHelp = true
	}
	items.Separator()
	if items.Item(m.t("menu.about")).Clicked() {
		m.about = true
	}
}

// shortcutRow is one line of the keyboard reference. Every row here is a keystroke the
// page really handles, so the list cannot drift away from the shortcuts function.
type shortcutRow struct {
	keys string
	desc string
}

var shortcutRows = []shortcutRow{
	{"Ctrl+N", "shortcuts.newDatabase"},
	{"Ctrl+O", "shortcuts.openDatabase"},
	{"Ctrl+S", "shortcuts.save"},
	{"Ctrl+Shift+N", "shortcuts.newEntry"},
	{"Ctrl+Shift+G", "shortcuts.newGroup"},
	{"Ctrl+D", "shortcuts.duplicate"},
	{"Ctrl+G", "shortcuts.generator"},
	{"Ctrl+F", "shortcuts.find"},
	{"Ctrl+R", "shortcuts.refresh"},
	{"Ctrl+L", "shortcuts.lock"},
	{"Shift+Delete", "shortcuts.deleteForever"},
}

// toolbar is the row of buttons under the menu bar.
func (m *Model) toolbar(c ui.C) {
	open := m.sess.Unlocked()
	has := m.hasDetail
	ui.Row(c).FillWidth().Gap(4).Pad(6).Items(ui.Center).
		Background(m.theme.Card).Border(1, m.theme.Border).Children(func() {
		m.toolButton(c, m.t("toolbar.newDatabase"), false, m.cmdNewDatabase)
		m.toolButton(c, m.t("toolbar.open"), false, m.cmdOpenDatabase)
		m.toolButton(c, m.t("toolbar.save"), !open, m.cmdSave)
		m.toolButton(c, m.t("toolbar.lock"), !open, m.cmdLock)
		ui.Divider(c).Vertical().Height(20).MarginX(4)
		m.toolButton(c, m.t("toolbar.duplicate"), !has, m.cmdDuplicate)
		m.toolButton(c, m.t("toolbar.delete"), !has, func() { m.cmdDeleteEntry(false) })
		ui.Divider(c).Vertical().Height(20).MarginX(4)
		m.toolButton(c, m.t("toolbar.copyUser"), !has, func() { m.cmdCopyField("UserName") })
		m.toolButton(c, m.t("toolbar.copyPassword"), !has, func() { m.cmdCopyField("Password") })
		m.toolButton(c, m.t("toolbar.copyUrl"), !has, func() { m.cmdCopyField("URL") })
		m.toolButton(c, m.t("toolbar.openUrl"), !has, m.cmdOpenURL)
		ui.Divider(c).Vertical().Height(20).MarginX(4)
		m.toolButton(c, m.t("toolbar.generator"), !open, func() { m.openGenerator("") })
		m.toolButton(c, m.t("toolbar.settings"), !open, m.openSettings)
		ui.Spacer(c)
		field := ui.FocusBind(
			ui.TextField(c, &m.search, m.t("toolbar.search"), m.t("toolbar.search")).Width(220).Disabled(!open),
			&m.focus, focusSearch)
		if field.Changed() {
			m.reloadEntries()
		}
		if ui.Button(c, m.t("toolbar.searchOptions")).Ghost().Small().Disabled(!open).Clicked() {
			m.searchOpen = true
		}
	})
}

// toolButton is one toolbar button: a ghost button that shows its command.
func (m *Model) toolButton(c ui.C, label string, disabled bool, action func()) {
	if ui.Button(c, label).Ghost().Small().Disabled(disabled).Clicked() {
		after(c, action)
	}
}

// statusbar reports the open database, the entry count, the clipboard countdown and
// the version.
func (m *Model) statusbar(c ui.C) {
	theme := m.theme
	ui.Row(c).FillWidth().Height(26).Gap(16).Items(ui.Center).PadSides(0, 10, 0, 10).
		Background(theme.Card).Border(1, theme.Border).Children(func() {
		v := m.vault()
		switch {
		case v == nil:
			ui.Text(c, m.t("status.notOpen")).Muted().FontSize(12)
		default:
			info := v.Info()
			ui.Text(c, v.Path()).Muted().FontSize(12).Ellipsis().MaxWidth(520)
			ui.Text(c, m.tf("status.summary", "entries", info.EntryCount, "groups", info.GroupCount)).Muted().FontSize(12)
		}
		ui.Spacer(c)
		if !m.sess.Config().Get().ShowRecycleBin {
			ui.Text(c, m.t("status.recycleBinOffHint")).Color(theme.Danger).FontSize(12)
		}
		if active, remaining := m.sess.ClipboardState(); active {
			ui.Text(c, m.tf("status.clipboard", "seconds", remaining)).Muted().FontSize(12)
		}
	})
}

// searchSummary describes what the entry list currently shows.
func (m *Model) searchSummary() string {
	if strings.TrimSpace(m.search) != "" {
		return fmt.Sprintf("%s: %s", m.t("search.title"), m.search)
	}
	return ""
}
