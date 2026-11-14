package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"gokeep/internal/config"
	"gokeep/internal/vault"
)

// newTestPage opens the page in a headless driver with a configuration file of its
// own, in the language the test asks for.
func newTestPage(t *testing.T, language string) (*app.Driver, *Model) {
	t.Helper()
	store, err := config.NewAt(filepath.Join(t.TempDir(), "GoKeep.config.json"))
	if err != nil {
		t.Fatalf("cannot open the test configuration: %v", err)
	}
	cfg := store.Get()
	cfg.Language = language
	if err := store.Set(cfg); err != nil {
		t.Fatalf("cannot save the test configuration: %v", err)
	}
	session := NewSession("test", store)
	model := newModel(session)
	page := ui.NewPage(ui.PageConfig{View: model.page, Theme: &model.theme})
	windowConfig := page.Config(app.Config{Title: "GoKeep", Width: 1180, Height: 760})
	// The window's repaint request is the session's, exactly as in the application.
	session.SetInvalidator(windowConfig.Invalidator)
	driver, err := app.NewDriver(windowConfig, nil)
	if err != nil {
		t.Fatalf("cannot start the headless driver: %v", err)
	}
	t.Cleanup(driver.Close)
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the page: %v", err)
	}
	return driver, model
}

// paint asks for a repaint and applies it: a test that changed the model between two
// clicks is looking at a frame that has not been drawn yet otherwise.
func paint(driver *app.Driver, model *Model) error {
	model.sess.Invalidate()
	return driver.Pump()
}

// clickLabel presses the control the page labeled label, the way a click on its middle
// does, and paints the frame the click causes.
func clickLabel(t *testing.T, driver *app.Driver, model *Model, label string) {
	t.Helper()
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint before clicking %q: %v", label, err)
	}
	snapshot := driver.Snapshot()
	// Hits are in back-to-front paint order, so the last one is the topmost.
	for index := len(snapshot.Hits) - 1; index >= 0; index-- {
		hit := snapshot.Hits[index]
		if hit.Label != label || hit.Disabled || hit.Bounds.Width <= 0 || hit.Bounds.Height <= 0 {
			continue
		}
		x := hit.Bounds.X + hit.Bounds.Width/2
		y := hit.Bounds.Y + hit.Bounds.Height/2
		if err := driver.Click(x, y); err != nil {
			t.Fatalf("clicking %q: %v", label, err)
		}
		if err := paint(driver, model); err != nil {
			t.Fatalf("cannot paint after clicking %q: %v", label, err)
		}
		return
	}
	t.Fatalf("the page shows no control labeled %q", label)
}

// create opens a new database for the test.
func create(t *testing.T, model *Model, password string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Database"+databaseExtension)
	if _, err := model.sess.Create(path, "Test database", password, "", settingsPreset(presetStandard)); err != nil {
		t.Fatalf("cannot create %s: %v", path, err)
	}
	model.reload()
	return path
}

func TestNewDatabaseHasNoGroupsAtAll(t *testing.T) {
	_, model := newTestPage(t, "en")
	create(t, model, "master")
	if len(model.rows) != 1 {
		names := make([]string, 0, len(model.rows))
		for _, row := range model.rows {
			names = append(names, row.name)
		}
		t.Fatalf("a new database shows %d group rows (%s), want only the root row",
			len(model.rows), strings.Join(names, ", "))
	}
	if len(model.entries) != 0 {
		t.Fatalf("a new database shows %d entries, want none", len(model.entries))
	}
}

func TestDeleteMovesTheEntryToTheRecycleBinAndShowsIt(t *testing.T) {
	_, model := newTestPage(t, "en")
	create(t, model, "master")
	v := model.vault()
	groupID, err := v.CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	entryID, err := v.CreateEntry(groupID, vault.EntryDTO{Title: "Mailbox", UserName: "user", Password: "secret"})
	if err != nil {
		t.Fatalf("cannot create the entry: %v", err)
	}
	model.reload()
	model.selectGroup(groupID)

	model.deleteEntry(entryID, false)

	binID := model.recycleBinID()
	if binID == "" {
		t.Fatal("deleting an entry did not create the recycle bin")
	}
	if entries, err := v.Entries(binID, false, true); err != nil || len(entries) != 1 {
		t.Fatalf("the recycle bin holds %d entries (err %v), want 1", len(entries), err)
	}
	model.reload()
	found := false
	for _, row := range model.rows {
		if row.id != binID {
			continue
		}
		found = true
		if row.name != model.t("app.recycleBin") {
			t.Fatalf("the recycle bin row is named %q, want %q", row.name, model.t("app.recycleBin"))
		}
		if !row.recycled {
			t.Fatal("the recycle bin row is not marked as the recycle bin")
		}
	}
	if !found {
		t.Fatalf("the recycle bin does not appear in the group tree: %v", rowNames(model))
	}
	model.selectGroup(binID)
	if len(model.entries) != 1 || !model.entries[0].IsRecycled {
		t.Fatalf("the recycle bin shows %d entries, want the deleted one", len(model.entries))
	}
}

