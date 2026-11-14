package vault

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"gokeep/internal/dbdoc"
)

// CreateEntry adds a new entry to a group.
func (v *Vault) CreateEntry(groupID string, dto EntryDTO) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return "", ErrLocked
	}
	group, err := v.findGroupLocked(groupID)
	if err != nil {
		return "", err
	}
	if group.UUID == v.recycleBinIDLocked() {
		// KeePass does not allow creating entries directly in the recycle bin.
		return "", errors.New("vault: entries cannot be created inside the recycle bin")
	}
	entry := dbdoc.NewEntry()
	entry.SetField(dbdoc.FieldTitle, dto.Title, false)
	entry.SetField(dbdoc.FieldUserName, dto.UserName, v.db.Meta.MemoryProtection.ProtectUserName)
	entry.SetField(dbdoc.FieldPassword, dto.Password, v.db.Meta.MemoryProtection.ProtectPassword)
	entry.SetField(dbdoc.FieldURL, dto.URL, v.db.Meta.MemoryProtection.ProtectURL)
	entry.SetField(dbdoc.FieldNotes, dto.Notes, v.db.Meta.MemoryProtection.ProtectNotes)
	if err := applyEntryDTO(v.db, entry, dto); err != nil {
		return "", err
	}
	group.Entries = append(group.Entries, entry)
	group.Times.Touch()
	v.touch()
	return idOf(entry.UUID), nil
}

// UpdateEntry stores the edited entry, keeping a history revision of the old state.
func (v *Vault) UpdateEntry(dto EntryDTO) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, _, err := v.findEntryLocked(dto.ID)
	if err != nil {
		return err
	}
	v.pushHistoryLocked(entry)
	entry.SetField(dbdoc.FieldTitle, dto.Title, entry.IsFieldProtected(dbdoc.FieldTitle))
	entry.SetField(dbdoc.FieldUserName, dto.UserName, entry.IsFieldProtected(dbdoc.FieldUserName))
	entry.SetField(dbdoc.FieldPassword, dto.Password, entry.IsFieldProtected(dbdoc.FieldPassword))
	entry.SetField(dbdoc.FieldURL, dto.URL, entry.IsFieldProtected(dbdoc.FieldURL))
	entry.SetField(dbdoc.FieldNotes, dto.Notes, entry.IsFieldProtected(dbdoc.FieldNotes))
	if err := applyEntryDTO(v.db, entry, dto); err != nil {
		return err
	}
	entry.Times.Touch()
	v.touch()
	return nil
}

// AddAttachment stores a file inside the entry.
func (v *Vault) AddAttachment(entryID, name string, data []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, _, err := v.findEntryLocked(entryID)
	if err != nil {
		return err
	}
	pool := v.db.AddBinary(data, false)
	for i := range entry.Binaries {
		if entry.Binaries[i].Key == name {
			entry.Binaries[i].Pool = pool
			entry.Times.Touch()
			v.touch()
			return nil
		}
	}
	entry.Binaries = append(entry.Binaries, dbdoc.BinaryRef{Key: name, Pool: pool})
	entry.Times.Touch()
	v.touch()
	return nil
}

// RemoveAttachment deletes an attachment from the entry.
func (v *Vault) RemoveAttachment(entryID, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, _, err := v.findEntryLocked(entryID)
	if err != nil {
		return err
	}
	for i := range entry.Binaries {
		if entry.Binaries[i].Key == name {
			entry.Binaries = append(entry.Binaries[:i], entry.Binaries[i+1:]...)
			entry.Times.Touch()
			v.touch()
			return nil
		}
	}
	return fmt.Errorf("vault: entry has no attachment named %q", name)
}

// Attachment returns the bytes of one attachment.
func (v *Vault) Attachment(entryID, name string) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return nil, ErrLocked
	}
	entry, _, err := v.findEntryLocked(entryID)
	if err != nil {
		return nil, err
	}
	for _, ref := range entry.Binaries {
		if ref.Key == name {
			if data, ok := v.db.Binary(ref.Pool); ok {
				return append([]byte(nil), data...), nil
			}
			return nil, errors.New("vault: attachment data is missing")
		}
	}
	return nil, fmt.Errorf("vault: entry has no attachment named %q", name)
}

