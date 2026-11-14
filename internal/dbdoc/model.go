// Package dbdoc is the document model of a GoKeep database, in the shape KeePass 2 uses:
// a tree of groups, the entries with their fields, history and attachments, the metadata
// and the recycle bin.
//
// The package is deliberately free of any UI, file and container concerns: internal/gkdb
// seals a document into the .gkdb container, and internal/vault is the session on top.
package dbdoc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// UUID is a 16 byte identifier used for groups, entries, icons and ciphers.
type UUID [16]byte

// ZeroUUID is the "no object" UUID, written by KeePass as 16 zero bytes.
var ZeroUUID UUID

// NewUUID returns a random UUID.
func NewUUID() UUID {
	var u UUID
	if _, err := rand.Read(u[:]); err != nil {
		panic("gokeep: cannot read random bytes: " + err.Error())
	}
	return u
}

// UUIDFromBytes builds a UUID from raw bytes, which must be exactly 16 bytes long.
func UUIDFromBytes(b []byte) (UUID, error) {
	var u UUID
	if len(b) != 16 {
		return u, fmt.Errorf("gokeep: uuid must be 16 bytes, got %d", len(b))
	}
	copy(u[:], b)
	return u, nil
}

// IsZero reports whether the UUID is the nil UUID.
func (u UUID) IsZero() bool { return u == ZeroUUID }

// Base64 returns the canonical KeePass representation: standard base64 of the raw bytes.
func (u UUID) Base64() string { return base64.StdEncoding.EncodeToString(u[:]) }

// Hex returns the lower case hex representation, used by the JSON API.
func (u UUID) Hex() string { return hex.EncodeToString(u[:]) }

// MarshalText lets encoding/json store UUIDs as base64 (GoKeep's native format keeps the
// document as JSON, where a base64 string is far friendlier than an array of numbers).
func (u UUID) MarshalText() ([]byte, error) { return []byte(u.Base64()), nil }

// UnmarshalText parses the base64, hex or GUID representation.
func (u *UUID) UnmarshalText(text []byte) error {
	parsed, err := ParseUUID(string(text))
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}

// String implements fmt.Stringer.
func (u UUID) String() string { return u.Hex() }