func TestRecycleBinCanBeHiddenWithoutLosingIt(t *testing.T) {
	_, model := newTestPage(t, "en")
	create(t, model, "master")
	v := model.vault()
	groupID, err := v.CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	entryID, err := v.CreateEntry(groupID, vault.EntryDTO{Title: "Mailbox", Password: "secret"})
	if err != nil {
		t.Fatalf("cannot create the entry: %v", err)
	}
	model.deleteEntry(entryID, false)
	binID := model.recycleBinID()

	cfg := model.sess.Config().Get()
	cfg.ShowRecycleBin = false
	if err := model.sess.Config().Set(cfg); err != nil {
		t.Fatalf("cannot save the configuration: %v", err)
	}
	model.reload()
	for _, row := range model.rows {
		if row.id == binID {
			t.Fatal("the recycle bin is still shown although it was switched off")
		}
	}
	if entries, err := v.Entries(binID, false, true); err != nil || len(entries) != 1 {
		t.Fatalf("switching the recycle bin off lost the entry: %d entries, err %v", len(entries), err)
	}
}

func TestTheDeleteDialogMovesTheEntryToTheRecycleBin(t *testing.T) {
	driver, model := newTestPage(t, "en")
	create(t, model, "master")
	v := model.vault()
	groupID, err := v.CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	entryID, err := v.CreateEntry(groupID, vault.EntryDTO{Title: "Mailbox", Password: "secret"})
	if err != nil {
		t.Fatalf("cannot create the entry: %v", err)
	}
	model.reload()
	model.selectGroup(groupID)
	model.selectEntry(entryID)

	// The toolbar's Delete asks first; the dialog's own OK button must delete, which
	// is the answer a click on it means.
	clickLabel(t, driver, model, model.t("toolbar.delete"))
	clickLabel(t, driver, model, model.t("confirm.moveToBin"))

	binID := model.recycleBinID()
	if binID == "" {
		t.Fatal("the confirmed delete did not create the recycle bin")
	}
	if entries, err := v.Entries(binID, false, true); err != nil || len(entries) != 1 {
		t.Fatalf("after the confirmed delete the recycle bin holds %d entries (err %v), want 1", len(entries), err)
	}
}

func TestTheCancelButtonOfTheDeleteDialogKeepsTheEntry(t *testing.T) {
	driver, model := newTestPage(t, "en")
	create(t, model, "master")
	v := model.vault()
	groupID, err := v.CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	entryID, err := v.CreateEntry(groupID, vault.EntryDTO{Title: "Mailbox", Password: "secret"})
	if err != nil {
		t.Fatalf("cannot create the entry: %v", err)
	}
	model.reload()
	model.selectGroup(groupID)
	model.selectEntry(entryID)

	clickLabel(t, driver, model, model.t("toolbar.delete"))
	clickLabel(t, driver, model, model.t("dialog.cancel"))

	if entries, err := v.Entries(groupID, false, false); err != nil || len(entries) != 1 {
		t.Fatalf("cancelling the dialog left %d entries in the group (err %v), want 1", len(entries), err)
	}
	if model.confirm.open {
		t.Fatal("the confirmation dialog is still open after Cancel")
	}
}

func TestWrongPasswordGivesATranslatedSentence(t *testing.T) {
	// The container authenticates its contents, so a wrong password has to be answered
	// with the sentence for it rather than with the storage layer's English.
	_, model := newTestPage(t, "ja")
	path := create(t, model, "right")
	model.sess.Lock()
	model.lock = lockState{path: path, password: "wrong"}
	model.unlock()
	if !model.lock.messageErr {
		t.Fatalf("a wrong password was accepted: %q", model.lock.message)
	}
	if strings.Contains(model.lock.message, "[object") || strings.Contains(model.lock.message, "object Object") {
		t.Fatalf("the message is not readable: %q", model.lock.message)
	}
	if want := model.t("errors.wrong-key"); model.lock.message != want {
		t.Fatalf("the message is %q, want the translated %q", model.lock.message, want)
	}
}

// write puts a file where the lock screen expects a database, so the tests can hand it
// contents no writer of this program would produce.
func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	return path
}

func TestADamagedFileGivesATranslatedSentence(t *testing.T) {
	_, model := newTestPage(t, "zh-CN")
	// The signature of a GoKeep database and nothing after it.
	path := write(t, "Truncated.gkdb", []byte("GKDB"))
	model.lock = lockState{path: path, password: "master"}
	model.unlock()
	if !model.lock.messageErr {
		t.Fatal("a truncated database was accepted")
	}
	if strings.Contains(model.lock.message, "gkdb:") {
		t.Fatalf("the raw message of the storage layer reached the user: %q", model.lock.message)
	}
	if want := model.t("errors.damaged"); model.lock.message != want {
		t.Fatalf("the message is %q, want the translated %q", model.lock.message, want)
	}
}

