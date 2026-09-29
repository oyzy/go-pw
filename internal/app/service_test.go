package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gokeep/internal/kdbx"
	"gokeep/internal/pwgen"
	"gokeep/internal/vault"
)

// TestConfigStoreRoundTrip checks that the configuration is written and read back.
func TestConfigStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := NewConfigStoreAt(path)
	if err != nil {
		t.Fatalf("NewConfigStoreAt: %v", err)
	}
	if store.Path() != path {
		t.Fatalf("unexpected configuration path %q", store.Path())
	}
	cfg := store.Get()
	cfg.ClipboardClearSeconds = 7
	cfg.RecentFiles = []string{`C:\tmp\a.kdbx`, `C:\tmp\b.kdbx`}
	if err := store.Set(cfg); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.AddRecent(`C:\tmp\b.kdbx`); err != nil {
		t.Fatalf("AddRecent: %v", err)
	}
	reloaded := store.Get()
	if reloaded.ClipboardClearSeconds != 7 {
		t.Errorf("clipboard seconds = %d", reloaded.ClipboardClearSeconds)
	}
	if len(reloaded.RecentFiles) != 2 || reloaded.RecentFiles[0] != `C:\tmp\b.kdbx` {
		t.Errorf("recent files = %v", reloaded.RecentFiles)
	}
	// Restore the previous configuration.
	if err := store.Set(DefaultConfig()); err != nil {
		t.Fatal(err)
	}
}

