package vault

import (
	"path/filepath"
	"testing"

	"gokeep/internal/dbdoc"
)

// TestLegacyDatabaseGetsARecycleBin reproduces the situation of databases created by
// early builds: the recycle bin policy was stored as "disabled", so deleting an entry
// removed it for good and no recycle bin group ever appeared.
func TestLegacyDatabaseGetsARecycleBin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.gkdb")
	key := dbdoc.Key{Password: "pw"}

	// Build the database the way the old code did.
	db := dbdoc.NewDatabase("Legacy", "")
	db.Meta.RecycleBinEnabled = false
	db.Meta.RecycleBinUUID = dbdoc.ZeroUUID
	entry := dbdoc.NewEntry()
	entry.SetField(dbdoc.FieldTitle, "Keep me", false)
	db.Root.Entries = append(db.Root.Entries, entry)
	writeDatabase(t, path, db, key)

	opened, err := Open(path, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !opened.Database().Meta.RecycleBinEnabled {
		t.Error("opening a legacy database should switch the recycle bin policy on")
	}
	if !opened.Modified() {
		t.Error("the migration should mark the database as modified so it gets saved")
	}
	rootID := opened.RootID()
	if _, err := opened.CreateEntry(rootID, EntryDTO{Title: "Second", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	entries, err := opened.Entries(rootID, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if err := opened.DeleteEntry(entries[1].ID, false); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	bin := opened.Database().RecycleBin()
	if bin == nil {
		t.Fatal("the recycle bin group was not created")
	}
	if len(bin.Entries) != 1 {
		t.Errorf("the recycle bin holds %d entries, want 1", len(bin.Entries))
	}
	// The tree (which is what the user interface renders) must contain the bin.
	tree := opened.Tree()
	found := false
	for _, group := range tree {
		if group.Name == dbdoc.RecycleBinName {
			found = true
			if len(group.EntryIDs) != 1 {
				t.Errorf("the tree reports %d entries in the recycle bin", len(group.EntryIDs))
			}
		}
	}
	if !found {
		t.Fatalf("the recycle bin is missing from the tree: %+v", tree)
	}
}

// A database whose recycle bin the user disabled on purpose must keep that setting.
func TestDeliberatelyDisabledRecycleBinStaysDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-bin.gkdb")
	key := dbdoc.Key{Password: "pw"}

	v, err := Create(path, key, "No bin", DefaultSaveSettings())
	if err != nil {
		t.Fatal(err)
	}
	if err := v.ApplyDatabaseSettings(DatabaseSettings{Name: "No bin", RecycleBinEnabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Database().Meta.RecycleBinEnabled {
		t.Error("an explicit choice must survive the migration")
	}
	rootID := reopened.RootID()
	entryID, err := reopened.CreateEntry(rootID, EntryDTO{Title: "Gone", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.DeleteEntry(entryID, false); err != nil {
		t.Fatal(err)
	}
	if reopened.Database().RecycleBin() != nil {
		t.Error("no recycle bin should be created when it is disabled")
	}
	if _, err := reopened.Entry(entryID); err == nil {
		t.Error("the entry should have been deleted permanently")
	}
}

// TestLegacyDisabledMarkerIsCleanedUp covers the databases written by builds that stored a
// "recycle bin disabled" marker without the user asking for it: deleting an entry removed it
// for good even though the interface offered to move it to the recycle bin.
func TestLegacyDisabledMarkerIsCleanedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy-marker.gkdb")
	key := dbdoc.Key{Password: "pw"}

	db := dbdoc.NewDatabase("Legacy marker", "")
	db.Meta.RecycleBinEnabled = false
	db.Meta.CustomData = dbdoc.SetCustomDataValue(db.Meta.CustomData, recycleBinDisabledKey, "true")
	entry := dbdoc.NewEntry()
	entry.SetField(dbdoc.FieldTitle, "Deleted by mistake", false)
	db.Root.Entries = append(db.Root.Entries, entry)
	writeDatabase(t, path, db, key)

	v, err := Open(path, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !v.Database().Meta.RecycleBinEnabled {
		t.Fatal("a stale marker must not keep the recycle bin switched off")
	}
	if _, ok := dbdoc.CustomDataValue(v.Database().Meta.CustomData, recycleBinDisabledKey); ok {
		t.Error("the stale marker should have been removed from the database")
	}
	rootID := v.RootID()
	entryID, err := v.CreateEntry(rootID, EntryDTO{Title: "Second", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteEntry(entryID, false); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	bin := v.Database().RecycleBin()
	if bin == nil || len(bin.Entries) != 1 {
		t.Fatalf("the deleted entry did not end up in the recycle bin: %+v", bin)
	}
}

// TestOrphanedRecycleBinGroupIsAdopted covers databases that already contain a group named
// "Recycle Bin" but never recorded its identifier in the metadata. The group was displayed
// as an ordinary group, the entries deleted into it lost their "recycled" marking and a
// second bin was created on the next delete.
func TestOrphanedRecycleBinGroupIsAdopted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orphan.gkdb")
	key := dbdoc.Key{Password: "pw"}

	db := dbdoc.NewDatabase("Orphan", "")
	db.Meta.RecycleBinEnabled = true
	db.Meta.RecycleBinUUID = dbdoc.ZeroUUID
	bin := dbdoc.NewGroup(dbdoc.LegacyRecycleBinName)
	db.Root.Groups = append(db.Root.Groups, bin)
	entry := dbdoc.NewEntry()
	entry.SetField(dbdoc.FieldTitle, "Old deleted entry", false)
	bin.Entries = append(bin.Entries, entry)
	writeDatabase(t, path, db, key)

	v, err := Open(path, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	adopted := v.Database().RecycleBin()
	if adopted == nil {
		t.Fatal("the existing recycle bin group should have been adopted")
	}
	if adopted.UUID != bin.UUID {
		t.Error("a second recycle bin group was created instead of adopting the existing one")
	}
	// The entries of the adopted bin count as recycled, so the tree and the restore command
	// see them the way the user expects.
	summaries, err := v.Entries(idOf(bin.UUID), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || !summaries[0].IsRecycled {
		t.Fatalf("the entries of the adopted bin are not marked as recycled: %+v", summaries)
	}
	if len(v.Database().Root.Groups) != 1 {
		t.Errorf("the database has %d root groups, want only the adopted bin", len(v.Database().Root.Groups))
	}
}

// TestStaleRecycleBinReferenceIsForgotten makes sure a metadata entry that points at a
// group which no longer exists does not block the creation of a new recycle bin.
func TestStaleRecycleBinReferenceIsForgotten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.gkdb")
	key := dbdoc.Key{Password: "pw"}

	db := dbdoc.NewDatabase("Stale", "")
	missing := dbdoc.NewGroup("Gone")
	db.Meta.RecycleBinEnabled = true
	db.Meta.RecycleBinUUID = missing.UUID
	entry := dbdoc.NewEntry()
	entry.SetField(dbdoc.FieldTitle, "Keep", false)
	db.Root.Entries = append(db.Root.Entries, entry)
	writeDatabase(t, path, db, key)

	v, err := Open(path, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !v.Database().Meta.RecycleBinUUID.IsZero() {
		t.Error("the stale group reference should have been dropped")
	}
	rootID := v.RootID()
	entryID, err := v.CreateEntry(rootID, EntryDTO{Title: "Second", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteEntry(entryID, false); err != nil {
		t.Fatal(err)
	}
	bin := v.Database().RecycleBin()
	deleted, err := dbdoc.ParseUUID(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if bin == nil || bin.FindEntry(deleted) == nil {
		t.Fatal("deleting should create a working recycle bin")
	}
}