func TestAFileThatIsNotADatabaseSaysSo(t *testing.T) {
	_, model := newTestPage(t, "zh-CN")
	// The signature of a KDBX file: a database of another password manager, which this
	// build does not read.
	path := write(t, "Other.kdbx", []byte{0x03, 0xD9, 0xA2, 0x9A, 0x67, 0xFB, 0x4B, 0xB5, 0x01, 0x00, 0x03, 0x00})
	model.lock = lockState{path: path, password: "master"}
	model.unlock()
	if !model.lock.messageErr {
		t.Fatal("a file of another format was accepted as a database")
	}
	if strings.Contains(model.lock.message, "gkdb:") {
		t.Fatalf("the raw message of the storage layer reached the user: %q", model.lock.message)
	}
	want := model.tf("errors.not-a-database", "name", fileLabel(path))
	if model.lock.message != want {
		t.Fatalf("the message is %q, want the translated %q", model.lock.message, want)
	}
}

// TestClosingTheWindowAsksOnlyAboutUnsavedChanges covers the whole close path: a
// workspace with nothing to lose closes at once, and one with unsaved changes keeps the
// window until the question has been answered.
func TestClosingTheWindowAsksOnlyAboutUnsavedChanges(t *testing.T) {
	// Nothing is open: closing closes, without a question.
	_, locked := newTestPage(t, "zh-CN")
	closed := 0
	allow := func() { closed++ }
	locked.handleCloseRequest(allow)
	if closed != 1 {
		t.Fatal("closing a locked workspace asked a question instead of closing")
	}
	if locked.quitAsk {
		t.Fatal("closing a locked workspace opened the unsaved-changes question")
	}

	// An open database with a change to it: the question appears. This is a page of its
	// own, because answering the question above is what ends a real session.
	driver, model := newTestPage(t, "zh-CN")
	create(t, model, "master")
	if _, err := model.vault().CreateGroup("", "Mail", 0); err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	model.reload()
	closed = 0
	model.handleCloseRequest(allow)
	if closed != 0 {
		t.Fatal("the window closed over unsaved changes")
	}
	if !model.quitAsk {
		t.Fatal("no question was asked about the unsaved changes")
	}

	// Cancel keeps the window open.
	clickLabel(t, driver, model, model.t("dialog.cancel"))
	if closed != 0 || model.quitAsk {
		t.Fatalf("cancel closed the window (%d) or left the question open", closed)
	}

	// Asking again and choosing not to save closes it.
	model.handleCloseRequest(allow)
	clickLabel(t, driver, model, model.t("confirm.unsavedDiscard"))
	if closed != 1 {
		t.Fatalf("choosing not to save left the window open after %d closes", closed)
	}
}

// TestBrowsingAnEntryDoesNotAskAboutUnsavedChanges reproduces the case where the only
// action after saving is selecting an entry. Selecting updates its access counters, but
// that bookkeeping alone must not make closing the window show the unsaved-change prompt.
func TestBrowsingAnEntryDoesNotAskAboutUnsavedChanges(t *testing.T) {
	_, model := newTestPage(t, "zh-CN")
	create(t, model, "master")
	v := model.vault()
	groupID, err := v.CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	entryID, err := v.CreateEntry(groupID, vault.EntryDTO{Title: "Mailbox", Password: "secret"})
	if err != nil {
		t.Fatalf("cannot create the entry: %v", err)
	}
	if err := v.Save(); err != nil {
		t.Fatalf("cannot save the test database: %v", err)
	}
	model.reload()
	model.selectGroup(groupID)
	model.selectEntry(entryID)
	if v.Modified() {
		t.Fatal("selecting an entry marked the database as modified")
	}

	closed := 0
	model.handleCloseRequest(func() { closed++ })
	if closed != 1 {
		t.Fatal("closing after only browsing an entry did not close the window")
	}
	if model.quitAsk {
		t.Fatal("closing after only browsing an entry opened the unsaved-changes question")
	}
}

// TestTheRecentListIsFullWidthRows pins the shape of the list: picking a database is a
// click anywhere on its row, not on a button as wide as its label in the middle of the card.
func TestTheRecentListIsFullWidthRows(t *testing.T) {
	driver, model := newTestPage(t, "zh-CN")
	path := create(t, model, "master")
	model.sess.Lock()
	model.lock = lockState{}
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the lock screen: %v", err)
	}
	snapshot := driver.Snapshot()
	widest := float32(0)
	for _, hit := range snapshot.Hits {
		if hit.Label == fileLabel(path) {
			widest = max(widest, hit.Bounds.Width)
		}
	}
	if widest < 400 {
		t.Fatalf("the recent row is %v wide, want the width of the card it is in", widest)
	}
	clickLabel(t, driver, model, fileLabel(path))
	if model.lock.path != path {
		t.Fatalf("clicking the recent row left the file as %q, want %q", model.lock.path, path)
	}
	if model.lock.messageErr {
		t.Fatalf("picking a recent file reported an error: %q", model.lock.message)
	}
}

func TestARecentDatabaseCanBeRemovedFromTheLockScreen(t *testing.T) {
	driver, model := newTestPage(t, "zh-CN")
	path := create(t, model, "master")
	model.sess.Lock()
	model.lock = lockState{path: path}
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the lock screen: %v", err)
	}

	clickLabel(t, driver, model, model.t("lock.removeRecent"))

	cfg := model.sess.Config().Get()
	if len(cfg.RecentFiles) != 0 {
		t.Fatalf("the recent list still holds %d files after removing the only one", len(cfg.RecentFiles))
	}
	if cfg.LastDatabase != "" {
		t.Fatalf("the removed database is still remembered as the last file: %q", cfg.LastDatabase)
	}
	if model.lock.path != "" {
		t.Fatalf("removing the selected recent file left %q in the path field", model.lock.path)
	}
	if model.sess.Unlocked() {
		t.Fatal("removing a recent file opened the database")
	}
}

