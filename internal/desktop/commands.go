package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"

	"gokeep/internal/dbdoc"
	"gokeep/internal/vault"
)

// cmdNewDatabase locks the workspace and shows the new-database card.
func (m *Model) cmdNewDatabase() {
	if m.sess.Unlocked() {
		m.sess.Lock()
	}
	m.lock = lockState{
		create:     true,
		preset:     presetStandard,
		useKeyFile: false,
	}
	m.lock.path = m.defaultDatabasePath("Database")
}

// cmdOpenDatabase asks for a database file and shows the unlock card.
func (m *Model) cmdOpenDatabase() {
	if m.sess.Unlocked() {
		m.sess.Lock()
	}
	m.lock = lockState{preset: presetStandard}
	m.browseDatabase()
	if m.lock.path == "" {
		m.lock.path = m.sess.Config().Get().LastDatabase
	}
}

// cmdSave writes the open database in place.
func (m *Model) cmdSave() {
	if !m.sess.Unlocked() {
		return
	}
	if err := m.sess.Save(); err != nil {
		m.fail(err)
		return
	}
	m.info(m.t("toast.saved"))
}

// cmdSaveAs writes the open database to a new file, with the same master key.
func (m *Model) cmdSaveAs() {
	v := m.vault()
	if v == nil {
		return
	}
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            m.t("fileDialog.saveAs"),
		SuggestedName:    filepath.Base(v.Path()),
		DefaultExtension: filepath.Ext(v.Path()),
		Filters: []app.FileFilter{{
			Name:    m.t("fileDialog.databases"),
			Pattern: "*" + filepath.Ext(v.Path()),
		}},
	})
	if err != nil || path == "" {
		return
	}
	key := v.Key()
	if err := v.SaveAs(path, key); err != nil {
		m.fail(err)
		return
	}
	_ = m.sess.Config().AddRecent(path)
	m.info(m.tf("toast.savedAsPath", "path", path))
}

// cmdCloseDatabase asks about unsaved changes and then locks the workspace.
func (m *Model) cmdCloseDatabase() {
	v := m.vault()
	if v == nil {
		return
	}
	if v.Modified() {
		m.ask(m.t("confirm.unsavedTitle"), m.t("confirm.unsavedMessage"), m.t("fileDialog.saveAs"), false, func() {
			if err := m.sess.Save(); err != nil {
				m.fail(err)
				return
			}
			m.sess.Lock()
			m.afterClose()
		})
		return
	}
	m.sess.Lock()
	m.afterClose()
}

// cmdLock saves when needed and locks the workspace.
func (m *Model) cmdLock() {
	v := m.vault()
	if v == nil {
		return
	}
	if v.Modified() {
		if err := m.sess.Save(); err != nil {
			m.fail(err)
			return
		}
	}
	m.sess.Lock()
	m.afterClose()
}

// afterClose resets the panes after the database was closed.
func (m *Model) afterClose() {
	m.groupID, m.entryID, m.search = "", "", ""
	m.entries, m.rows = nil, nil
	m.detail = vault.EntryDTO{}
	m.hasDetail = false
	m.editor = editorState{}
	m.group = groupState{}
	m.gen = generatorState{}
	m.opts = optionsState{}
	m.dbset = settingsState{}
	m.key = keyState{}
	m.move = moveState{}
	m.confirm = confirmState{}
	m.searchOpen, m.searchResult = false, false
	m.dups, m.expired = nil, nil
	m.about, m.shortcutsHelp = false, false
	m.quitAsk, m.allowClose = false, nil
	m.focus = ""
	m.lock = lockState{preset: presetStandard, path: m.sess.Config().Get().LastDatabase}
	m.info(m.t("toast.clipboardCleared"))
}

// handleCloseRequest is what the window asks when the user closes it: the page draws the
// question about unsaved changes, so the window stays open until it has been answered,
// and a workspace with nothing to save closes at once.
func (m *Model) handleCloseRequest(allow func()) {
	if m.sess.Quitting() {
		allow()
		return
	}
	if v := m.vault(); v == nil || !v.Modified() {
		m.finishQuit(allow)
		return
	}
	m.allowClose = allow
	m.quitAsk = true
}