// TestVaultServiceLifecycle drives the whole service layer the way the user interface
// does: create a database, add and edit entries, search, export, delete and reopen.
func TestVaultServiceLifecycle(t *testing.T) {
	dir := t.TempDir()
	store := &ConfigStore{path: filepath.Join(dir, "config.json"), cfg: DefaultConfig()}
	manager := &Manager{config: store, Version: "test"}
	service := NewVaultService(manager)

	path := filepath.Join(dir, "test.kdbx")
	info, err := service.CreateDatabase(path, "Test DB", "master-password", "", vault.DatabaseSettings{
		HistoryMaxItems:   5,
		HistoryMaxSize:    1024 * 1024,
		ProtectPassword:   true,
		FileVersion:       0x00040001,
		KDFName:           "Argon2d",
		KDFMemory:         16 * 1024 * 1024,
		KDFIterations:     1,
		KDFParallelism:    2,
		RecycleBinEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	if info.Name != "Test DB" {
		t.Errorf("name = %q", info.Name)
	}

	tree, err := service.Tree()
	if err != nil {
		t.Fatal(err)
	}
	// A brand new database must not contain any predefined group.
	if len(tree) != 0 {
		t.Fatalf("a new database contains %d groups, want none", len(tree))
	}
	if info.GroupCount != 0 {
		t.Errorf("group count = %d, want 0 (the root group must not be counted)", info.GroupCount)
	}
	groupID, err := service.RootID()
	if err != nil || groupID == "" {
		t.Fatalf("RootID: %v", err)
	}

	entryID, err := service.CreateEntry(groupID, vault.EntryDTO{
		Title:    "Example",
		UserName: "user",
		Password: "secret",
		URL:      "https://example.com",
		Tags:     []string{"work"},
		Fields:   []vault.FieldDTO{{Key: "PIN", Value: "4242", Protected: true}},
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	entry, err := service.Entry(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "Example" || entry.Password != "secret" {
		t.Fatalf("entry = %+v", entry)
	}
	if len(entry.Fields) != 1 || entry.Fields[0].Value != "4242" {
		t.Errorf("custom fields = %+v", entry.Fields)
	}

	// Editing must create a history revision.
	entry.Password = "secret2"
	if err := service.UpdateEntry(entry); err != nil {
		t.Fatal(err)
	}
	updated, err := service.Entry(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Password != "secret2" {
		t.Errorf("password = %q", updated.Password)
	}
	if len(updated.History) != 1 {
		t.Errorf("history = %d, want 1", len(updated.History))
	}

	// Searching finds the entry, searching for something absent does not.
	found, err := service.Search(vault.SearchOptions{Query: "example", SearchInAllGroups: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Errorf("search returned %d entries", len(found))
	}
	none, err := service.Search(vault.SearchOptions{Query: "does-not-exist", SearchInAllGroups: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("search returned %d entries for a missing term", len(none))
	}

	// CSV export and import round trip.
	csvPath := filepath.Join(dir, "export.csv")
	if _, err := service.ExportCSV(csvPath); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	data, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Example") {
		t.Errorf("CSV export does not contain the entry: %s", string(data))
	}
	if _, err := service.ImportCSV(csvPath, groupID); err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	all, err := service.AllEntries(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("after import there are %d entries, want 2", len(all))
	}

	// Deleting moves the entry to the recycle bin, emptying it removes it for good.
	if err := service.DeleteEntry(entryID, false); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	recycled, err := service.Entry(entryID)
	if err != nil {
		t.Fatalf("the entry should still exist in the recycle bin: %v", err)
	}
	if !recycled.IsRecycled {
		t.Error("the entry should be marked as recycled")
	}
	// The recycle bin is created on demand, so it exists now but did not before.
	groups, err := service.Groups(true)
	if err != nil {
		t.Fatal(err)
	}
	hasBin := false
	binID := ""
	for _, group := range groups {
		if group.Name == kdbx.RecycleBinName {
			hasBin = true
			binID = group.ID
		}
	}
	if !hasBin {
		t.Error("the recycle bin group should have been created by the delete")
	}
	// Selecting the recycle bin in the tree must list the deleted entries.
	binEntries, err := service.Entries(binID, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(binEntries) != 1 || binEntries[0].ID != entryID {
		t.Fatalf("the recycle bin lists %d entries, want the deleted one", len(binEntries))
	}
	if !binEntries[0].IsRecycled {
		t.Error("the entry in the recycle bin should be marked as recycled")
	}
	// Restoring without a target returns the entry to the group it came from.
	if err := service.RestoreToPreviousGroup(entryID); err != nil {
		t.Fatalf("RestoreToPreviousGroup: %v", err)
	}
	restored, err := service.Entry(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.IsRecycled || restored.GroupID != groupID {
		t.Errorf("restored entry is in %q recycled=%v, want group %q", restored.GroupID, restored.IsRecycled, groupID)
	}
	if err := service.DeleteEntry(entryID, false); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if err := service.RestoreEntry(entryID, groupID); err != nil {
		t.Fatalf("RestoreEntry: %v", err)
	}
	if err := service.DeleteEntry(entryID, true); err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	if _, err := service.Entry(entryID); err == nil {
		t.Error("the entry should be gone after a permanent delete")
	}

	// Saving and opening the file again must work with the same password.
	if err := service.SaveDatabase(); err != nil {
		t.Fatalf("SaveDatabase: %v", err)
	}
	manager.Lock()
	if _, err := service.OpenDatabase(path, "master-password", ""); err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	if _, err := service.OpenDatabase(path, "wrong-password", ""); err == nil {
		t.Error("opening with a wrong password must fail")
	}

	// Generator and password evaluation.
	generatorService := NewGeneratorService(manager)
	generated := generatorService.Generate(defaultGeneratorOptions())
	if generated.Error != "" || len(generated.Password) < 16 {
		t.Errorf("generator produced %q (%s)", generated.Password, generated.Error)
	}
	eval := generatorService.Evaluate("hunter2")
	if eval.Score >= 50 {
		t.Errorf("a weak password scored %d", eval.Score)
	}
}

func defaultGeneratorOptions() pwgen.Options {
	return pwgen.Options{
		Length: 20, Upper: true, Lower: true, Digits: true, Special: true,
	}
}

// TestErrorMarshalling guards the contract with the frontend: the message and the code
// must survive the trip through Wails' error marshalling, otherwise the user interface
// can only show "[object Object]".
func TestErrorMarshalling(t *testing.T) {
	raw := MarshalError(Errorf(CodeWrongKey, "主密码或密钥文件不正确，无法解密数据库"))
	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("the marshalled error is not valid JSON: %v (%s)", err, raw)
	}
	if payload["message"] != "主密码或密钥文件不正确，无法解密数据库" {
		t.Errorf("message = %q", payload["message"])
	}
	if payload["code"] != CodeWrongKey {
		t.Errorf("code = %q", payload["code"])
	}
	plain := MarshalError(errors.New("普通错误"))
	payload = nil
	if err := json.Unmarshal(plain, &payload); err != nil {
		t.Fatalf("plain error is not valid JSON: %v (%s)", err, plain)
	}
	if payload["message"] != "普通错误" {
		t.Errorf("plain message = %q", payload["message"])
	}
	if string(MarshalError(nil)) != "null" {
		t.Errorf("nil error should marshal to null, got %s", MarshalError(nil))
	}
}

// TestOpenDatabaseErrorCodes checks that the service reports the failures the unlock
// dialog reacts to.
func TestOpenDatabaseErrorCodes(t *testing.T) {
	dir := t.TempDir()
	store := &ConfigStore{path: filepath.Join(dir, "config.json"), cfg: DefaultConfig()}
	manager := &Manager{config: store, Version: "test"}
	service := NewVaultService(manager)
	path := filepath.Join(dir, "codes.kdbx")

	if _, err := service.CreateDatabase(path, "Codes", "right-password", "", vault.DatabaseSettings{}); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	manager.Lock()

	if _, err := service.OpenDatabase(path, "wrong-password", ""); ErrorCode(err) != CodeWrongKey {
		t.Errorf("wrong password code = %q (%v)", ErrorCode(err), err)
	}
	if _, err := service.OpenDatabase(filepath.Join(dir, "missing.kdbx"), "x", ""); ErrorCode(err) != CodeNoFile {
		t.Errorf("missing file code = %q (%v)", ErrorCode(err), err)
	}
	if _, err := service.OpenDatabase(path, "right-password", filepath.Join(dir, "missing.keyx")); ErrorCode(err) != CodeWrongKeyFile {
		t.Errorf("missing key file code = %q (%v)", ErrorCode(err), err)
	}
	if _, err := service.OpenDatabase(path, "right-password", ""); err != nil {
		t.Fatalf("the correct password must open the database: %v", err)
	}
}

// TestLanguageAndRecycleBinName checks the language plumbing: the frontend sends the
// language and the menu labels, and the recycle bin is created with the matching name.
func TestLanguageAndRecycleBinName(t *testing.T) {
	dir := t.TempDir()
	store := &ConfigStore{path: filepath.Join(dir, "config.json"), cfg: DefaultConfig()}
	manager := &Manager{config: store, Version: "test"}

	var seen map[string]string
	manager.SetMenuUpdater(func(labels map[string]string) { seen = labels })

	appService := NewAppService(manager)
	labels := map[string]string{"file": "Datei", "recycleBin": "Papierkorb"}
	if err := appService.ApplyLanguage("de", labels); err != nil {
		t.Fatalf("ApplyLanguage: %v", err)
	}
	if store.Get().Language != "de" {
		t.Errorf("stored language = %q", store.Get().Language)
	}
	if seen["file"] != "Datei" {
		t.Errorf("the menu updater did not receive the labels: %v", seen)
	}
	if manager.RecycleBinName() != "Papierkorb" {
		t.Errorf("recycle bin name = %q", manager.RecycleBinName())
	}

	// A database created after the language change uses the localised name.
	service := NewVaultService(manager)
	path := filepath.Join(dir, "lang.kdbx")
	if _, err := service.CreateDatabase(path, "Lang", "pw", "", vault.DatabaseSettings{}); err != nil {
		t.Fatal(err)
	}
	v, err := manager.RequireVault()
	if err != nil {
		t.Fatal(err)
	}
	rootID := v.RootID()
	entryID, err := v.CreateEntry(rootID, vault.EntryDTO{Title: "x", Password: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteEntry(entryID, false); err != nil {
		t.Fatal(err)
	}
	bin := v.Database().RecycleBin()
	if bin == nil {
		t.Fatal("the recycle bin is missing")
	}
	if bin.Name != "Papierkorb" {
		t.Errorf("recycle bin group name = %q, want the localised name", bin.Name)
	}
	// Switching language renames a bin that still carries a default name.
	if err := appService.ApplyLanguage("en", map[string]string{"recycleBin": "Recycle Bin"}); err != nil {
		t.Fatal(err)
	}
	if bin := v.Database().RecycleBin(); bin.Name != "Recycle Bin" {
		t.Errorf("after the language change the bin is named %q", bin.Name)
	}
}

// TestNewDatabaseRecyclesDeletedEntries follows the interface step by step: create a
// database with the settings of the "new database" dialog, add an entry, delete it, save,
// reopen and delete again. The security preset the dialog sends carries
// "recycleBinEnabled: false", which must not be stored as an explicit choice - deleting an
// entry would then remove it for good and no recycle bin would ever appear.
func TestNewDatabaseRecyclesDeletedEntries(t *testing.T) {
	dir := t.TempDir()
	store := &ConfigStore{path: filepath.Join(dir, "config.json"), cfg: DefaultConfig()}
	manager := &Manager{config: store, Version: "test"}
	service := NewVaultService(manager)
	path := filepath.Join(dir, "fresh.kdbx")

	// Exactly the payload the presets of AppService and the new-database dialog produce.
	preset := NewAppService(manager).VaultSettingsPresets()["keepass-defaults"]
	preset.Name = "Fresh"
	preset.HistoryMaxItems = 10
	preset.HistoryMaxSize = 6 * 1024 * 1024
	preset.ProtectPassword = true
	info, err := service.CreateDatabase(path, "Fresh", "master", "", preset)
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	if !info.RecycleBinEnabled {
		t.Fatal("a new database must recycle deleted entries")
	}
	// The interface decides whether a deletion is final based on this field, so it has to
	// survive the JSON trip to the frontend.
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"recycleBinEnabled":true`) {
		t.Fatalf("the database information does not report the recycle bin: %s", raw)
	}
	rootID, err := service.RootID()
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateEntry(rootID, vault.EntryDTO{Title: "First", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteEntry(first, false); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if entry, err := service.Entry(first); err != nil || !entry.IsRecycled {
		t.Fatalf("the entry should sit in the recycle bin: %v (%+v)", err, entry)
	}
	if err := service.SaveDatabase(); err != nil {
		t.Fatal(err)
	}

	// The same has to hold after the database has been written and read back.
	if _, err := service.OpenDatabase(path, "master", ""); err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	if info, err := service.Info(); err != nil || !info.RecycleBinEnabled {
		t.Fatalf("the recycle bin policy was lost while saving: %v (%+v)", err, info)
	}
	second, err := service.CreateEntry(rootID, vault.EntryDTO{Title: "Second", Password: "y"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteEntry(second, false); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if entry, err := service.Entry(second); err != nil || !entry.IsRecycled {
		t.Fatalf("the entry should sit in the recycle bin after reopening: %v (%+v)", err, entry)
	}
	bin := manager.Vault().Database().RecycleBin()
	if bin == nil || len(bin.Entries) != 2 {
		t.Fatalf("the recycle bin holds %d entries, want 2", len(bin.Entries))
	}
}