func TestTheLockScreenOpensTheDatabase(t *testing.T) {
	_, model := newTestPage(t, "en")
	path := create(t, model, "master")
	model.sess.Lock()
	model.lock = lockState{path: path, password: "master"}
	model.unlock()
	if model.lock.messageErr {
		t.Fatalf("the correct password was refused: %q", model.lock.message)
	}
	if !model.sess.Unlocked() {
		t.Fatal("the database is not open after unlocking")
	}
	if model.vault().Path() != path {
		t.Fatalf("the open database is %q, want %q", model.vault().Path(), path)
	}
}

// TestTheCreateCardListsThePresetsOnSeparateRows pins the shape of the security presets:
// they used to be one segmented control whose four long labels were painted on top of
// each other, which is unreadable in every language.
func TestTheCreateCardListsThePresetsOnSeparateRows(t *testing.T) {
	driver, model := newTestPage(t, "zh-CN")
	if model.sess.Tr().Has("create.formatKdbx") {
		t.Error("the new-database card still offers a choice of file formats")
	}
	model.lock = lockState{
		create: true,
		preset: presetStandard,
		path:   filepath.Join(t.TempDir(), "New"+databaseExtension),
	}
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the new-database card: %v", err)
	}
	snapshot := driver.Snapshot()
	var tops, bottoms, widths []float32
	for _, preset := range presetNames {
		label := model.presetLabel(preset)
		found := false
		for index := len(snapshot.Hits) - 1; index >= 0; index-- {
			hit := snapshot.Hits[index]
			if hit.Label != label {
				continue
			}
			found = true
			tops = append(tops, hit.Bounds.Y)
			bottoms = append(bottoms, hit.Bounds.Y+hit.Bounds.Height)
			widths = append(widths, hit.Bounds.Width)
			break
		}
		if !found {
			t.Fatalf("the preset %q is not on the card", label)
		}
	}
	for i := 1; i < len(tops); i++ {
		if tops[i] < bottoms[i-1]-0.5 {
			t.Fatalf("preset row %d starts at %v, above the bottom %v of the row before it",
				i, tops[i], bottoms[i-1])
		}
	}
	for i, width := range widths {
		if width < 300 {
			t.Fatalf("preset row %d is %v wide, want the width of the card it is in", i, width)
		}
	}
	// The whole row is the target, not only a marker at its left.
	clickLabel(t, driver, model, model.presetLabel(presetParanoid))
	if model.lock.preset != presetParanoid {
		t.Fatalf("clicking a preset row left the chosen preset at %q", model.lock.preset)
	}
}

func TestTheLockScreenCreatesADatabase(t *testing.T) {
	driver, model := newTestPage(t, "en")
	path := filepath.Join(t.TempDir(), "Created"+databaseExtension)
	model.lock = lockState{create: true, preset: presetStandard, path: path, name: "Created", password: "master", repeat: "master"}
	clickLabel(t, driver, model, model.t("create.create"))

	if model.lock.messageErr {
		t.Fatalf("creating the database failed: %q", model.lock.message)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database file was not written: %v", err)
	}
	if !model.sess.Unlocked() {
		t.Fatal("the new database is not open")
	}
	if len(model.rows) != 1 {
		t.Fatalf("the new database shows %d group rows, want only the root row", len(model.rows))
	}
}

// TestEveryErrorCodeReadsAsAWholeSentence covers the two ways an error line reaches the
// user unfinished: a code the language pack does not hold, and a {placeholder} the code
// that raises the error never filled in.
func TestEveryErrorCodeReadsAsAWholeSentence(t *testing.T) {
	_, model := newTestPage(t, "zh-CN")
	cases := []*Error{
		{Code: CodeWrongKey},
		{Code: CodeWrongKeyFile, Values: map[string]string{"path": `C:\keys\mail.key`}},
		{Code: CodeNoFile, Values: map[string]string{"path": `C:\db\mail.gkdb`}},
		{Code: CodeNotADatabase, Values: map[string]string{"name": "Mail.kdbx"}},
		{Code: CodeVersionNew},
		{Code: CodeConflict, Values: map[string]string{"path": `C:\db\mail.gkdb`}},
		{Code: CodeEmptyField},
		{Code: CodeNoDatabase},
		{Code: CodeDamaged},
		{Code: CodeCreateFailed},
		{Code: CodeSaveFailed},
		{Code: "no-such-code"},
	}
	for _, err := range cases {
		text := model.errorText(err)
		if strings.TrimSpace(text) == "" {
			t.Errorf("the sentence for %q is empty", err.Code)
		}
		if strings.ContainsAny(text, "{}") {
			t.Errorf("the sentence for %q is %q: a placeholder was never filled", err.Code, text)
		}
	}
}

