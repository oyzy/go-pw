package vault

import (
	"path/filepath"
	"testing"

	"gokeep/internal/gkdb"
	"gokeep/internal/kdbx"
)

// Opening a database whose recycle bin still carries the English default name must
// rename it to the localised one without changing its identity.
func TestOpenLocalisesRecycleBin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.kdbx")
	key := kdbx.Key{Password: "secret"}

	db := kdbx.NewDatabase("Legacy", "")
	bin := db.EnableRecycleBin()
	bin.Name = kdbx.LegacyRecycleBinName
	banned := kdbx.NewEntry()
	banned.SetField(kdbx.FieldTitle, "Deleted", false)
	bin.Entries = append(bin.Entries, banned)
	if err := kdbx.SaveFile(path, db, key, kdbx.SaveOptions{}); err != nil {
		t.Fatal(err)
	}

	loaded, err := Open(path, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	reloaded := loaded.Database()
	got := reloaded.RecycleBin()
	if got == nil {
		t.Fatal("the recycle bin is missing")
	}
	if got.Name != kdbx.RecycleBinName {
		t.Errorf("recycle bin name = %q, want %q", got.Name, kdbx.RecycleBinName)
	}
	if got.UUID != db.Meta.RecycleBinUUID {
		t.Error("the recycle bin UUID must not change when it is renamed")
	}
	if !loaded.Modified() {
		t.Error("the database should be marked as modified after the rename")
	}
	// The renamed database must still be readable, also by KeePass: the UUID in the
	// metadata is what identifies the bin.
	if err := loaded.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := kdbx.LoadFile(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if bin := again.RecycleBin(); bin == nil || bin.Name != kdbx.RecycleBinName {
		t.Fatalf("after saving the bin is %v", bin)
	}
}

// A brand new database has no groups at all, and the recycle bin appears with the first
// deletion inside it.
func TestDeleteCreatesRecycleBinOnDemand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.kdbx")
	key := kdbx.Key{Password: "secret"}

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

// TestGoKeepFormatThroughTheService creates a database in the native format, reopens it
// and converts it to the KeePass format (and back again).
func TestGoKeepFormatThroughTheService(t *testing.T) {
	dir := t.TempDir()
	key := kdbx.Key{Password: "native-password"}

	native := filepath.Join(dir, "native"+gkdb.Extension)
	v, err := Create(native, key, "Native", SaveSettings{
		Format:      FormatGoKeep,
		Cipher:      kdbx.CipherAES256,
		Compression: kdbx.CompressionGzip,
		KDF:         kdbx.DefaultKDFParams(),
	})
	if err != nil {
		t.Fatalf("Create (native): %v", err)
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
	if info.Format != FormatGoKeep || info.FormatName != "GoKeep v1" {
		t.Errorf("format = %q / %q", info.Format, info.FormatName)
	}
	if info.Cipher != "AES-256-GCM" {
		t.Errorf("cipher = %q", info.Cipher)
	}

	// Reopening has to pick the native reader from the file signature.
	reopened, err := Open(native, key)
	if err != nil {
		t.Fatalf("Open (native): %v", err)
	}
	entry, err := reopened.Entry(idOf(kdbx.MustParseUUID(entryID)))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "Native entry" || entry.Password != "secret" {
		t.Errorf("entry = %+v", entry)
	}
	if reopened.Info().Format != FormatGoKeep {
		t.Errorf("reopened format = %q", reopened.Info().Format)
	}

	// Saving as .kdbx converts the database to the KeePass format...
	kdbxPath := filepath.Join(dir, "converted.kdbx")
	if err := reopened.SaveAs(kdbxPath, key); err != nil {
		t.Fatalf("SaveAs (kdbx): %v", err)
	}
	if got := reopened.Info().Format; got != FormatKDBX {
		t.Errorf("after SaveAs the format is %q", got)
	}
	converted, err := Open(kdbxPath, key)
	if err != nil {
		t.Fatalf("Open (converted): %v", err)
	}
	if converted.Info().Format != FormatKDBX {
		t.Errorf("converted format = %q", converted.Info().Format)
	}
	if entry := findEntryByTitle(converted.Database(), "Native entry"); entry == nil {
		t.Error("the converted database lost its entry")
	}
	// ...and back to the native format.
	back := filepath.Join(dir, "back"+gkdb.Extension)
	if err := converted.SaveAs(back, key); err != nil {
		t.Fatalf("SaveAs (native): %v", err)
	}
	final, err := Open(back, key)
	if err != nil {
		t.Fatalf("Open (back): %v", err)
	}
	if final.Info().Format != FormatGoKeep {
		t.Errorf("final format = %q", final.Info().Format)
	}
	entry, err = final.Entry(idOf(kdbx.MustParseUUID(entryID)))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Password != "secret" {
		t.Errorf("password after the round trip = %q", entry.Password)
	}
}

func findEntryByTitle(db *kdbx.Database, title string) *kdbx.Entry {
	for _, entry := range db.Entries() {
		if got, _ := entry.Field(kdbx.FieldTitle); got == title {
			return entry
		}
	}
	return nil
}
