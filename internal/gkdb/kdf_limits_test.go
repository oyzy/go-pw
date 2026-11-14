package gkdb

import (
	"encoding/binary"
	"testing"

	"gokeep/internal/dbdoc"
)

func TestUnsafeKDFParametersAreRejectedBeforeDerivation(t *testing.T) {
	key := dbdoc.Key{Password: "correct horse battery staple"}
	argonData, err := Save(testDatabase(), key, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	aesData, err := Save(testDatabase(), key, Options{
		Cipher:      CipherAES256GCM,
		Compression: CompressionGzip,
		KDF:         dbdoc.DefaultAESKDFParams(),
	})
	if err != nil {
		t.Fatal(err)
	}

	putUint64 := func(data []byte, offset int, value uint64) []byte {
		out := append([]byte(nil), data...)
		binary.LittleEndian.PutUint64(out[offset:], value)
		return out
	}
	putUint32 := func(data []byte, offset int, value uint32) []byte {
		out := append([]byte(nil), data...)
		binary.LittleEndian.PutUint32(out[offset:], value)
		return out
	}
	putByte := func(data []byte, offset int, value byte) []byte {
		out := append([]byte(nil), data...)
		out[offset] = value
		return out
	}

	cases := []struct {
		name string
		data []byte
	}{
		{"argon memory", putUint64(argonData, 12, 1<<40)},
		{"argon iterations", putUint32(argonData, 20, uint32(dbdoc.MaxArgon2Iterations)+1)},
		{"argon parallelism", putUint32(argonData, 24, dbdoc.MaxArgon2Parallelism+1)},
		{"argon version", putUint32(argonData, 36, 0x10)},
		{"unknown KDF", putByte(argonData, 8, 99)},
		{"aes rounds", putUint64(aesData, 28, dbdoc.MaxAESKDFRounds+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(tc.data, key); err == nil {
				t.Fatal("Load accepted a file with unsafe KDF parameters")
			}
			if _, err := Inspect(tc.data); err == nil {
				t.Fatal("Inspect accepted a file with unsafe KDF parameters")
			}
		})
	}
}
