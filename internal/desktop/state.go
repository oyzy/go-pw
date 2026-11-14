package desktop

import (
	"gokeep/internal/config"
	"gokeep/internal/pwgen"
	"gokeep/internal/vault"
)

// editorState is the entry editor: a working copy of the entry, so a cancelled edit
// changes nothing.
type editorState struct {
	open          bool
	isNew         bool
	tab           string
	entry         vault.EntryDTO
	passwordShown bool
	message       string
	newFieldName  string
	newFieldValue string
	newFieldProt  bool
}

// groupState is the group editor.
type groupState struct {
	open       bool
	isNew      bool
	id         string
	parentID   string
	name       string
	notes      string
	iconID     int
	tags       string
	expires    bool
	expiryTime string
	autoType   string // "inherit", "enabled" or "disabled"
	searching  string
	message    string
}

// generatorState is the password generator dialog.
type generatorState struct {
	open     bool
	opts     pwgen.Options
	password string
	message  string
	// target is "editor" when the password goes into the open entry editor.
	target string
}

// optionsState is the application options dialog.
type optionsState struct {
	open bool
	cfg  config.Config
}

// settingsState is the database settings dialog.
type settingsState struct {
	open     bool
	settings vault.DatabaseSettings
	message  string
}

// keyState is the master key change dialog.
type keyState struct {
	open         bool
	password     string
	repeat       string
	keyFile      string
	useKeyFile   bool
	showPassword bool
	showRepeat   bool
	message      string
}

// moveState is the dialog that moves an entry or a group to another group.
type moveState struct {
	open   bool
	entry  bool
	id     string
	target string
	label  string
}
