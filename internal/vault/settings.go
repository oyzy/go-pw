package vault

import (
	"crypto/rand"

	"gokeep/internal/kdbx"
)

// DatabaseSettings is the editable part of the database metadata shown in the database
// settings dialog.
type DatabaseSettings struct {
	// Format selects the file format: "kdbx" (KeePass compatible) or "gokeep" (native).
	Format                 string `json:"format"`
	Name                   string `json:"name"`
	Description            string `json:"description"`
	DefaultUserName        string `json:"defaultUserName"`
	Cipher                 string `json:"cipher"`
	Compression            string `json:"compression"`
	FileVersion            uint32 `json:"fileVersion"`
	KDFName                string `json:"kdfName"`
	KDFRounds              uint64 `json:"kdfRounds"`
	KDFMemory              uint64 `json:"kdfMemory"`
	KDFIterations          uint64 `json:"kdfIterations"`
	KDFParallelism         uint32 `json:"kdfParallelism"`
	RecycleBinEnabled      bool   `json:"recycleBinEnabled"`
	HistoryMaxItems        int    `json:"historyMaxItems"`
	HistoryMaxSize         int64  `json:"historyMaxSize"`
	MaintenanceHistoryDays uint32 `json:"maintenanceHistoryDays"`
	ProtectTitle           bool   `json:"protectTitle"`
	ProtectUserName        bool   `json:"protectUserName"`
	ProtectPassword        bool   `json:"protectPassword"`
	ProtectURL             bool   `json:"protectUrl"`
	ProtectNotes           bool   `json:"protectNotes"`
}

// DatabaseSettings returns the current settings.
func (v *Vault) DatabaseSettings() (DatabaseSettings, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return DatabaseSettings{}, ErrLocked
	}
	db := v.db
	settings := DatabaseSettings{
		Name:                   db.Meta.DatabaseName,
		Description:            db.Meta.DatabaseDescription,
		DefaultUserName:        db.Meta.DefaultUserName,
		Compression:            "GZip",
		FileVersion:            v.settings.Version,
		RecycleBinEnabled:      db.Meta.RecycleBinEnabled,
		HistoryMaxItems:        db.Meta.HistoryMaxItems,
		HistoryMaxSize:         db.Meta.HistoryMaxSize,
		MaintenanceHistoryDays: db.Meta.MaintenanceHistoryDays,
		ProtectTitle:           db.Meta.MemoryProtection.ProtectTitle,
		ProtectUserName:        db.Meta.MemoryProtection.ProtectUserName,
		ProtectPassword:        db.Meta.MemoryProtection.ProtectPassword,
		ProtectURL:             db.Meta.MemoryProtection.ProtectURL,
		ProtectNotes:           db.Meta.MemoryProtection.ProtectNotes,
		Cipher:                 cipherName(v.settings.Cipher),
	}
	if v.settings.Compression == kdbx.CompressionNone {
		settings.Compression = "None"
	}
	if v.settings.KDF != nil {
		settings.KDFName = v.settings.KDF.Name()
		settings.KDFRounds = v.settings.KDF.Dict.GetUint64(kdbx.KdfParamRounds, 0)
		settings.KDFMemory = v.settings.KDF.Dict.GetUint64(kdbx.KdfParamMemory, 0)
		settings.KDFIterations = v.settings.KDF.Dict.GetUint64(kdbx.KdfParamIterations, 0)
		settings.KDFParallelism = v.settings.KDF.Dict.GetUint32(kdbx.KdfParamParallelism, 0)
	}
	return settings, nil
}

// ApplyDatabaseSettings stores the settings dialog back into the database and into the
// save configuration. A new KDF salt is generated whenever the parameters change, which
// is what KeePass does as well.
func (v *Vault) ApplyDatabaseSettings(s DatabaseSettings) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	db := v.db
	db.Meta.DatabaseName = s.Name
	db.Meta.DatabaseNameChanged = now()
	db.Meta.DatabaseDescription = s.Description
	db.Meta.DatabaseDescriptionChanged = now()
	db.Meta.DefaultUserName = s.DefaultUserName
	db.Meta.DefaultUserNameChanged = now()
	db.Meta.HistoryMaxItems = s.HistoryMaxItems
	db.Meta.HistoryMaxSize = s.HistoryMaxSize
	db.Meta.MaintenanceHistoryDays = s.MaintenanceHistoryDays
	db.Meta.MemoryProtection = kdbx.MemoryProtection{
		ProtectTitle:    s.ProtectTitle,
		ProtectUserName: s.ProtectUserName,
		ProtectPassword: s.ProtectPassword,
		ProtectURL:      s.ProtectURL,
		ProtectNotes:    s.ProtectNotes,
	}
	db.Meta.SettingsChanged = now()
	if s.RecycleBinEnabled {
		v.enableRecycleBinLocked()
	}
	db.Meta.RecycleBinEnabled = s.RecycleBinEnabled
	setRecycleBinDisabled(db, !s.RecycleBinEnabled)
	v.applyFormatSettings(s)
	v.touch()
	return nil
}

