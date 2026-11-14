package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gokeep/internal/dbdoc"
	"gokeep/internal/gkdb"
)

func createLifecycleVault(t *testing.T, path string, key dbdoc.Key) *Vault {
	t.Helper()
	v, err := Create(path, key, "Lifecycle", DefaultSaveSettings())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return v
}

func TestChangeKeyReencryptsTheDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "change-key.gkdb")
	oldKey := dbdoc.Key{Password: "old-password"}
	newKey := dbdoc.Key{Password: "new-password"}
	v := createLifecycleVault(t, path, oldKey)

	entryID, err := v.CreateEntry(v.RootID(), EntryDTO{Title: "Keep me", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.ChangeKey(newKey); err != nil {
		t.Fatalf("ChangeKey: %v", err)
	}
	if got := v.Key().Password; got != newKey.Password {
		t.Errorf("stored key = %q, want %q", got, newKey.Password)
	}
	if v.Modified() {
		t.Error("the database is still marked as modified after changing the key")
	}
	if v.db.Meta.MasterKeyChanged.IsZero() {
		t.Error("the master key change time was not recorded")
	}

	if _, err := Open(path, oldKey); err == nil {
		t.Fatal("the old master key still opens the database")
	}
	reopened, err := Open(path, newKey)
	if err != nil {
		t.Fatalf("Open with the new key: %v", err)
	}
	entry, err := reopened.Entry(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "Keep me" || entry.Password != "secret" {
		t.Errorf("entry after changing the key = %+v", entry)
	}
}

func TestFailedChangeKeyRestoresCredentialsAndMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "failed-change-key.gkdb")
	oldKey := dbdoc.Key{Password: "old-password"}
	newKey := dbdoc.Key{Password: "new-password"}
	v := createLifecycleVault(t, path, oldKey)
	entryID, err := v.CreateEntry(v.RootID(), EntryDTO{Title: "Still here", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}

	oldChanged := v.db.Meta.MasterKeyChanged
	oldDirty := v.dirty
	v.path = dir
	if err := v.ChangeKey(newKey); err == nil {
		t.Fatal("changing the key to an unwritable path succeeded")
	}
	if got := v.Key().Password; got != oldKey.Password {
		t.Errorf("key after the failed change = %q, want %q", got, oldKey.Password)
	}
	if !v.db.Meta.MasterKeyChanged.Equal(oldChanged) {
		t.Error("the master key change time changed although saving failed")
	}
	if v.dirty != oldDirty {
		t.Errorf("modified state after the failed change = %v, want %v", v.dirty, oldDirty)
	}
	v.path = path

	if _, err := Open(path, newKey); err == nil {
		t.Fatal("the failed change nevertheless re-encrypted the file")
	}
	reopened, err := Open(path, oldKey)
	if err != nil {
		t.Fatalf("the original key no longer opens the database: %v", err)
	}
	entry, err := reopened.Entry(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "Still here" {
		t.Errorf("entry after the failed change = %+v", entry)
	}
}

func TestFailedSaveAsRestoresCredentialsAndPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "save-as.gkdb")
	oldKey := dbdoc.Key{Password: "old-password"}
	newKey := dbdoc.Key{Password: "new-password"}
	v := createLifecycleVault(t, path, oldKey)

	target := filepath.Join(dir, "missing", "copy.gkdb")
	if err := v.SaveAs(target, newKey); err == nil {
		t.Fatal("saving into a missing directory succeeded")
	}
	if got := v.Key().Password; got != oldKey.Password {
		t.Errorf("key after the failed save = %q, want %q", got, oldKey.Password)
	}
	if got := v.Path(); got != path {
		t.Errorf("path after the failed save = %q, want %q", got, path)
	}
	if _, err := Open(path, oldKey); err != nil {
		t.Fatalf("the original file no longer opens with the old key: %v", err)
	}
}

