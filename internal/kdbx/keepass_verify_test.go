package kdbx

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// keepassExe locates a KeePass 2 installation. The interoperability tests are skipped
// when it is not available, so the suite still runs on machines without KeePass.
func keepassExe(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("the KeePass 2 interoperability checks require Windows")
	}
	candidates := []string{}
	if env := os.Getenv("KEEPASS_EXE"); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates,
		`D:\software\KeePass\KeePass.exe`,
		`C:\Program Files\KeePass Password Safe 2\KeePass.exe`,
		`C:\Program Files (x86)\KeePass Password Safe 2\KeePass.exe`,
	)
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Skip("KeePass.exe not found; set KEEPASS_EXE to run the interoperability checks")
	return ""
}

type keepassField struct {
	Value     string `json:"value"`
	Protected bool   `json:"protected"`
}

type keepassEntry struct {
	UUID     string                  `json:"uuid"`
	IconID   int                     `json:"iconId"`
	Tags     []string                `json:"tags"`
	Fields   map[string]keepassField `json:"fields"`
	Binaries []struct {
		Key    string `json:"key"`
		Length int    `json:"length"`
		SHA256 string `json:"sha256"`
	} `json:"binaries"`
	History []keepassEntry `json:"history"`
}

type keepassGroup struct {
	Name    string         `json:"name"`
	Notes   string         `json:"notes"`
	IconID  int            `json:"iconId"`
	Entries []keepassEntry `json:"entries"`
	Groups  []keepassGroup `json:"groups"`
}

type keepassDump struct {
	Name               string       `json:"name"`
	Description        string       `json:"description"`
	DefaultUserName    string       `json:"defaultUserName"`
	CipherUUID         string       `json:"cipherUuid"`
	Compression        string       `json:"compression"`
	CustomIconCount    int          `json:"customIconCount"`
	DeletedObjectCount int          `json:"deletedObjectCount"`
	RecycleBinEnabled  bool         `json:"recycleBinEnabled"`
	Root               keepassGroup `json:"root"`
}

// inspectWithKeePass asks the real KeePass library to open a database and report what
// it found, which proves that our output is readable by KeePass itself.
func inspectWithKeePass(t *testing.T, path string, key Key) keepassDump {
	t.Helper()
	exe := keepassExe(t)
	script := filepath.Join(fixtureDir, "inspect-db.ps1")
	if _, err := os.Stat(script); err != nil {
		t.Skipf("inspection script missing: %v", err)
	}
	args := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script,
		"-Path", path, "-KeepassExe", exe, "-Password", key.Password}
	if len(key.KeyFile) > 0 {
		keyPath := path + ".keyx"
		if err := os.WriteFile(keyPath, key.KeyFile, 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(keyPath)
		args = append(args, "-KeyFile", keyPath)
	}
	cmd := exec.Command("powershell", args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("KeePass could not read %s: %v\n%s", filepath.Base(path), err, ee.Stderr)
		}
		t.Fatalf("running the KeePass inspection failed: %v", err)
	}
	var dump keepassDump
	if err := json.Unmarshal(out, &dump); err != nil {
		t.Fatalf("cannot parse the KeePass output: %v\n%s", err, out)
	}
	return dump
}

func findKeePassEntry(g *keepassGroup, title string) *keepassEntry {
	for i := range g.Entries {
		if g.Entries[i].Fields["Title"].Value == title {
			return &g.Entries[i]
		}
	}
	for i := range g.Groups {
		if found := findKeePassEntry(&g.Groups[i], title); found != nil {
			return found
		}
	}
	return nil
}

func findKeePassGroup(g *keepassGroup, name string) *keepassGroup {
	if g.Name == name {
		return g
	}
	for i := range g.Groups {
		if found := findKeePassGroup(&g.Groups[i], name); found != nil {
			return found
		}
	}
	return nil
}