// TestAnErrorWithNoSentenceOfItsOwnStillReadsInTheUsersLanguage keeps the storage
// layer's English out of the interface: what it says goes to the log instead.
func TestAnErrorWithNoSentenceOfItsOwnStillReadsInTheUsersLanguage(t *testing.T) {
	_, model := newTestPage(t, "zh-CN")
	if got, want := model.errorText(vault.ErrLocked), model.t("errors.no-database"); got != want {
		t.Fatalf("a locked vault reads as %q, want %q", got, want)
	}
	raw := errors.New("vault: the root group cannot be deleted")
	if got, want := model.errorText(raw), model.t("errors.unexpected"); got != want {
		t.Fatalf("an untranslated error reads as %q, want %q", got, want)
	}
}

func TestTheLanguageChoiceTranslatesTheInterface(t *testing.T) {
	_, model := newTestPage(t, "en")
	english := model.t("menu.file")
	if err := model.sess.SetLanguage("zh-CN"); err != nil {
		t.Fatalf("cannot switch to Chinese: %v", err)
	}
	chinese := model.t("menu.file")
	if chinese == english {
		t.Fatalf("the menu name did not change: %q in both languages", english)
	}
	if err := model.sess.SetLanguage("ja"); err != nil {
		t.Fatalf("cannot switch to Japanese: %v", err)
	}
	if japanese := model.t("menu.file"); japanese == chinese || japanese == english {
		t.Fatalf("the Japanese menu name is %q, want its own", japanese)
	}
	if missing := model.sess.Tr().Missing(); len(missing) > 0 {
		t.Fatalf("the Japanese pack is missing %d keys, first %q", len(missing), missing[0])
	}
}

func TestTheThemeSwitchesBothWays(t *testing.T) {
	_, model := newTestPage(t, "en")
	dark := model.theme.Background
	if !model.dark() {
		t.Fatal("the default appearance is not the dark one")
	}
	model.toggleTheme()
	if model.dark() {
		t.Fatal("switching the theme left the dark appearance on")
	}
	if model.theme.Background == dark {
		t.Fatal("the light theme paints the same background as the dark one")
	}
	model.toggleTheme()
	if !model.dark() {
		t.Fatal("switching the theme back did not restore the dark appearance")
	}
	if model.theme.Background != dark {
		t.Fatal("the dark theme is not the one it started with")
	}
	if model.sess.Config().Get().Theme != "dark" {
		t.Fatalf("the stored theme is %q, want dark", model.sess.Config().Get().Theme)
	}
}

func TestTheGeneratorFillsTheEditor(t *testing.T) {
	driver, model := newTestPage(t, "en")
	create(t, model, "master")
	groupID, err := model.vault().CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	model.reload()
	model.selectGroup(groupID)
	model.cmdNewEntry()
	if !model.editor.open {
		t.Fatal("the editor did not open")
	}
	model.openGenerator("editor")
	if model.gen.password == "" {
		t.Fatal("the generator produced no password")
	}
	generated := model.gen.password
	clickLabel(t, driver, model, model.t("generator.use"))

	if model.editor.entry.Password != generated {
		t.Fatalf("the editor holds %q, want the generated password", model.editor.entry.Password)
	}
	if model.gen.open {
		t.Fatal("the generator stayed open after Use")
	}
	if !model.editor.open {
		t.Fatal("using the generated password closed the editor")
	}
}

func TestSavingTheEditorWritesTheEntry(t *testing.T) {
	driver, model := newTestPage(t, "en")
	create(t, model, "master")
	groupID, err := model.vault().CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	model.reload()
	model.selectGroup(groupID)
	model.cmdNewEntry()
	model.editor.entry.Title = "Mailbox"
	model.editor.entry.UserName = "user"
	model.editor.entry.Password = "secret"
	clickLabel(t, driver, model, model.t("editor.save"))

	entries, err := model.vault().Entries(groupID, false, false)
	if err != nil {
		t.Fatalf("cannot list the group: %v", err)
	}
	if len(entries) != 1 || entries[0].Title != "Mailbox" {
		t.Fatalf("the group holds %d entries (%v), want the saved one", len(entries), entries)
	}
	if model.editor.open {
		t.Fatal("the editor stayed open after Save")
	}
}

// TestNoSentenceReachesTheScreenWithAPlaceholder covers the promise the language packs
// make: a prompt reads “Mailbox” 已移到回收站, never “{title}” 已移到回收站. Every text of
// the painted frame is checked, so a value a call forgets to pass fails here too, next
// to TestEverySentenceMatchesTheCallThatShowsIt, which checks the calls themselves.
func TestNoSentenceReachesTheScreenWithAPlaceholder(t *testing.T) {
	driver, model := newTestPage(t, "zh-CN")
	create(t, model, "master")
	v := model.vault()
	groupID, err := v.CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	entryID, err := v.CreateEntry(groupID, vault.EntryDTO{Title: "Mailbox", Password: "secret"})
	if err != nil {
		t.Fatalf("cannot create the entry: %v", err)
	}
	model.reload()
	model.selectGroup(groupID)
	model.selectEntry(entryID)
	model.deleteEntry(entryID, false)
	// The database settings dialog is where the label-and-value lines live.
	model.openSettings()
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the workspace: %v", err)
	}
	named := false
	for _, text := range driver.Snapshot().Texts {
		if strings.ContainsAny(text.Value, "{}") {
			t.Errorf("the screen shows a raw placeholder: %q", text.Value)
		}
		if strings.Contains(text.Value, "Mailbox") {
			named = true
		}
	}
	if !named {
		t.Fatal("the toast about the deleted entry names no entry")
	}
}

