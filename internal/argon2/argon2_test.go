package argon2

import (
	"bytes"
	"encoding/hex"
	"testing"

	xargon2 "golang.org/x/crypto/argon2"
)

// The implementation must produce exactly the same output as the reference
// implementation shipped with x/crypto for the two variants it exposes.
func TestMatchesXCrypto(t *testing.T) {
	password := []byte("correct horse battery staple")
	salt := []byte("0123456789abcdef")

	cases := []struct {
		name    string
		time    uint32
		memory  uint32
		threads uint8
		keyLen  uint32
	}{
		{"tiny", 1, 8, 1, 32},
		{"keeps-default", 2, 64, 2, 32},
		{"threads4", 3, 96, 4, 64},
		{"keylen128", 2, 64, 1, 128},
		{"single-pass", 1, 32, 2, 32},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IDKey(password, salt, tc.time, tc.memory, tc.threads, tc.keyLen)
			want := xargon2.IDKey(password, salt, tc.time, tc.memory, tc.threads, tc.keyLen)
			if !bytes.Equal(got, want) {
				t.Fatalf("argon2id mismatch\n got: %s\nwant: %s", hex.EncodeToString(got), hex.EncodeToString(want))
			}
			gotKey := Key(password, salt, tc.time, tc.memory, tc.threads, tc.keyLen)
			wantKey := xargon2.Key(password, salt, tc.time, tc.memory, tc.threads, tc.keyLen)
			if !bytes.Equal(gotKey, wantKey) {
				t.Fatalf("argon2i mismatch\n got: %s\nwant: %s", hex.EncodeToString(gotKey), hex.EncodeToString(wantKey))
			}
		})
	}
}

func TestSecretAndAssociatedData(t *testing.T) {
	password := []byte("password")
	salt := []byte("somesalt")
	secret := []byte("secret-key")
	data := []byte("associated-data")

	derive := func(sec []byte) []byte {
		out, err := DeriveKey(password, Config{
			Type: TypeID, Version: Version13, Time: 2, Memory: 32, Threads: 1, KeyLen: 32,
			Salt: salt, Secret: sec, Data: data,
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	a := derive(secret)
	b := derive(secret)
	if !bytes.Equal(a, b) {
		t.Fatal("derivation is not deterministic")
	}
	if bytes.Equal(a, derive([]byte("other"))) {
		t.Fatal("the secret key must influence the output")
	}
}

func TestValidation(t *testing.T) {
	base := Config{Type: TypeD, Version: Version13, Time: 1, Memory: 8, Threads: 1, KeyLen: 32, Salt: []byte("saltysalt")}
	if _, err := DeriveKey([]byte("x"), base); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := base
	bad.Salt = []byte("short")
	if _, err := DeriveKey([]byte("x"), bad); err == nil {
		t.Fatal("short salt must be rejected")
	}
	bad = base
	// Only Argon2 version 1.3 is implemented; version 1.0 must be refused instead of
	// silently producing different key material.
	bad.Version = Version10
	if _, err := DeriveKey([]byte("x"), bad); err == nil {
		t.Fatal("version 1.0 must be rejected")
	}
	bad = base
	bad.Memory = MaxMemoryKiB + 1
	if _, err := DeriveKey([]byte("x"), bad); err == nil {
		t.Fatal("unbounded memory must be rejected")
	}
	bad = base
	bad.Time = MaxPasses + 1
	if _, err := DeriveKey([]byte("x"), bad); err == nil {
		t.Fatal("unbounded passes must be rejected")
	}
	bad = base
	bad.Threads = MaxThreads + 1
	if _, err := DeriveKey([]byte("x"), bad); err == nil {
		t.Fatal("unbounded parallelism must be rejected")
	}
}

func BenchmarkArgon2dKeePassDefaults(b *testing.B) {
	password := []byte("benchmark-password")
	salt := []byte("0123456789abcdef0123456789abcdef")
	cfg := Config{Type: TypeD, Version: Version13, Time: 2, Memory: 64 * 1024, Threads: 2, KeyLen: 32, Salt: salt}
	for i := 0; i < b.N; i++ {
		if _, err := DeriveKey(password, cfg); err != nil {
			b.Fatal(err)
		}
	}
}
