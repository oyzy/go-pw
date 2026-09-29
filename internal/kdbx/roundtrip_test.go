package kdbx

import (
	"bytes"
	"testing"
	"time"
)

// buildRichDatabase creates a database that uses every structure the format supports,
// so the round trip and the interoperability tests cover all of them.
func buildRichDatabase(t *testing.T) *Database {
	t.Helper()
	db := NewDatabase("Round Trip DB", "default-user")
	db.Meta.DatabaseDescription = "Description with special characters: <>&\"'"

	root := db.RootGroup()
	// A new database contains no groups at all, so the test builds its own hierarchy.
	general := NewGroup("General")
	root.Groups = append(root.Groups, general)
	general.Notes = "Group notes"
	general.Tags = []string{"root-tag"}

	entry := NewEntry()
	entry.SetField(FieldTitle, "Example Entry", false)
	entry.SetField(FieldUserName, "user@example.com", false)
	entry.SetField(FieldPassword, "s3cret-pässword-\n", true)
	entry.SetField(FieldURL, "https://example.com/login?a=1&b=2", false)
	entry.SetField(FieldNotes, "line one\nline two\ttabbed", false)
	entry.SetField("PIN", "1234", true)
	entry.SetField("Plain", "visible", false)
	entry.AddTag("work")
	entry.AddTag("important")
	entry.IconID = 12
	entry.ForegroundColor = "#FF0000"
	entry.BackgroundColor = "#FFD700"
	entry.OverrideURL = "cmd://notepad.exe"
	entry.Times.Expires = true
	entry.Times.ExpiryTime = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	entry.AutoType = AutoType{
		Enabled:         true,
		DefaultSequence: "{USERNAME}{TAB}{PASSWORD}{ENTER}",
		Associations:    []AutoTypeAssociation{{WindowName: "Example*", Sequence: "{PASSWORD}{ENTER}"}},
	}
	entry.CustomData = []CustomDataItem{{Key: "plugin", Value: "value"}}
	// History: an older revision of the same entry.
	old := entry.CloneWithoutHistory()
	old.SetField(FieldPassword, "old-password", true)
	old.Times.LastModification = time.Date(2020, 5, 6, 7, 8, 9, 0, time.UTC)
	entry.History = []*Entry{old}
	general.Entries = append(general.Entries, entry)

	// Attachment: referenced from the shared binary pool.
	blob := []byte("binary attachment payload")
	pool := db.AddBinary(blob, false)
	entry.Binaries = append(entry.Binaries, BinaryRef{Key: "data.bin", Pool: pool})

	// Unicode heavy entry in a nested group.
	sub := NewGroup("子分组 ✓")
	sub.Notes = "nested group"
	general.Groups = append(general.Groups, sub)
	unicode := NewEntry()
	unicode.SetField(FieldTitle, "中文条目 🔑", false)
	unicode.SetField(FieldUserName, "用户名", false)
	unicode.SetField(FieldPassword, "密码-🔐", true)
	sub.Entries = append(sub.Entries, unicode)

	// Custom icon and deleted object bookkeeping.
	db.Meta.CustomIcons = append(db.Meta.CustomIcons, CustomIcon{
		UUID: NewUUID(),
		Data: []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4},
		Name: "test icon",
	})
	entry.CustomIconUUID = db.Meta.CustomIcons[0].UUID
	db.DeletedObjects = append(db.DeletedObjects, DeletedObject{
		UUID:         NewUUID(),
		DeletionTime: time.Date(2021, 2, 3, 4, 5, 6, 0, time.UTC),
	})

	// Recycle bin content. The bin is created on demand, so the test enables it first.
	bin := db.EnableRecycleBin()
	deleted := NewEntry()
	deleted.SetField(FieldTitle, "Deleted Entry", false)
	deleted.SetField(FieldPassword, "deleted-secret", true)
	bin.Entries = append(bin.Entries, deleted)
	db.Meta.RecycleBinEnabled = true
	return db
}

func TestRoundTripAllFormats(t *testing.T) {
	cases := []struct {
		name        string
		version     uint32
		cipher      UUID
		kdf         func() *KDFParams
		compression uint32
	}{
		{"4.1 AES Argon2d gzip", FileVersion41, CipherAES256, DefaultKDFParams, CompressionGzip},
		{"4.1 ChaCha20 Argon2id gzip", FileVersion41, CipherChaCha20, func() *KDFParams {
			p := NewKDFParams(KDFArgon2id)
			p.Dict.SetBytes(KdfParamSeed, bytes.Repeat([]byte{7}, 32))
			p.Dict.SetUint32(KdfParamParallelism, 2)
			p.Dict.SetUint64(KdfParamMemory, 16*1024*1024)
			p.Dict.SetUint64(KdfParamIterations, 2)
			p.Dict.SetUint32(KdfParamVersion, 0x13)
			return p
		}, CompressionGzip},
		{"4.0 AES AES-KDF no compression", FileVersion4, CipherAES256, DefaultAESKDFParams, CompressionNone},
		{"3.1 AES AES-KDF gzip", FileVersion31, CipherAES256, DefaultAESKDFParams, CompressionGzip},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := Key{Password: "round-trip-password"}
			source := buildRichDatabase(t)
			compression := tc.compression
			data, err := Save(source, key, SaveOptions{
				Version:     tc.version,
				Cipher:      tc.cipher,
				KDF:         tc.kdf(),
				Compression: &compression,
			})
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			db, err := Load(data, key)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			verifyRoundTrip(t, db)
		})
	}
}