// TestThePasswordFieldsHaveAnEyeThatRevealsThem covers the reveal button the lock
// screen and the entry editor share: the field paints one bullet per character until
// the eye inside it is pressed, and a press on the crossed eye hides it again.
func TestThePasswordFieldsHaveAnEyeThatRevealsThem(t *testing.T) {
	const secret = "{{SECRET_oh69gjwa}}"
	driver, model := newTestPage(t, "zh-CN")
	model.lock.password = secret
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the lock screen: %v", err)
	}
	requireMasked(t, driver, secret)
	requireInsideHit(t, driver, model.t("field.showPassword"), model.t("lock.password"))

	clickLabel(t, driver, model, model.t("field.showPassword"))
	requireRevealed(t, driver, secret)

	clickLabel(t, driver, model, model.t("field.hidePassword"))
	requireMasked(t, driver, secret)

	// The editor's password field carries the same eye inside the same border.
	create(t, model, "master")
	groupID, err := model.vault().CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	model.reload()
	model.selectGroup(groupID)
	model.cmdNewEntry()
	model.editor.entry.Password = secret
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the editor: %v", err)
	}
	requireMasked(t, driver, secret)
	requireInsideHit(t, driver, model.t("field.showPassword"), model.t("field.password"))

	clickLabel(t, driver, model, model.t("field.showPassword"))
	requireRevealed(t, driver, secret)

	// The new-database card the create screen shows has the eye on both of its password
	// fields, each with a state of its own.
	createDriver, creating := newTestPage(t, "zh-CN")
	creating.lock.create = true
	creating.lock.password, creating.lock.repeat = secret, secret
	if err := paint(createDriver, creating); err != nil {
		t.Fatalf("cannot paint the new-database card: %v", err)
	}
	requireMasked(t, createDriver, secret)
	requireInsideHit(t, createDriver, creating.t("field.showPassword"), creating.t("create.password"))

	// Both fields wear the same eye, so the first press reveals the field it is over and
	// the second one reveals the other: the two keep a state each.
	clickLabel(t, createDriver, creating, creating.t("field.showPassword"))
	if got := revealedCount(createDriver, secret); got != 1 {
		t.Fatalf("after one press %d fields show the password, want 1", got)
	}
	clickLabel(t, createDriver, creating, creating.t("field.showPassword"))
	if got := revealedCount(createDriver, secret); got != 2 {
		t.Fatalf("after the second press %d fields show the password, want 2", got)
	}
	if !creating.lock.showPassword || !creating.lock.showRepeat {
		t.Fatalf("the two eyes share one state: showPassword=%v, showRepeat=%v",
			creating.lock.showPassword, creating.lock.showRepeat)
	}
}

// paintedTexts is every string the frame the driver last painted put on the screen.
func paintedTexts(driver *app.Driver) []string {
	snapshot := driver.Snapshot()
	texts := make([]string, 0, len(snapshot.Texts))
	for _, text := range snapshot.Texts {
		texts = append(texts, text.Value)
	}
	return texts
}

// requireMasked fails unless the password is painted as one bullet per character and
// appears nowhere in clear.
func requireMasked(t *testing.T, driver *app.Driver, secret string) {
	t.Helper()
	bullets := utf8.RuneCountInString(secret)
	masked := false
	for _, text := range paintedTexts(driver) {
		if strings.Contains(text, secret) {
			t.Fatalf("the frame paints the password in clear: %q", text)
		}
		if strings.Trim(text, "•") == "" && utf8.RuneCountInString(text) == bullets {
			masked = true
		}
	}
	if !masked {
		t.Fatalf("no field paints %d bullets, so the password is not masked: %v", bullets, paintedTexts(driver))
	}
}

// requireRevealed fails unless the password is painted in clear.
func requireRevealed(t *testing.T, driver *app.Driver, secret string) {
	t.Helper()
	if revealedCount(driver, secret) == 0 {
		t.Fatalf("the eye did not reveal the password: %v", paintedTexts(driver))
	}
}

// revealedCount is how many of the painted strings show the password in clear, so a
// frame with two password fields can be told apart from one that revealed both.
func revealedCount(driver *app.Driver, secret string) int {
	count := 0
	for _, text := range paintedTexts(driver) {
		if strings.Contains(text, secret) {
			count++
		}
	}
	return count
}

// requireInsideHit fails unless the control labeled inner sits inside the one labeled
// outer, which is what "an eye in the box" means.
func requireInsideHit(t *testing.T, driver *app.Driver, inner, outer string) {
	t.Helper()
	in := widestHitBounds(t, driver, inner)
	out := widestHitBounds(t, driver, outer)
	if in.X < out.X || in.X+in.Width > out.X+out.Width || in.Y < out.Y || in.Y+in.Height > out.Y+out.Height {
		t.Fatalf("the control %q at %+v is not inside %q at %+v", inner, in, outer, out)
	}
}

