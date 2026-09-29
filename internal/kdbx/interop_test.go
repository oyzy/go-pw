package kdbx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fixturePassword = "Test-Password-123!"
	fixtureDir      = "../../testdata/reference"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(fixtureDir, name)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture %s is missing (run testdata/reference/make-fixtures.ps1)", name)
	}
	return path
}

func fixtureKey(t *testing.T, withKeyFile bool) Key {
	t.Helper()
	key := Key{Password: fixturePassword}
	if withKeyFile {
		path := filepath.Join(fixtureDir, "fixture.keyx")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("fixture key file is missing: %v", err)
		}
		key.KeyFile = data
		key.KeyFileName = path
	}
	return key
}

// TestReadKeePassFixtures opens databases written by the real KeePass 2 library and
// checks that every structure survived: groups, entries, protected values, custom
// fields, attachments, history, tags, colours, auto-type, icons and the recycle bin.
func TestReadKeePassFixtures(t *testing.T) {
	cases := []struct {
		name         string
		expectKDF    string
		expectCipher string
		withKeyFile  bool
	}{
		{"kdbx4-aes-argon2d.kdbx", "Argon2d", "AES-256", false},
		{"kdbx4-chacha20-argon2id.kdbx", "Argon2id", "ChaCha20", false},
		{"kdbx4-aes-aeskdf-nogzip.kdbx", "AES-KDF", "AES-256", false},
		{"kdbx40-aes-argon2d.kdbx", "Argon2d", "AES-256", false},
		{"kdbx31-aes-aeskdf.kdbx", "AES-KDF", "AES-256", false},
		{"kdbx4-aes-argon2d-keyfile.kdbx", "Argon2d", "AES-256", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := fixture(t, tc.name)
			info, err := InspectFile(path)
			if err != nil {
				t.Fatalf("InspectFile: %v", err)
			}
			if info.KDFName != tc.expectKDF {
				t.Errorf("KDF = %q, want %q", info.KDFName, tc.expectKDF)
			}
			if info.CipherName != tc.expectCipher {
				t.Errorf("cipher = %q, want %q", info.CipherName, tc.expectCipher)
			}
			db, err := LoadFile(path, fixtureKey(t, tc.withKeyFile))
			if err != nil {
				t.Fatalf("LoadFile: %v", err)
			}
			verifyFixtureContent(t, db, tc.name)
		})
	}
}

