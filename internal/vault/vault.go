package vault

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gokeep/internal/gkdb"
	"gokeep/internal/kdbx"
)

// Vault is one open database plus everything the user interface needs to know about it.
// All methods are safe for concurrent use.
type Vault struct {
	mu             sync.Mutex
	path           string
	db             *kdbx.Database
	key            kdbx.Key
	dirty          bool
	openedAt       time.Time
	lastSaved      time.Time
	readOnly       bool
	settings       SaveSettings
	recycleBinName string
}

// SaveSettings remembers the on-disk format chosen by the user.
type SaveSettings struct {
	// Format is "kdbx" (KeePass compatible) or "gokeep" (the native format).
	Format      string
	Version     uint32
	Cipher      kdbx.UUID
	Compression uint32
	KDF         *kdbx.KDFParams
}

// Database file formats.
const (
	FormatKDBX   = "kdbx"
	FormatGoKeep = "gokeep"
)

// DefaultSaveSettings matches what KeePass 2.60 writes for a new database.
func DefaultSaveSettings() SaveSettings {
	return SaveSettings{
		Format:      FormatKDBX,
		Version:     kdbx.FileVersion41,
		Cipher:      kdbx.CipherAES256,
		Compression: kdbx.CompressionGzip,
		KDF:         kdbx.DefaultKDFParams(),
	}
}

// ErrLocked is returned when an operation needs an open database.
var ErrLocked = errors.New("vault: no database is open")

// Open loads a database from disk.
func Open(path string, key kdbx.Key) (*Vault, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	db, err := loadDatabase(data, key)
	if err != nil {
		return nil, err
	}
	v := &Vault{
		path:           path,
		db:             db,
		key:            key,
		openedAt:       time.Now(),
		lastSaved:      fileModTime(path),
		settings:       settingsFromFile(path),
		recycleBinName: kdbx.RecycleBinName,
	}
	// The state of the recycle bin as it is stored in the file, logged before the migration
	// touches it. Nothing but these flags is written, so the log stays free of user data.
	marker, hasMarker := recycleBinMarker(db)
	slog.Info("database opened",
		"format", v.settings.Format,
		"recycleBinEnabled", db.Meta.RecycleBinEnabled,
		"recycleBinGroup", db.RecycleBin() != nil,
		"recycleBinMarker", marker,
		"hasMarker", hasMarker,
		"rootGroups", len(db.Root.Groups))
	v.renameLegacyRecycleBin()
	v.migrateRecycleBin()
	return v, nil
}

// migrateRecycleBin fixes databases written by early builds of this application, which
// stored the recycle bin policy as "off". Deleting an entry in such a database removed it
// for good and no recycle bin group ever appeared. A database where the user switched the
// recycle bin off in the current version carries the current marker value and is left
// alone; markers written by older builds are stale and are cleaned up here.
func (v *Vault) migrateRecycleBin() {
	if recycleBinDisabled(v.db) {
		// An explicit choice of the current version, keep it.
		return
	}
	marker, hasMarker := recycleBinMarker(v.db)
	switch {
	case v.db.Meta.RecycleBinEnabled && hasMarker:
		// Only the marker is left over from an older build.
		setRecycleBinDisabled(v.db, false)
		v.dirty = true
		slog.Info("cleaned up a stale recycle bin marker", "marker", marker)
	case !v.db.Meta.RecycleBinEnabled:
		// The database stores "recycle bin off" without a marker of the current version:
		// this is the state early builds produced for every database. Switch it back on so
		// that deleting an entry moves it to the recycle bin again, as KeePass does.
		v.db.Meta.RecycleBinEnabled = true
		if v.db.RecycleBin() == nil {
			v.db.Meta.RecycleBinChanged = time.Now().UTC().Truncate(time.Second)
		}
		setRecycleBinDisabled(v.db, false)
		v.dirty = true
		slog.Info("enabled the recycle bin of a legacy database", "legacyMarker", marker, "hadMarker", hasMarker)
	}
	v.adoptRecycleBinGroup()
}