// widestHitBounds is the widest control the frame painted under label: the one a user
// sees, when a label is painted as text and as the box that owns it.
func widestHitBounds(t *testing.T, driver *app.Driver, label string) gfx.Rect {
	t.Helper()
	var widest gfx.Rect
	found := false
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label != label || hit.Bounds.Width <= 0 || hit.Bounds.Height <= 0 {
			continue
		}
		if !found || hit.Bounds.Width > widest.Width {
			widest, found = hit.Bounds, true
		}
	}
	if !found {
		t.Fatalf("the frame paints no control labeled %q", label)
	}
	return widest
}

// TestTheToolbarHasNoNewEntryOrNewGroupButton pins what the row of buttons above the
// list offers. New Entry and New Group are the Edit menu's commands, with Control+Shift
// on them, so the toolbar does not need a button each for them as well.
func TestTheToolbarHasNoNewEntryOrNewGroupButton(t *testing.T) {
	driver, model := newTestPage(t, "zh-CN")
	create(t, model, "master")
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the workspace: %v", err)
	}
	// The row is found by the button that opens it, so the small button the group pane
	// carries in its own header, a row below, is not read as one of the toolbar's.
	row := widestHitBounds(t, driver, model.t("toolbar.newDatabase"))
	for _, label := range []string{model.t("toolbar.newEntry"), model.t("toolbar.newGroup")} {
		for _, hit := range driver.Snapshot().Hits {
			if hit.Label != label || hit.Bounds.Width <= 0 {
				continue
			}
			if hit.Bounds.Y >= row.Y && hit.Bounds.Y < row.Y+row.Height {
				t.Errorf("the toolbar still shows a %q button", label)
			}
		}
	}

	// The commands are still there: they are what the Edit menu lists.
	model.menus[menuEdit] = true
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the Edit menu: %v", err)
	}
	for _, label := range []string{model.t("menu.newEntry"), model.t("menu.newGroup")} {
		if bounds := widestHitBounds(t, driver, label); bounds.Width <= 0 {
			t.Errorf("the Edit menu offers no %q", label)
		}
	}
}

// TestTheFormsCaptionTheirFields covers the caption above a field. Viva reads a text
// field's label as its accessible name and paints nothing for it, so a form that only
// passes one is a column of anonymous boxes: the entry editor was exactly that.
func TestTheFormsCaptionTheirFields(t *testing.T) {
	driver, model := newTestPage(t, "zh-CN")
	create(t, model, "master")
	groupID, err := model.vault().CreateGroup("", "Mail", 0)
	if err != nil {
		t.Fatalf("cannot create the group: %v", err)
	}
	model.reload()
	model.selectGroup(groupID)
	model.cmdNewEntry()
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the entry editor: %v", err)
	}
	captions := []string{
		model.t("field.title"), model.t("field.userName"), model.t("field.password"),
		model.t("field.url"), model.t("field.notes"), model.t("field.overrideUrl"),
		model.t("field.group"), model.t("field.tags"),
	}
	// Cancel is the dialog's own button in the footer: the toolbar carries a Save of its
	// own, so the frame's two "保存" buttons cannot tell the footer apart on their own.
	footer := widestHitBounds(t, driver, model.t("editor.cancel"))
	for _, caption := range captions {
		captionAbove(t, driver, caption)
		field := widestHitBounds(t, driver, caption)
		if field.Y+field.Height > footer.Y {
			t.Errorf("the %q field ends at %v, under the dialog's buttons at %v: the tab does not fit the dialog",
				caption, field.Y+field.Height, footer.Y)
		}
	}
	// Two fields to a row is what makes the eight of them fit, so the fields of a pair
	// share a row: the same top edge, one left of the other.
	pairs := [][2]string{
		{model.t("field.title"), model.t("field.userName")},
		{model.t("field.password"), model.t("field.url")},
		{model.t("field.overrideUrl"), model.t("field.group")},
	}
	for _, pair := range pairs {
		left, right := widestHitBounds(t, driver, pair[0]), widestHitBounds(t, driver, pair[1])
		if diff := left.Y - right.Y; diff > 1 || diff < -1 {
			t.Errorf("%q starts at %v and %q at %v: they are not on one row", pair[0], left.Y, pair[1], right.Y)
		}
		if left.X+left.Width > right.X {
			t.Errorf("%q at %+v is not left of %q at %+v", pair[0], left, pair[1], right)
		}
	}
	// A caption no longer decides how wide its column is: the pairs without a button
	// between them give each field the same width.
	title := widestHitBounds(t, driver, model.t("field.title"))
	user := widestHitBounds(t, driver, model.t("field.userName"))
	if diff := title.Width - user.Width; diff > 1 || diff < -1 {
		t.Errorf("the title field is %v wide and the user name field %v: the columns are not equal",
			title.Width, user.Width)
	}
}

