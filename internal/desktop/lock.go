package desktop

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

// databaseExtension is the file name extension of a GoKeep database. The application
// has exactly one file format: the container internal/gkdb writes.
const databaseExtension = ".gkdb"

// The security presets a new database can be created with.
const (
	presetStandard = "standard"
	presetStrong   = "strong"
	presetParanoid = "paranoid"
	presetFast     = "fast"
)

// presetNames lists the presets in the order the card shows them.
var presetNames = []string{presetStandard, presetStrong, presetParanoid, presetFast}

// lockPage is the window's content while no database is open: the card that unlocks
// an existing file, or the one that creates a new one.
func (m *Model) lockPage(c ui.C) {
	ui.Row(c).Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
		ui.Column(c).Width(520).Gap(12).Pad(20).
			Background(m.theme.Card).Radius(12).Border(1, m.theme.Border).Children(func() {
			ui.Text(c, appTitle).FontSize(20).Bold()
			ui.Text(c, m.t("app.tagline")).Muted().FontSize(12)
			ui.Divider(c)
			if m.lock.create {
				m.lockCreateFields(c)
			} else {
				m.lockUnlockFields(c)
			}
			if m.lock.message != "" {
				color := m.theme.MutedForeground
				if m.lock.messageErr {
					color = m.theme.Danger
				}
				ui.Paragraph(c, m.lock.message, 3).Color(color)
			}
		})
	})
}

// lockUnlockFields is the unlock half of the lock screen.
func (m *Model) lockUnlockFields(c ui.C) {
	ui.Text(c, m.t("lock.file")).Muted().FontSize(12)
	ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
		ui.TextField(c, &m.lock.path, m.t("lock.file"), m.t("lock.pathPlaceholder")).Grow(1)
		if ui.Button(c, m.t("lock.browse")).Outline().Clicked() {
			after(c, m.browseDatabase)
		}
	})
	var field ui.Box
	captioned(c, m.t("lock.password"), func() {
		field = m.passwordField(
			ui.TextField(c, &m.lock.password, m.t("lock.password"), m.t("lock.passwordPlaceholder")),
			&m.lock.showPassword)
	})
	ui.Checkbox(c, &m.lock.useKeyFile, m.t("lock.useKeyFile"))
	if m.lock.useKeyFile {
		captioned(c, m.t("lock.keyFilePlaceholder"), func() {
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.TextField(c, &m.lock.keyFile, m.t("lock.keyFilePlaceholder"), m.t("lock.keyFilePlaceholder")).Grow(1)
				if ui.Button(c, m.t("lock.chooseKeyFile")).Outline().Clicked() {
					after(c, m.browseKeyFile)
				}
			})
		})
	}
	entered := field.Submitted()
	ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
		if ui.Button(c, m.t("lock.unlock")).Clicked() || entered {
			after(c, m.unlock)
		}
		if ui.Button(c, m.t("lock.newDatabase")).Outline().Clicked() {
			m.lock.create = true
			m.lock.message = ""
			if m.lock.path == "" {
				m.lock.path = m.defaultDatabasePath("Database")
			}
		}
		ui.Spacer(c)
	})
	m.lockRecents(c)
}

// lockRecents lists the databases used before as rows: the name is what the user
// recognizes the file by and the directory tells two files of the same name apart.
func (m *Model) lockRecents(c ui.C) {
	recent := m.sess.Config().Get().RecentFiles
	if len(recent) == 0 {
		return
	}
	ui.Divider(c)
	ui.Text(c, m.t("menu.openRecent")).Muted().FontSize(12)
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		for _, path := range recent {
			path := path
			row := ui.Row(c).FillWidth().Height(32).Items(ui.Center).Gap(10).PadX(8).
				Radius(6).Hit(ui.ID(c), fileLabel(path)).Hover(ui.Look{}.Background(m.theme.Hover)).
				Tooltip(path)
			if path == m.lock.path {
				row.Background(m.theme.Selection)
			}
			row.Children(func() {
				ui.Text(c, fileLabel(path)).Grow(1).Ellipsis()
				ui.Text(c, shortDir(path)).Muted().FontSize(11).Ellipsis()
				if ui.Button(c, m.t("lock.removeRecent")).Ghost().Height(26).FontSize(12).
					Tooltip(m.tf("lock.removeRecentNamed", "name", fileLabel(path))).Clicked() {
					after(c, func() { m.removeRecent(path) })
				}
			})
			if row.Clicked() {
				m.lock.path = path
				m.lock.useKeyFile = false
				m.lock.message, m.lock.messageErr = "", false
			}
		}
	})
}

