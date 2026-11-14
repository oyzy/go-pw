package vault

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gokeep/internal/dbdoc"
)

// csvHeader is the column order KeePass 2 uses for its default CSV export.
var csvHeader = []string{
	"Group", "Title", "Username", "Password", "URL", "Notes", "TOTP",
	"Icon", "Last Modified", "Created",
}

// ExportCSV writes every entry to a CSV stream, using the KeePass column names so that
// the file can be imported by other password managers.
func (v *Vault) ExportCSV(w io.Writer) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return ErrLocked
	}
	writer := csv.NewWriter(w)
	if err := writer.Write(csvHeader); err != nil {
		return err
	}
	// Every entry is exported, including the ones that live directly in the root group
	// (which has no visible name, so its group column stays empty).
	var walk func(group *dbdoc.Group) error
	walk = func(group *dbdoc.Group) error {
		entries := append([]*dbdoc.Entry(nil), group.Entries...)
		sortEntriesByTitle(entries)
		groupName := ""
		if group != v.db.Root {
			groupName = groupPath(v.db.Root, group)
		}
		for _, e := range entries {
			password, _ := e.Field(dbdoc.FieldPassword)
			totp := ""
			for _, f := range e.CustomFields() {
				if f.Key == "TOTP" || f.Key == "otp" {
					totp = f.Value
				}
			}
			record := []string{
				groupName,
				fieldOf(e, dbdoc.FieldTitle),
				fieldOf(e, dbdoc.FieldUserName),
				password,
				fieldOf(e, dbdoc.FieldURL),
				fieldOf(e, dbdoc.FieldNotes),
				totp,
				strconv.Itoa(e.IconID),
				formatTime(e.Times.LastModification),
				formatTime(e.Times.CreationTime),
			}
			if err := writer.Write(record); err != nil {
				return err
			}
		}
		for _, sub := range group.Groups {
			if err := walk(sub); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(v.db.Root); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

// ImportCSV reads entries from a CSV file. Columns are matched by name so that exports
// of other applications can be imported as well.
func (v *Vault) ImportCSV(r io.Reader, targetGroupID string) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.db == nil {
		return 0, ErrLocked
	}
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	records, err := reader.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("cannot read the CSV file: %w", err)
	}
	if len(records) == 0 {
		return 0, nil
	}
	header := records[0]
	index := map[string]int{}
	for i, name := range header {
		index[strings.ToLower(strings.TrimSpace(name))] = i
	}
	column := func(record []string, names ...string) string {
		for _, name := range names {
			if i, ok := index[name]; ok && i < len(record) {
				return record[i]
			}
		}
		return ""
	}
	target, err := v.findGroupLocked(targetGroupID)
	if err != nil {
		return 0, err
	}
	imported := 0
	for _, record := range records[1:] {
		if len(record) == 0 {
			continue
		}
		title := column(record, "title", "account", "name")
		user := column(record, "username", "user name", "login name", "login")
		password := column(record, "password")
		url := column(record, "url", "web site", "website")
		notes := column(record, "notes", "comments", "comment")
		groupName := column(record, "group", "folder")
		if title == "" && user == "" && password == "" && url == "" {
			continue
		}
		destination := target
		if groupName != "" && target == v.db.Root {
			destination = findOrCreateGroup(v.db.Root, groupName)
		}
		entry := dbdoc.NewEntry()
		entry.SetField(dbdoc.FieldTitle, title, false)
		entry.SetField(dbdoc.FieldUserName, user, v.db.Meta.MemoryProtection.ProtectUserName)
		entry.SetField(dbdoc.FieldPassword, password, true)
		entry.SetField(dbdoc.FieldURL, url, v.db.Meta.MemoryProtection.ProtectURL)
		entry.SetField(dbdoc.FieldNotes, notes, false)
		if totp := column(record, "totp"); totp != "" {
			entry.SetField("TOTP", totp, true)
		}
		if icon := column(record, "icon", "iconid"); icon != "" {
			if value, err := strconv.Atoi(icon); err == nil && value >= 0 && value <= dbdoc.MaxBuiltinIconID {
				entry.IconID = value
			}
		}
		if modified := column(record, "last modified", "modified"); modified != "" {
			if t := dbdoc.ParseTime(modified); !t.IsZero() {
				entry.Times.LastModification = t
			}
		}
		destination.Entries = append(destination.Entries, entry)
		destination.Times.Touch()
		imported++
	}
	v.touch()
	return imported, nil
}

func fieldOf(e *dbdoc.Entry, key string) string {
	value, _ := e.Field(key)
	return value
}

// findOrCreateGroup resolves a "/" or "\\" separated group path.
func findOrCreateGroup(root *dbdoc.Group, path string) *dbdoc.Group {
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	current := root
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		next := current.FindGroupByName(part)
		if next == nil {
			next = dbdoc.NewGroup(part)
			current.Groups = append(current.Groups, next)
		}
		current = next
	}
	return current
}