// cmdQuit closes the window after asking about unsaved changes.
func (m *Model) cmdQuit() {
	if m.window == nil {
		return
	}
	if v := m.vault(); v != nil && v.Modified() {
		m.quitAsk = true
		return
	}
	m.finishQuit(nil)
}

// cmdNewEntry opens the editor for a new entry in the selected group.
func (m *Model) cmdNewEntry() {
	v := m.vault()
	if v == nil {
		return
	}
	groupID := m.groupID
	if groupID == "" {
		groupID = m.firstGroupID()
	}
	settings, err := v.DatabaseSettings()
	if err != nil {
		m.fail(err)
		return
	}
	m.editor = editorState{
		open:  true,
		isNew: true,
		tab:   "general",
		entry: vault.EntryDTO{
			GroupID:         groupID,
			IconID:          0,
			AutoTypeEnabled: true,
			UserName:        settings.DefaultUserName,
		},
	}
}

// firstGroupID is the group a new entry goes into when the whole database is
// selected: the first real group, or the root when there is none.
func (m *Model) firstGroupID() string {
	for _, row := range m.rows {
		if row.id != "" {
			return row.id
		}
	}
	if v := m.vault(); v != nil {
		return v.RootID()
	}
	return ""
}

// cmdNewGroup opens the editor for a new group.
func (m *Model) cmdNewGroup() {
	parent := m.groupID
	if parent == "" {
		if v := m.vault(); v != nil {
			parent = v.RootID()
		}
	}
	m.group = groupState{open: true, isNew: true, parentID: parent, autoType: "inherit", searching: "inherit"}
}

// openGroupEditor opens the editor for an existing group.
func (m *Model) openGroupEditor(id string) {
	for _, row := range m.rows {
		if row.id != id {
			continue
		}
		m.group = groupState{
			open:      true,
			id:        row.id,
			parentID:  "",
			name:      row.name,
			autoType:  "inherit",
			searching: "inherit",
		}
	}
}

// openEntryEditor opens the editor for an existing entry.
func (m *Model) openEntryEditor(id string) {
	v := m.vault()
	if v == nil || id == "" {
		return
	}
	dto, err := v.Entry(id)
	if err != nil {
		m.fail(err)
		return
	}
	m.editor = editorState{open: true, tab: "general", entry: dto}
}

// cmdDuplicate copies the selected entry.
func (m *Model) cmdDuplicate() {
	v := m.vault()
	if v == nil || m.entryID == "" {
		return
	}
	if _, err := v.DuplicateEntry(m.entryID); err != nil {
		m.fail(err)
		return
	}
	m.reload()
	m.info(m.t("toast.created"))
}

// cmdDeleteEntry deletes the selected entry, asking first.
func (m *Model) cmdDeleteEntry(permanent bool) {
	if m.entryID == "" {
		return
	}
	entry, _ := m.currentEntry()
	m.confirmDeleteEntry(m.entryID, entry.Title, permanent)
}

// confirmDeleteEntry asks before an entry is deleted.
func (m *Model) confirmDeleteEntry(id, title string, permanent bool) {
	if m.sess.Config().Get().ConfirmDelete {
		body := m.tf("confirm.deleteToBin", "title", title)
		ok := m.t("confirm.moveToBin")
		if permanent {
			body = m.tf("confirm.deletePermanent", "title", title)
			ok = m.t("confirm.deleteForever")
		}
		m.ask(m.t("confirm.deleteTitle"), body, ok, true, func() { m.deleteEntry(id, permanent) })
		return
	}
	m.deleteEntry(id, permanent)
}