func verifyRoundTrip(t *testing.T, db *Database) {
	t.Helper()
	if db.Meta.DatabaseName != "Round Trip DB" {
		t.Errorf("database name = %q", db.Meta.DatabaseName)
	}
	if db.Meta.DatabaseDescription != "Description with special characters: <>&\"'" {
		t.Errorf("description = %q", db.Meta.DatabaseDescription)
	}
	if db.Meta.DefaultUserName != "default-user" {
		t.Errorf("default user = %q", db.Meta.DefaultUserName)
	}
	if len(db.Meta.CustomIcons) != 1 {
		t.Errorf("custom icons = %+v", db.Meta.CustomIcons)
	} else if db.Meta.CustomIcons[0].Name != "" && db.Meta.CustomIcons[0].Name != "test icon" {
		t.Errorf("custom icon name = %q", db.Meta.CustomIcons[0].Name)
	}
	if len(db.DeletedObjects) != 1 {
		t.Errorf("deleted objects = %d", len(db.DeletedObjects))
	}

	entry := findEntryByTitle(db, "Example Entry")
	if entry == nil {
		t.Fatal("entry is missing")
	}
	if got, _ := entry.Field(FieldPassword); got != "s3cret-pässword-\n" {
		t.Errorf("password = %q", got)
	}
	if got, _ := entry.Field(FieldNotes); got != "line one\nline two\ttabbed" {
		t.Errorf("notes = %q", got)
	}
	if got, _ := entry.Field(FieldURL); got != "https://example.com/login?a=1&b=2" {
		t.Errorf("url = %q", got)
	}
	if got, _ := entry.Field("PIN"); got != "1234" || !entry.IsFieldProtected("PIN") {
		t.Errorf("PIN = %q protected=%v", got, entry.IsFieldProtected("PIN"))
	}
	if got, _ := entry.Field("Plain"); got != "visible" || entry.IsFieldProtected("Plain") {
		t.Errorf("Plain = %q protected=%v", got, entry.IsFieldProtected("Plain"))
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "work" || entry.Tags[1] != "important" {
		t.Errorf("tags = %v", entry.Tags)
	}
	if entry.IconID != 12 || entry.ForegroundColor != "#FF0000" || entry.BackgroundColor != "#FFD700" {
		t.Errorf("appearance = icon %d fg %q bg %q", entry.IconID, entry.ForegroundColor, entry.BackgroundColor)
	}
	if entry.OverrideURL != "cmd://notepad.exe" {
		t.Errorf("override url = %q", entry.OverrideURL)
	}
	if !entry.Times.Expires || !entry.Times.ExpiryTime.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("expiry = %v expires=%v", entry.Times.ExpiryTime, entry.Times.Expires)
	}
	if len(entry.AutoType.Associations) != 1 || entry.AutoType.Associations[0].WindowName != "Example*" {
		t.Errorf("auto type = %+v", entry.AutoType)
	}
	// Custom data on entries is a KDBX 4.1 feature and is expected to be dropped when
	// the database is written in an older format.
	if len(entry.CustomData) > 1 || (len(entry.CustomData) == 1 && entry.CustomData[0].Value != "value") {
		t.Errorf("custom data = %+v", entry.CustomData)
	}
	if len(entry.History) != 1 {
		t.Fatalf("history = %d", len(entry.History))
	}
	if got, _ := entry.History[0].Field(FieldPassword); got != "old-password" {
		t.Errorf("history password = %q", got)
	}
	if len(entry.Binaries) != 1 {
		t.Fatalf("attachments = %d", len(entry.Binaries))
	}
	blob, ok := db.Binary(entry.Binaries[0].Pool)
	if !ok || string(blob) != "binary attachment payload" {
		t.Errorf("attachment = %q", string(blob))
	}
	if !entry.CustomIconUUID.IsZero() && db.FindGroup(entry.CustomIconUUID) != nil {
		t.Error("custom icon uuid must not resolve to a group")
	}

	unicode := findEntryByTitle(db, "中文条目 🔑")
	if unicode == nil {
		t.Fatal("unicode entry is missing")
	}
	if got, _ := unicode.Field(FieldPassword); got != "密码-🔐" {
		t.Errorf("unicode password = %q", got)
	}
	if db.FindGroup(NewGroup("子分组 ✓").UUID) != nil {
		t.Error("random uuid must not resolve")
	}
	sub := db.Root.FindGroupByName("子分组 ✓")
	if sub == nil || len(sub.Entries) != 1 {
		t.Errorf("nested group not restored: %+v", sub)
	}

	bin := db.RecycleBin()
	if bin == nil {
		t.Fatal("recycle bin is missing")
	}
	if findEntryByTitle(&Database{Root: bin}, "Deleted Entry") == nil {
		t.Error("deleted entry is missing from the recycle bin")
	}
}
