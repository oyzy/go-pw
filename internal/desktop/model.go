package desktop

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"gokeep/internal/config"
	"gokeep/internal/pwgen"
	"gokeep/internal/pwquality"
	"gokeep/internal/vault"
)

// groupRow is one visible row of the group tree: a flattened view of the database's
// groups plus the root row that shows every entry.
type groupRow struct {
	id       string
	name     string
	depth    int
	branch   bool
	expanded bool
	entries  int
	recycled bool
}

// confirmState is the question dialog: KeePass asks before deleting, emptying the
// recycle bin and losing unsaved changes.
type confirmState struct {
	open    bool
	title   string
	body    string
	okLabel string
	danger  bool
	action  func()
}

// Model is the whole application state. The page reads it and only a click changes it.
type Model struct {
	sess *Session
	// product is the name and version every piece of chrome shows, so the title strip and
	// the status bar read alike: GoKeep 0.1.0.
	product string
	theme   gfx.Theme

	// window
	window      *app.Window
	application *app.Application

	// lock screen
	lock lockState

	// main view
	groupID   string
	entryID   string
	expanded  map[string]bool
	treeKeys  map[string]int
	entryKeys map[string]int
	nextKey   int
	rows      []groupRow
	tree      ui.TreeState
	list      ui.ListState
	// splitTree and splitList are the dividers between the three panes: Pos is the width
	// of the pane before each, kept across paints and window resizes.
	splitTree ui.SplitterState
	splitList ui.SplitterState
	entries   []vault.EntrySummary
	detail    vault.EntryDTO
	hasDetail bool

	// pageC is the page context of the paint in progress. The tree's row builder is
	// given no context of its own, so it declares its rows with this one; the closures
	// handed to Tree and List are made once, here, instead of once per paint. It is
	// reassigned on the first line of every paint and read only from within that same
	// paint, so it never outlives the context it names: viva vet cannot see the
	// reassignment and asks for the ignore below.
	pageC       ui.C // viva:ignore kept-context
	treeName    func(int) string
	treeContent func(int)
	listContent func(ui.ListRow)

	search       string
	searchField  ui.Field
	searchAll    bool
	searchCase   bool
	searchRegex  bool
	searchProt   bool
	searchOpen   bool
	searchResult bool
	// focus is the value the keyboard focus request is bound to.
	focus string

	menus   [menuCount]bool
	toaster ui.Toaster

	confirm confirmState
	editor  editorState
	group   groupState
	gen     generatorState
	opts    optionsState
	dbset   settingsState
	key     keyState
	move    moveState
	about   bool
	// shortcutsHelp shows the reference of the keystrokes the page handles.
	shortcutsHelp bool
	dups          [][]vault.EntrySummary
	expired       []vault.EntrySummary

	quitAsk    bool
	allowClose func()
}

// lockState is the lock screen: opening an existing file, or creating a new one.
type lockState struct {
	path         string
	password     string
	repeat       string
	keyFile      string
	useKeyFile   bool
	create       bool
	name         string
	preset       string
	showPassword bool
	showRepeat   bool
	message      string
	messageErr   bool
}

const menuCount = 5

// focusSearch is the focus request of the toolbar's search box.
const focusSearch = "search"

const (
	menuFile = iota
	menuEdit
	menuView
	menuTools
	menuHelp
)

func newModel(sess *Session) *Model {
	version := sess.Version()
	m := &Model{
		sess:      sess,
		product:   appTitle + " " + version,
		splitTree: ui.SplitterState{Pos: 250},
		splitList: ui.SplitterState{Pos: 500},
		expanded:  map[string]bool{},
		treeKeys:  map[string]int{},
		entryKeys: map[string]int{},
		lock:      lockState{preset: presetStandard},
	}
	m.applyTheme()
	cfg := sess.Config().Get()
	m.lock.path = cfg.LastDatabase
	m.treeName = m.treeRowName
	m.treeContent = m.treeRow
	m.listContent = m.listRow
	m.list.Key = func(i int) int { return m.keyForEntry(m.entries[i].ID) }
	m.list.Label = func(i int) string { return m.entries[i].Title }
	m.tree.Key = func(i int) int { return m.keyForGroup(m.rows[i].id) }
	m.tree.Node = func(i int) ui.TreeNode {
		row := m.rows[i]
		return ui.TreeNode{
			Depth:    row.depth,
			Branch:   row.branch,
			Expanded: row.expanded,
			Selected: row.id == m.groupID,
		}
	}
	m.tree.RowHeight = 26
	m.tree.Indent = 16
	return m
}