// adoptRecycleBinGroup repairs the group reference of the recycle bin. Early builds created
// the group without recording its identifier in the metadata, which made the application
// treat the entries inside it as ordinary ones and hide the bin styling; a metadata entry
// pointing at a group that no longer exists is dropped so that a fresh bin is created.
func (v *Vault) adoptRecycleBinGroup() {
	if !v.db.Meta.RecycleBinEnabled {
		return
	}
	if !v.db.Meta.RecycleBinUUID.IsZero() {
		if v.db.RecycleBin() != nil {
			return
		}
		v.db.Meta.RecycleBinUUID = kdbx.ZeroUUID
		v.dirty = true
		slog.Info("forgot a recycle bin group that no longer exists")
	}
	for _, group := range v.db.Root.Groups {
		if v.isRecycleBinName(group.Name) {
			v.db.Meta.RecycleBinUUID = group.UUID
			v.db.Meta.RecycleBinChanged = time.Now().UTC().Truncate(time.Second)
			v.dirty = true
			slog.Info("adopted the recycle bin group of an older database")
			return
		}
	}
}

// isRecycleBinName reports whether a group name is one of the default names of the recycle
// bin, so that a group the user renamed is never touched.
func (v *Vault) isRecycleBinName(name string) bool {
	switch name {
	case kdbx.RecycleBinName, kdbx.LegacyRecycleBinName, v.recycleBinNameOrDefault():
		return true
	}
	return false
}

// loadDatabase picks the reader that matches the file signature, so the user can open
// KeePass databases and GoKeep databases with the same command.
func loadDatabase(data []byte, key kdbx.Key) (*kdbx.Database, error) {
	if gkdb.IsGoKeepFile(data) {
		return gkdb.Load(data, key)
	}
	return kdbx.Load(data, key)
}

// renameLegacyRecycleBin gives the recycle bin of an existing database the localised
// name. The group is identified by the UUID stored in the metadata, so renaming it never
// breaks compatibility with other KeePass clients.
func (v *Vault) renameLegacyRecycleBin() {
	bin := v.db.RecycleBin()
	if bin == nil || bin.Name == v.recycleBinNameOrDefault() {
		return
	}
	if bin.Name != kdbx.LegacyRecycleBinName && bin.Name != kdbx.RecycleBinName {
		// The user renamed the group on purpose, leave it alone.
		return
	}
	bin.Name = v.recycleBinNameOrDefault()
	bin.Times.Touch()
	v.dirty = true
}

// SetRecycleBinName stores the localised name of the recycle bin and renames the
// existing group when it still carries a default name.
func (v *Vault) SetRecycleBinName(name string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if name == "" {
		return
	}
	previous := v.recycleBinNameOrDefault()
	v.recycleBinName = name
	if v.db == nil {
		return
	}
	bin := v.db.RecycleBin()
	if bin == nil || bin.Name == name {
		return
	}
	if bin.Name == kdbx.LegacyRecycleBinName || bin.Name == kdbx.RecycleBinName || bin.Name == previous {
		bin.Name = name
		bin.Times.Touch()
		v.dirty = true
	}
}

func (v *Vault) recycleBinNameOrDefault() string {
	if v.recycleBinName == "" {
		return kdbx.RecycleBinName
	}
	return v.recycleBinName
}

// enableRecycleBinLocked creates the recycle bin with the localised name.
func (v *Vault) enableRecycleBinLocked() *kdbx.Group {
	if bin := v.db.RecycleBin(); bin != nil {
		return bin
	}
	bin := kdbx.NewGroup(v.recycleBinNameOrDefault())
	bin.IconID = kdbx.IconTrash
	v.db.Root.Groups = append(v.db.Root.Groups, bin)
	v.db.Meta.RecycleBinEnabled = true
	v.db.Meta.RecycleBinUUID = bin.UUID
	v.db.Meta.RecycleBinChanged = time.Now().UTC().Truncate(time.Second)
	slog.Info("created the recycle bin group", "name", bin.Name, "id", bin.UUID.Hex())
	return bin
}

// Create makes a new database and writes it to disk.
func Create(path string, key kdbx.Key, name string, settings SaveSettings) (*Vault, error) {
	db := kdbx.NewDatabase(name, "")
	v := &Vault{db: db, key: key, settings: settings}
	if err := v.SaveAs(path, key); err != nil {
		return nil, err
	}
	v.dirty = false
	return v, nil
}

// Close forgets the database.
func (v *Vault) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db != nil {
		v.zero()
	}
	v.db = nil
	v.key = kdbx.Key{}
}

// zero overwrites the key material we can reach.
func (v *Vault) zero() {
	v.key.Password = ""
	for i := range v.key.KeyFile {
		v.key.KeyFile[i] = 0
	}
	v.key.KeyFile = nil
}

// Locked reports whether the database was closed.
func (v *Vault) Locked() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.db == nil
}

// Path returns the file name of the database.
func (v *Vault) Path() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.path
}

// Modified reports whether there are unsaved changes.
func (v *Vault) Modified() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.dirty
}