// removeRecent forgets one file from the lock screen without opening it. When the
// removed file is the current selection, the path field is cleared as well, so the
// screen never appears to select an entry that is no longer in the list.
func (m *Model) removeRecent(path string) {
	if err := m.sess.Config().RemoveRecent(path); err != nil {
		m.fail(err)
		return
	}
	if m.lock.path == path {
		m.lock.path = ""
		m.lock.useKeyFile = false
		m.lock.message, m.lock.messageErr = "", false
	}
	m.info(m.tf("toast.recentRemoved", "name", fileLabel(path)))
}

// lockCreateFields is the new-database half of the lock screen.
func (m *Model) lockCreateFields(c ui.C) {
	ui.Text(c, m.t("create.title")).Bold()
	ui.Text(c, m.t("create.file")).Muted().FontSize(12)
	ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
		ui.TextField(c, &m.lock.path, m.t("create.file"), m.t("create.pathPlaceholder")).Grow(1)
		if ui.Button(c, m.t("lock.browse")).Outline().Clicked() {
			after(c, m.browseCreateTarget)
		}
	})
	captioned(c, m.t("create.name"), func() {
		ui.TextField(c, &m.lock.name, m.t("create.name"), m.t("create.defaultName"))
	})
	captioned(c, m.t("create.password"), func() {
		m.passwordField(
			ui.TextField(c, &m.lock.password, m.t("create.password"), m.t("lock.passwordPlaceholder")),
			&m.lock.showPassword)
	})
	captioned(c, m.t("create.repeat"), func() {
		m.passwordField(
			ui.TextField(c, &m.lock.repeat, m.t("create.repeat"), m.t("lock.passwordPlaceholder")),
			&m.lock.showRepeat)
	})
	ui.Text(c, m.t("create.preset")).Muted().FontSize(12)
	m.presetRows(c)
	ui.Paragraph(c, m.t("create.presetHint"), 3).Muted().FontSize(12)
	ui.Paragraph(c, m.tf("create.formatHint", "ext", databaseExtension), 2).Muted().FontSize(12)
	ui.Checkbox(c, &m.lock.useKeyFile, m.t("lock.useKeyFile"))
	if m.lock.useKeyFile {
		captioned(c, m.t("lock.keyFilePlaceholder"), func() {
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.TextField(c, &m.lock.keyFile, m.t("lock.keyFilePlaceholder"), m.t("lock.keyFilePlaceholder")).Grow(1)
				if ui.Button(c, m.t("lock.chooseKeyFile")).Outline().Clicked() {
					after(c, m.browseKeyFile)
				}
				if ui.Button(c, m.t("create.generateKeyFile")).Outline().Clicked() {
					after(c, m.createKeyFileAndUse)
				}
			})
		})
	}
	if m.lock.password != "" {
		_, score := estimate(m.lock.password, false)
		ui.Text(c, m.tf("create.strength", "label", m.qualityLabel(score), "score", score)).Muted().FontSize(12)
	}
	ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
		if ui.Button(c, m.t("create.create")).Clicked() {
			after(c, m.createDatabase)
		}
		if ui.Button(c, m.t("create.cancel")).Outline().Clicked() {
			m.lock.create = false
			m.lock.message = ""
		}
		ui.Spacer(c)
	})
}

// presetRows are the security presets of a new database, one radio row each. Every
// option carries its own label, so the choice stays readable in every language; the
// whole row is the target, not only the marker.
func (m *Model) presetRows(c ui.C) {
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		for _, preset := range presetNames {
			preset := preset
			chosen := m.lock.preset == preset
			row := ui.RadioBase(c, &m.lock.preset, preset, m.presetLabel(preset)).
				FillWidth().Height(28).PadX(8).Radius(6).
				Hover(ui.Look{}.Background(m.theme.Hover))
			if chosen {
				row.Background(m.theme.Selection)
			}
			row.Children(func() {
				marker := "○"
				if chosen {
					marker = "●"
				}
				ui.Text(c, marker).Width(18)
				ui.Text(c, m.presetLabel(preset)).Grow(1).Ellipsis()
			})
		}
	})
}

// presetLabel names a security preset.
func (m *Model) presetLabel(name string) string {
	switch name {
	case presetStrong:
		return m.t("create.presetStrong")
	case presetParanoid:
		return m.t("create.presetParanoid")
	case presetFast:
		return m.t("create.presetFast")
	default:
		return m.t("create.presetStandard")
	}
}