// applyTheme recomputes the theme value: switching between the light and dark
// appearance is switching the value the page paints with.
func (m *Model) applyTheme() {
	cfg := m.sess.Config().Get()
	if cfg.Theme == "light" {
		m.theme = gfx.LightTheme()
	} else {
		m.theme = gfx.DarkTheme()
	}
}

// dark reports whether the dark appearance is active.
func (m *Model) dark() bool { return m.sess.Config().Get().Theme != "light" }

// toggleTheme switches appearance and remembers it.
func (m *Model) toggleTheme() {
	cfg := m.sess.Config().Get()
	if cfg.Theme == "light" {
		cfg.Theme = "dark"
	} else {
		cfg.Theme = "light"
	}
	_ = m.sess.Config().Set(cfg)
	m.applyTheme()
}

// keyForGroup and keyForEntry name rows by their database identity, so a row keeps
// its focus while rows above it open, close or are added.
func (m *Model) keyForGroup(id string) int { return m.stableKey(m.treeKeys, id) }
func (m *Model) keyForEntry(id string) int { return m.stableKey(m.entryKeys, id) }
func (m *Model) stableKey(keys map[string]int, id string) int {
	if key, ok := keys[id]; ok {
		return key
	}
	m.nextKey++
	keys[id] = m.nextKey
	return m.nextKey
}

// vault returns the open database, which is nil while the workspace is locked.
func (m *Model) vault() *vault.Vault { return m.sess.Vault() }

// config returns the current settings.
func (m *Model) config() (cfg config.Config) { return m.sess.Config().Get() }

// t translates a key with the active language.
func (m *Model) t(key string) string { return m.sess.Tr().T(key) }

// tf translates a key and fills its {placeholders}: the language packs use
// "{entries}" rather than printf verbs.
func (m *Model) tf(key string, pairs ...any) string {
	text := m.t(key)
	for i := 0; i+1 < len(pairs); i += 2 {
		name := fmt.Sprint(pairs[i])
		text = strings.ReplaceAll(text, "{"+name+"}", fmt.Sprint(pairs[i+1]))
	}
	return text
}

// info shows a short confirmation toast.
func (m *Model) info(message string) {
	m.toaster.Show(message, "", ui.ToastSuccess)
}

// warn shows a toast that reports something that did not work.
func (m *Model) warn(message string) {
	m.toaster.Show(appTitle, message, ui.ToastError)
}

// fail reports an error, translating the codes the storage layer knows.
func (m *Model) fail(err error) {
	if err == nil {
		return
	}
	m.toaster.Show(m.t("detail.actionFailed"), m.errorText(err), ui.ToastError)
}

