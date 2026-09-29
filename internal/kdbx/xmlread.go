package kdbx

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// xmlReader turns the decrypted XML back into a Database. It decodes protected values
// strictly in document order, which is what the inner random stream requires.
type xmlReader struct {
	dec    *xml.Decoder
	stream randomStream
	db     *Database
}

// parseDatabaseXML parses a decrypted KeePass XML document.
func parseDatabaseXML(data []byte, stream randomStream) (*Database, error) {
	r := &xmlReader{
		dec:    xml.NewDecoder(bytes.NewReader(data)),
		stream: stream,
		db:     &Database{},
	}
	// KeePass does not use namespaces, but being lenient here costs nothing.
	r.dec.Strict = true

	found := false
	for !found {
		tok, err := r.dec.Token()
		if err == io.EOF {
			return nil, errors.New("kdbx: XML document does not contain a KeePassFile element")
		}
		if err != nil {
			return nil, fmt.Errorf("kdbx: invalid XML: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "KeePassFile" {
				found = true
				break
			}
			if err := r.dec.Skip(); err != nil {
				return nil, err
			}
		}
	}

	for {
		tok, err := r.dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("kdbx: invalid XML: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "Meta":
			if err := r.parseMeta(se); err != nil {
				return nil, err
			}
		case "Root":
			if err := r.parseRoot(se); err != nil {
				return nil, err
			}
		default:
			if err := r.dec.Skip(); err != nil {
				return nil, err
			}
		}
	}
	if r.db.Root == nil {
		return nil, errors.New("kdbx: XML document has no root group")
	}
	return r.db, nil
}

// text reads the text content of a leaf element.
func (r *xmlReader) text(se xml.StartElement) (string, error) {
	var s string
	if err := r.dec.DecodeElement(&s, &se); err != nil {
		return "", fmt.Errorf("kdbx: invalid <%s>: %w", se.Name.Local, err)
	}
	return s, nil
}

func (r *xmlReader) parseMeta(se xml.StartElement) error {
	meta := &r.db.Meta
	var rawBinaries []xmlBinaryPoolEntry
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return fmt.Errorf("kdbx: invalid <Meta>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Meta" {
				for _, b := range rawBinaries {
					data := b.Data
					if b.Compressed {
						if decompressed, err := gzipDecompress(data); err == nil {
							data = decompressed
						}
					}
					r.db.Binaries = append(r.db.Binaries, Binary{Data: data, Protected: b.Protected})
				}
				return nil
			}
		case xml.StartElement:
			name := t.Name.Local
			switch name {
			case "Binary":
				entry, err := r.parseMetaBinary(t)
				if err != nil {
					return err
				}
				rawBinaries = append(rawBinaries, entry)
			case "Binaries":
				entries, err := r.parseBinaries()
				if err != nil {
					return err
				}
				rawBinaries = append(rawBinaries, entries...)
			case "MemoryProtection":
				if err := r.parseMemoryProtection(&meta.MemoryProtection); err != nil {
					return err
				}
			case "CustomIcons":
				icons, err := r.parseCustomIcons()
				if err != nil {
					return err
				}
				meta.CustomIcons = icons
			case "CustomData":
				items, err := r.parseCustomData()
				if err != nil {
					return err
				}
				meta.CustomData = items
			default:
				text, err := r.text(t)
				if err != nil {
					return err
				}
				text = strings.TrimSpace(text)
				switch name {
				case "Generator":
					meta.Generator = text
				case "HeaderHash":
					if data, err := base64.StdEncoding.DecodeString(text); err == nil {
						meta.HeaderHash = data
					}
				case "SettingsChanged":
					meta.SettingsChanged = ParseTime(text)
				case "DatabaseName":
					meta.DatabaseName = text
				case "DatabaseNameChanged":
					meta.DatabaseNameChanged = ParseTime(text)
				case "DatabaseDescription":
					meta.DatabaseDescription = text
				case "DatabaseDescriptionChanged":
					meta.DatabaseDescriptionChanged = ParseTime(text)
				case "DefaultUserName":
					meta.DefaultUserName = text
				case "DefaultUserNameChanged":
					meta.DefaultUserNameChanged = ParseTime(text)
				case "MaintenanceHistoryDays":
					meta.MaintenanceHistoryDays = uint32(parseInt(text, 365))
				case "Color":
					meta.Color = text
				case "MasterKeyChanged":
					meta.MasterKeyChanged = ParseTime(text)
				case "MasterKeyChangeRec":
					meta.MasterKeyChangeRec = parseInt(text, -1)
				case "MasterKeyChangeForce":
					meta.MasterKeyChangeForce = parseInt(text, -1)
				case "RecycleBinEnabled":
					meta.RecycleBinEnabled = parseBool(text, false)
				case "RecycleBinUUID", "RecycleBinUuid":
					meta.RecycleBinUUID = parseUUIDString(text)
				case "RecycleBinChanged":
					meta.RecycleBinChanged = ParseTime(text)
				case "EntryTemplatesGroup":
					meta.EntryTemplatesGroup = parseUUIDString(text)
				case "EntryTemplatesGroupChanged":
					meta.EntryTemplatesGroupChanged = ParseTime(text)
				case "HistoryMaxItems":
					meta.HistoryMaxItems = int(parseInt(text, 10))
				case "HistoryMaxSize":
					meta.HistoryMaxSize = parseInt(text, 6*1024*1024)
				case "LastSelectedGroup":
					meta.LastSelectedGroup = parseUUIDString(text)
				case "LastTopVisibleGroup":
					meta.LastTopVisibleGroup = parseUUIDString(text)
				case "ProtectTitle", "ProtectUserName", "ProtectPassword", "ProtectURL", "ProtectNotes":
					// Handled through MemoryProtection; ignore stray copies.
				case "InnerRandomStreamID":
					// KDBX 3.x compatibility: the value lives in the header, but some
					// producers also write it here. Handled by the caller.
				case "ProtectedStreamKey", "StreamStartBytes", "InnerRandomStreamKey":
					// Same as above: informational only.
				}
			}
		}
	}
}