// browseDatabase asks for an existing database file.
func (m *Model) browseDatabase() {
	paths, err := app.OpenFiles(app.OpenFileOptions{
		Title: m.t("fileDialog.openDatabase"),
		Filters: []app.FileFilter{
			{Name: m.t("fileDialog.databases"), Pattern: "*" + databaseExtension},
			{Name: m.t("fileDialog.allFiles"), Pattern: "*.*"},
		},
	})
	if err != nil || len(paths) == 0 {
		return
	}
	m.lock.path = paths[0]
	m.lock.message = ""
}

// browseCreateTarget asks where a new database should be written.
func (m *Model) browseCreateTarget() {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            m.t("create.title"),
		SuggestedName:    defaultFileName(m.lock),
		DefaultExtension: defaultExtension(m.lock),
		Filters: []app.FileFilter{{
			Name:    m.t("fileDialog.databases"),
			Pattern: "*" + defaultExtension(m.lock),
		}},
	})
	if err != nil || path == "" {
		return
	}
	m.lock.path = path
	if m.lock.name == "" {
		m.lock.name = strings.TrimSuffix(fileLabel(path), defaultExtension(m.lock))
	}
	m.lock.message = ""
}

// browseKeyFile asks for an existing key file.
func (m *Model) browseKeyFile() {
	paths, err := app.OpenFiles(app.OpenFileOptions{
		Title: m.t("fileDialog.saveKeyFile"),
		Filters: []app.FileFilter{
			{Name: "Key files", Pattern: "*.keyx;*.key"},
			{Name: m.t("fileDialog.allFiles"), Pattern: "*.*"},
		},
	})
	if err != nil || len(paths) == 0 {
		return
	}
	m.lock.keyFile = paths[0]
	m.lock.useKeyFile = true
}

// unlock opens the chosen database.
func (m *Model) unlock() {
	keyFile := ""
	if m.lock.useKeyFile {
		keyFile = m.lock.keyFile
	}
	_, err := m.sess.Open(m.lock.path, m.lock.password, keyFile)
	if err != nil {
		m.lock.message = m.errorText(err)
		m.lock.messageErr = true
		return
	}
	// The password is left behind with the screen that asked for it, and so is a field
	// the user had revealed: the next database starts masked.
	m.lock.password, m.lock.repeat = "", ""
	m.lock.showPassword, m.lock.showRepeat = false, false
	m.lock.message, m.lock.messageErr = "", false
	m.reload()
	m.info(m.tf("toast.opened", "name", fileLabel(m.lock.path)))
}

// createDatabase creates the new database the user described.
func (m *Model) createDatabase() {
	if m.lock.password != m.lock.repeat {
		m.lock.message = m.t("create.passwordMismatch")
		m.lock.messageErr = true
		return
	}
	keyFile := ""
	if m.lock.useKeyFile {
		keyFile = m.lock.keyFile
	}
	if m.lock.password == "" && keyFile == "" {
		m.lock.message = m.t("create.needKey")
		m.lock.messageErr = true
		return
	}
	settings := settingsPreset(m.lock.preset)
	settings.Name = m.lock.name
	if _, err := m.sess.Create(m.lock.path, m.lock.name, m.lock.password, keyFile, settings); err != nil {
		m.lock.message = m.errorText(err)
		m.lock.messageErr = true
		return
	}
	m.lock.create = false
	m.lock.password, m.lock.repeat = "", ""
	m.lock.showPassword, m.lock.showRepeat = false, false
	m.lock.message, m.lock.messageErr = "", false
	m.reload()
	m.info(m.t("toast.created"))
}

// openRecent opens a database from the recent files list.
func (m *Model) openRecent(path string) {
	m.lock.path = path
	m.lock.create = false
	if !m.sess.Unlocked() {
		m.lock.password = ""
		m.lock.showPassword = false
		m.lock.message = m.t("lock.passwordPlaceholder")
		m.lock.messageErr = false
		return
	}
	m.unlock()
}

// defaultFileName suggests a name for a new database file.
func defaultFileName(lock lockState) string {
	name := strings.TrimSpace(lock.name)
	if name == "" {
		name = "Database"
	}
	return name + defaultExtension(lock)
}

// defaultExtension is the file extension the chosen format uses.
func defaultExtension(lock lockState) string {
	return databaseExtension
}
