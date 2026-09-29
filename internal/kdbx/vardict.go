package kdbx

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// Variant dictionary value types, as defined by the KeePass KDBX 4 specification.
const (
	vdEnd    byte = 0x00
	vdUint32 byte = 0x04
	vdUint64 byte = 0x05
	vdBool   byte = 0x08
	vdInt32  byte = 0x0C
	vdInt64  byte = 0x0D
	vdString byte = 0x18
	vdBytes  byte = 0x42
)

// VariantDict is the typed key/value container KeePass uses for KDF parameters and
// public custom data.
type VariantDict struct {
	Version uint16
	keys    []string
	values  map[string]any
	types   map[string]byte
}

// NewVariantDict creates an empty dictionary with the current version (0x0100).
func NewVariantDict() *VariantDict {
	return &VariantDict{
		Version: 0x0100,
		values:  map[string]any{},
		types:   map[string]byte{},
	}
}

func (d *VariantDict) set(key string, value any, typ byte) {
	if d.values == nil {
		d.values = map[string]any{}
		d.types = map[string]byte{}
	}
	if _, ok := d.values[key]; !ok {
		d.keys = append(d.keys, key)
	}
	d.values[key] = value
	d.types[key] = typ
}

// SetBytes stores a byte array.
func (d *VariantDict) SetBytes(key string, v []byte) {
	d.set(key, append([]byte(nil), v...), vdBytes)
}

// SetUint32 stores a 32 bit unsigned integer.
func (d *VariantDict) SetUint32(key string, v uint32) { d.set(key, v, vdUint32) }

// SetUint64 stores a 64 bit unsigned integer.
func (d *VariantDict) SetUint64(key string, v uint64) { d.set(key, v, vdUint64) }

// SetBool stores a boolean.
func (d *VariantDict) SetBool(key string, v bool) { d.set(key, v, vdBool) }

// SetString stores a UTF-8 string.
func (d *VariantDict) SetString(key string, v string) { d.set(key, v, vdString) }

// Remove deletes a key.
func (d *VariantDict) Remove(key string) {
	if _, ok := d.values[key]; !ok {
		return
	}
	delete(d.values, key)
	delete(d.types, key)
	for i, k := range d.keys {
		if k == key {
			d.keys = append(d.keys[:i], d.keys[i+1:]...)
			break
		}
	}
}

// Has reports whether the key exists.
func (d *VariantDict) Has(key string) bool {
	_, ok := d.values[key]
	return ok
}

// Keys returns the keys in insertion order.
func (d *VariantDict) Keys() []string { return append([]string(nil), d.keys...) }

// GetBytes returns a byte array value.
func (d *VariantDict) GetBytes(key string) []byte {
	if v, ok := d.values[key].([]byte); ok {
		return v
	}
	return nil
}

// GetUint32 returns an unsigned 32 bit value, or def when absent.
func (d *VariantDict) GetUint32(key string, def uint32) uint32 {
	switch v := d.values[key].(type) {
	case uint32:
		return v
	case uint64:
		return uint32(v)
	}
	return def
}

// GetUint64 returns an unsigned 64 bit value, or def when absent.
func (d *VariantDict) GetUint64(key string, def uint64) uint64 {
	switch v := d.values[key].(type) {
	case uint64:
		return v
	case uint32:
		return uint64(v)
	}
	return def
}

// GetBool returns a boolean value, or def when absent.
func (d *VariantDict) GetBool(key string, def bool) bool {
	if v, ok := d.values[key].(bool); ok {
		return v
	}
	return def
}

// GetString returns a string value, or def when absent.
func (d *VariantDict) GetString(key string, def string) string {
	if v, ok := d.values[key].(string); ok {
		return v
	}
	return def
}