// deleteEntry removes one entry, into the recycle bin unless it is permanent.
func (m *Model) deleteEntry(id string, permanent bool) {
	v := m.vault()
	if v == nil {
		return
	}
	// The title is read before the entry goes away: the confirmation names what was
	// removed, and a deleted entry cannot be asked for its name afterwards.
	title := id
	if dto, err := v.Entry(id); err == nil && strings.TrimSpace(dto.Title) != "" {
		title = dto.Title
	}
	if err := v.DeleteEntry(id, permanent); err != nil {
		m.fail(err)
		return
	}
	if m.entryID == id {
		m.entryID = ""
		m.hasDetail = false
	}
	m.reload()
	if permanent {
		m.info(m.tf("toast.deletedForever", "title", title))
	} else {
		m.info(m.tf("toast.movedToBin", "title", title))
	}
}

// askDeleteGroup asks before a group and everything in it goes away.
func (m *Model) askDeleteGroup(id, name string) {
	body := m.tf("confirm.deleteGroupMessage", "name", name)
	m.ask(m.tf("confirm.deleteGroupTitle", "name", name), body, m.t("confirm.deleteGroup"), true, func() {
		v := m.vault()
		if v == nil {
			return
		}
		if err := v.DeleteGroup(id, false); err != nil {
			m.fail(err)
			return
		}
		if m.groupID == id {
			m.groupID = ""
			m.entryID = ""
		}
		m.reload()
		m.info(m.tf("toast.movedToBin", "title", name))
	})
}

// cmdCopyField copies a field of the selected entry.
func (m *Model) cmdCopyField(field string) {
	if m.entryID == "" {
		return
	}
	m.copyEntryField(m.entryID, field)
}

// copyEntryField reads one field and puts it on the clipboard.
func (m *Model) copyEntryField(entryID, field string) {
	v := m.vault()
	if v == nil {
		return
	}
	dto, err := v.Entry(entryID)
	if err != nil {
		m.fail(err)
		return
	}
	value, sensitive := dto.FieldValue(field)
	if strings.TrimSpace(value) == "" {
		m.warn(m.tf("toast.fieldEmpty", "field", m.fieldLabel(field)))
		return
	}
	if err := m.sess.CopyToClipboard(value, sensitive); err != nil {
		m.fail(err)
		return
	}
	if sensitive {
		m.info(m.t("toast.copiedToClipboard"))
	} else {
		m.info(m.tf("toast.copied", "field", m.fieldLabel(field)))
	}
}

// cmdOpenURL opens the selected entry's URL in the default browser.
func (m *Model) cmdOpenURL() {
	if !m.hasDetail {
		return
	}
	target := strings.TrimSpace(m.detail.URL)
	if target == "" {
		m.warn(m.t("toast.noUrl"))
		return
	}
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	if err := app.OpenURL(target); err != nil {
		m.fail(err)
		return
	}
	if v := m.vault(); v != nil {
		_ = v.TouchEntryAccess(m.detail.ID)
	}
}

// openMoveDialog opens the dialog that moves an entry or a group.
func (m *Model) openMoveDialog(entry bool, id, label string) {
	m.move = moveState{open: true, entry: entry, id: id, label: label}
}

// openSettings reads the database metadata into the settings dialog.
func (m *Model) openSettings() {
	v := m.vault()
	if v == nil {
		return
	}
	settings, err := v.DatabaseSettings()
	if err != nil {
		m.fail(err)
		return
	}
	m.dbset = settingsState{open: true, settings: settings}
}

// cmdEmptyRecycleBin asks before the recycle bin is emptied.
func (m *Model) cmdEmptyRecycleBin(permanent bool) {
	if m.recycleBinID() == "" {
		m.warn(m.t("group.menuEmptyBin"))
		return
	}
	m.ask(m.t("confirm.emptyBinTitle"), m.t("confirm.emptyBinMessage"), m.t("confirm.emptyBin"), true, func() {
		v := m.vault()
		if v == nil {
			return
		}
		if err := v.EmptyRecycleBin(); err != nil {
			m.fail(err)
			return
		}
		m.reload()
		m.info(m.t("toast.recycleEmptied"))
	})
}

// cmdFindDuplicates looks for entries that share a password.
func (m *Model) cmdFindDuplicates() {
	v := m.vault()
	if v == nil {
		return
	}
	groups, err := v.FindDuplicates()
	if err != nil {
		m.fail(err)
		return
	}
	m.dups = groups
}