// Settings returns the format settings used for the next save.
func (v *Vault) Settings() SaveSettings {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.settings
}

// SetSettings replaces the format settings used for the next save.
func (v *Vault) SetSettings(s SaveSettings) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.settings = s
}

// Key returns a copy of the credentials of the open database.
func (v *Vault) Key() kdbx.Key {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.key
}

// Save writes the database back to its own file.
func (v *Vault) Save() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.saveLocked(v.path)
}

// SaveAs writes the database to a new location and switches to it.
func (v *Vault) SaveAs(path string, key kdbx.Key) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if key.Password != "" || len(key.KeyFile) > 0 {
		v.key = key
	}
	// The extension decides the format, which is also how a database gets converted
	// between the KeePass format and the native one.
	switch strings.ToLower(filepath.Ext(path)) {
	case gkdb.Extension:
		v.settings.Format = FormatGoKeep
		v.settings.Cipher = gokeepCipher(v.settings.Cipher)
	case ".kdbx":
		v.settings.Format = FormatKDBX
		v.settings.Cipher = kdbxCipher(v.settings.Cipher)
	}
	if err := v.saveLocked(path); err != nil {
		return err
	}
	v.path = path
	return nil
}

// gokeepCipher maps a KDBX cipher identifier onto the GoKeep format (which uses an
// authenticated cipher and keeps the same "AES-256 or ChaCha20" choice).
func gokeepCipher(cipher kdbx.UUID) kdbx.UUID {
	if cipher == kdbx.CipherChaCha20 {
		return kdbx.CipherChaCha20
	}
	return kdbx.CipherAES256
}

// kdbxCipher maps the choice back for KDBX files.
func kdbxCipher(cipher kdbx.UUID) kdbx.UUID {
	if cipher == kdbx.CipherChaCha20 {
		return kdbx.CipherChaCha20
	}
	return kdbx.CipherAES256
}

func (v *Vault) saveLocked(path string) error {
	if v.db == nil {
		return ErrLocked
	}
	if v.readOnly {
		return errors.New("vault: the database is open in read-only mode")
	}
	if _, err := os.Stat(path); err == nil {
		// KeePass keeps a ".bak" copy of the previous revision.
		if err := kdbx.RotateBackups(path, 10); err != nil {
			return err
		}
		if err := kdbx.WriteBackup(path); err != nil {
			return err
		}
	}
	if v.settings.Format == FormatGoKeep {
		options := gkdb.DefaultOptions()
		if v.settings.Cipher == kdbx.CipherChaCha20 {
			options.Cipher = gkdb.CipherChaCha20Poly1305
		}
		options.Compression = gkdb.CompressionNone
		if v.settings.Compression == kdbx.CompressionGzip {
			options.Compression = gkdb.CompressionGzip
		}
		if v.settings.KDF != nil {
			options.KDF = ensureKDFSalt(v.settings.KDF)
		}
		if err := gkdb.SaveFile(path, v.db, v.key, options); err != nil {
			return err
		}
	} else {
		opts := kdbx.SaveOptions{
			Version:     v.settings.Version,
			Cipher:      v.settings.Cipher,
			Compression: &v.settings.Compression,
			KDF:         ensureKDFSalt(v.settings.KDF),
		}
		if opts.Version == 0 {
			opts.Version = kdbx.FileVersion41
		}
		if opts.KDF == nil {
			opts.KDF = kdbx.DefaultKDFParams()
		}
		if err := kdbx.SaveFile(path, v.db, v.key, opts); err != nil {
			return err
		}
	}
	v.dirty = false
	v.lastSaved = time.Now()
	return nil
}

// ChangeKey re-encrypts the database with new credentials.
func (v *Vault) ChangeKey(newKey kdbx.Key) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	oldKey := v.key
	v.key = newKey
	if err := v.saveLocked(v.path); err != nil {
		v.key = oldKey
		return err
	}
	v.db.Meta.MasterKeyChanged = time.Now().UTC().Truncate(time.Second)
	v.dirty = true
	return v.saveLocked(v.path)
}

// Database returns the underlying database. Callers must not retain it.
func (v *Vault) Database() *kdbx.Database {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.db
}