type xmlBinaryPoolEntry struct {
	Data       []byte
	Compressed bool
	Protected  bool
}

// parseBinaries reads the KDBX 3.1 attachment pool stored in the XML.
func (r *xmlReader) parseBinaries() ([]xmlBinaryPoolEntry, error) {
	var out []xmlBinaryPoolEntry
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return nil, fmt.Errorf("kdbx: invalid <Binaries>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Binaries" {
				return out, nil
			}
		case xml.StartElement:
			if t.Name.Local != "Binary" {
				if err := r.dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			entry, err := r.parseMetaBinary(t)
			if err != nil {
				return nil, err
			}
			out = append(out, entry)
		}
	}
}

func (r *xmlReader) parseMetaBinary(se xml.StartElement) (xmlBinaryPoolEntry, error) {
	var out xmlBinaryPoolEntry
	for _, attr := range se.Attr {
		switch attr.Name.Local {
		case "Compressed":
			out.Compressed = parseBool(attr.Value, false)
		case "Protected":
			out.Protected = parseBool(attr.Value, false)
		}
	}
	text, err := r.text(se)
	if err != nil {
		return out, err
	}
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
	if err != nil {
		return out, fmt.Errorf("kdbx: invalid binary data: %w", err)
	}
	out.Data = data
	return out, nil
}

func (r *xmlReader) parseMemoryProtection(mp *MemoryProtection) error {
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return fmt.Errorf("kdbx: invalid <MemoryProtection>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "MemoryProtection" {
				return nil
			}
		case xml.StartElement:
			text, err := r.text(t)
			if err != nil {
				return err
			}
			value := parseBool(text, false)
			switch t.Name.Local {
			case "ProtectTitle":
				mp.ProtectTitle = value
			case "ProtectUserName":
				mp.ProtectUserName = value
			case "ProtectPassword":
				mp.ProtectPassword = value
			case "ProtectURL":
				mp.ProtectURL = value
			case "ProtectNotes":
				mp.ProtectNotes = value
			}
		}
	}
}

