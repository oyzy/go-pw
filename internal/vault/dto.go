// Package vault turns the raw GoKeep document model into the session oriented operations
// the user interface needs: opening and saving files, editing groups and entries with
// history and recycle bin semantics, searching and clipboard handling.
package vault

import (
	"sort"
	"strings"
	"time"

	"gokeep/internal/dbdoc"
	"gokeep/internal/pwquality"
)

// GroupDTO is the flattened group representation used by the tree view.
type GroupDTO struct {
	ID              string     `json:"id"`
	ParentID        string     `json:"parentId"`
	Name            string     `json:"name"`
	Notes           string     `json:"notes"`
	IconID          int        `json:"iconId"`
	CustomIconID    string     `json:"customIconId"`
	Expires         bool       `json:"expires"`
	ExpiryTime      string     `json:"expiryTime"`
	Created         string     `json:"created"`
	Modified        string     `json:"modified"`
	IsExpanded      bool       `json:"isExpanded"`
	Tags            []string   `json:"tags"`
	EnableAutoType  *bool      `json:"enableAutoType"`
	EnableSearching *bool      `json:"enableSearching"`
	Children        []GroupDTO `json:"children"`
	EntryIDs        []string   `json:"entryIds"`
}

// FieldDTO is one custom (non standard) string field of an entry.
type FieldDTO struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Protected bool   `json:"protected"`
}

// AttachmentDTO describes one attachment of an entry.
type AttachmentDTO struct {
	Key       string `json:"key"`
	Size      int    `json:"size"`
	Protected bool   `json:"protected"`
}

// HistoryDTO summarises one stored revision of an entry.
type HistoryDTO struct {
	ID       string `json:"id"`
	Index    int    `json:"index"`
	Modified string `json:"modified"`
	Title    string `json:"title"`
	UserName string `json:"userName"`
}

// AutoTypeAssociationDTO maps a window pattern to a keystroke sequence.
type AutoTypeAssociationDTO struct {
	Window   string `json:"window"`
	Sequence string `json:"sequence"`
}

// EntryDTO is the full representation of an entry, as shown in the editor.
type EntryDTO struct {
	ID                string                   `json:"id"`
	GroupID           string                   `json:"groupId"`
	GroupPath         string                   `json:"groupPath"`
	Title             string                   `json:"title"`
	UserName          string                   `json:"userName"`
	Password          string                   `json:"password"`
	PasswordProtected bool                     `json:"passwordProtected"`
	URL               string                   `json:"url"`
	Notes             string                   `json:"notes"`
	IconID            int                      `json:"iconId"`
	CustomIconID      string                   `json:"customIconId"`
	ForegroundColor   string                   `json:"foregroundColor"`
	BackgroundColor   string                   `json:"backgroundColor"`
	OverrideURL       string                   `json:"overrideUrl"`
	Tags              []string                 `json:"tags"`
	Fields            []FieldDTO               `json:"fields"`
	Attachments       []AttachmentDTO          `json:"attachments"`
	History           []HistoryDTO             `json:"history"`
	AutoTypeEnabled   bool                     `json:"autoTypeEnabled"`
	AutoTypeSequence  string                   `json:"autoTypeSequence"`
	AutoTypeItems     []AutoTypeAssociationDTO `json:"autoTypeItems"`
	Expires           bool                     `json:"expires"`
	ExpiryTime        string                   `json:"expiryTime"`
	Created           string                   `json:"created"`
	Modified          string                   `json:"modified"`
	Accessed          string                   `json:"accessed"`
	UsageCount        uint64                   `json:"usageCount"`
	Quality           int                      `json:"quality"`
	QualityBits       float64                  `json:"qualityBits"`
	IsExpired         bool                     `json:"isExpired"`
	IsRecycled        bool                     `json:"isRecycled"`
}

// EntrySummary is the compact row shown in the entry list.
type EntrySummary struct {
	ID           string   `json:"id"`
	GroupID      string   `json:"groupId"`
	GroupPath    string   `json:"groupPath"`
	Title        string   `json:"title"`
	UserName     string   `json:"userName"`
	URL          string   `json:"url"`
	IconID       int      `json:"iconId"`
	CustomIconID string   `json:"customIconId"`
	Tags         []string `json:"tags"`
	Modified     string   `json:"modified"`
	Expires      bool     `json:"expires"`
	IsExpired    bool     `json:"isExpired"`
	IsRecycled   bool     `json:"isRecycled"`
	HasNotes     bool     `json:"hasNotes"`
	CustomFields int      `json:"customFields"`
	Attachments  int      `json:"attachments"`
	HistoryItems int      `json:"historyItems"`
	Quality      int      `json:"quality"`
	HasTOTP      bool     `json:"hasTotp"`
}

// CustomIconDTO carries a user supplied icon.
type CustomIconDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Data string `json:"data"` // base64 encoded PNG
}