func verifyFixtureContent(t *testing.T, db *Database, name string) {
	t.Helper()
	if !strings.HasPrefix(db.Meta.DatabaseName, "GoKeep Test DB") {
		t.Errorf("database name = %q", db.Meta.DatabaseName)
	}
	if db.Meta.DefaultUserName != "default-user" {
		t.Errorf("default user name = %q", db.Meta.DefaultUserName)
	}
	if !db.Meta.MemoryProtection.ProtectPassword || db.Meta.MemoryProtection.ProtectTitle {
		t.Errorf("unexpected memory protection settings: %+v", db.Meta.MemoryProtection)
	}
	if len(db.Meta.CustomIcons) != 1 {
		t.Errorf("custom icons = %d, want 1", len(db.Meta.CustomIcons))
	}
	if len(db.DeletedObjects) != 1 {
		t.Errorf("deleted objects = %d, want 1", len(db.DeletedObjects))
	}
	if db.RecycleBin() == nil {
		t.Error("recycle bin group is missing")
	}

	github := findEntryByTitle(db, "GitHub")
	if github == nil {
		t.Fatal("entry GitHub is missing")
	}
	if got, _ := github.Field(FieldUserName); got != "octocat" {
		t.Errorf("GitHub user = %q", got)
	}
	if got, _ := github.Field(FieldPassword); got != "gh-P@ssw0rd-002" {
		t.Errorf("GitHub password = %q", got)
	}
	if got, _ := github.Field(FieldURL); got != "https://github.com/login" {
		t.Errorf("GitHub URL = %q", got)
	}
	if got, _ := github.Field(FieldNotes); !strings.Contains(got, "Multi-line") || !strings.Contains(got, "<special> & characters.") {
		t.Errorf("GitHub notes = %q", got)
	}
	if got, _ := github.Field("PIN"); got != "4242" || !github.IsFieldProtected("PIN") {
		t.Errorf("custom protected field PIN = %q protected=%v", got, github.IsFieldProtected("PIN"))
	}
	if got, _ := github.Field("Department"); got != "Platform" || github.IsFieldProtected("Department") {
		t.Errorf("custom field Department = %q protected=%v", got, github.IsFieldProtected("Department"))
	}
	if len(github.Tags) != 2 || github.Tags[0] != "dev" || github.Tags[1] != "work" {
		t.Errorf("GitHub tags = %v", github.Tags)
	}
	if !github.Times.Expires {
		t.Error("GitHub entry should expire")
	}
	if len(github.History) != 1 {
		t.Fatalf("GitHub history length = %d, want 1", len(github.History))
	}
	if got, _ := github.History[0].Field(FieldPassword); got != "gh-P@ssw0rd-001" {
		t.Errorf("history password = %q", got)
	}
	if len(github.Binaries) != 1 {
		t.Fatalf("GitHub attachments = %d, want 1", len(github.Binaries))
	}
	blob, ok := db.Binary(github.Binaries[0].Pool)
	if !ok {
		t.Fatal("attachment pool index is out of range")
	}
	if string(blob) != "attachment payload - hello from KeePassLib" {
		t.Errorf("attachment content = %q", string(blob))
	}
	if github.Binaries[0].Key != "notes.txt" {
		t.Errorf("attachment key = %q", github.Binaries[0].Key)
	}
	if !github.AutoType.Enabled || len(github.AutoType.Associations) != 1 {
		t.Errorf("auto-type config = %+v", github.AutoType)
	}
	if seq := github.AutoType.Associations[0].Sequence; !strings.Contains(seq, "{PASSWORD}") {
		t.Errorf("auto-type sequence = %q", seq)
	}

	mail := findEntryByTitle(db, "Mail")
	if mail == nil {
		t.Fatal("entry Mail is missing")
	}
	if got, _ := mail.Field(FieldPassword); got != "mail-secret-42" {
		t.Errorf("Mail password = %q", got)
	}
	if mail.IconID != 19 {
		t.Errorf("Mail icon = %d, want 19", mail.IconID)
	}
	if mail.CustomIconUUID.IsZero() && name != "kdbx31-aes-aeskdf.kdbx" {
		t.Error("Mail entry should reference the custom icon")
	}

	nested := findEntryByTitle(db, "Nested Entry")
	if nested == nil {
		t.Fatal("entry Nested Entry is missing")
	}
	if nested.OverrideURL != "cmd://notepad.exe" {
		t.Errorf("OverrideURL = %q", nested.OverrideURL)
	}
	if nested.ForegroundColor == "" || nested.BackgroundColor == "" {
		t.Errorf("colours lost: fg=%q bg=%q", nested.ForegroundColor, nested.BackgroundColor)
	}
	if len(nested.CustomData) != 1 || nested.CustomData[0].Key != "custom-plugin" {
		t.Errorf("entry custom data = %+v", nested.CustomData)
	}

	unicode := findEntryByTitleContaining(db, "中文条目")
	if unicode == nil {
		t.Fatal("unicode entry is missing")
	}
	if got, _ := unicode.Field(FieldPassword); !strings.HasPrefix(got, "密码-") {
		t.Errorf("unicode password = %q", got)
	}
	if got, _ := unicode.Field(FieldNotes); !strings.Contains(got, "\n") {
		t.Errorf("multi line notes lost: %q", got)
	}

	deleted := findEntryByTitle(db, "Deleted Entry")
	if deleted == nil {
		t.Fatal("entry inside the recycle bin is missing")
	}
	if bin := db.RecycleBin(); bin != nil && bin.FindEntry(deleted.UUID) == nil {
		t.Error("deleted entry is not inside the recycle bin")
	}
}

func findEntryByTitle(db *Database, title string) *Entry {
	for _, e := range db.Entries() {
		if got, _ := e.Field(FieldTitle); got == title {
			return e
		}
	}
	return nil
}

func findEntryByTitleContaining(db *Database, part string) *Entry {
	for _, e := range db.Entries() {
		if got, _ := e.Field(FieldTitle); strings.Contains(got, part) {
			return e
		}
	}
	return nil
}

func TestWrongPasswordIsRejected(t *testing.T) {
	path := fixture(t, "kdbx4-aes-argon2d.kdbx")
	if _, err := LoadFile(path, Key{Password: "not-the-password"}); err == nil {
		t.Fatal("opening with a wrong password must fail")
	}
	if _, err := LoadFile(path, Key{}); err == nil {
		t.Fatal("opening without credentials must fail")
	}
}

func TestTamperedHeaderIsRejected(t *testing.T) {
	path := fixture(t, "kdbx4-aes-argon2d.kdbx")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a bit inside the master seed: the header digest must catch it.
	data[60] ^= 0x40
	if _, err := Load(data, Key{Password: fixturePassword}); err == nil {
		t.Fatal("a modified header must be detected")
	}
}