func (r *xmlReader) parseCustomIcons() ([]CustomIcon, error) {
	var icons []CustomIcon
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return nil, fmt.Errorf("kdbx: invalid <CustomIcons>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "CustomIcons" {
				return icons, nil
			}
		case xml.StartElement:
			if t.Name.Local != "Icon" {
				if err := r.dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			icon, err := r.parseCustomIcon()
			if err != nil {
				return nil, err
			}
			icons = append(icons, icon)
		}
	}
}

func (r *xmlReader) parseCustomIcon() (CustomIcon, error) {
	var icon CustomIcon
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return icon, fmt.Errorf("kdbx: invalid <Icon>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Icon" {
				return icon, nil
			}
		case xml.StartElement:
			text, err := r.text(t)
			if err != nil {
				return icon, err
			}
			switch t.Name.Local {
			case "UUID":
				icon.UUID = parseUUIDString(text)
			case "Data":
				if data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), "")); err == nil {
					icon.Data = data
				}
			case "Name":
				icon.Name = text
			case "LastModificationTime":
				icon.LastModificationTime = ParseTime(text)
			}
		}
	}
}

func (r *xmlReader) parseCustomData() ([]CustomDataItem, error) {
	var items []CustomDataItem
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return nil, fmt.Errorf("kdbx: invalid <CustomData>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "CustomData" {
				return items, nil
			}
		case xml.StartElement:
			if t.Name.Local != "Item" {
				if err := r.dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			item, err := r.parseCustomDataItem()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
	}
}

func (r *xmlReader) parseCustomDataItem() (CustomDataItem, error) {
	var item CustomDataItem
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return item, fmt.Errorf("kdbx: invalid <Item>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Item" {
				return item, nil
			}
		case xml.StartElement:
			text, err := r.text(t)
			if err != nil {
				return item, err
			}
			switch t.Name.Local {
			case "Key":
				item.Key = text
			case "Value":
				item.Value = text
			}
		}
	}
}

func (r *xmlReader) parseRoot(se xml.StartElement) error {
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return fmt.Errorf("kdbx: invalid <Root>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Root" {
				return nil
			}
		case xml.StartElement:
			switch t.Name.Local {
			case "Group":
				group, err := r.parseGroup()
				if err != nil {
					return err
				}
				if r.db.Root == nil {
					r.db.Root = group
				}
			case "DeletedObjects":
				objs, err := r.parseDeletedObjects()
				if err != nil {
					return err
				}
				r.db.DeletedObjects = objs
			default:
				if err := r.dec.Skip(); err != nil {
					return err
				}
			}
		}
	}
}

func (r *xmlReader) parseDeletedObjects() ([]DeletedObject, error) {
	var out []DeletedObject
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return nil, fmt.Errorf("kdbx: invalid <DeletedObjects>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "DeletedObjects" {
				return out, nil
			}
		case xml.StartElement:
			if t.Name.Local != "DeletedObject" {
				if err := r.dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			obj, err := r.parseDeletedObject()
			if err != nil {
				return nil, err
			}
			out = append(out, obj)
		}
	}
}

func (r *xmlReader) parseDeletedObject() (DeletedObject, error) {
	var obj DeletedObject
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return obj, fmt.Errorf("kdbx: invalid <DeletedObject>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "DeletedObject" {
				return obj, nil
			}
		case xml.StartElement:
			text, err := r.text(t)
			if err != nil {
				return obj, err
			}
			switch t.Name.Local {
			case "UUID":
				obj.UUID = parseUUIDString(text)
			case "DeletionTime":
				obj.DeletionTime = ParseTime(text)
			}
		}
	}
}