// errorText is the explanation line of an error toast. Every path out of it is a
// sentence in the active language: a database with no open session is named as such,
// a coded error is the pack's sentence with its placeholders filled, and anything else
// is a sentence in the active language with the raw message sent to the log, which is
// where the byte offsets and file names of a real failure belong.
func (m *Model) errorText(err error) string {
	e, ok := err.(*Error)
	if !ok {
		if errors.Is(err, vault.ErrLocked) {
			return m.t("errors.no-database")
		}
		slog.Error("the action failed", "error", err)
		return m.t("errors.unexpected")
	}
	if !m.sess.Tr().Has("errors." + e.Code) {
		// The message of a coded error is written for the log; a code whose sentence is
		// missing from the pack sends it there and shows the user the generic line.
		slog.Error("an error has no sentence in this language", "code", e.Code, "message", e.Message)
		return m.t("errors.unexpected")
	}
	text := m.t("errors." + e.Code)
	for name, value := range e.Values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

// ask opens the confirmation dialog.
func (m *Model) ask(title, body, okLabel string, danger bool, action func()) {
	m.confirm = confirmState{open: true, title: title, body: body, okLabel: okLabel, danger: danger, action: action}
}

// reload reads the open database into the rows the panes show. It is called after
// every change, so the page never keeps a copy of anything the vault owns.
func (m *Model) reload() {
	m.rows = m.buildRows()
	m.reloadEntries()
	m.loadDetail()
}

// buildRows flattens the group tree, honouring which groups are open.
func (m *Model) buildRows() []groupRow {
	v := m.vault()
	if v == nil {
		return nil
	}
	all := m.countEntries()
	rows := []groupRow{{
		id:       "",
		name:     m.t("tree.allEntries"),
		branch:   true,
		expanded: true,
		entries:  all,
	}}
	tree := v.Tree()
	info := v.Info()
	showBin := m.sess.Config().Get().ShowRecycleBin
	m.expanded[""] = true
	var walk func(groups []vault.GroupDTO, depth int)
	walk = func(groups []vault.GroupDTO, depth int) {
		for _, g := range groups {
			if g.ID == v.RootID() {
				walk(g.Children, depth)
				continue
			}
			// The recycle bin is one of the database's groups: the interface names it
			// in the selected language, and can leave it out altogether.
			recycled := info.RecycleBinID != "" && g.ID == info.RecycleBinID
			if recycled && !showBin {
				continue
			}
			name := g.Name
			if recycled {
				name = m.t("app.recycleBin")
			}
			if strings.TrimSpace(name) == "" {
				name = m.t("tree.unnamed")
			}
			branch := len(g.Children) > 0 || len(g.EntryIDs) > 0
			open, known := m.expanded[g.ID]
			if !known {
				open = true
				m.expanded[g.ID] = true
			}
			rows = append(rows, groupRow{
				id:       g.ID,
				name:     name,
				depth:    depth,
				branch:   branch,
				expanded: open,
				entries:  len(g.EntryIDs),
				recycled: recycled,
			})
			if open {
				walk(g.Children, depth+1)
			}
		}
	}
	walk(tree, 1)
	return rows
}

// countEntries counts every entry the database holds outside the recycle bin.
func (m *Model) countEntries() int {
	v := m.vault()
	if v == nil {
		return 0
	}
	entries, err := v.AllEntries(false)
	if err != nil {
		return 0
	}
	return len(entries)
}

// includeRecycled reports whether recycled entries belong in the current view: they do
// inside the recycle bin, and nowhere else.
func (m *Model) includeRecycled() bool {
	if m.groupID == "" {
		return false
	}
	v := m.vault()
	if v == nil {
		return false
	}
	return v.Info().RecycleBinID == m.groupID
}

// reloadEntries fills the entry list for the selected group or the active search.
func (m *Model) reloadEntries() {
	v := m.vault()
	if v == nil {
		m.entries = nil
		return
	}
	if strings.TrimSpace(m.search) != "" {
		entries, err := v.Search(vault.SearchOptions{
			Query:             m.search,
			CaseSensitive:     m.searchCase,
			Regex:             m.searchRegex,
			IncludeProtected:  m.searchProt,
			GroupID:           m.groupRootID(),
			SearchInAllGroups: m.searchAll || m.groupID == "",
			IncludeRecycled:   m.includeRecycled(),
		})
		if err != nil {
			m.entries = nil
			return
		}
		m.entries = entries
		return
	}
	if m.groupID == "" {
		entries, err := v.AllEntries(m.includeRecycled())
		if err != nil {
			m.entries = nil
			return
		}
		m.entries = entries
		return
	}
	entries, err := v.Entries(m.groupID, false, m.includeRecycled())
	if err != nil {
		m.entries = nil
		return
	}
	m.entries = entries
}

// groupRootID is the group a search starts from: the selected group, or the whole
// database when the search covers every group.
func (m *Model) groupRootID() string {
	if m.searchAll || m.groupID == "" {
		return ""
	}
	return m.groupID
}

// loadDetail reads the selected entry in full.
func (m *Model) loadDetail() {
	v := m.vault()
	m.hasDetail = false
	if v == nil || m.entryID == "" {
		return
	}
	dto, err := v.Entry(m.entryID)
	if err != nil {
		m.entryID = ""
		return
	}
	m.detail = dto
	m.hasDetail = true
}

// selectGroup moves the selection to a group and shows its entries.
func (m *Model) selectGroup(id string) {
	m.groupID = id
	m.entryID = ""
	m.search = ""
	cfg := m.sess.Config().Get()
	cfg.LastGroupSelected = id
	_ = m.sess.Config().Set(cfg)
	m.reloadEntries()
	m.loadDetail()
	m.list.Select(-1)
}

// selectEntry moves the selection to one entry.
func (m *Model) selectEntry(id string) {
	m.entryID = id
	m.loadDetail()
	if v := m.vault(); v != nil && id != "" {
		_ = v.TouchEntryAccess(id)
	}
}

// currentEntry returns the selected entry's row, when there is one.
func (m *Model) currentEntry() (vault.EntrySummary, bool) {
	for _, entry := range m.entries {
		if entry.ID == m.entryID {
			return entry, true
		}
	}
	return vault.EntrySummary{}, false
}

// qualityLabel turns a KeePass quality score into the name the interface shows.
func (m *Model) qualityLabel(score int) string {
	switch {
	case score >= 80:
		return m.t("quality.excellent")
	case score >= 60:
		return m.t("quality.good")
	case score >= 40:
		return m.t("quality.fair")
	case score >= 20:
		return m.t("quality.weak")
	default:
		return m.t("quality.veryWeak")
	}
}

// estimate is the quality of a password as a score and a number of bits.
func estimate(password string, hasTOTP bool) (float64, int) {
	return pwquality.Estimate(password, hasTOTP)
}

// fieldLabel names a field the way the interface writes it, so a sentence about one
// reads "Copied password" instead of "Copied Password". A custom field is not a
// translatable name, so it is shown as the database stores it.
func (m *Model) fieldLabel(field string) string {
	switch field {
	case "Title":
		return m.t("field.title")
	case "UserName":
		return m.t("field.userName")
	case "Password":
		return m.t("field.password")
	case "URL":
		return m.t("field.url")
	case "Notes":
		return m.t("field.notes")
	case "Tags":
		return m.t("field.tags")
	}
	return field
}

// defaultGenerator returns the saved generator settings.
func (m *Model) defaultGenerator() pwgen.Options {
	return m.sess.Config().Get().Generator
}

// saveGeneratorDefaults remembers generator settings.
func (m *Model) saveGeneratorDefaults(opts pwgen.Options) error {
	cfg := m.sess.Config().Get()
	cfg.Generator = opts
	return m.sess.Config().Set(cfg)
}

// defaultDatabasePath suggests a file name next to the last database.
func (m *Model) defaultDatabasePath(name string) string {
	cfg := m.sess.Config().Get()
	if name == "" {
		name = "Database"
	}
	dir := ""
	if cfg.LastDatabase != "" {
		dir = filepath.Dir(cfg.LastDatabase)
	}
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home
		}
	}
	return filepath.Join(dir, name+databaseExtension)
}