// Info summarises the database.
func (v *Vault) Info() DatabaseInfo {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return DatabaseInfo{}
	}
	db := v.db
	info := DatabaseInfo{
		Path:                   v.path,
		Name:                   db.Meta.DatabaseName,
		Description:            db.Meta.DatabaseDescription,
		DefaultUserName:        db.Meta.DefaultUserName,
		RecycleBinEnabled:      db.Meta.RecycleBinEnabled,
		RecycleBinID:           idOf(db.Meta.RecycleBinUUID),
		HistoryMaxItems:        db.Meta.HistoryMaxItems,
		HistoryMaxSize:         db.Meta.HistoryMaxSize,
		MaintenanceHistoryDays: db.Meta.MaintenanceHistoryDays,
		Modified:               v.dirty,
		ReadOnly:               v.readOnly,
		ProtectedTitle:         db.Meta.MemoryProtection.ProtectTitle,
		ProtectedUserName:      db.Meta.MemoryProtection.ProtectUserName,
		ProtectedPassword:      db.Meta.MemoryProtection.ProtectPassword,
		ProtectedURL:           db.Meta.MemoryProtection.ProtectURL,
		ProtectedNotes:         db.Meta.MemoryProtection.ProtectNotes,
		LastSaved:              formatTime(v.lastSaved),
	}
	info.EntryCount = len(db.Entries())
	// The root group is an implementation detail and is never shown, so it is not part
	// of the group count in the status bar.
	info.GroupCount = len(db.Groups()) - 1
	if info.GroupCount < 0 {
		info.GroupCount = 0
	}
	if v.settings.Format == FormatGoKeep {
		info.Format = FormatGoKeep
		info.FormatName = "GoKeep v1"
		info.VersionLabel = "GoKeep v1"
	} else {
		info.Format = FormatKDBX
		info.FormatName = versionLabel(v.settings.Version)
		info.VersionLabel = versionLabel(v.settings.Version)
	}
	info.FileVersion = v.settings.Version
	info.Cipher = cipherName(v.settings.Cipher)
	if v.settings.Format == FormatGoKeep {
		if v.settings.Cipher == kdbx.CipherChaCha20 {
			info.Cipher = "ChaCha20-Poly1305"
		} else {
			info.Cipher = "AES-256-GCM"
		}
	}
	info.CipherUUID = v.settings.Cipher.Hex()
	if v.settings.Compression == kdbx.CompressionGzip {
		info.Compression = "GZip"
	} else {
		info.Compression = "None"
	}
	if v.settings.KDF != nil {
		info.KDFName = v.settings.KDF.Name()
		info.KDFUUID = v.settings.KDF.UUID().Hex()
		info.KDFRounds = v.settings.KDF.Dict.GetUint64(kdbx.KdfParamRounds, 0)
		info.KDFMemory = v.settings.KDF.Dict.GetUint64(kdbx.KdfParamMemory, 0)
		info.KDFIterations = v.settings.KDF.Dict.GetUint64(kdbx.KdfParamIterations, 0)
		info.KDFParallelism = v.settings.KDF.Dict.GetUint32(kdbx.KdfParamParallelism, 0)
		info.KDFSeed = fmt.Sprintf("%x", v.settings.KDF.Dict.GetBytes(kdbx.KdfParamSeed))
		info.SecurityLevel = securityLevel(v.settings.KDF)
	}
	for _, icon := range db.Meta.CustomIcons {
		info.CustomIcons = append(info.CustomIcons, CustomIconDTO{
			ID:   idOf(icon.UUID),
			Name: icon.Name,
			Data: base64Encode(icon.Data),
		})
	}
	return info
}

// Tree returns the group hierarchy.
func (v *Vault) Tree() []GroupDTO {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return nil
	}
	var out []GroupDTO
	for _, g := range v.db.Root.Groups {
		out = append(out, groupToDTO(g, v.db.Root, v.db.Meta.RecycleBinUUID))
	}
	return out
}

// RootID returns the identifier of the root group.
func (v *Vault) RootID() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ""
	}
	return idOf(v.db.Root.UUID)
}

// Entries lists the entries of a group. When recursive is set, entries of all
// subgroups are included, and includeRecycled controls whether entries inside the
// recycle bin are returned.
func (v *Vault) Entries(groupID string, recursive, includeRecycled bool) ([]EntrySummary, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return nil, ErrLocked
	}
	group, err := v.findGroupLocked(groupID)
	if err != nil {
		return nil, err
	}
	return v.collectSummaries(group, recursive, includeRecycled), nil
}

// AllEntries lists every entry of the database.
func (v *Vault) AllEntries(includeRecycled bool) ([]EntrySummary, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return nil, ErrLocked
	}
	return v.collectSummaries(v.db.Root, true, includeRecycled), nil
}

