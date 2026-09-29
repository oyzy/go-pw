package vault

import (
	"path/filepath"
	"testing"

	"gokeep/internal/kdbx"
)

// TestNativeFormatKeepsRecycleBin checks the native .gkdb round trip: the bin group and the
// metadata entry that points at it both have to survive saving and loading.
func TestNativeFormatKeepsRecycleBin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin.gkdb")
	key := kdbx.Key{Password: "pw"}
	v, err := Create(path, key, "Native", DefaultSaveSettings())
	if err != nil {
		t.Fatal(err)
	}
	rootID := v.RootID()
	id, err := v.CreateEntry(rootID, EntryDTO{Title: "Gone", Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteEntry(id, false); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	bin := again.Database().RecycleBin()
	if bin == nil {
		t.Fatal("the recycle bin was lost while saving the native format")
	}
	if len(bin.Entries) != 1 {
		t.Fatalf("the recycle bin holds %d entries after reloading", len(bin.Entries))
	}
	if !again.Database().Meta.RecycleBinEnabled {
		t.Error("the recycle bin policy was lost")
	}
	binID := idOf(again.Database().Meta.RecycleBinUUID)
	for _, group := range again.Tree() {
		if group.ID == binID {
			if len(group.EntryIDs) != 1 {
				t.Errorf("the tree lists %d entries in the recycle bin", len(group.EntryIDs))
			}
			return
		}
	}
	t.Fatalf("the recycle bin is missing from the tree of %+v", again.Tree())
}