// DatabaseInfo describes the open database for the title bar and the settings dialog.
type DatabaseInfo struct {
	Format                 string          `json:"format"`
	FormatName             string          `json:"formatName"`
	Path                   string          `json:"path"`
	Name                   string          `json:"name"`
	Description            string          `json:"description"`
	DefaultUserName        string          `json:"defaultUserName"`
	Cipher                 string          `json:"cipher"`
	CipherUUID             string          `json:"cipherUuid"`
	Compression            string          `json:"compression"`
	KDFName                string          `json:"kdfName"`
	KDFUUID                string          `json:"kdfUuid"`
	KDFRounds              uint64          `json:"kdfRounds"`
	KDFMemory              uint64          `json:"kdfMemory"`
	KDFIterations          uint64          `json:"kdfIterations"`
	KDFParallelism         uint32          `json:"kdfParallelism"`
	KDFSeed                string          `json:"kdfSeed"`
	FileVersion            uint32          `json:"fileVersion"`
	VersionLabel           string          `json:"versionLabel"`
	RecycleBinEnabled      bool            `json:"recycleBinEnabled"`
	RecycleBinID           string          `json:"recycleBinId"`
	HistoryMaxItems        int             `json:"historyMaxItems"`
	HistoryMaxSize         int64           `json:"historyMaxSize"`
	MaintenanceHistoryDays uint32          `json:"maintenanceHistoryDays"`
	Modified               bool            `json:"modified"`
	ReadOnly               bool            `json:"readOnly"`
	EntryCount             int             `json:"entryCount"`
	GroupCount             int             `json:"groupCount"`
	CustomIcons            []CustomIconDTO `json:"customIcons"`
	ProtectedTitle         bool            `json:"protectedTitle"`
	ProtectedUserName      bool            `json:"protectedUserName"`
	ProtectedPassword      bool            `json:"protectedPassword"`
	ProtectedURL           bool            `json:"protectedUrl"`
	ProtectedNotes         bool            `json:"protectedNotes"`
	LastSaved              string          `json:"lastSaved"`
	SecurityLevel          string          `json:"securityLevel"`
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func idOf(u dbdoc.UUID) string { return u.Hex() }

// groupToDTO converts a group and its subtree.
func groupToDTO(g *dbdoc.Group, parent *dbdoc.Group, recycleBin dbdoc.UUID) GroupDTO {
	parentID := ""
	if parent != nil {
		parentID = idOf(parent.UUID)
	}
	dto := GroupDTO{
		ID:              idOf(g.UUID),
		ParentID:        parentID,
		Name:            g.Name,
		Notes:           g.Notes,
		IconID:          g.IconID,
		CustomIconID:    idOf(g.CustomIconUUID),
		Expires:         g.Times.Expires,
		ExpiryTime:      formatTime(g.Times.ExpiryTime),
		Created:         formatTime(g.Times.CreationTime),
		Modified:        formatTime(g.Times.LastModification),
		IsExpanded:      g.IsExpanded,
		Tags:            append([]string{}, g.Tags...),
		EnableAutoType:  g.EnableAutoType,
		EnableSearching: g.EnableSearching,
	}
	for _, e := range g.Entries {
		dto.EntryIDs = append(dto.EntryIDs, idOf(e.UUID))
	}
	for _, sub := range g.Groups {
		dto.Children = append(dto.Children, groupToDTO(sub, g, recycleBin))
	}
	return dto
}

func entryToDTO(db *dbdoc.Database, e *dbdoc.Entry, parent *dbdoc.Group) EntryDTO {
	dto := EntryDTO{
		ID:               idOf(e.UUID),
		IconID:           e.IconID,
		CustomIconID:     idOf(e.CustomIconUUID),
		ForegroundColor:  e.ForegroundColor,
		BackgroundColor:  e.BackgroundColor,
		OverrideURL:      e.OverrideURL,
		Tags:             append([]string{}, e.Tags...),
		AutoTypeEnabled:  e.AutoType.Enabled,
		AutoTypeSequence: e.AutoType.DefaultSequence,
		Expires:          e.Times.Expires,
		ExpiryTime:       formatTime(e.Times.ExpiryTime),
		Created:          formatTime(e.Times.CreationTime),
		Modified:         formatTime(e.Times.LastModification),
		Accessed:         formatTime(e.Times.LastAccessTime),
		UsageCount:       e.Times.UsageCount,
	}
	if parent != nil {
		dto.GroupID = idOf(parent.UUID)
		dto.GroupPath = groupPath(db.Root, parent)
		dto.IsRecycled = isInRecycleBin(db, parent)
	}
	dto.Title, _ = e.Field(dbdoc.FieldTitle)
	dto.UserName, _ = e.Field(dbdoc.FieldUserName)
	dto.Password, _ = e.Field(dbdoc.FieldPassword)
	dto.URL, _ = e.Field(dbdoc.FieldURL)
	dto.Notes, _ = e.Field(dbdoc.FieldNotes)
	dto.PasswordProtected = e.IsFieldProtected(dbdoc.FieldPassword)
	for _, f := range e.CustomFields() {
		dto.Fields = append(dto.Fields, FieldDTO{Key: f.Key, Value: f.Value, Protected: f.Protected})
	}
	for _, ref := range e.Binaries {
		att := AttachmentDTO{Key: ref.Key}
		if data, ok := db.Binary(ref.Pool); ok {
			att.Size = len(data)
		}
		if ref.Pool >= 0 && ref.Pool < len(db.Binaries) {
			att.Protected = db.Binaries[ref.Pool].Protected
		}
		dto.Attachments = append(dto.Attachments, att)
	}
	for i, h := range e.History {
		title, _ := h.Field(dbdoc.FieldTitle)
		user, _ := h.Field(dbdoc.FieldUserName)
		dto.History = append(dto.History, HistoryDTO{
			ID:       idOf(h.UUID),
			Index:    i,
			Modified: formatTime(h.Times.LastModification),
			Title:    title,
			UserName: user,
		})
	}
	for _, a := range e.AutoType.Associations {
		dto.AutoTypeItems = append(dto.AutoTypeItems, AutoTypeAssociationDTO{Window: a.WindowName, Sequence: a.Sequence})
	}
	bits, score := pwquality.Estimate(dto.Password, dto.Password == "" && dto.HasTOTP())
	dto.QualityBits = bits
	dto.Quality = score
	dto.IsExpired = e.IsExpired(time.Now().UTC())
	return dto
}

// HasTOTP reports whether the entry carries an otpauth secret.
func (d EntryDTO) HasTOTP() bool {
	for _, f := range d.Fields {
		if f.Key == "otp" || f.Key == "TOTP Seed" || f.Key == "TOTP" {
			return true
		}
	}
	return false
}

// FieldValue returns the value of a field and whether it is sensitive. It accepts the
// standard field names as well as custom field keys.
func (d EntryDTO) FieldValue(field string) (string, bool) {
	switch field {
	case dbdoc.FieldTitle:
		return d.Title, false
	case dbdoc.FieldUserName:
		return d.UserName, false
	case dbdoc.FieldPassword:
		return d.Password, true
	case dbdoc.FieldURL:
		return d.URL, false
	case dbdoc.FieldNotes:
		return d.Notes, false
	}
	for _, f := range d.Fields {
		if f.Key == field {
			return f.Value, f.Protected
		}
	}
	return "", false
}

func entryToSummary(db *dbdoc.Database, e *dbdoc.Entry, parent *dbdoc.Group) EntrySummary {
	title, _ := e.Field(dbdoc.FieldTitle)
	user, _ := e.Field(dbdoc.FieldUserName)
	url, _ := e.Field(dbdoc.FieldURL)
	notes, _ := e.Field(dbdoc.FieldNotes)
	password, _ := e.Field(dbdoc.FieldPassword)
	s := EntrySummary{
		ID:           idOf(e.UUID),
		Title:        title,
		UserName:     user,
		URL:          url,
		IconID:       e.IconID,
		CustomIconID: idOf(e.CustomIconUUID),
		Tags:         append([]string{}, e.Tags...),
		Modified:     formatTime(e.Times.LastModification),
		Expires:      e.Times.Expires,
		IsExpired:    e.IsExpired(time.Now().UTC()),
		HasNotes:     strings.TrimSpace(notes) != "",
		CustomFields: len(e.CustomFields()),
		Attachments:  len(e.Binaries),
		HistoryItems: len(e.History),
	}
	if parent != nil {
		s.GroupID = idOf(parent.UUID)
		s.GroupPath = groupPath(db.Root, parent)
		s.IsRecycled = isInRecycleBin(db, parent)
	}
	_, s.Quality = pwquality.Estimate(password, false)
	for _, f := range e.Strings {
		if f.Key == "otp" || f.Key == "TOTP Seed" || f.Key == "TOTP" {
			s.HasTOTP = true
		}
	}
	return s
}

// groupPath builds the "/" separated path of a group inside the database.
func groupPath(root, target *dbdoc.Group) string {
	var path []string
	var walk func(g *dbdoc.Group, acc []string) bool
	walk = func(g *dbdoc.Group, acc []string) bool {
		acc = append(acc, g.Name)
		if g == target || g.UUID == target.UUID {
			path = append([]string{}, acc...)
			return true
		}
		for _, sub := range g.Groups {
			if walk(sub, acc) {
				return true
			}
		}
		return false
	}
	if root != nil {
		walk(root, nil)
	}
	if len(path) == 0 {
		return target.Name
	}
	return strings.Join(path, "/")
}

// isInRecycleBin reports whether the group is the recycle bin or below it.
func isInRecycleBin(db *dbdoc.Database, g *dbdoc.Group) bool {
	bin := db.RecycleBin()
	if bin == nil {
		return false
	}
	if g.UUID == bin.UUID {
		return true
	}
	found := false
	bin.Walk(func(sub *dbdoc.Group) {
		if sub.UUID == g.UUID {
			found = true
		}
	}, func(*dbdoc.Entry) {})
	return found
}

func sortEntriesByTitle(entries []*dbdoc.Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		ti, _ := entries[i].Field(dbdoc.FieldTitle)
		tj, _ := entries[j].Field(dbdoc.FieldTitle)
		return strings.ToLower(ti) < strings.ToLower(tj)
	})
}