// ParseUUID accepts either the base64 form used inside KDBX files, a 32 character
// hex string, or a dashed GUID.
func ParseUUID(s string) (UUID, error) {
	var u UUID
	s = strings.TrimSpace(s)
	if s == "" {
		return u, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 16 {
		copy(u[:], b)
		return u, nil
	}
	cleaned := strings.ReplaceAll(s, "-", "")
	if len(cleaned) == 32 {
		if b, err := hex.DecodeString(cleaned); err == nil {
			copy(u[:], b)
			return u, nil
		}
	}
	return u, fmt.Errorf("gokeep: invalid uuid %q", s)
}

// MustParseUUID panics when s is not a valid UUID. It is meant for constants.
func MustParseUUID(s string) UUID {
	u, err := ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

// Equal reports whether two UUIDs are identical.
func (u UUID) Equal(v UUID) bool { return u == v }

// --- time -------------------------------------------------------------------

// ParseTime parses a timestamp the way a user or a CSV file writes it: RFC 3339, with or
// without fractional seconds and with or without a "T" between date and time.
func ParseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// Times holds the four timestamps plus counters that every object carries.
type Times struct {
	CreationTime     time.Time
	LastModification time.Time
	LastAccessTime   time.Time
	ExpiryTime       time.Time
	LocationChanged  time.Time
	Expires          bool
	UsageCount       uint64
}

// Touch sets the modification time to now.
func (t *Times) Touch() { t.LastModification = time.Now().UTC().Truncate(time.Second) }

// NewTimes returns timestamps initialised to now, matching KeePass behaviour for new objects.
func NewTimes() Times {
	now := time.Now().UTC().Truncate(time.Second)
	return Times{
		CreationTime:     now,
		LastModification: now,
		LastAccessTime:   now,
		LocationChanged:  now,
	}
}

// --- strings, binaries, custom data ------------------------------------------

// StringField is a single key/value pair of an entry. The standard KeePass keys are
// Title, UserName, Password, URL and Notes; any other key is a custom field.
type StringField struct {
	Key       string
	Value     string
	Protected bool
}

// Standard field names.
const (
	FieldTitle    = "Title"
	FieldUserName = "UserName"
	FieldPassword = "Password"
	FieldURL      = "URL"
	FieldNotes    = "Notes"
)

// BinaryRef attaches a binary from the database pool to an entry.
type BinaryRef struct {
	Key  string
	Pool int
}

// Binary is one blob in the database wide binary pool. KDBX 4 stores the pool in the
// encrypted inner header, KDBX 3.1 stores it in the XML <Meta><Binaries> list.
type Binary struct {
	Data      []byte
	Protected bool
}

// Hash returns the SHA-256 of the binary payload, used for de-duplication.
func (b Binary) Hash() [32]byte { return sha256.Sum256(b.Data) }

// CustomDataItem is a <Item><Key/><Value/></Item> pair of the CustomData plugin areas.
type CustomDataItem struct {
	Key   string
	Value string
}

// CustomDataValue returns the value of a CustomData entry.
func CustomDataValue(items []CustomDataItem, key string) (string, bool) {
	for _, item := range items {
		if item.Key == key {
			return item.Value, true
		}
	}
	return "", false
}

// SetCustomDataValue stores a CustomData entry, keeping the order of the existing ones.
func SetCustomDataValue(items []CustomDataItem, key, value string) []CustomDataItem {
	for i := range items {
		if items[i].Key == key {
			items[i].Value = value
			return items
		}
	}
	return append(items, CustomDataItem{Key: key, Value: value})
}

// RemoveCustomData removes a CustomData entry and reports whether it existed.
func RemoveCustomData(items []CustomDataItem, key string) ([]CustomDataItem, bool) {
	for i := range items {
		if items[i].Key == key {
			return append(items[:i], items[i+1:]...), true
		}
	}
	return items, false
}

// AutoTypeAssociation maps a window title pattern to a keystroke sequence.
type AutoTypeAssociation struct {
	WindowName string
	Sequence   string
}

// AutoType is the per-entry auto-type configuration.
type AutoType struct {
	Enabled                 bool
	DataTransferObfuscation int
	DefaultSequence         string
	Associations            []AutoTypeAssociation
}

// NewAutoType returns the default configuration KeePass applies to new entries.
func NewAutoType() AutoType {
	return AutoType{Enabled: true, DataTransferObfuscation: 0}
}

// MemoryProtection mirrors the <MemoryProtection> settings block.
type MemoryProtection struct {
	ProtectTitle    bool
	ProtectUserName bool
	ProtectPassword bool
	ProtectURL      bool
	ProtectNotes    bool
}

// DefaultMemoryProtection matches KeePass defaults: only the password is protected.
func DefaultMemoryProtection() MemoryProtection {
	return MemoryProtection{ProtectPassword: true}
}

// CustomIcon is a user supplied icon stored inside the database.
type CustomIcon struct {
	UUID                 UUID
	Data                 []byte
	Name                 string
	LastModificationTime time.Time
}

// Entry is a single password record.
type Entry struct {
	UUID                UUID
	IconID              int
	CustomIconUUID      UUID
	ForegroundColor     string
	BackgroundColor     string
	OverrideURL         string
	Tags                []string
	PreviousParentGroup UUID
	QualityCheck        *bool
	Times               Times
	Strings             []StringField
	Binaries            []BinaryRef
	AutoType            AutoType
	History             []*Entry
	CustomData          []CustomDataItem
}

// NewEntry creates an entry with KeePass defaults and a fresh UUID.
func NewEntry() *Entry {
	return &Entry{
		UUID:     NewUUID(),
		Times:    NewTimes(),
		AutoType: NewAutoType(),
		Strings: []StringField{
			{Key: FieldTitle},
			{Key: FieldUserName},
			{Key: FieldPassword, Protected: true},
			{Key: FieldURL},
			{Key: FieldNotes},
		},
	}
}

// Field returns the value of a string field and whether it exists.
func (e *Entry) Field(key string) (string, bool) {
	for i := range e.Strings {
		if e.Strings[i].Key == key {
			return e.Strings[i].Value, true
		}
	}
	return "", false
}

// IsFieldProtected reports whether the field is stored using memory protection.
func (e *Entry) IsFieldProtected(key string) bool {
	for i := range e.Strings {
		if e.Strings[i].Key == key {
			return e.Strings[i].Protected
		}
	}
	return false
}

// SetField sets a field value, creating the field when needed. The protected flag is
// only applied when the field is created.
func (e *Entry) SetField(key, value string, protected bool) {
	for i := range e.Strings {
		if e.Strings[i].Key == key {
			e.Strings[i].Value = value
			e.Strings[i].Protected = protected
			return
		}
	}
	e.Strings = append(e.Strings, StringField{Key: key, Value: value, Protected: protected})
}

// SetProtected toggles memory protection of an existing field.
func (e *Entry) SetProtected(key string, protected bool) {
	for i := range e.Strings {
		if e.Strings[i].Key == key {
			e.Strings[i].Protected = protected
			return
		}
	}
}

// RemoveField deletes a field, but never the standard fields.
func (e *Entry) RemoveField(key string) bool {
	if isStandardField(key) {
		return false
	}
	for i := range e.Strings {
		if e.Strings[i].Key == key {
			e.Strings = append(e.Strings[:i], e.Strings[i+1:]...)
			return true
		}
	}
	return false
}

// CustomFields returns every non standard field in order.
func (e *Entry) CustomFields() []StringField {
	var out []StringField
	for _, f := range e.Strings {
		if !isStandardField(f.Key) {
			out = append(out, f)
		}
	}
	return out
}

func isStandardField(key string) bool {
	switch key {
	case FieldTitle, FieldUserName, FieldPassword, FieldURL, FieldNotes:
		return true
	}
	return false
}

// HasTag reports whether the entry carries a tag.
func (e *Entry) HasTag(tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// AddTag adds a tag when it is not present yet.
func (e *Entry) AddTag(tag string) {
	if tag == "" || e.HasTag(tag) {
		return
	}
	e.Tags = append(e.Tags, tag)
}

// IsExpired reports whether the entry has an expiry date in the past.
func (e *Entry) IsExpired(now time.Time) bool {
	return e.Times.Expires && !e.Times.ExpiryTime.IsZero() && e.Times.ExpiryTime.Before(now)
}

// Clone returns a deep copy of the entry. History entries share nothing with the original.
func (e *Entry) Clone() *Entry {
	if e == nil {
		return nil
	}
	c := *e
	c.Strings = append([]StringField(nil), e.Strings...)
	c.Binaries = append([]BinaryRef(nil), e.Binaries...)
	c.Tags = append([]string(nil), e.Tags...)
	c.CustomData = append([]CustomDataItem(nil), e.CustomData...)
	c.AutoType = e.AutoType.Clone()
	if e.QualityCheck != nil {
		v := *e.QualityCheck
		c.QualityCheck = &v
	}
	c.History = nil
	for _, h := range e.History {
		c.History = append(c.History, h.Clone())
	}
	return &c
}

// CloneWithoutHistory returns a deep copy that keeps no history, which is what KeePass
// stores for each backup revision.
func (e *Entry) CloneWithoutHistory() *Entry {
	c := e.Clone()
	c.History = nil
	return c
}

// Clone deep copies an auto-type configuration.
func (a AutoType) Clone() AutoType {
	c := a
	c.Associations = append([]AutoTypeAssociation(nil), a.Associations...)
	return c
}

// Group is a folder that holds entries and/or subgroups.
type Group struct {
	UUID                    UUID
	Name                    string
	Notes                   string
	IconID                  int
	CustomIconUUID          UUID
	Times                   Times
	IsExpanded              bool
	DefaultAutoTypeSequence string
	EnableAutoType          *bool
	EnableSearching         *bool
	LastTopVisibleEntry     UUID
	PreviousParentGroup     UUID
	Tags                    []string
	CustomData              []CustomDataItem
	Groups                  []*Group
	Entries                 []*Entry
}

// NewGroup creates an empty group with KeePass defaults.
func NewGroup(name string) *Group {
	return &Group{
		UUID:       NewUUID(),
		Name:       name,
		IconID:     IconFolder,
		Times:      NewTimes(),
		IsExpanded: true,
	}
}

// Clone returns a deep copy of the group, including all children.
func (g *Group) Clone() *Group {
	if g == nil {
		return nil
	}
	c := *g
	c.Tags = append([]string(nil), g.Tags...)
	c.CustomData = append([]CustomDataItem(nil), g.CustomData...)
	if g.EnableAutoType != nil {
		v := *g.EnableAutoType
		c.EnableAutoType = &v
	}
	if g.EnableSearching != nil {
		v := *g.EnableSearching
		c.EnableSearching = &v
	}
	c.Groups = nil
	c.Entries = nil
	for _, sub := range g.Groups {
		c.Groups = append(c.Groups, sub.Clone())
	}
	for _, e := range g.Entries {
		c.Entries = append(c.Entries, e.Clone())
	}
	return &c
}

// CloneStructure returns a copy of the group without entries, as used when creating a
// new empty group from an existing one.
func (g *Group) CloneStructure() *Group {
	c := g.Clone()
	c.Entries = nil
	return c
}

// ChildGroups returns the direct subgroups.
func (g *Group) ChildGroups() []*Group { return g.Groups }

// Walk visits all groups and entries below g (including g itself).
func (g *Group) Walk(fn func(*Group), entries func(*Entry)) {
	fn(g)
	for _, sub := range g.Groups {
		sub.Walk(fn, entries)
	}
	for _, e := range g.Entries {
		entries(e)
	}
}

// FindGroupByName searches the subtree for a group with the given name.
func (g *Group) FindGroupByName(name string) *Group {
	if g.Name == name {
		return g
	}
	for _, sub := range g.Groups {
		if found := sub.FindGroupByName(name); found != nil {
			return found
		}
	}
	return nil
}

// FindGroup locates a group by UUID.
func (g *Group) FindGroup(u UUID) *Group {
	if g.UUID == u {
		return g
	}
	for _, sub := range g.Groups {
		if found := sub.FindGroup(u); found != nil {
			return found
		}
	}
	return nil
}

// FindEntry locates an entry by UUID.
func (g *Group) FindEntry(u UUID) *Entry {
	for _, e := range g.Entries {
		if e.UUID == u {
			return e
		}
	}
	for _, sub := range g.Groups {
		if found := sub.FindEntry(u); found != nil {
			return found
		}
	}
	return nil
}

// Meta holds the database header metadata mirrored in the XML <Meta> element.
type Meta struct {
	Generator                  string
	HeaderHash                 []byte
	SettingsChanged            time.Time
	DatabaseName               string
	DatabaseNameChanged        time.Time
	DatabaseDescription        string
	DatabaseDescriptionChanged time.Time
	DefaultUserName            string
	DefaultUserNameChanged     time.Time
	MaintenanceHistoryDays     uint32
	Color                      string
	MasterKeyChanged           time.Time
	MasterKeyChangeRec         int64
	MasterKeyChangeForce       int64
	MemoryProtection           MemoryProtection
	CustomIcons                []CustomIcon
	RecycleBinEnabled          bool
	RecycleBinUUID             UUID
	RecycleBinChanged          time.Time
	EntryTemplatesGroup        UUID
	EntryTemplatesGroupChanged time.Time
	LastSelectedGroup          UUID
	LastTopVisibleGroup        UUID
	HistoryMaxItems            int
	HistoryMaxSize             int64
	CustomData                 []CustomDataItem
}

// NewMeta returns metadata with the same defaults KeePass uses for a new database.
func NewMeta(name string) Meta {
	now := time.Now().UTC().Truncate(time.Second)
	return Meta{
		Generator:                  "GoKeep",
		SettingsChanged:            now,
		DatabaseName:               name,
		DatabaseNameChanged:        now,
		DatabaseDescription:        "",
		DatabaseDescriptionChanged: now,
		DefaultUserName:            "",
		DefaultUserNameChanged:     now,
		MaintenanceHistoryDays:     365,
		MasterKeyChanged:           now,
		MasterKeyChangeRec:         -1,
		MasterKeyChangeForce:       -1,
		MemoryProtection:           DefaultMemoryProtection(),
		RecycleBinEnabled:          true,
		RecycleBinChanged:          now,
		EntryTemplatesGroupChanged: now,
		HistoryMaxItems:            10,
		HistoryMaxSize:             6 * 1024 * 1024,
	}
}

// DeletedObject records an object that was removed, so that other clients can
// replicate the deletion.
type DeletedObject struct {
	UUID         UUID
	DeletionTime time.Time
}

// Database is a complete KeePass database.
type Database struct {
	Meta           Meta
	Root           *Group
	Binaries       []Binary
	DeletedObjects []DeletedObject
}

// New creates an empty database with just the invisible root group. The recycle bin is
// enabled as a policy but its group is only created the first time something is deleted,
// so a brand new database does not show any predefined group.
func New(name string) *Database {
	root := NewGroup("Root")
	root.IconID = IconFolderOpen
	db := &Database{
		Meta: NewMeta(name),
		Root: root,
	}
	db.Meta.RecycleBinEnabled = true
	db.Meta.RecycleBinUUID = ZeroUUID
	db.Meta.RecycleBinChanged = time.Now().UTC().Truncate(time.Second)
	return db
}

// RecycleBin returns the recycle bin group, or nil when it does not exist.
func (db *Database) RecycleBin() *Group {
	if db.Meta.RecycleBinUUID.IsZero() {
		return nil
	}
	return db.Root.FindGroup(db.Meta.RecycleBinUUID)
}

// RootGroup returns the top level group, creating it when a database was built by hand.
func (db *Database) RootGroup() *Group {
	if db.Root == nil {
		db.Root = NewGroup("Root")
	}
	return db.Root
}

// The recycle bin is identified by its UUID, not by its name, so the name is purely a
// display label. KeePass uses the localised name of its user interface, which is what
// this default mirrors.
const (
	// RecycleBinName is the name given to a newly created recycle bin group.
	RecycleBinName = "回收站"
	// LegacyRecycleBinName is the English default used by earlier versions and by
	// KeePass' English user interface.
	LegacyRecycleBinName = "Recycle Bin"
)

// EnableRecycleBin creates the recycle bin on demand and returns it.
func (db *Database) EnableRecycleBin() *Group {
	if bin := db.RecycleBin(); bin != nil {
		return bin
	}
	bin := NewGroup(RecycleBinName)
	bin.IconID = IconTrash
	db.Root.Groups = append(db.Root.Groups, bin)
	db.Meta.RecycleBinEnabled = true
	db.Meta.RecycleBinUUID = bin.UUID
	db.Meta.RecycleBinChanged = time.Now().UTC().Truncate(time.Second)
	return bin
}

// FindEntry locates an entry anywhere in the database.
func (db *Database) FindEntry(u UUID) *Entry { return db.Root.FindEntry(u) }

// FindGroup locates a group anywhere in the database.
func (db *Database) FindGroup(u UUID) *Group { return db.Root.FindGroup(u) }

// ParentGroup returns the group that directly contains the given group or entry.
func (db *Database) ParentGroup(u UUID) *Group {
	var found *Group
	db.Root.Walk(func(g *Group) {
		for _, sub := range g.Groups {
			if sub.UUID == u {
				found = g
			}
		}
		for _, e := range g.Entries {
			if e.UUID == u {
				found = g
			}
		}
	}, func(*Entry) {})
	return found
}

// Entries returns every entry of the database, the recycle bin included.
func (db *Database) Entries() []*Entry {
	var out []*Entry
	db.Root.Walk(func(*Group) {}, func(e *Entry) { out = append(out, e) })
	return out
}

// Groups returns every group of the database.
func (db *Database) Groups() []*Group {
	var out []*Group
	db.Root.Walk(func(g *Group) { out = append(out, g) }, func(*Entry) {})
	return out
}

// AddBinary inserts a blob into the pool, reusing an identical existing entry.
func (db *Database) AddBinary(data []byte, protected bool) int {
	sum := sha256.Sum256(data)
	for i, b := range db.Binaries {
		if b.Hash() == sum {
			return i
		}
	}
	db.Binaries = append(db.Binaries, Binary{Data: append([]byte(nil), data...), Protected: protected})
	return len(db.Binaries) - 1
}

// Binary returns the payload of a pool entry.
func (db *Database) Binary(index int) ([]byte, bool) {
	if index < 0 || index >= len(db.Binaries) {
		return nil, false
	}
	return db.Binaries[index].Data, true
}

// CompactBinaries removes pool entries that are no longer referenced by any entry,
// including history entries, and rewrites the references. The order of the remaining
// blobs is preserved, exactly like KeePass does when saving.
func (db *Database) CompactBinaries() {
	used := map[int]int{}
	var order []int
	db.Root.Walk(func(*Group) {}, func(e *Entry) {
		for _, ref := range allBinaryRefs(e) {
			if _, ok := used[ref.Pool]; !ok {
				used[ref.Pool] = -1
				order = append(order, ref.Pool)
			}
		}
	})
	remap := make(map[int]int, len(order))
	var pool []Binary
	for _, old := range order {
		if old < 0 || old >= len(db.Binaries) {
			continue
		}
		remap[old] = len(pool)
		pool = append(pool, db.Binaries[old])
	}
	db.Root.Walk(func(*Group) {}, func(e *Entry) {
		forEachEntryTree(e, func(x *Entry) {
			for i := range x.Binaries {
				if n, ok := remap[x.Binaries[i].Pool]; ok {
					x.Binaries[i].Pool = n
				} else {
					x.Binaries[i].Pool = -1
				}
			}
		})
	})
	db.Binaries = pool
}

func allBinaryRefs(e *Entry) []BinaryRef {
	var out []BinaryRef
	forEachEntryTree(e, func(x *Entry) { out = append(out, x.Binaries...) })
	return out
}

// forEachEntryTree calls fn for the entry and every history revision.
func forEachEntryTree(e *Entry, fn func(*Entry)) {
	fn(e)
	for _, h := range e.History {
		forEachEntryTree(h, fn)
	}
}
