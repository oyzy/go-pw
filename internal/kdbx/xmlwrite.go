package kdbx

import (
	"bytes"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// xmlWriter serialises a database the way KeePass 2 does, including the pretty printed
// layout, so that the output is diffable and familiar to KeePass users.
type xmlWriter struct {
	buf      bytes.Buffer
	stream   randomStream
	version  uint32
	depth    int
	binaries []Binary
}

func newXMLWriter(version uint32, stream randomStream, binaries []Binary) *xmlWriter {
	return &xmlWriter{stream: stream, version: version, binaries: binaries}
}

func (w *xmlWriter) is41() bool { return w.version >= FileVersion41 }
func (w *xmlWriter) is4() bool  { return w.version >= FileVersion4 }

func (w *xmlWriter) raw(s string) { w.buf.WriteString(s) }

func (w *xmlWriter) indent() {
	for i := 0; i < w.depth; i++ {
		w.buf.WriteByte('\t')
	}
}

func (w *xmlWriter) open(name string, attrs ...[2]string) {
	w.indent()
	w.buf.WriteByte('<')
	w.buf.WriteString(name)
	for _, a := range attrs {
		w.buf.WriteByte(' ')
		w.buf.WriteString(a[0])
		w.buf.WriteString("=\"")
		w.buf.WriteString(escapeAttr(a[1]))
		w.buf.WriteByte('"')
	}
	w.buf.WriteString(">\r\n")
	w.depth++
}

func (w *xmlWriter) close(name string) {
	w.depth--
	w.indent()
	w.buf.WriteString("</")
	w.buf.WriteString(name)
	w.buf.WriteString(">\r\n")
}

func (w *xmlWriter) empty(name string, attrs ...[2]string) {
	w.indent()
	w.buf.WriteByte('<')
	w.buf.WriteString(name)
	for _, a := range attrs {
		w.buf.WriteByte(' ')
		w.buf.WriteString(a[0])
		w.buf.WriteString("=\"")
		w.buf.WriteString(escapeAttr(a[1]))
		w.buf.WriteByte('"')
	}
	w.buf.WriteString(" />\r\n")
}

func (w *xmlWriter) leaf(name, value string) {
	w.indent()
	w.buf.WriteByte('<')
	w.buf.WriteString(name)
	w.buf.WriteByte('>')
	w.buf.WriteString(escapeText(value))
	w.buf.WriteString("</")
	w.buf.WriteString(name)
	w.buf.WriteString(">\r\n")
}

func (w *xmlWriter) leafInt(name string, value int64) {
	w.leaf(name, strconv.FormatInt(value, 10))
}

func (w *xmlWriter) leafBool(name string, value bool) {
	if value {
		w.leaf(name, "True")
		return
	}
	w.leaf(name, "False")
}

// leafTime writes a timestamp using the representation of the target file version.
func (w *xmlWriter) leafTime(name string, t time.Time) {
	if !w.is4() {
		w.leaf(name, FormatTimeISO(t))
		return
	}
	w.leaf(name, base64.StdEncoding.EncodeToString(encodeTime(t)))
}

// leafProtected writes a memory protected value using the inner random stream.
func (w *xmlWriter) leafProtected(name, value string, protected bool) {
	if !protected {
		w.leaf(name, value)
		return
	}
	plain := []byte(value)
	enc := make([]byte, len(plain))
	w.stream.XORKeyStream(enc, plain)
	// KeePass writes "Protected" in encrypted documents and only uses
	// "ProtectInMemory" in unencrypted XML exports, so both readers accept either.
	w.leafWithAttr(name, "Protected", "True", base64.StdEncoding.EncodeToString(enc))
}

func (w *xmlWriter) leafWithAttr(name string, attrKey, attrValue, value string) {
	w.indent()
	w.buf.WriteByte('<')
	w.buf.WriteString(name)
	w.buf.WriteByte(' ')
	w.buf.WriteString(attrKey)
	w.buf.WriteString("=\"")
	w.buf.WriteString(escapeAttr(attrValue))
	w.buf.WriteString("\">")
	w.buf.WriteString(escapeText(value))
	w.buf.WriteString("</")
	w.buf.WriteString(name)
	w.buf.WriteString(">\r\n")
}

// escapeText escapes XML text so that the document survives a round trip through any
// conforming parser: carriage returns become character references, which also stops
// parsers from normalising them away.
func escapeText(s string) string {
	var b strings.Builder
	for _, r := range sanitizeRunes(s) {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '\r':
			b.WriteString("&#xD;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func escapeAttr(s string) string {
	var b strings.Builder
	for _, r := range sanitizeRunes(s) {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\r':
			b.WriteString("&#xD;")
		case '\n':
			b.WriteString("&#xA;")
		case '\t':
			b.WriteString("&#x9;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeRunes drops characters that are not allowed in XML 1.0 documents; KeePass
// does the same, otherwise a stray control character would corrupt the whole file.
func sanitizeRunes(s string) string {
	if utf8.ValidString(s) {
		ok := true
		for _, r := range s {
			if !validXMLRune(r) {
				ok = false
				break
			}
		}
		if ok {
			return s
		}
	}
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "") {
		if validXMLRune(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validXMLRune(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		return true
	case r >= 0x20 && r <= 0xD7FF:
		return true
	case r >= 0xE000 && r <= 0xFFFD:
		return true
	case r >= 0x10000 && r <= 0x10FFFF:
		return true
	}
	return false
}

// writeDocument serialises the whole database.
func (w *xmlWriter) writeDocument(db *Database) []byte {
	w.raw("<?xml version=\"1.0\" encoding=\"utf-8\" standalone=\"yes\"?>\r\n")
	w.raw("<KeePassFile>\r\n")
	w.depth = 1
	w.writeMeta(&db.Meta)
	w.writeRoot(db)
	w.depth = 0
	w.raw("</KeePassFile>\r\n")
	return w.buf.Bytes()
}

func (w *xmlWriter) writeMeta(m *Meta) {
	w.open("Meta")
	w.leaf("Generator", m.Generator)
	if !w.is4() && len(m.HeaderHash) > 0 {
		w.leaf("HeaderHash", base64.StdEncoding.EncodeToString(m.HeaderHash))
	}
	if w.is41() {
		w.leafTime("SettingsChanged", m.SettingsChanged)
	}
	w.leaf("DatabaseName", m.DatabaseName)
	w.leafTime("DatabaseNameChanged", m.DatabaseNameChanged)
	w.leaf("DatabaseDescription", m.DatabaseDescription)
	w.leafTime("DatabaseDescriptionChanged", m.DatabaseDescriptionChanged)
	w.leaf("DefaultUserName", m.DefaultUserName)
	w.leafTime("DefaultUserNameChanged", m.DefaultUserNameChanged)
	w.leafInt("MaintenanceHistoryDays", int64(m.MaintenanceHistoryDays))
	w.leaf("Color", m.Color)
	w.leafTime("MasterKeyChanged", m.MasterKeyChanged)
	w.leafInt("MasterKeyChangeRec", m.MasterKeyChangeRec)
	w.leafInt("MasterKeyChangeForce", m.MasterKeyChangeForce)

	w.open("MemoryProtection")
	w.leafBool("ProtectTitle", m.MemoryProtection.ProtectTitle)
	w.leafBool("ProtectUserName", m.MemoryProtection.ProtectUserName)
	w.leafBool("ProtectPassword", m.MemoryProtection.ProtectPassword)
	w.leafBool("ProtectURL", m.MemoryProtection.ProtectURL)
	w.leafBool("ProtectNotes", m.MemoryProtection.ProtectNotes)
	w.close("MemoryProtection")

	if len(m.CustomIcons) == 0 {
		w.empty("CustomIcons")
	} else {
		w.open("CustomIcons")
		for _, icon := range m.CustomIcons {
			w.open("Icon")
			w.leaf("UUID", icon.UUID.Base64())
			w.leaf("Data", base64.StdEncoding.EncodeToString(icon.Data))
			if w.is41() {
				w.leaf("Name", icon.Name)
				w.leafTime("LastModificationTime", icon.LastModificationTime)
			}
			w.close("Icon")
		}
		w.close("CustomIcons")
	}

	w.leafBool("RecycleBinEnabled", m.RecycleBinEnabled)
	w.leaf("RecycleBinUUID", m.RecycleBinUUID.Base64())
	w.leafTime("RecycleBinChanged", m.RecycleBinChanged)
	w.leaf("EntryTemplatesGroup", m.EntryTemplatesGroup.Base64())
	w.leafTime("EntryTemplatesGroupChanged", m.EntryTemplatesGroupChanged)
	w.leafInt("HistoryMaxItems", int64(m.HistoryMaxItems))
	w.leafInt("HistoryMaxSize", m.HistoryMaxSize)
	w.leaf("LastSelectedGroup", m.LastSelectedGroup.Base64())
	w.leaf("LastTopVisibleGroup", m.LastTopVisibleGroup.Base64())

	// KDBX 3.1 keeps the attachment pool inside the XML; KDBX 4 stores it in the
	// encrypted inner header instead.
	if !w.is4() && len(w.binaries) > 0 {
		w.open("Binaries")
		for i, bin := range w.binaries {
			compressed, payload := compressBinary(bin.Data)
			attrs := [][2]string{{"ID", strconv.Itoa(i)}}
			if compressed {
				attrs = append(attrs, [2]string{"Compressed", "True"})
			}
			w.leafWithAttrs("Binary", attrs, base64.StdEncoding.EncodeToString(payload))
		}
		w.close("Binaries")
	}
	w.writeCustomData(m.CustomData)
	w.close("Meta")
}

func (w *xmlWriter) leafWithAttrs(name string, attrs [][2]string, value string) {
	w.indent()
	w.buf.WriteByte('<')
	w.buf.WriteString(name)
	for _, a := range attrs {
		w.buf.WriteByte(' ')
		w.buf.WriteString(a[0])
		w.buf.WriteString("=\"")
		w.buf.WriteString(escapeAttr(a[1]))
		w.buf.WriteByte('"')
	}
	w.buf.WriteByte('>')
	w.buf.WriteString(escapeText(value))
	w.buf.WriteString("</")
	w.buf.WriteString(name)
	w.buf.WriteString(">\r\n")
}

func (w *xmlWriter) writeCustomData(items []CustomDataItem) {
	if len(items) == 0 {
		w.empty("CustomData")
		return
	}
	w.open("CustomData")
	for _, item := range items {
		w.open("Item")
		w.leaf("Key", item.Key)
		w.leaf("Value", item.Value)
		w.close("Item")
	}
	w.close("CustomData")
}

func (w *xmlWriter) writeRoot(db *Database) {
	w.open("Root")
	w.writeGroup(db.Root)
	if len(db.DeletedObjects) == 0 {
		w.empty("DeletedObjects")
	} else {
		w.open("DeletedObjects")
		for _, obj := range db.DeletedObjects {
			w.open("DeletedObject")
			w.leaf("UUID", obj.UUID.Base64())
			w.leafTime("DeletionTime", obj.DeletionTime)
			w.close("DeletedObject")
		}
		w.close("DeletedObjects")
	}
	w.close("Root")
}

func (w *xmlWriter) writeGroup(g *Group) {
	w.open("Group")
	w.leaf("UUID", g.UUID.Base64())
	w.leaf("Name", g.Name)
	w.leaf("Notes", g.Notes)
	w.leafInt("IconID", int64(g.IconID))
	if !g.CustomIconUUID.IsZero() {
		w.leaf("CustomIconUUID", g.CustomIconUUID.Base64())
	}
	w.writeTimes(&g.Times)
	if w.is41() {
		w.writeTags(g.Tags)
		if !g.PreviousParentGroup.IsZero() {
			w.leaf("PreviousParentGroup", g.PreviousParentGroup.Base64())
		}
	}
	w.leafBool("IsExpanded", g.IsExpanded)
	w.leaf("DefaultAutoTypeSequence", g.DefaultAutoTypeSequence)
	w.leaf("EnableAutoType", nullableBool(g.EnableAutoType))
	w.leaf("EnableSearching", nullableBool(g.EnableSearching))
	w.leaf("LastTopVisibleEntry", g.LastTopVisibleEntry.Base64())
	if w.is41() {
		w.writeCustomData(g.CustomData)
	}
	for _, e := range g.Entries {
		w.writeEntry(e)
	}
	for _, sub := range g.Groups {
		w.writeGroup(sub)
	}
	w.close("Group")
}

func (w *xmlWriter) writeTimes(t *Times) {
	w.open("Times")
	w.leafTime("CreationTime", t.CreationTime)
	w.leafTime("LastModificationTime", t.LastModification)
	w.leafTime("LastAccessTime", t.LastAccessTime)
	w.leafTime("ExpiryTime", t.ExpiryTime)
	w.leafBool("Expires", t.Expires)
	w.leafInt("UsageCount", int64(t.UsageCount))
	w.leafTime("LocationChanged", t.LocationChanged)
	w.close("Times")
}

func (w *xmlWriter) writeTags(tags []string) {
	w.leaf("Tags", strings.Join(tags, ";"))
}

func (w *xmlWriter) writeEntry(e *Entry) {
	w.open("Entry")
	w.leaf("UUID", e.UUID.Base64())
	w.leafInt("IconID", int64(e.IconID))
	if !e.CustomIconUUID.IsZero() {
		w.leaf("CustomIconUUID", e.CustomIconUUID.Base64())
	}
	w.leaf("ForegroundColor", e.ForegroundColor)
	w.leaf("BackgroundColor", e.BackgroundColor)
	w.leaf("OverrideURL", e.OverrideURL)
	w.writeTags(e.Tags)
	if w.is41() {
		if !e.PreviousParentGroup.IsZero() {
			w.leaf("PreviousParentGroup", e.PreviousParentGroup.Base64())
		}
		if e.QualityCheck != nil {
			w.leafBool("QualityCheck", *e.QualityCheck)
		}
	}
	w.writeTimes(&e.Times)

	fields := append([]StringField(nil), e.Strings...)
	sortStringFields(fields)
	for _, f := range fields {
		w.open("String")
		w.leaf("Key", f.Key)
		w.leafProtected("Value", f.Value, f.Protected)
		w.close("String")
	}
	for _, ref := range e.Binaries {
		w.open("Binary")
		w.leaf("Key", ref.Key)
		w.empty("Value", [2]string{"Ref", strconv.Itoa(ref.Pool)})
		w.close("Binary")
	}
	w.writeAutoType(&e.AutoType)
	if len(e.History) == 0 {
		w.empty("History")
	} else {
		w.open("History")
		for _, h := range e.History {
			w.writeEntry(h)
		}
		w.close("History")
	}
	if w.is41() {
		w.writeCustomData(e.CustomData)
	}
	w.close("Entry")
}

func (w *xmlWriter) writeAutoType(a *AutoType) {
	w.open("AutoType")
	w.leafBool("Enabled", a.Enabled)
	w.leafInt("DataTransferObfuscation", int64(a.DataTransferObfuscation))
	if a.DefaultSequence != "" {
		w.leaf("DefaultSequence", a.DefaultSequence)
	}
	for _, assoc := range a.Associations {
		w.open("Association")
		w.leaf("Window", assoc.WindowName)
		w.leaf("KeystrokeSequence", assoc.Sequence)
		w.close("Association")
	}
	w.close("AutoType")
}

func nullableBool(v *bool) string {
	if v == nil {
		return "null"
	}
	if *v {
		return "True"
	}
	return "False"
}

// sortStringFields orders fields the way KeePass does: case insensitive by key, which
// puts the standard fields in their familiar places.
func sortStringFields(fields []StringField) {
	for i := 1; i < len(fields); i++ {
		for j := i; j > 0; j-- {
			if lessFold(fields[j].Key, fields[j-1].Key) {
				fields[j], fields[j-1] = fields[j-1], fields[j]
				continue
			}
			break
		}
	}
}

func lessFold(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la == lb {
		return a < b
	}
	return la < lb
}

// compressBinary gzips a binary when that makes it smaller, mirroring the "Compressed"
// attribute of KDBX 3.1 attachment pools.
func compressBinary(data []byte) (bool, []byte) {
	if len(data) < 64 {
		return false, data
	}
	compressed := gzipCompress(data)
	if len(compressed) >= len(data) {
		return false, data
	}
	return true, compressed
}

// innerHeaderBytes builds the KDBX 4 inner header.
func (w *xmlWriter) innerHeaderBytes(randomStreamID uint32, key []byte, binaries []Binary) []byte {
	ih := &innerHeader{
		RandomStreamID:  randomStreamID,
		RandomStreamKey: key,
		Binaries:        binaries,
	}
	return ih.write()
}