// TouchEntryAccess updates the last access time and the usage counter, like KeePass does
// when an entry is used. Access bookkeeping alone is not a user-facing edit: selecting an
// entry must not make closing the application ask about unsaved changes. The updated
// counters are written the next time a real edit causes the database to be saved.
func (v *Vault) TouchEntryAccess(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, _, err := v.findEntryLocked(id)
	if err != nil {
		return err
	}
	entry.Times.LastAccessTime = now()
	entry.Times.UsageCount++
	return nil
}

// Groups lists every group, optionally including the recycle bin, for move menus.
func (v *Vault) Groups(includeRecycled bool) ([]GroupDTO, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return nil, ErrLocked
	}
	var out []GroupDTO
	var walk func(g *dbdoc.Group, parent *dbdoc.Group)
	walk = func(g *dbdoc.Group, parent *dbdoc.Group) {
		if includeRecycled || !isInRecycleBin(v.db, g) {
			out = append(out, groupToDTO(g, parent, v.db.Meta.RecycleBinUUID))
		}
		for _, sub := range g.Groups {
			walk(sub, g)
		}
	}
	walk(v.db.Root, nil)
	return out, nil
}

// TagList returns every tag used in the database.
func (v *Vault) TagList() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return nil
	}
	seen := map[string]bool{}
	var tags []string
	for _, e := range v.db.Entries() {
		for _, t := range e.Tags {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}
	sort.Strings(tags)
	return tags
}

// recycleBinIDLocked returns the recycle bin UUID, or the zero UUID when disabled.
func (v *Vault) recycleBinIDLocked() dbdoc.UUID {
	if !v.db.Meta.RecycleBinEnabled {
		return dbdoc.ZeroUUID
	}
	return v.db.Meta.RecycleBinUUID
}

func removeEntry(parent *dbdoc.Group, entry *dbdoc.Entry) {
	for i, e := range parent.Entries {
		if e == entry || e.UUID == entry.UUID {
			parent.Entries = append(parent.Entries[:i], parent.Entries[i+1:]...)
			return
		}
	}
}

func removeGroup(parent, group *dbdoc.Group) {
	for i, g := range parent.Groups {
		if g == group || g.UUID == group.UUID {
			parent.Groups = append(parent.Groups[:i], parent.Groups[i+1:]...)
			return
		}
	}
}

func maxU32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

func maxU64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// applyEntryDTO copies the fields shared by create and update.
func applyEntryDTO(db *dbdoc.Database, entry *dbdoc.Entry, dto EntryDTO) error {
	if dto.IconID >= 0 {
		entry.IconID = dto.IconID
	}
	entry.CustomIconUUID = dbdoc.ZeroUUID
	if dto.CustomIconID != "" {
		uuid, err := dbdoc.ParseUUID(dto.CustomIconID)
		if err != nil {
			return err
		}
		entry.CustomIconUUID = uuid
	}
	entry.ForegroundColor = dto.ForegroundColor
	entry.BackgroundColor = dto.BackgroundColor
	entry.OverrideURL = dto.OverrideURL
	entry.Tags = trimTags(dto.Tags)
	entry.Times.Expires = dto.Expires
	if dto.Expires && dto.ExpiryTime != "" {
		entry.Times.ExpiryTime = dbdoc.ParseTime(dto.ExpiryTime)
	}
	entry.AutoType.Enabled = dto.AutoTypeEnabled
	entry.AutoType.DefaultSequence = dto.AutoTypeSequence
	entry.AutoType.Associations = nil
	for _, a := range dto.AutoTypeItems {
		if strings.TrimSpace(a.Window) == "" && strings.TrimSpace(a.Sequence) == "" {
			continue
		}
		entry.AutoType.Associations = append(entry.AutoType.Associations, dbdoc.AutoTypeAssociation{
			WindowName: a.Window,
			Sequence:   a.Sequence,
		})
	}
	// Custom fields are replaced wholesale; memory protection flags are preserved.
	for _, existing := range entry.CustomFields() {
		entry.RemoveField(existing.Key)
	}
	for _, f := range dto.Fields {
		if strings.TrimSpace(f.Key) == "" || isStandardKey(f.Key) {
			continue
		}
		entry.SetField(f.Key, f.Value, f.Protected)
	}
	return nil
}

