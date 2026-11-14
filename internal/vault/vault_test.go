package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gokeep/internal/dbdoc"
	"gokeep/internal/gkdb"
)

// Opening a database whose recycle bin still carries the English default name must
// rename it to the localised one without changing its identity.
func TestOpenLocalisesRecycleBin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.gkdb")
	key := dbdoc.Key{Password: "secret"}

	db := dbdoc.NewDatabase("Legacy", "")
	bin := db.EnableRecycleBin()
	bin.Name = dbdoc.LegacyRecycleBinName
	banned := dbdoc.NewEntry()
	banned.SetField(dbdoc.FieldTitle, "Deleted", false)
	bin.Entries = append(bin.Entries, banned)
	writeDatabase(t, path, db, key)

	loaded, err := Open(path, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	reloaded := loaded.Database()
	got := reloaded.RecycleBin()
	if got == nil {
		t.Fatal("the recycle bin is missing")
	}
	if got.Name != dbdoc.RecycleBinName {
		t.Errorf("recycle bin name = %q, want %q", got.Name, dbdoc.RecycleBinName)
	}
	if got.UUID != db.Meta.RecycleBinUUID {
		t.Error("the recycle bin UUID must not change when it is renamed")
	}
	if !loaded.Modified() {
		t.Error("the database should be marked as modified after the rename")
	}
	// The renamed database must still be readable, and the UUID in the metadata is
	// what identifies the bin.
	if err := loaded.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := gkdb.LoadFile(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if bin := again.RecycleBin(); bin == nil || bin.Name != dbdoc.RecycleBinName {
		t.Fatalf("after saving the bin is %v", bin)
	}
}

// A brand new database has no groups at all, and the recycle bin appears with the first
// deletion inside it.
func TestDeleteCreatesRecycleBinOnDemand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.gkdb")
	key := dbdoc.Key{Password: "secret"}

	v, err := Create(path, key, "Fresh", DefaultSaveSettings())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if groups := v.Database().Groups(); len(groups) != 1 {
		t.Fatalf("a new database has %d groups (including the root), want 1", len(groups))
	}
	rootID := v.RootID()
	entryID, err := v.CreateEntry(rootID, EntryDTO{Title: "One", Password: "pw"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if err := v.DeleteEntry(entryID, false); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	bin := v.Database().RecycleBin()
	if bin == nil {
		t.Fatal("the recycle bin should have been created by the delete")
	}
	if len(bin.Entries) != 1 {
		t.Fatalf("the recycle bin holds %d entries, want 1", len(bin.Entries))
	}
	summaries, err := v.Entries(idOf(bin.UUID), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].ID != entryID {
		t.Fatalf("listing the recycle bin returned %d entries", len(summaries))
	}
	if err := v.RestoreToPreviousGroup(entryID); err != nil {
		t.Fatalf("RestoreToPreviousGroup: %v", err)
	}
	entry, err := v.Entry(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.IsRecycled {
		t.Error("the entry is still flagged as recycled after restoring it")
	}
}

// writeDatabase writes a database straight through the container, the way a build from
// before the current one would have written it.
func writeDatabase(t *testing.T, path string, db *dbdoc.Database, key dbdoc.Key) {
	t.Helper()
	options := gkdb.DefaultOptions()
	options.Generator = "test"
	if err := gkdb.SaveFile(path, db, key, options); err != nil {
		t.Fatal(err)
	}
}

// TestDatabaseThroughTheService creates a database, reopens it from disk and saves a
// copy somewhere else; the entry has to survive every step.
func TestDatabaseThroughTheService(t *testing.T) {
	dir := t.TempDir()
	key := dbdoc.Key{Password: "master-password"}

	first := filepath.Join(dir, "first"+gkdb.Extension)
	v, err := Create(first, key, "First", SaveSettings{
		Cipher:      dbdoc.CipherAES256,
		Compression: dbdoc.CompressionGzip,
		KDF:         gkdb.DefaultKDFParams(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rootID := v.RootID()
	entryID, err := v.CreateEntry(rootID, EntryDTO{Title: "Native entry", Password: "secret", Fields: []FieldDTO{
		{Key: "PIN", Value: "4242", Protected: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info := v.Info()
	if info.Format != "gkdb" || info.FormatName != "GoKeep 1" {
		t.Errorf("format = %q / %q", info.Format, info.FormatName)
	}
	if info.Cipher != "AES-256-GCM" {
		t.Errorf("cipher = %q", info.Cipher)
	}
	if info.FileVersion != uint32(gkdb.FormatVersion) {
		t.Errorf("file version = %d", info.FileVersion)
	}

	// Reopening has to find the database again.
	reopened, err := Open(first, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	entry, err := reopened.Entry(idOf(dbdoc.MustParseUUID(entryID)))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "Native entry" || entry.Password != "secret" {
		t.Errorf("entry = %+v", entry)
	}
	if reopened.Info().Format != "gkdb" {
		t.Errorf("reopened format = %q", reopened.Info().Format)
	}

	// Save as writes a second file and switches to it.
	second := filepath.Join(dir, "second"+gkdb.Extension)
	if err := reopened.SaveAs(second, key); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	final, err := Open(second, key)
	if err != nil {
		t.Fatalf("Open (second): %v", err)
	}
	if final.Info().Format != "gkdb" {
		t.Errorf("final format = %q", final.Info().Format)
	}
	entry, err = final.Entry(idOf(dbdoc.MustParseUUID(entryID)))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Password != "secret" {
		t.Errorf("password after the round trip = %q", entry.Password)
	}
	if entry := findEntryByTitle(final.Database(), "Native entry"); entry == nil {
		t.Error("the copy lost its entry")
	}
	// A file of another password manager is not a database: the container is the only
	// format this build reads.
	stranger := filepath.Join(dir, "stranger.gkdb")
	if err := os.WriteFile(stranger, []byte("\x03\xd9\xa2\x9ag\xfbK\xb5\x00\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(stranger, key); !errors.Is(err, gkdb.ErrNotDatabase) {
		t.Errorf("opening a file of another format returned %v", err)
	}
}

func findEntryByTitle(db *dbdoc.Database, title string) *dbdoc.Entry {
	for _, entry := range db.Entries() {
		if got, _ := entry.Field(dbdoc.FieldTitle); got == title {
			return entry
		}
	}
	return nil
}