// captionAbove fails unless the sentence is painted just above the control that carries
// the same name, which is the field the caption belongs to.
func captionAbove(t *testing.T, driver *app.Driver, caption string) {
	t.Helper()
	field := widestHitBounds(t, driver, caption)
	for _, text := range driver.Snapshot().Texts {
		if text.Value != caption || text.Bounds.Height >= field.Height {
			continue
		}
		overlaps := text.Bounds.X+text.Bounds.Width > field.X+4 && text.Bounds.X < field.X+field.Width
		above := text.Bounds.Y+text.Bounds.Height <= field.Y+2
		near := field.Y-(text.Bounds.Y+text.Bounds.Height) < 30
		if overlaps && above && near {
			return
		}
	}
	t.Fatalf("no %q is painted above the field at %+v", caption, field)
}

// TestTheTitleStripShowsOnlyTheDatabase covers the strip the window buttons sit in. It
// used to carry the tagline and the version beside the file's name, "用 Go 编写的密码管理
// 器 0.1.0": the strip names the database, and the application tells its own name and
// version in the About dialog.
func TestTheTitleStripShowsOnlyTheDatabase(t *testing.T) {
	driver, model := newTestPage(t, "zh-CN")
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the lock screen: %v", err)
	}
	// The welcome card keeps the tagline: there it describes the application, under its
	// name, rather than standing in for the name in the chrome.
	tagline := false
	for _, text := range paintedTexts(driver) {
		if text == model.t("app.tagline") {
			tagline = true
		}
	}
	if !tagline {
		t.Errorf("the welcome card no longer paints the tagline")
	}
	path := create(t, model, "master")
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the workspace: %v", err)
	}
	requireTitleStrip(t, driver, model, filepath.Base(path))
	// The name and version left the window entirely, not just its corner.
	for _, text := range paintedTexts(driver) {
		if text == model.product {
			t.Errorf("the window paints the application's name and version %q", text)
		}
	}
	// They live in the About dialog, which is where a reader looks for them.
	model.about = true
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the About dialog: %v", err)
	}
	about := false
	for _, text := range paintedTexts(driver) {
		if text == model.product {
			about = true
		}
	}
	if !about {
		t.Errorf("the About dialog shows no %q", model.product)
	}
}

// requireTitleStrip fails unless the strip the window buttons sit in paints the file's
// name and nothing of the application's own naming.
func requireTitleStrip(t *testing.T, driver *app.Driver, model *Model, file string) {
	t.Helper()
	found := false
	for _, text := range driver.Snapshot().Texts {
		if text.Bounds.Height <= 0 || text.Bounds.Y+text.Bounds.Height > titlebarHeight {
			continue
		}
		if text.Value == model.product || text.Value == model.t("app.tagline") {
			t.Errorf("the title strip shows %q, want only the file name", text.Value)
		}
		if text.Value == file {
			found = true
		}
	}
	if !found {
		t.Errorf("the title strip shows no %q", file)
	}
}

// TestTheDividersResizeThePanes covers the two dividers between the three panes: a drag
// moves the divider, the pane beside it takes the new width, and the divider stops at the
// least width instead of letting a pane vanish. The list and the details used to take
// fixed shares of the window that only a window resize could change.
func TestTheDividersResizeThePanes(t *testing.T) {
	driver, model := newTestPage(t, "zh-CN")
	create(t, model, "master")
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint the workspace: %v", err)
	}
	tree := widestHitBounds(t, driver, model.t("tree.allEntries"))
	drag(t, driver, model, model.t("split.groups"), 90)
	if got := widestHitBounds(t, driver, model.t("tree.allEntries")).Width; got < tree.Width+60 {
		t.Fatalf("the group pane is %v wide after the drag, was %v: the divider did not move it", got, tree.Width)
	}

	list := widestHitBounds(t, driver, model.t("split.entries"))
	drag(t, driver, model, model.t("split.entries"), -80)
	if got := widestHitBounds(t, driver, model.t("split.entries")).X; got > list.X-50 {
		t.Fatalf("the list divider sits at %v after the drag, was %v: it did not move", got, list.X)
	}

	// Dragging far past the left edge stops at the group pane's least width.
	drag(t, driver, model, model.t("split.groups"), -400)
	if got := widestHitBounds(t, driver, model.t("split.groups")).X; got < minTreePane {
		t.Fatalf("the group divider sits at %v, left of the least width %v", got, minTreePane)
	}
}

// drag presses the middle of the control labeled label, moves the pointer dx pixels to
// the right and lets it go, the way a user moves a divider.
func drag(t *testing.T, driver *app.Driver, model *Model, label string, dx float32) {
	t.Helper()
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint before dragging %q: %v", label, err)
	}
	hit := widestHitBounds(t, driver, label)
	x, y := hit.X+hit.Width/2, hit.Y+hit.Height/2
	if err := driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatalf("pressing %q: %v", label, err)
	}
	if err := driver.Move(x+dx, y); err != nil {
		t.Fatalf("moving %q: %v", label, err)
	}
	if err := driver.Release(x+dx, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatalf("releasing %q: %v", label, err)
	}
	if err := paint(driver, model); err != nil {
		t.Fatalf("cannot paint after dragging %q: %v", label, err)
	}
}

// rowNames lists the group rows, for a failure message.
func rowNames(model *Model) string {
	names := make([]string, 0, len(model.rows))
	for _, row := range model.rows {
		names = append(names, row.name)
	}
	return strings.Join(names, ", ")
}