func isStandardKey(key string) bool {
	switch key {
	case dbdoc.FieldTitle, dbdoc.FieldUserName, dbdoc.FieldPassword, dbdoc.FieldURL, dbdoc.FieldNotes:
		return true
	}
	return false
}

// pushHistoryLocked stores the current state of the entry as a revision and applies the
// history limits of the database.
func (v *Vault) pushHistoryLocked(entry *dbdoc.Entry) {
	backup := entry.CloneWithoutHistory()
	backup.Times.LastModification = entry.Times.LastModification
	entry.History = append(entry.History, backup)
	v.trimHistoryLocked(entry)
}

func (v *Vault) trimHistoryLocked(entry *dbdoc.Entry) {
	maxItems := v.db.Meta.HistoryMaxItems
	if maxItems > 0 && len(entry.History) > maxItems {
		entry.History = entry.History[len(entry.History)-maxItems:]
	}
	if v.db.Meta.HistoryMaxSize <= 0 {
		return
	}
	for len(entry.History) > 1 && int64(entrySize(entry.History)) > v.db.Meta.HistoryMaxSize {
		entry.History = entry.History[1:]
	}
}

func entrySize(history []*dbdoc.Entry) int {
	total := 0
	for _, h := range history {
		for _, f := range h.Strings {
			total += len(f.Key) + len(f.Value)
		}
	}
	return total
}

// DeleteEntry moves an entry to the recycle bin, or removes it for good when the
// recycle bin is disabled or the entry already sits inside it.
func (v *Vault) DeleteEntry(id string, permanently bool) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, parent, err := v.findEntryLocked(id)
	if err != nil {
		return err
	}
	// The recycle bin group is created on demand, so the decision only depends on the
	// user's explicit choice and on whether the entry already sits inside the bin.
	// Databases written by older builds stored "recycle bin off" as a default, which must
	// not silently turn deletions into permanent ones, hence the marker check.
	explicitlyDisabled := recycleBinDisabled(v.db)
	useBin := !permanently && !explicitlyDisabled && !isInRecycleBin(v.db, parent)
	// No entry data is logged, only the decision, which is what support questions need.
	slog.Info("deleting entry",
		"toRecycleBin", useBin,
		"permanentlyRequested", permanently,
		"recycleBinEnabled", v.db.Meta.RecycleBinEnabled,
		"recycleBinDisabledByUser", explicitlyDisabled)
	if useBin {
		bin := v.enableRecycleBinLocked()
		removeEntry(parent, entry)
		// Remember where the entry came from, so it can be restored to that group.
		entry.PreviousParentGroup = parent.UUID
		entry.Times.LocationChanged = now()
		entry.Times.Touch()
		bin.Entries = append(bin.Entries, entry)
		bin.Times.Touch()
		v.touch()
		return nil
	}
	removeEntry(parent, entry)
	v.recordDeletionLocked(entry)
	parent.Times.Touch()
	v.touch()
	return nil
}

// RestoreToPreviousGroup moves an entry out of the recycle bin into the group it was
// deleted from, falling back to the root group when that group no longer exists.
func (v *Vault) RestoreToPreviousGroup(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, parent, err := v.findEntryLocked(id)
	if err != nil {
		return err
	}
	var target *dbdoc.Group
	if !entry.PreviousParentGroup.IsZero() {
		if previous := v.db.Root.FindGroup(entry.PreviousParentGroup); previous != nil && !isInRecycleBin(v.db, previous) {
			target = previous
		}
	}
	if target == nil {
		target = v.db.Root
	}
	if parent.UUID == target.UUID {
		return nil
	}
	removeEntry(parent, entry)
	parent.Times.Touch()
	entry.Times.LocationChanged = now()
	entry.Times.Touch()
	target.Entries = append(target.Entries, entry)
	target.Times.Touch()
	v.touch()
	return nil
}

// RestoreEntry moves an entry out of the recycle bin into a target group.
func (v *Vault) RestoreEntry(id, targetGroupID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, parent, err := v.findEntryLocked(id)
	if err != nil {
		return err
	}
	target, err := v.findGroupLocked(targetGroupID)
	if err != nil {
		return err
	}
	if parent.UUID == target.UUID {
		return nil
	}
	removeEntry(parent, entry)
	parent.Times.Touch()
	target.Entries = append(target.Entries, entry)
	target.Times.Touch()
	entry.Times.LocationChanged = now()
	entry.Times.Touch()
	v.touch()
	return nil
}

