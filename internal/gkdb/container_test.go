package gkdb

import (
	"bytes"
	"testing"

	"gokeep/internal/dbdoc"
)

// TestHeaderSaltDrivesKeyDerivation guards the invariant that bit us once: the key must
// be derived from the parameters as they appear in the header (including the header
// salt), otherwise the file cannot be decrypted again.
func TestHeaderSaltDrivesKeyDerivation(t *testing.T) {
	key := dbdoc.Key{Password: "pw"}
	data, err := Save(testDatabase(), key, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	h, err := parseHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.salt) != 32 {
		t.Fatalf("header salt length = %d", len(h.salt))
	}
	composite, _ := key.CompositeKey()
	transformed, err := h.kdfParams().Transform(composite)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := h.newAEAD(transformed[:])
	if err != nil {
		t.Fatal(err)
	}
	plain, err := aead.Open(nil, h.nonce, data[headerSize:], data[:headerSize])
	if err != nil {
		t.Fatalf("the header alone is not enough to decrypt the file: %v", err)
	}
	if len(plain) == 0 {
		t.Fatal("empty payload")
	}
	// Header comparison: the bytes used as AAD while saving must equal the file header.
	if !bytes.Equal(h.raw, data[:headerSize]) {
		t.Error("the header used as associated data differs from the file header")
	}
}