func (r *xmlReader) parseGroup() (*Group, error) {
	g := &Group{}
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return nil, fmt.Errorf("kdbx: invalid <Group>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Group" {
				return g, nil
			}
		case xml.StartElement:
			switch t.Name.Local {
			case "Group":
				sub, err := r.parseGroup()
				if err != nil {
					return nil, err
				}
				g.Groups = append(g.Groups, sub)
			case "Entry":
				entry, err := r.parseEntry()
				if err != nil {
					return nil, err
				}
				g.Entries = append(g.Entries, entry)
			case "Times":
				if err := r.parseTimes(&g.Times); err != nil {
					return nil, err
				}
			case "CustomData":
				items, err := r.parseCustomData()
				if err != nil {
					return nil, err
				}
				g.CustomData = items
			default:
				text, err := r.text(t)
				if err != nil {
					return nil, err
				}
				switch t.Name.Local {
				case "UUID":
					g.UUID = parseUUIDString(text)
				case "Name":
					g.Name = text
				case "Notes":
					g.Notes = text
				case "IconID":
					g.IconID = int(parseInt(text, int64(IconFolder)))
				case "CustomIconUUID":
					g.CustomIconUUID = parseUUIDString(text)
				case "IsExpanded":
					g.IsExpanded = parseBool(text, true)
				case "DefaultAutoTypeSequence":
					g.DefaultAutoTypeSequence = text
				case "EnableAutoType":
					g.EnableAutoType = parseNullableBool(text)
				case "EnableSearching":
					g.EnableSearching = parseNullableBool(text)
				case "LastTopVisibleEntry":
					g.LastTopVisibleEntry = parseUUIDString(text)
				case "PreviousParentGroup":
					g.PreviousParentGroup = parseUUIDString(text)
				case "Tags":
					g.Tags = splitTags(text)
				}
			}
		}
	}
}

func (r *xmlReader) parseEntry() (*Entry, error) {
	e := &Entry{}
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return nil, fmt.Errorf("kdbx: invalid <Entry>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Entry" {
				return e, nil
			}
		case xml.StartElement:
			switch t.Name.Local {
			case "String":
				field, err := r.parseString()
				if err != nil {
					return nil, err
				}
				e.Strings = append(e.Strings, field)
			case "Binary":
				ref, err := r.parseBinaryRef()
				if err != nil {
					return nil, err
				}
				e.Binaries = append(e.Binaries, ref)
			case "Times":
				if err := r.parseTimes(&e.Times); err != nil {
					return nil, err
				}
			case "AutoType":
				if err := r.parseAutoType(&e.AutoType); err != nil {
					return nil, err
				}
			case "History":
				history, err := r.parseHistory()
				if err != nil {
					return nil, err
				}
				e.History = history
			case "CustomData":
				items, err := r.parseCustomData()
				if err != nil {
					return nil, err
				}
				e.CustomData = items
			default:
				text, err := r.text(t)
				if err != nil {
					return nil, err
				}
				switch t.Name.Local {
				case "UUID":
					e.UUID = parseUUIDString(text)
				case "IconID":
					e.IconID = int(parseInt(text, 0))
				case "CustomIconUUID":
					e.CustomIconUUID = parseUUIDString(text)
				case "ForegroundColor":
					e.ForegroundColor = text
				case "BackgroundColor":
					e.BackgroundColor = text
				case "OverrideURL":
					e.OverrideURL = text
				case "Tags":
					e.Tags = splitTags(text)
				case "PreviousParentGroup":
					e.PreviousParentGroup = parseUUIDString(text)
				case "QualityCheck":
					e.QualityCheck = parseNullableBool(text)
				}
			}
		}
	}
}

func (r *xmlReader) parseString() (StringField, error) {
	field := StringField{}
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return field, fmt.Errorf("kdbx: invalid <String>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "String" {
				return field, nil
			}
		case xml.StartElement:
			switch t.Name.Local {
			case "Key":
				text, err := r.text(t)
				if err != nil {
					return field, err
				}
				field.Key = text
			case "Value":
				protected := false
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "ProtectInMemory", "Protected":
						protected = parseBool(attr.Value, false)
					}
				}
				text, err := r.text(t)
				if err != nil {
					return field, err
				}
				if protected {
					raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
					if err != nil {
						return field, fmt.Errorf("kdbx: protected value is not valid base64: %w", err)
					}
					plain := make([]byte, len(raw))
					r.stream.XORKeyStream(plain, raw)
					field.Value = string(plain)
					field.Protected = true
				} else {
					field.Value = text
				}
			}
		}
	}
}