// MoveEntry moves an entry to another group.
func (v *Vault) MoveEntry(id, groupID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, parent, err := v.findEntryLocked(id)
	if err != nil {
		return err
	}
	target, err := v.findGroupLocked(groupID)
	if err != nil {
		return err
	}
	if target.UUID == parent.UUID {
		return nil
	}
	bin := v.db.RecycleBin()
	if bin != nil && target.UUID != bin.UUID && isInRecycleBin(v.db, target) {
		return errors.New("vault: entries cannot be moved below the recycle bin")
	}
	removeEntry(parent, entry)
	parent.Times.Touch()
	entry.PreviousParentGroup = parent.UUID
	entry.Times.LocationChanged = now()
	entry.Times.Touch()
	target.Entries = append(target.Entries, entry)
	target.Times.Touch()
	v.touch()
	return nil
}

// DuplicateEntry creates a copy of an entry, including its attachments.
func (v *Vault) DuplicateEntry(id string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return "", ErrLocked
	}
	entry, parent, err := v.findEntryLocked(id)
	if err != nil {
		return "", err
	}
	clone := entry.CloneWithoutHistory()
	clone.UUID = dbdoc.NewUUID()
	clone.Times = dbdoc.NewTimes()
	title, _ := clone.Field(dbdoc.FieldTitle)
	clone.SetField(dbdoc.FieldTitle, title+" (copy)", clone.IsFieldProtected(dbdoc.FieldTitle))
	parent.Entries = append(parent.Entries, clone)
	parent.Times.Touch()
	v.touch()
	return idOf(clone.UUID), nil
}

// CreateGroup adds a subgroup.
func (v *Vault) CreateGroup(parentID, name string, iconID int) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return "", ErrLocked
	}
	parent, err := v.findGroupLocked(parentID)
	if err != nil {
		return "", err
	}
	if isInRecycleBin(v.db, parent) {
		return "", errors.New("vault: groups cannot be created inside the recycle bin")
	}
	group := dbdoc.NewGroup(name)
	if iconID >= 0 {
		group.IconID = iconID
	}
	parent.Groups = append(parent.Groups, group)
	parent.Times.Touch()
	v.touch()
	return idOf(group.UUID), nil
}

// UpdateGroup stores edited group properties.
func (v *Vault) UpdateGroup(dto GroupDTO) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	group, err := v.findGroupLocked(dto.ID)
	if err != nil {
		return err
	}
	group.Name = dto.Name
	group.Notes = dto.Notes
	if dto.IconID >= 0 {
		group.IconID = dto.IconID
	}
	group.CustomIconUUID = dbdoc.ZeroUUID
	if dto.CustomIconID != "" {
		uuid, err := dbdoc.ParseUUID(dto.CustomIconID)
		if err != nil {
			return err
		}
		group.CustomIconUUID = uuid
	}
	group.IsExpanded = dto.IsExpanded
	group.Tags = trimTags(dto.Tags)
	group.EnableAutoType = dto.EnableAutoType
	group.EnableSearching = dto.EnableSearching
	group.Times.Expires = dto.Expires
	if dto.Expires && dto.ExpiryTime != "" {
		group.Times.ExpiryTime = dbdoc.ParseTime(dto.ExpiryTime)
	}
	group.Times.Touch()
	v.touch()
	return nil
}