// TestKeePassReadsOurFiles is the counterpart of TestReadKeePassFixtures: it writes
// databases with this implementation and has the real KeePass 2 library read them back.
func TestKeePassReadsOurFiles(t *testing.T) {
	keepassExe(t)
	key := Key{Password: fixturePassword}
	keyFile, err := CreateKeyFile()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		version uint32
		cipher  UUID
		kdf     *KDFParams
		key     Key
		argon2  bool
	}{
		{"4.1-aes-argon2d", FileVersion41, CipherAES256, DefaultKDFParams(), key, true},
		{"4.1-chacha20-argon2d", FileVersion41, CipherChaCha20, DefaultKDFParams(), key, true},
		{"4.0-aes-aeskdf", FileVersion4, CipherAES256, DefaultAESKDFParams(), key, false},
		{"3.1-aes-aeskdf", FileVersion31, CipherAES256, DefaultAESKDFParams(), key, false},
		{"4.1-aes-keyfile", FileVersion41, CipherAES256, DefaultKDFParams(),
			Key{Password: fixturePassword, KeyFile: keyFile}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := buildRichDatabase(t)
			compression := uint32(CompressionGzip)
			data, err := Save(db, tc.key, SaveOptions{
				Version:     tc.version,
				Cipher:      tc.cipher,
				KDF:         tc.kdf,
				Compression: &compression,
			})
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			path := filepath.Join(t.TempDir(), tc.name+".kdbx")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}

			dump := inspectWithKeePass(t, path, tc.key)
			if dump.Name != "Round Trip DB" {
				t.Errorf("KeePass reports name %q", dump.Name)
			}
			if dump.DefaultUserName != "default-user" {
				t.Errorf("KeePass reports default user %q", dump.DefaultUserName)
			}
			if !dump.RecycleBinEnabled {
				t.Error("KeePass does not see the recycle bin as enabled")
			}
			if dump.DeletedObjectCount != 1 {
				t.Errorf("KeePass reports %d deleted objects", dump.DeletedObjectCount)
			}
			if dump.CustomIconCount != 1 {
				t.Errorf("KeePass reports %d custom icons", dump.CustomIconCount)
			}
			if tc.cipher == CipherChaCha20 && !strings.EqualFold(dump.CipherUUID, CipherChaCha20.Hex()) {
				t.Errorf("KeePass reports cipher %s", dump.CipherUUID)
			}

			entry := findKeePassEntry(&dump.Root, "Example Entry")
			if entry == nil {
				t.Fatal("KeePass does not see the example entry")
			}
			if got := entry.Fields["Password"].Value; got != "s3cret-pässword-\n" {
				t.Errorf("KeePass reads password %q", got)
			}
			if !entry.Fields["Password"].Protected {
				t.Error("KeePass did not treat the password as protected")
			}
			if got := entry.Fields["Notes"].Value; got != "line one\nline two\ttabbed" {
				t.Errorf("KeePass reads notes %q", got)
			}
			if got := entry.Fields["URL"].Value; got != "https://example.com/login?a=1&b=2" {
				t.Errorf("KeePass reads url %q", got)
			}
			if got := entry.Fields["PIN"].Value; got != "1234" {
				t.Errorf("KeePass reads the custom protected field as %q", got)
			}
			if !entry.Fields["PIN"].Protected {
				t.Error("KeePass did not treat the custom field as protected")
			}
			if entry.Fields["Plain"].Protected {
				t.Error("KeePass treats the plain custom field as protected")
			}
			if len(entry.Tags) != 2 {
				t.Errorf("KeePass reads tags %v", entry.Tags)
			}
			if entry.IconID != 12 {
				t.Errorf("KeePass reads icon %d", entry.IconID)
			}
			if len(entry.Binaries) != 1 || entry.Binaries[0].Key != "data.bin" {
				t.Errorf("KeePass reads attachments %+v", entry.Binaries)
			}
			if len(entry.History) != 1 {
				t.Fatalf("KeePass reads %d history entries", len(entry.History))
			}
			if got := entry.History[0].Fields["Password"].Value; got != "old-password" {
				t.Errorf("KeePass reads the history password %q", got)
			}

			unicode := findKeePassEntry(&dump.Root, "中文条目 🔑")
			if unicode == nil {
				t.Fatal("KeePass does not see the unicode entry")
			}
			if got := unicode.Fields["Password"].Value; got != "密码-🔐" {
				t.Errorf("KeePass reads the unicode password %q", got)
			}
			if group := findKeePassGroup(&dump.Root, "子分组 ✓"); group == nil {
				t.Error("KeePass does not see the nested group")
			}
			bin := findKeePassGroup(&dump.Root, RecycleBinName)
			if bin == nil || findKeePassEntry(bin, "Deleted Entry") == nil {
				t.Error("KeePass does not see the recycle bin content")
			}
		})
	}
}
