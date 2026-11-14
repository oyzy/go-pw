package vault

import (
	"encoding/base64"
	"os"
	"time"

	"gokeep/internal/dbdoc"
	"gokeep/internal/gkdb"
)

func base64Encode(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

// settingsFromFile reads the on-disk settings of an existing database so that saving
// keeps the cipher, the compression and the key derivation function the user chose
// earlier. All of it lives in the header, so nothing has to be decrypted to read it
// back, and a file that cannot be read at all simply falls back to the defaults.
func settingsFromFile(path string) SaveSettings {
	settings := DefaultSaveSettings()
	data, err := os.ReadFile(path)
	if err != nil || !gkdb.IsGoKeepFile(data) {
		return settings
	}
	info, err := gkdb.Inspect(data)
	if err != nil {
		return settings
	}
	settings.Compression = dbdoc.CompressionNone
	if info.Compression == "GZip" {
		settings.Compression = dbdoc.CompressionGzip
	}
	if info.Cipher == "ChaCha20-Poly1305" {
		settings.Cipher = dbdoc.CipherChaCha20
	}
	kdf := dbdoc.NewKDFParams(dbdoc.KDFArgon2d)
	switch info.KDF {
	case "Argon2id":
		kdf = dbdoc.NewKDFParams(dbdoc.KDFArgon2id)
	case "AES-KDF":
		kdf = dbdoc.NewKDFParams(dbdoc.KDFAES)
	}
	kdf.Dict.SetUint64(dbdoc.KdfParamRounds, info.KDFRounds)
	kdf.Dict.SetUint64(dbdoc.KdfParamMemory, info.KDFMemory)
	kdf.Dict.SetUint64(dbdoc.KdfParamIterations, uint64(info.KDFIterations))
	kdf.Dict.SetUint32(dbdoc.KdfParamParallelism, info.KDFParallelism)
	kdf.Dict.SetUint32(dbdoc.KdfParamVersion, 0x13)
	// The KDF salt is not exposed by Inspect on purpose (it is only needed when
	// decrypting), and a save always generates a fresh one.
	settings.KDF = kdf
	return settings
}

// ensureKDFSalt returns KDF parameters that are safe to write: parameters read back from
// the header of an existing database do not carry the salt of that file, so a fresh
// random one is generated here.
func ensureKDFSalt(params *dbdoc.KDFParams) *dbdoc.KDFParams {
	if params == nil {
		return gkdb.DefaultKDFParams()
	}
	if len(params.Dict.GetBytes(dbdoc.KdfParamSeed)) >= 32 {
		return params
	}
	clone := dbdoc.NewKDFParams(params.UUID())
	for _, key := range params.Dict.Keys() {
		switch key {
		case dbdoc.KdfParamUUID, dbdoc.KdfParamSeed:
			continue
		case dbdoc.KdfParamRounds, dbdoc.KdfParamMemory, dbdoc.KdfParamIterations:
			clone.Dict.SetUint64(key, params.Dict.GetUint64(key, 0))
		case dbdoc.KdfParamParallelism, dbdoc.KdfParamVersion:
			clone.Dict.SetUint32(key, params.Dict.GetUint32(key, 0))
		case dbdoc.KdfParamSecretKey, dbdoc.KdfParamAssocData:
			clone.Dict.SetBytes(key, params.Dict.GetBytes(key))
		}
	}
	clone.Dict.SetBytes(dbdoc.KdfParamSeed, randomBytes(32))
	if clone.UUID() == dbdoc.KDFAES && clone.Dict.GetUint64(dbdoc.KdfParamRounds, 0) == 0 {
		clone.Dict.SetUint64(dbdoc.KdfParamRounds, 60000)
	}
	return clone
}

// now returns the current time truncated to whole seconds, the resolution the document
// model stores timestamps with.
func now() time.Time { return time.Now().UTC().Truncate(time.Second) }