// cmdCheckExpired lists entries whose expiry date has passed.
func (m *Model) cmdCheckExpired() {
	v := m.vault()
	if v == nil {
		return
	}
	entries, err := v.ExpiredEntries()
	if err != nil {
		m.fail(err)
		return
	}
	m.expired = entries
}

// cmdCreateKeyFile writes a new key file the user chooses.
func (m *Model) cmdCreateKeyFile() {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            m.t("fileDialog.createKeyFile"),
		SuggestedName:    "GoKeep.keyx",
		DefaultExtension: ".keyx",
		Filters:          []app.FileFilter{{Name: "Key files", Pattern: "*.keyx;*.key"}},
	})
	if err != nil || path == "" {
		return
	}
	if err := dbdoc.WriteKeyFile(path, false); err != nil {
		m.fail(err)
		return
	}
	m.info(m.t("toast.keyFileCreatedPlain"))
}

// createKeyFileAndUse writes a key file and selects it for the database being created.
func (m *Model) createKeyFileAndUse() {
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            m.t("fileDialog.createKeyFile"),
		SuggestedName:    "GoKeep.keyx",
		DefaultExtension: ".keyx",
		Filters:          []app.FileFilter{{Name: "Key files", Pattern: "*.keyx;*.key"}},
	})
	if err != nil || path == "" {
		return
	}
	if err := dbdoc.WriteKeyFile(path, false); err != nil {
		m.fail(err)
		return
	}
	m.lock.keyFile = path
	m.lock.useKeyFile = true
	m.info(m.tf("toast.keyFileCreated", "path", path))
}

// cmdExportCSV writes every entry of the open database to a CSV file.
func (m *Model) cmdExportCSV() {
	v := m.vault()
	if v == nil {
		return
	}
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            m.t("fileDialog.exportCsv"),
		SuggestedName:    strings.TrimSuffix(filepath.Base(v.Path()), filepath.Ext(v.Path())) + ".csv",
		DefaultExtension: ".csv",
		Filters:          []app.FileFilter{{Name: "CSV", Pattern: "*.csv"}},
	})
	if err != nil || path == "" {
		return
	}
	file, err := os.Create(path)
	if err != nil {
		m.fail(err)
		return
	}
	defer file.Close()
	if err := v.ExportCSV(file); err != nil {
		m.fail(err)
		return
	}
	m.info(m.t("toast.exported"))
}

// cmdImportCSV reads entries from a CSV file into the selected group.
func (m *Model) cmdImportCSV() {
	v := m.vault()
	if v == nil {
		return
	}
	paths, err := app.OpenFiles(app.OpenFileOptions{
		Title:   m.t("fileDialog.importCsv"),
		Filters: []app.FileFilter{{Name: "CSV", Pattern: "*.csv"}},
	})
	if err != nil || len(paths) == 0 {
		return
	}
	file, err := os.Open(paths[0])
	if err != nil {
		m.fail(err)
		return
	}
	defer file.Close()
	target := m.groupID
	if target == "" {
		target = m.firstGroupID()
	}
	count, err := v.ImportCSV(file, target)
	if err != nil {
		m.fail(err)
		return
	}
	m.reload()
	m.info(m.tf("toast.imported", "count", count))
}

// cmdBackup copies the database file next to itself, the way KeePass keeps backups.
func (m *Model) cmdBackup() {
	v := m.vault()
	if v == nil {
		return
	}
	source := v.Path()
	data, err := os.ReadFile(source)
	if err != nil {
		m.fail(err)
		return
	}
	dir := filepath.Dir(source)
	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	ext := filepath.Ext(source)
	target := filepath.Join(dir, fmt.Sprintf("%s-%s%s", base, time.Now().Format("20060102-150405"), ext))
	if err := os.WriteFile(target, data, 0o600); err != nil {
		m.fail(err)
		return
	}
	m.info(m.tf("toast.backupDone", "path", target))
}

// refreshWindowTitle is kept for the places that used to rename the window after the
// open database changed. Viva takes the native title from app.Config.Title once, so the
// database name is shown by the title strip the page draws instead.
func (m *Model) refreshWindowTitle() {
}
