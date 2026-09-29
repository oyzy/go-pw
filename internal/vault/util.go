package vault

import (
	"encoding/base64"
	"os"
	"time"

	"gokeep/internal/gkdb"
	"gokeep/internal/kdbx"
)

func base64Encode(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

// settingsFromFile reads the on-disk format of an existing file so that saving keeps the
// cipher, the key derivation function and the file version the user chose earlier. All
// of it lives in the header, so nothing has to be decrypted to read it back.
func settingsFromFile(path string) SaveSettings {
	// The native format first: it has its own header with the same kind of information.
	if data, err := os.ReadFile(path); err == nil && gkdb.IsGoKeepFile(data) {
		settings := DefaultSaveSettings()
		settings.Format = FormatGoKeep
		if info, err := gkdb.Inspect(data); err == nil {
			settings.Compression = kdbx.CompressionGzip
			if info.Compression == "None" {
				settings.Compression = kdbx.CompressionNone
			}
			if info.Cipher == "ChaCha20-Poly1305" {
				settings.Cipher = kdbx.CipherChaCha20
			}
			kdf := kdbx.NewKDFParams(kdbx.KDFArgon2d)
			switch info.KDF {
			case "Argon2id":
				kdf = kdbx.NewKDFParams(kdbx.KDFArgon2id)
			case "AES-KDF":
				kdf = kdbx.NewKDFParams(kdbx.KDFAES)
			}
			kdf.Dict.SetUint64(kdbx.KdfParamRounds, info.KDFRounds)
			kdf.Dict.SetUint64(kdbx.KdfParamMemory, info.KDFMemory)
			kdf.Dict.SetUint64(kdbx.KdfParamIterations, uint64(info.KDFIterations))
			kdf.Dict.SetUint32(kdbx.KdfParamParallelism, info.KDFParallelism)
			kdf.Dict.SetUint32(kdbx.KdfParamVersion, 0x13)
			// The KDF salt is not exposed by Inspect on purpose (it is only needed when
			// decrypting), and a save always generates a fresh one.
			settings.KDF = kdf
		}
		return settings
	}
	info, err := kdbx.InspectFile(path)
	if err != nil {
		return DefaultSaveSettings()
	}
	kdf := kdbx.NewKDFParams(info.KDFUUID)
	kdf.Dict.SetBytes(kdbx.KdfParamSeed, info.KDFSalt)
	switch info.KDFName {
	case "Argon2d", "Argon2id":
		kdf.Dict.SetUint64(kdbx.KdfParamMemory, info.KDFMemory)
		kdf.Dict.SetUint64(kdbx.KdfParamIterations, info.KDFIterations)
		kdf.Dict.SetUint32(kdbx.KdfParamParallelism, info.KDFParallel)
		kdf.Dict.SetUint32(kdbx.KdfParamVersion, 0x13)
	case "AES-KDF":
		kdf.Dict.SetUint64(kdbx.KdfParamRounds, info.KDFRounds)
	}
	settings := SaveSettings{
		Version:     info.Version,
		Cipher:      info.CipherUUID,
		Compression: info.Compression,
		KDF:         kdf,
	}
	if settings.Version == 0 {
		settings.Version = kdbx.FileVersion41
	}
	if settings.Cipher.IsZero() {
		settings.Cipher = kdbx.CipherAES256
	}
	return settings
}

// now returns the current time truncated to whole seconds, like KeePass does.
func now() time.Time { return time.Now().UTC().Truncate(time.Second) }

// ensureKDFSalt returns KDF parameters that are safe to write: parameters loaded from an
// existing file may not carry a salt (the native format keeps it in the header), so a
// fresh random one is generated in that case. A new salt is also what KeePass does when
// the parameters change.
func ensureKDFSalt(params *kdbx.KDFParams) *kdbx.KDFParams {
	if params == nil {
		return kdbx.DefaultKDFParams()
	}
	if len(params.Dict.GetBytes(kdbx.KdfParamSeed)) >= 32 {
		return params
	}
	clone := kdbx.NewKDFParams(params.UUID())
	for _, key := range params.Dict.Keys() {
		switch key {
		case kdbx.KdfParamUUID, kdbx.KdfParamSeed:
			continue
		case kdbx.KdfParamRounds:
			clone.Dict.SetUint64(key, params.Dict.GetUint64(key, 0))
		case kdbx.KdfParamMemory, kdbx.KdfParamIterations:
			clone.Dict.SetUint64(key, params.Dict.GetUint64(key, 0))
		case kdbx.KdfParamParallelism, kdbx.KdfParamVersion:
			clone.Dict.SetUint32(key, params.Dict.GetUint32(key, 0))
		case kdbx.KdfParamSecretKey, kdbx.KdfParamAssocData:
			clone.Dict.SetBytes(key, params.Dict.GetBytes(key))
		}
	}
	clone.Dict.SetBytes(kdbx.KdfParamSeed, randomBytes(32))
	if clone.UUID() == kdbx.KDFAES && clone.Dict.GetUint64(kdbx.KdfParamRounds, 0) == 0 {
		clone.Dict.SetUint64(kdbx.KdfParamRounds, 60000)
	}
	return clone
}
