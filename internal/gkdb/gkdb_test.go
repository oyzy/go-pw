package gkdb

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"gokeep/internal/kdbx"
)

func testDatabase() *kdbx.Database {
	db := kdbx.NewDatabase("Native DB", "user")
	root := db.RootGroup()
	group := kdbx.NewGroup("General")
	root.Groups = append(root.Groups, group)

	entry := kdbx.NewEntry()
	entry.SetField(kdbx.FieldTitle, "Example", false)
	entry.SetField(kdbx.FieldUserName, "octocat", false)
	entry.SetField(kdbx.FieldPassword, "s3cret-pässword", true)
	entry.SetField(kdbx.FieldNotes, "line one\nline two", false)
	entry.SetField("PIN", "4242", true)
	entry.AddTag("work")
	entry.IconID = 12
	entry.Times.Expires = true
	entry.Times.ExpiryTime = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	group.Entries = append(group.Entries, entry)

	pool := db.AddBinary([]byte("attachment payload"), false)
	entry.Binaries = append(entry.Binaries, kdbx.BinaryRef{Key: "data.bin", Pool: pool})
	backup := entry.CloneWithoutHistory()
	backup.SetField(kdbx.FieldPassword, "old-password", true)
	entry.History = []*kdbx.Entry{backup}

	db.Meta.CustomIcons = append(db.Meta.CustomIcons, kdbx.CustomIcon{UUID: kdbx.NewUUID(), Data: []byte{1, 2, 3}})
	db.DeletedObjects = append(db.DeletedObjects, kdbx.DeletedObject{UUID: kdbx.NewUUID(), DeletionTime: time.Now().UTC()})
	return db
}

func TestRoundTrip(t *testing.T) {
	key := kdbx.Key{Password: "master-password"}
	cases := []struct {
		name        string
		cipher      byte
		compression uint16
		kdf         *kdbx.KDFParams
	}{
		{"gcm+gzip+argon2d", CipherAES256GCM, CompressionGzip, kdbx.DefaultKDFParams()},
		{"chacha+none+argon2id", CipherChaCha20Poly1305, CompressionNone, func() *kdbx.KDFParams {
			params := kdbx.NewKDFParams(kdbx.KDFArgon2id)
			params.Dict.SetBytes(kdbx.KdfParamSeed, bytes.Repeat([]byte{9}, 32))
			params.Dict.SetUint32(kdbx.KdfParamParallelism, 2)
			params.Dict.SetUint64(kdbx.KdfParamMemory, 16*1024*1024)
			params.Dict.SetUint64(kdbx.KdfParamIterations, 2)
			params.Dict.SetUint32(kdbx.KdfParamVersion, 0x13)
			return params
		}()},
		{"gcm+aeskdf", CipherAES256GCM, CompressionGzip, kdbx.DefaultAESKDFParams()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Save(testDatabase(), key, Options{Cipher: tc.cipher, Compression: tc.compression, KDF: tc.kdf})
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if !IsGoKeepFile(data) {
				t.Fatal("the file does not carry the GoKeep signature")
			}
			if _, err := kdbx.Load(data, key); err == nil {
				t.Fatal("the native format must not be readable as a KDBX file")
			}
			db, err := Load(data, key)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			entry := db.Root.FindGroupByName("General").Entries[0]
			if got, _ := entry.Field(kdbx.FieldPassword); got != "s3cret-pässword" {
				t.Errorf("password = %q", got)
			}
			if got, _ := entry.Field("PIN"); got != "4242" || !entry.IsFieldProtected("PIN") {
				t.Errorf("custom protected field = %q protected=%v", got, entry.IsFieldProtected("PIN"))
			}
			if got, _ := entry.Field(kdbx.FieldNotes); got != "line one\nline two" {
				t.Errorf("notes = %q", got)
			}
			if len(entry.History) != 1 {
				t.Fatalf("history = %d", len(entry.History))
			}
			if got, _ := entry.History[0].Field(kdbx.FieldPassword); got != "old-password" {
				t.Errorf("history password = %q", got)
			}
			if len(entry.Binaries) != 1 {
				t.Fatalf("attachments = %d", len(entry.Binaries))
			}
			if blob, ok := db.Binary(entry.Binaries[0].Pool); !ok || string(blob) != "attachment payload" {
				t.Errorf("attachment = %q", string(blob))
			}
			if len(db.Meta.CustomIcons) != 1 || len(db.DeletedObjects) != 1 {
				t.Errorf("meta = %+v", db.Meta)
			}
			if !entry.Times.Expires || !entry.Times.ExpiryTime.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)) {
				t.Errorf("expiry = %v", entry.Times.ExpiryTime)
			}
		})
	}
}

func TestWrongKeyIsRejected(t *testing.T) {
	data, err := Save(testDatabase(), kdbx.Key{Password: "right"}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(data, kdbx.Key{Password: "wrong"}); err == nil {
		t.Fatal("a wrong password must be rejected")
	}
	if _, err := Load(data, kdbx.Key{}); err == nil {
		t.Fatal("missing credentials must be rejected")
	}
}

// TestTamperingIsDetected flips bits in every part of the container; the authenticated
// encryption has to notice each of them.
func TestTamperingIsDetected(t *testing.T) {
	key := kdbx.Key{Password: "right"}
	original, err := Save(testDatabase(), key, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]int{
		"magic":       0,
		"version":     4,
		"flags":       6,
		"kdf id":      8,
		"cipher id":   9,
		"memory":      12,
		"iterations":  20,
		"rounds":      28,
		"salt":        40,
		"nonce":       72,
		"payload len": 88,
		"payload":     headerSize + 2,
	}
	for name, position := range positions {
		tampered := append([]byte(nil), original...)
		tampered[position] ^= 0x40
		if _, err := Load(tampered, key); err == nil {
			t.Errorf("modifying the %s was not detected", name)
		}
	}
	// Truncation must be detected as well.
	if _, err := Load(original[:len(original)-1], key); err == nil {
		t.Error("truncating the file was not detected")
	}
}

func TestKeyFileAndInspect(t *testing.T) {
	keyFile, err := kdbx.CreateKeyFile()
	if err != nil {
		t.Fatal(err)
	}
	key := kdbx.Key{Password: "pw", KeyFile: keyFile}
	data, err := Save(testDatabase(), key, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(data, key); err != nil {
		t.Fatalf("loading with a key file failed: %v", err)
	}
	if _, err := Load(data, kdbx.Key{Password: "pw"}); err == nil {
		t.Fatal("the key file must be required")
	}

	info, err := Inspect(data)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.FormatVersion != FormatVersion || info.Cipher != "AES-256-GCM" || info.KDF != "Argon2d" {
		t.Errorf("info = %+v", info)
	}
	if info.KDFMemory == 0 || info.KDFIterations == 0 {
		t.Errorf("KDF parameters are missing from the header: %+v", info)
	}
}

func TestUnsupportedInput(t *testing.T) {
	if _, err := Load([]byte("not a database at all"), kdbx.Key{Password: "x"}); err == nil {
		t.Error("garbage must be rejected")
	} else if !strings.Contains(err.Error(), "signature") {
		t.Errorf("unexpected error: %v", err)
	}
}