// DeleteGroup removes a group, honouring the recycle bin.
func (v *Vault) DeleteGroup(id string, permanently bool) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	group, err := v.findGroupLocked(id)
	if err != nil {
		return err
	}
	if group.UUID == v.db.Root.UUID {
		return errors.New("vault: the root group cannot be deleted")
	}
	if group.UUID == v.recycleBinIDLocked() {
		return errors.New("vault: the recycle bin cannot be deleted")
	}
	parent := v.db.ParentGroup(group.UUID)
	if parent == nil {
		return errors.New("vault: the group has no parent")
	}
	useBin := !permanently && v.db.Meta.RecycleBinEnabled &&
		!isInRecycleBin(v.db, parent) && !isInRecycleBin(v.db, group)
	slog.Info("deleting group",
		"toRecycleBin", useBin,
		"permanentlyRequested", permanently,
		"recycleBinEnabled", v.db.Meta.RecycleBinEnabled,
		"recycleBinDisabledByUser", recycleBinDisabled(v.db))
	if useBin {
		bin := v.enableRecycleBinLocked()
		removeGroup(parent, group)
		group.PreviousParentGroup = parent.UUID
		group.Times.LocationChanged = now()
		group.Times.Touch()
		bin.Groups = append(bin.Groups, group)
		bin.Times.Touch()
		v.touch()
		return nil
	}
	removeGroup(parent, group)
	group.Walk(func(g *dbdoc.Group) {
		v.db.DeletedObjects = append(v.db.DeletedObjects, dbdoc.DeletedObject{UUID: g.UUID, DeletionTime: now()})
	}, func(e *dbdoc.Entry) {
		v.recordDeletionLocked(e)
	})
	parent.Times.Touch()
	v.touch()
	return nil
}

// MoveGroup moves a group into another group.
func (v *Vault) MoveGroup(id, parentID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	group, err := v.findGroupLocked(id)
	if err != nil {
		return err
	}
	target, err := v.findGroupLocked(parentID)
	if err != nil {
		return err
	}
	if group.UUID == v.db.Root.UUID {
		return errors.New("vault: the root group cannot be moved")
	}
	if group.UUID == target.UUID || target.FindGroup(group.UUID) != nil {
		return errors.New("vault: a group cannot be moved into itself")
	}
	current := v.db.ParentGroup(group.UUID)
	if current == nil {
		return errors.New("vault: the group has no parent")
	}
	if current.UUID == target.UUID {
		return nil
	}
	if isInRecycleBin(v.db, target) {
		return errors.New("vault: groups cannot be moved into the recycle bin")
	}
	removeGroup(current, group)
	current.Times.Touch()
	group.PreviousParentGroup = current.UUID
	group.Times.LocationChanged = now()
	group.Times.Touch()
	target.Groups = append(target.Groups, group)
	target.Times.Touch()
	v.touch()
	return nil
}

// EmptyRecycleBin removes everything inside the recycle bin for good.
func (v *Vault) EmptyRecycleBin() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	bin := v.db.RecycleBin()
	if bin == nil {
		return nil
	}
	for _, e := range bin.Entries {
		v.recordDeletionLocked(e)
	}
	for _, g := range bin.Groups {
		g.Walk(func(sub *dbdoc.Group) {
			v.db.DeletedObjects = append(v.db.DeletedObjects, dbdoc.DeletedObject{UUID: sub.UUID, DeletionTime: now()})
		}, func(e *dbdoc.Entry) {
			v.recordDeletionLocked(e)
		})
	}
	bin.Entries = nil
	bin.Groups = nil
	bin.Times.Touch()
	v.touch()
	return nil
}

// recordDeletionLocked appends deletion markers for an entry and its history.
func (v *Vault) recordDeletionLocked(entry *dbdoc.Entry) {
	v.db.DeletedObjects = append(v.db.DeletedObjects, dbdoc.DeletedObject{UUID: entry.UUID, DeletionTime: now()})
	for _, h := range entry.History {
		v.db.DeletedObjects = append(v.db.DeletedObjects, dbdoc.DeletedObject{UUID: h.UUID, DeletionTime: now()})
	}
}

// RestoreHistory replaces an entry with one of its stored revisions.
func (v *Vault) RestoreHistory(entryID string, index int) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, _, err := v.findEntryLocked(entryID)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(entry.History) {
		return fmt.Errorf("vault: history index %d out of range", index)
	}
	revision := entry.History[index].Clone()
	v.pushHistoryLocked(entry)
	revision.UUID = entry.UUID
	revision.Times.LastModification = now()
	*entry = *revision
	v.touch()
	return nil
}

// DeleteHistory removes one stored revision.
func (v *Vault) DeleteHistory(entryID string, index int) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	entry, _, err := v.findEntryLocked(entryID)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(entry.History) {
		return fmt.Errorf("vault: history index %d out of range", index)
	}
	entry.History = append(entry.History[:index], entry.History[index+1:]...)
	entry.Times.Touch()
	v.touch()
	return nil
}