func TestBackupRemainsReadableAfterTheMainFileIsCorrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.gkdb")
	key := dbdoc.Key{Password: "backup-password"}
	v := createLifecycleVault(t, path, key)

	if _, err := v.CreateEntry(v.RootID(), EntryDTO{Title: "First", Password: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := v.CreateEntry(v.RootID(), EntryDTO{Title: "Second", Password: "two"}); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}

	backupDB, err := gkdb.LoadFile(dbdoc.BackupPath(path), key)
	if err != nil {
		t.Fatalf("loading the latest backup: %v", err)
	}
	if findEntryByTitle(backupDB, "First") == nil {
		t.Error("the latest backup lost the first revision")
	}
	if findEntryByTitle(backupDB, "Second") != nil {
		t.Error("the latest backup contains the newest revision")
	}
	if _, err := os.Stat(dbdoc.BackupPath(path) + ".1"); err != nil {
		t.Fatalf("the previous backup was not rotated: %v", err)
	}

	if err := os.WriteFile(path, []byte("corrupted database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, key); err == nil {
		t.Fatal("a corrupted database opened successfully")
	}
	recovered, err := Open(dbdoc.BackupPath(path), key)
	if err != nil {
		t.Fatalf("Open backup: %v", err)
	}
	if findEntryByTitle(recovered.db, "First") == nil {
		t.Error("the backup cannot be used to recover the previous revision")
	}
}

func TestAttachmentSurvivesSaveAndReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "attachment.gkdb")
	key := dbdoc.Key{Password: "attachment-password"}
	v := createLifecycleVault(t, path, key)

	entryID, err := v.CreateEntry(v.RootID(), EntryDTO{Title: "With file"})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{0x00, 0x01, 0x02, 0xfe, 0xff, 'G', 'o', 'K', 'e', 'e', 'p'}
	if err := v.AddAttachment(entryID, "data.bin", payload); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Attachment(entryID, "data.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("attachment bytes = %v, want %v", got, payload)
	}
	entry, err := reopened.Entry(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Attachments) != 1 || entry.Attachments[0].Size != len(payload) {
		t.Errorf("attachments = %+v", entry.Attachments)
	}

	if err := reopened.RemoveAttachment(entryID, "data.bin"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := again.Attachment(entryID, "data.bin"); err == nil {
		t.Error("the removed attachment is still present after reopening")
	}
}

func TestCSVRoundTripPreservesEntriesAndGroups(t *testing.T) {
	dir := t.TempDir()
	sourceKey := dbdoc.Key{Password: "csv-source"}
	source := createLifecycleVault(t, filepath.Join(dir, "source.gkdb"), sourceKey)
	groupID, err := source.CreateGroup(source.RootID(), "Work", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.CreateEntry(groupID, EntryDTO{
		Title:    "Mail",
		UserName: "alice",
		Password: "s3cret",
		URL:      "https://example.com",
		Notes:    "line one\nline two",
		Fields:   []FieldDTO{{Key: "TOTP", Value: "otpauth://example", Protected: true}},
	}); err != nil {
		t.Fatal(err)
	}
	var exported bytes.Buffer
	if err := source.ExportCSV(&exported); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	targetKey := dbdoc.Key{Password: "csv-target"}
	targetPath := filepath.Join(dir, "target.gkdb")
	target := createLifecycleVault(t, targetPath, targetKey)
	count, err := target.ImportCSV(bytes.NewReader(exported.Bytes()), target.RootID())
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if count != 1 {
		t.Fatalf("imported %d entries, want 1", count)
	}
	if err := target.Save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(targetPath, targetKey)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := reopened.AllEntries(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the imported database has %d entries, want 1", len(entries))
	}
	entry, err := reopened.Entry(entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "Mail" || entry.UserName != "alice" || entry.Password != "s3cret" {
		t.Errorf("imported entry = %+v", entry)
	}
	if entry.URL != "https://example.com" || entry.Notes != "line one\nline two" {
		t.Errorf("imported URL or notes = %q / %q", entry.URL, entry.Notes)
	}
	if entry.GroupPath != "Root/Work" {
		t.Errorf("imported group path = %q, want Root/Work", entry.GroupPath)
	}
	if len(entry.Fields) != 1 || entry.Fields[0].Key != "TOTP" || !entry.Fields[0].Protected {
		t.Errorf("imported custom fields = %+v", entry.Fields)
	}
}