// settingsPreset describes a new database for one security preset: the cipher, the
// compression and the work the master key derivation costs. The presets differ only in
// the key derivation function and its parameters, which is what decides how expensive
// a single guess of the master password is.
func settingsPreset(name string) vault.DatabaseSettings {
	settings := vault.DatabaseSettings{
		Cipher:          "AES-256",
		Compression:     "GZip",
		KDFName:         "Argon2id",
		KDFMemory:       64 * 1024 * 1024,
		KDFIterations:   2,
		KDFParallelism:  2,
		HistoryMaxItems: 10,
		HistoryMaxSize:  6 * 1024 * 1024,
	}
	switch name {
	case presetStrong:
		settings.KDFMemory = 256 * 1024 * 1024
		settings.KDFIterations = 4
		settings.KDFParallelism = 4
	case presetParanoid:
		settings.KDFMemory = 1024 * 1024 * 1024
		settings.KDFIterations = 8
		settings.KDFParallelism = 4
	case presetFast:
		// AES-KDF is the cheapest of the three: it does nothing but AES rounds, so it
		// runs on machines that cannot spare the memory Argon2 wants.
		settings.KDFName = "AES-KDF"
		settings.KDFRounds = 60000
	}
	return settings
}

// formatName names the file format of the open database for the settings dialog.
func (m *Model) formatName() string {
	name := "GoKeep 1"
	if v := m.vault(); v != nil && !v.Locked() {
		if info := v.Info(); info.FormatName != "" {
			name = info.FormatName
		}
	}
	return name + " (" + databaseExtension + ")"
}

// fileLabel shortens a path for the status bar.
func fileLabel(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// shortDir names the folder a database sits in, cut to its last few parts so a row of
// recent files stays one line. The whole path is the row's tooltip.
func shortDir(path string) string {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return ""
	}
	parts := strings.Split(dir, string(filepath.Separator))
	const keep = 3
	if len(parts) > keep {
		parts = append([]string{"…"}, parts[len(parts)-keep:]...)
	}
	return strings.Join(parts, string(filepath.Separator))
}

// sortEntriesByName keeps a stable order when the vault hands entries back in a
// different one.
func sortEntriesByName(entries []vault.EntrySummary) {
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Title) < strings.ToLower(entries[j].Title)
	})
}

// describeEntry is the one-line summary the status bar shows.
func describeEntry(entry vault.EntrySummary) string {
	if entry.Title == "" {
		return entry.UserName
	}
	return fmt.Sprintf("%s — %s", entry.Title, entry.UserName)
}