// applyFormatSettings stores the cipher, compression, file version and key derivation
// function used for the next save. The caller must hold the lock.
func (v *Vault) applyFormatSettings(s DatabaseSettings) {
	if s.Format == FormatGoKeep || s.Format == FormatKDBX {
		v.settings.Format = s.Format
	}
	if s.FileVersion != 0 {
		v.settings.Version = s.FileVersion
	}
	switch s.Cipher {
	case "ChaCha20", "ChaCha20-Poly1305":
		v.settings.Cipher = kdbx.CipherChaCha20
	default:
		v.settings.Cipher = kdbx.CipherAES256
	}
	if s.Compression == "None" {
		v.settings.Compression = kdbx.CompressionNone
	} else {
		v.settings.Compression = kdbx.CompressionGzip
	}
	switch s.KDFName {
	case "Argon2id":
		p := newArgon2Params(kdbx.KDFArgon2id, s)
		v.settings.KDF = p
	case "AES-KDF":
		p := kdbx.NewKDFParams(kdbx.KDFAES)
		p.Dict.SetBytes(kdbx.KdfParamSeed, randomBytes(32))
		p.Dict.SetUint64(kdbx.KdfParamRounds, maxU64(s.KDFRounds, 60000))
		v.settings.KDF = p
	case "Argon2d":
		v.settings.KDF = newArgon2Params(kdbx.KDFArgon2d, s)
	}
	if v.settings.KDF == nil {
		v.settings.KDF = kdbx.DefaultKDFParams()
	}
	// KDBX 3.1 predates ChaCha20 and Argon2, so those choices are downgraded.
	if v.settings.Version < kdbx.FileVersion4 {
		v.settings.Cipher = kdbx.CipherAES256
		if v.settings.KDF.UUID() != kdbx.KDFAES {
			p := kdbx.NewKDFParams(kdbx.KDFAES)
			p.Dict.SetBytes(kdbx.KdfParamSeed, randomBytes(32))
			p.Dict.SetUint64(kdbx.KdfParamRounds, maxU64(s.KDFRounds, 60000))
			v.settings.KDF = p
		}
	}
}

func newArgon2Params(uuid kdbx.UUID, s DatabaseSettings) *kdbx.KDFParams {
	p := kdbx.NewKDFParams(uuid)
	p.Dict.SetBytes(kdbx.KdfParamSeed, randomBytes(32))
	p.Dict.SetUint32(kdbx.KdfParamParallelism, maxU32(s.KDFParallelism, 1))
	p.Dict.SetUint64(kdbx.KdfParamMemory, maxU64(s.KDFMemory, 8*1024*1024))
	p.Dict.SetUint64(kdbx.KdfParamIterations, maxU64(s.KDFIterations, 1))
	p.Dict.SetUint32(kdbx.KdfParamVersion, 0x13)
	return p
}

// randomBytes returns n cryptographically random bytes.
func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("vault: cannot read random bytes: " + err.Error())
	}
	return b
}

// ApplyCreateOptions applies the settings of the "new database" dialog. It deliberately
// does not create any group: a fresh database only contains the invisible root group,
// and the recycle bin is created the first time something is deleted.
func (v *Vault) ApplyCreateOptions(s DatabaseSettings) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	db := v.db
	if s.Name != "" {
		db.Meta.DatabaseName = s.Name
	}
	db.Meta.DatabaseNameChanged = now()
	db.Meta.DatabaseDescription = s.Description
	db.Meta.DatabaseDescriptionChanged = now()
	db.Meta.DefaultUserName = s.DefaultUserName
	db.Meta.DefaultUserNameChanged = now()
	if s.HistoryMaxItems > 0 {
		db.Meta.HistoryMaxItems = s.HistoryMaxItems
	}
	if s.HistoryMaxSize > 0 {
		db.Meta.HistoryMaxSize = s.HistoryMaxSize
	}
	if s.MaintenanceHistoryDays > 0 {
		db.Meta.MaintenanceHistoryDays = s.MaintenanceHistoryDays
	}
	protection := db.Meta.MemoryProtection
	if s.ProtectTitle || s.ProtectUserName || s.ProtectPassword || s.ProtectURL || s.ProtectNotes {
		protection = kdbx.MemoryProtection{
			ProtectTitle:    s.ProtectTitle,
			ProtectUserName: s.ProtectUserName,
			ProtectPassword: s.ProtectPassword,
			ProtectURL:      s.ProtectURL,
			ProtectNotes:    s.ProtectNotes,
		}
	}
	db.Meta.MemoryProtection = protection
	db.Meta.RecycleBinEnabled = true
	db.Meta.SettingsChanged = now()
	v.applyFormatSettings(s)
	v.touch()
	return nil
}

// recycleBinDisabledKey lives in the database's CustomData area. It records that the user
// switched the recycle bin off on purpose, which keeps the migration for old databases
// (see migrateRecycleBin) from overriding that choice.
const recycleBinDisabledKey = "gokeep.recycleBinDisabled"

// recycleBinDisabledValue is the marker value written by the current version. Builds from
// before this value existed wrote "true" for every database they saved, including databases
// where the user never touched the recycle bin option; those deletions were permanent and
// no recycle bin ever appeared, so only the current value counts as an explicit choice.
const recycleBinDisabledValue = "2"

// recycleBinMarker returns the raw marker and whether it is present at all.
func recycleBinMarker(db *kdbx.Database) (string, bool) {
	return kdbx.CustomDataValue(db.Meta.CustomData, recycleBinDisabledKey)
}

// recycleBinDisabled reports whether the user explicitly disabled the recycle bin.
func recycleBinDisabled(db *kdbx.Database) bool {
	value, ok := recycleBinMarker(db)
	return ok && value == recycleBinDisabledValue
}

// setRecycleBinDisabled stores the explicit choice.
func setRecycleBinDisabled(db *kdbx.Database, disabled bool) {
	if disabled {
		db.Meta.CustomData = kdbx.SetCustomDataValue(db.Meta.CustomData, recycleBinDisabledKey, recycleBinDisabledValue)
		return
	}
	db.Meta.CustomData, _ = kdbx.RemoveCustomData(db.Meta.CustomData, recycleBinDisabledKey)
}