// Encode serialises the dictionary.
func (d *VariantDict) Encode() []byte {
	out := make([]byte, 0, 256)
	var tmp [8]byte
	version := d.Version
	if version == 0 {
		version = 0x0100
	}
	binary.LittleEndian.PutUint16(tmp[:2], version)
	out = append(out, tmp[:2]...)
	for _, key := range d.keys {
		typ := d.types[key]
		out = append(out, typ)
		binary.LittleEndian.PutUint32(tmp[:4], uint32(len(key)))
		out = append(out, tmp[:4]...)
		out = append(out, key...)
		var payload []byte
		switch typ {
		case vdUint32:
			binary.LittleEndian.PutUint32(tmp[:4], d.values[key].(uint32))
			payload = append([]byte(nil), tmp[:4]...)
		case vdUint64:
			binary.LittleEndian.PutUint64(tmp[:8], d.values[key].(uint64))
			payload = append([]byte(nil), tmp[:8]...)
		case vdBool:
			if d.values[key].(bool) {
				payload = []byte{1}
			} else {
				payload = []byte{0}
			}
		case vdInt32:
			binary.LittleEndian.PutUint32(tmp[:4], uint32(d.values[key].(int32)))
			payload = append([]byte(nil), tmp[:4]...)
		case vdInt64:
			binary.LittleEndian.PutUint64(tmp[:8], uint64(d.values[key].(int64)))
			payload = append([]byte(nil), tmp[:8]...)
		case vdString:
			payload = []byte(d.values[key].(string))
		case vdBytes:
			payload = d.values[key].([]byte)
		}
		binary.LittleEndian.PutUint32(tmp[:4], uint32(len(payload)))
		out = append(out, tmp[:4]...)
		out = append(out, payload...)
	}
	out = append(out, vdEnd)
	return out
}

// ParseVariantDict parses a serialised variant dictionary.
func ParseVariantDict(data []byte) (*VariantDict, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("kdbx: variant dictionary too short (%d bytes)", len(data))
	}
	d := &VariantDict{
		Version: binary.LittleEndian.Uint16(data[:2]),
		values:  map[string]any{},
		types:   map[string]byte{},
	}
	pos := 2
	for {
		if pos >= len(data) {
			return nil, fmt.Errorf("kdbx: variant dictionary is not terminated")
		}
		typ := data[pos]
		pos++
		if typ == vdEnd {
			return d, nil
		}
		if pos+4 > len(data) {
			return nil, fmt.Errorf("kdbx: truncated variant dictionary key length")
		}
		keyLen := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
		pos += 4
		if keyLen < 0 || pos+keyLen > len(data) {
			return nil, fmt.Errorf("kdbx: truncated variant dictionary key")
		}
		key := string(data[pos : pos+keyLen])
		pos += keyLen
		if !utf8.ValidString(key) {
			return nil, fmt.Errorf("kdbx: variant dictionary key is not valid UTF-8")
		}
		if pos+4 > len(data) {
			return nil, fmt.Errorf("kdbx: truncated variant dictionary value length")
		}
		valueLen := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
		pos += 4
		if valueLen < 0 || pos+valueLen > len(data) {
			return nil, fmt.Errorf("kdbx: truncated variant dictionary value")
		}
		value := data[pos : pos+valueLen]
		pos += valueLen

		switch typ {
		case vdUint32:
			if valueLen != 4 {
				return nil, fmt.Errorf("kdbx: uint32 variant must be 4 bytes, got %d", valueLen)
			}
			d.SetUint32(key, binary.LittleEndian.Uint32(value))
		case vdUint64:
			if valueLen != 8 {
				return nil, fmt.Errorf("kdbx: uint64 variant must be 8 bytes, got %d", valueLen)
			}
			d.SetUint64(key, binary.LittleEndian.Uint64(value))
		case vdBool:
			if valueLen != 1 {
				return nil, fmt.Errorf("kdbx: bool variant must be 1 byte, got %d", valueLen)
			}
			d.SetBool(key, value[0] != 0)
		case vdInt32:
			if valueLen != 4 {
				return nil, fmt.Errorf("kdbx: int32 variant must be 4 bytes, got %d", valueLen)
			}
			d.set(key, int32(binary.LittleEndian.Uint32(value)), vdInt32)
		case vdInt64:
			if valueLen != 8 {
				return nil, fmt.Errorf("kdbx: int64 variant must be 8 bytes, got %d", valueLen)
			}
			d.set(key, int64(binary.LittleEndian.Uint64(value)), vdInt64)
		case vdString:
			d.SetString(key, string(value))
		case vdBytes:
			d.SetBytes(key, value)
		default:
			return nil, fmt.Errorf("kdbx: unsupported variant dictionary value type 0x%02x", typ)
		}
	}
}