func (v *Vault) collectSummaries(group *kdbx.Group, recursive, includeRecycled bool) []EntrySummary {
	var out []EntrySummary
	// Listing the recycle bin itself always shows its content, so that clicking the bin
	// in the tree lists the deleted entries.
	if isInRecycleBin(v.db, group) {
		includeRecycled = true
	}
	var walkEntries func(g *kdbx.Group)
	walkEntries = func(g *kdbx.Group) {
		if !includeRecycled && isInRecycleBin(v.db, g) {
			return
		}
		entries := append([]*kdbx.Entry(nil), g.Entries...)
		sortEntriesByTitle(entries)
		for _, e := range entries {
			out = append(out, entryToSummary(v.db, e, g))
		}
	}
	walkEntries(group)
	if recursive {
		var walkGroups func(g *kdbx.Group)
		walkGroups = func(g *kdbx.Group) {
			for _, sub := range g.Groups {
				walkEntries(sub)
				walkGroups(sub)
			}
		}
		// The recycle bin group itself must be skipped when it is excluded, but the
		// function above already handles that per group.
		walkGroups(group)
	}
	return out
}

// Entry returns the full representation of one entry.
func (v *Vault) Entry(id string) (EntryDTO, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return EntryDTO{}, ErrLocked
	}
	entry, parent, err := v.findEntryLocked(id)
	if err != nil {
		return EntryDTO{}, err
	}
	return entryToDTO(v.db, entry, parent), nil
}

// findGroupLocked resolves a group id, accepting both the hex and the base64 form.
func (v *Vault) findGroupLocked(id string) (*kdbx.Group, error) {
	if id == "" {
		return v.db.Root, nil
	}
	uuid, err := kdbx.ParseUUID(id)
	if err != nil {
		return nil, err
	}
	if uuid == v.db.Root.UUID {
		return v.db.Root, nil
	}
	g := v.db.Root.FindGroup(uuid)
	if g == nil {
		return nil, fmt.Errorf("vault: group %s does not exist", id)
	}
	return g, nil
}

func (v *Vault) findEntryLocked(id string) (*kdbx.Entry, *kdbx.Group, error) {
	uuid, err := kdbx.ParseUUID(id)
	if err != nil {
		return nil, nil, err
	}
	if e := v.db.Root.FindEntry(uuid); e != nil {
		return e, v.db.ParentGroup(uuid), nil
	}
	return nil, nil, fmt.Errorf("vault: entry %s does not exist", id)
}

// touch marks the database as modified and updates the settings timestamp.
func (v *Vault) touch() { v.dirty = true }

func fileModTime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func versionLabel(v uint32) string {
	switch v {
	case kdbx.FileVersion31:
		return "KDBX 3.1"
	case kdbx.FileVersion4:
		return "KDBX 4.0"
	default:
		return "KDBX 4.1"
	}
}

func cipherName(u kdbx.UUID) string {
	switch u {
	case kdbx.CipherAES256:
		return "AES-256"
	case kdbx.CipherChaCha20:
		return "ChaCha20"
	case kdbx.CipherTwoFish:
		return "Twofish"
	}
	return "Unknown"
}

// securityLevel gives the user a rough idea how expensive the KDF is.
func securityLevel(kdf *kdbx.KDFParams) string {
	switch kdf.Name() {
	case "Argon2d", "Argon2id":
		mem := kdf.Dict.GetUint64(kdbx.KdfParamMemory, 0)
		iter := kdf.Dict.GetUint64(kdbx.KdfParamIterations, 0)
		switch {
		case mem >= 128*1024*1024 || iter >= 6:
			return "very strong"
		case mem >= 64*1024*1024:
			return "strong"
		case mem >= 16*1024*1024:
			return "moderate"
		default:
			return "weak"
		}
	case "AES-KDF":
		rounds := kdf.Dict.GetUint64(kdbx.KdfParamRounds, 0)
		switch {
		case rounds >= 200_000_000:
			return "very strong"
		case rounds >= 20_000_000:
			return "strong"
		case rounds >= 1_000_000:
			return "moderate"
		default:
			return "weak"
		}
	}
	return "unknown"
}

// GroupPath returns the "/" separated path of a group.
func (v *Vault) GroupPath(id string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return "", ErrLocked
	}
	g, err := v.findGroupLocked(id)
	if err != nil {
		return "", err
	}
	return groupPath(v.db.Root, g), nil
}

// FileName returns the base name of the database file.
func (v *Vault) FileName() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.path == "" {
		return ""
	}
	return filepath.Base(v.path)
}

// trimTags removes empty tags and duplicates.
func trimTags(tags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
	}
	return out
}