func (r *xmlReader) parseBinaryRef() (BinaryRef, error) {
	var ref BinaryRef
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return ref, fmt.Errorf("kdbx: invalid <Binary>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Binary" {
				return ref, nil
			}
		case xml.StartElement:
			switch t.Name.Local {
			case "Key":
				text, err := r.text(t)
				if err != nil {
					return ref, err
				}
				ref.Key = text
			case "Value":
				for _, attr := range t.Attr {
					if attr.Name.Local == "Ref" {
						ref.Pool = int(parseInt(attr.Value, -1))
					}
				}
				if _, err := r.text(t); err != nil {
					return ref, err
				}
			}
		}
	}
}

func (r *xmlReader) parseTimes(times *Times) error {
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return fmt.Errorf("kdbx: invalid <Times>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Times" {
				return nil
			}
		case xml.StartElement:
			text, err := r.text(t)
			if err != nil {
				return err
			}
			switch t.Name.Local {
			case "CreationTime":
				times.CreationTime = ParseTime(text)
			case "LastModificationTime":
				times.LastModification = ParseTime(text)
			case "LastAccessTime":
				times.LastAccessTime = ParseTime(text)
			case "ExpiryTime":
				times.ExpiryTime = ParseTime(text)
			case "LocationChanged":
				times.LocationChanged = ParseTime(text)
			case "Expires":
				times.Expires = parseBool(text, false)
			case "UsageCount":
				times.UsageCount = uint64(parseInt(text, 0))
			}
		}
	}
}

func (r *xmlReader) parseAutoType(at *AutoType) error {
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return fmt.Errorf("kdbx: invalid <AutoType>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "AutoType" {
				return nil
			}
		case xml.StartElement:
			switch t.Name.Local {
			case "Association":
				assoc, err := r.parseAutoTypeAssociation()
				if err != nil {
					return err
				}
				at.Associations = append(at.Associations, assoc)
			default:
				text, err := r.text(t)
				if err != nil {
					return err
				}
				switch t.Name.Local {
				case "Enabled":
					at.Enabled = parseBool(text, true)
				case "DataTransferObfuscation":
					at.DataTransferObfuscation = int(parseInt(text, 0))
				case "DefaultSequence":
					at.DefaultSequence = text
				}
			}
		}
	}
}

func (r *xmlReader) parseAutoTypeAssociation() (AutoTypeAssociation, error) {
	var assoc AutoTypeAssociation
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return assoc, fmt.Errorf("kdbx: invalid <Association>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "Association" {
				return assoc, nil
			}
		case xml.StartElement:
			text, err := r.text(t)
			if err != nil {
				return assoc, err
			}
			switch t.Name.Local {
			case "Window":
				assoc.WindowName = text
			case "KeystrokeSequence":
				assoc.Sequence = text
			}
		}
	}
}

func (r *xmlReader) parseHistory() ([]*Entry, error) {
	var out []*Entry
	for {
		tok, err := r.dec.Token()
		if err != nil {
			return nil, fmt.Errorf("kdbx: invalid <History>: %w", err)
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == "History" {
				return out, nil
			}
		case xml.StartElement:
			if t.Name.Local != "Entry" {
				if err := r.dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			entry, err := r.parseEntry()
			if err != nil {
				return nil, err
			}
			out = append(out, entry)
		}
	}
}

// --- small parsing helpers ----------------------------------------------------

func parseBool(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return def
}

func parseNullableBool(s string) *bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		v := true
		return &v
	case "false", "0", "no":
		v := false
		return &v
	}
	return nil
}

func parseInt(s string, def int64) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return def
	}
	return v
}

func parseUUIDString(s string) UUID {
	u, err := ParseUUID(strings.TrimSpace(s))
	if err != nil {
		return UUID{}
	}
	return u
}

func splitTags(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
