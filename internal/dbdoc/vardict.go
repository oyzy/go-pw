package dbdoc

// VariantDict is the typed key/value container a database uses for its key derivation
// parameters: every parameter has a name ("M" for memory, "I" for iterations, ...) and a
// type, and the container keeps the insertion order so a settings dialog reads the same
// way every time. It never touches a disk format of its own; the container header holds
// the parameters, and internal/gkdb copies them in and out.
type VariantDict struct {
	keys   []string
	values map[string]any
}

// NewVariantDict creates an empty dictionary.
func NewVariantDict() *VariantDict {
	return &VariantDict{values: map[string]any{}}
}

func (d *VariantDict) set(key string, value any) {
	if d.values == nil {
		d.values = map[string]any{}
	}
	if _, ok := d.values[key]; !ok {
		d.keys = append(d.keys, key)
	}
	d.values[key] = value
}

// SetBytes stores a byte array.
func (d *VariantDict) SetBytes(key string, v []byte) {
	d.set(key, append([]byte(nil), v...))
}

// SetUint32 stores a 32 bit unsigned integer.
func (d *VariantDict) SetUint32(key string, v uint32) { d.set(key, v) }

// SetUint64 stores a 64 bit unsigned integer.
func (d *VariantDict) SetUint64(key string, v uint64) { d.set(key, v) }

// SetBool stores a boolean.
func (d *VariantDict) SetBool(key string, v bool) { d.set(key, v) }

// SetString stores a UTF-8 string.
func (d *VariantDict) SetString(key string, v string) { d.set(key, v) }

// Remove deletes a key.
func (d *VariantDict) Remove(key string) {
	if _, ok := d.values[key]; !ok {
		return
	}
	delete(d.values, key)
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
