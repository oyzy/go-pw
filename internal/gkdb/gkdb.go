// Package gkdb implements the GoKeep database format.
//
// The container is deliberately simple:
//
//	magic "GKDB" | header | nonce | payload length | AEAD ciphertext (JSON document)
//
// Everything up to (and including) the payload length is authenticated as associated
// data, so header tampering is detected, and the document itself is sealed with
// AES-256-GCM or ChaCha20-Poly1305 using a key derived by Argon2d, Argon2id or AES-KDF.
// The document inside carries the whole database: groups, entries, custom fields,
// attachments, history, tags and the recycle bin.
package gkdb

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"gokeep/internal/dbdoc"
)

// Extension is the file extension of GoKeep databases.
const Extension = ".gkdb"

// Magic is the file signature of GoKeep databases.
var Magic = [4]byte{'G', 'K', 'D', 'B'}

// FormatVersion is the current version of the container format.
const FormatVersion uint16 = 1

// Header size in bytes: magic, version, flags, cipher, KDF parameters, salt and nonce.
const headerSize = 92

// Cipher identifiers.
const (
	CipherAES256GCM        byte = 1
	CipherChaCha20Poly1305 byte = 2
)

// KDF identifiers.
const (
	KDFArgon2d  byte = 1
	KDFArgon2id byte = 2
	KDFAES      byte = 3
)

// Compression flags.
const (
	CompressionNone uint16 = 0
	CompressionGzip uint16 = 1 << 0
)

// ErrWrongKey is returned when the database cannot be decrypted.
var ErrWrongKey = errors.New("gkdb: the master key is wrong or the file is corrupted")

// ErrNotDatabase is returned when the bytes are not a GoKeep container at all, so the
// caller can tell "wrong password" and "wrong file" apart without reading the text.
var ErrNotDatabase = errors.New("gkdb: not a GoKeep database file (bad signature)")

// Options controls how a database is written.
type Options struct {
	// Cipher defaults to AES-256-GCM.
	Cipher byte
	// Compression defaults to CompressionGzip.
	Compression uint16
	// KDF defaults to Argon2id with 64 MiB of memory, two iterations and two lanes.
	KDF *dbdoc.KDFParams
	// Generator is stored in the document for information.
	Generator string
}

// DefaultOptions returns the settings used for a new GoKeep database.
func DefaultOptions() Options {
	return Options{
		Cipher:      CipherAES256GCM,
		Compression: CompressionGzip,
		KDF:         DefaultKDFParams(),
		Generator:   "GoKeep",
	}
}

// DefaultKDFParams is how the master key of a new database is derived: Argon2id with
// 64 MiB of memory, two iterations and two lanes. It costs a fraction of a second on
// current hardware and matches the parameters the Argon2 authors recommend.
func DefaultKDFParams() *dbdoc.KDFParams {
	params := dbdoc.NewKDFParams(dbdoc.KDFArgon2id)
	params.Dict.SetBytes(dbdoc.KdfParamSeed, randomSalt())
	params.Dict.SetUint64(dbdoc.KdfParamMemory, 64*1024*1024)
	params.Dict.SetUint64(dbdoc.KdfParamIterations, 2)
	params.Dict.SetUint32(dbdoc.KdfParamParallelism, 2)
	params.Dict.SetUint32(dbdoc.KdfParamVersion, 0x13)
	return params
}

// randomSalt returns the 32 random bytes a key derivation starts from.
func randomSalt() []byte {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		panic("gkdb: cannot read random bytes: " + err.Error())
	}
	return salt
}

// envelope is the plaintext document stored inside the encrypted payload.
type envelope struct {
	Format    string          `json:"format"`
	Version   int             `json:"version"`
	Generator string          `json:"generator"`
	Modified  time.Time       `json:"modified"`
	Database  *dbdoc.Database `json:"database"`
}

// IsGoKeepFile reports whether the data starts with the GoKeep signature.
func IsGoKeepFile(data []byte) bool {
	return len(data) >= len(Magic) && bytes.Equal(data[:len(Magic)], Magic[:])
}

// header is the parsed container header.
type header struct {
	version     uint16
	flags       uint16
	kdfID       byte
	cipherID    byte
	memory      uint64
	iterations  uint32
	parallelism uint32
	rounds      uint64
	argon2Ver   uint32
	salt        []byte
	nonce       []byte
	payloadLen  uint64
	raw         []byte
}

// kdfParams rebuilds the KDF parameters from the header so the shared derivation code
// in internal/dbdoc can be used.
func (h *header) kdfParams() *dbdoc.KDFParams {
	var params *dbdoc.KDFParams
	switch h.kdfID {
	case KDFArgon2d:
		params = dbdoc.NewKDFParams(dbdoc.KDFArgon2d)
	case KDFArgon2id:
		params = dbdoc.NewKDFParams(dbdoc.KDFArgon2id)
	default:
		params = dbdoc.NewKDFParams(dbdoc.KDFAES)
	}
	params.Dict.SetBytes(dbdoc.KdfParamSeed, h.salt)
	switch h.kdfID {
	case KDFAES:
		params.Dict.SetUint64(dbdoc.KdfParamRounds, h.rounds)
	default:
		params.Dict.SetUint64(dbdoc.KdfParamMemory, h.memory)
		params.Dict.SetUint64(dbdoc.KdfParamIterations, uint64(h.iterations))
		params.Dict.SetUint32(dbdoc.KdfParamParallelism, h.parallelism)
		params.Dict.SetUint32(dbdoc.KdfParamVersion, h.argon2Ver)
	}
	return params
}

func kdfIDFor(params *dbdoc.KDFParams) (byte, error) {
	switch params.UUID() {
	case dbdoc.KDFArgon2d:
		return KDFArgon2d, nil
	case dbdoc.KDFArgon2id:
		return KDFArgon2id, nil
	case dbdoc.KDFAES:
		return KDFAES, nil
	}
	return 0, fmt.Errorf("gkdb: unsupported key derivation function %s", params.UUID())
}

// Info describes a database file without decrypting it.
type Info struct {
	FormatVersion  uint16
	Cipher         string
	KDF            string
	Compression    string
	KDFMemory      uint64
	KDFIterations  uint32
	KDFParallelism uint32
	KDFRounds      uint64
}

// Inspect reads the header of a GoKeep database.
func Inspect(data []byte) (*Info, error) {
	h, err := parseHeader(data)
	if err != nil {
		return nil, err
	}
	info := &Info{
		FormatVersion:  h.version,
		KDFMemory:      h.memory,
		KDFIterations:  h.iterations,
		KDFParallelism: h.parallelism,
		KDFRounds:      h.rounds,
		Compression:    "None",
		Cipher:         "AES-256-GCM",
		KDF:            "Argon2d",
	}
	if h.flags&CompressionGzip != 0 {
		info.Compression = "GZip"
	}
	switch h.cipherID {
	case CipherChaCha20Poly1305:
		info.Cipher = "ChaCha20-Poly1305"
	}
	switch h.kdfID {
	case KDFArgon2id:
		info.KDF = "Argon2id"
	case KDFAES:
		info.KDF = "AES-KDF"
	}
	return info, nil
}

func parseHeader(data []byte) (*header, error) {
	if !IsGoKeepFile(data) {
		return nil, ErrNotDatabase
	}
	if len(data) < headerSize {
		return nil, errors.New("gkdb: truncated header")
	}
	h := &header{
		version:     binary.LittleEndian.Uint16(data[4:6]),
		flags:       binary.LittleEndian.Uint16(data[6:8]),
		kdfID:       data[8],
		cipherID:    data[9],
		memory:      binary.LittleEndian.Uint64(data[12:20]),
		iterations:  binary.LittleEndian.Uint32(data[20:24]),
		parallelism: binary.LittleEndian.Uint32(data[24:28]),
		rounds:      binary.LittleEndian.Uint64(data[28:36]),
		argon2Ver:   binary.LittleEndian.Uint32(data[36:40]),
		salt:        append([]byte(nil), data[40:72]...),
		nonce:       append([]byte(nil), data[72:84]...),
		payloadLen:  binary.LittleEndian.Uint64(data[84:92]),
		raw:         append([]byte(nil), data[:headerSize]...),
	}
	if h.version > FormatVersion {
		return nil, fmt.Errorf("gkdb: file version %d is newer than this application supports", h.version)
	}
	if h.cipherID != CipherAES256GCM && h.cipherID != CipherChaCha20Poly1305 {
		return nil, fmt.Errorf("gkdb: unsupported cipher %d", h.cipherID)
	}
	if h.flags & ^CompressionGzip != 0 {
		return nil, fmt.Errorf("gkdb: unsupported flags 0x%04x", h.flags)
	}
	return h, nil
}

// newAEAD returns the authenticated cipher for the header.
func (h *header) newAEAD(key []byte) (cipher.AEAD, error) {
	switch h.cipherID {
	case CipherAES256GCM:
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	case CipherChaCha20Poly1305:
		return chacha20poly1305.New(key)
	}
	return nil, fmt.Errorf("gkdb: unsupported cipher %d", h.cipherID)
}

// Save serialises and encrypts a database.
func Save(db *dbdoc.Database, key dbdoc.Key, opts Options) ([]byte, error) {
	if opts.Cipher == 0 {
		opts.Cipher = CipherAES256GCM
	}
	if opts.KDF == nil {
		opts.KDF = dbdoc.DefaultKDFParams()
	}
	if err := opts.KDF.Validate(); err != nil {
		return nil, err
	}
	kdfID, err := kdfIDFor(opts.KDF)
	if err != nil {
		return nil, err
	}

	document := envelope{
		Format:    "gokeep",
		Version:   int(FormatVersion),
		Generator: opts.Generator,
		Modified:  time.Now().UTC().Truncate(time.Second),
		Database:  db,
	}
	plain, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("gkdb: cannot serialise the database: %w", err)
	}
	payload := plain
	flags := CompressionNone
	if opts.Compression&CompressionGzip != 0 {
		compressed, err := gzipCompress(plain)
		if err != nil {
			return nil, err
		}
		if len(compressed) < len(plain) {
			payload = compressed
			flags = CompressionGzip
		}
	}

	salt := make([]byte, 32)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	h := &header{
		version:    FormatVersion,
		flags:      flags,
		kdfID:      kdfID,
		cipherID:   opts.Cipher,
		salt:       salt,
		nonce:      nonce,
		payloadLen: 0,
	}
	switch kdfID {
	case KDFAES:
		h.rounds = opts.KDF.Dict.GetUint64(dbdoc.KdfParamRounds, 60000)
	default:
		h.memory = opts.KDF.Dict.GetUint64(dbdoc.KdfParamMemory, 64*1024*1024)
		h.iterations = uint32(opts.KDF.Dict.GetUint64(dbdoc.KdfParamIterations, 2))
		h.parallelism = opts.KDF.Dict.GetUint32(dbdoc.KdfParamParallelism, 2)
		h.argon2Ver = opts.KDF.Dict.GetUint32(dbdoc.KdfParamVersion, 0x13)
	}

	composite, err := key.CompositeKey()
	if err != nil {
		return nil, err
	}
	// The key is derived from the parameters exactly as they appear in the header, so
	// the loader can rebuild them from the file alone (the header salt is the KDF salt).
	transformed, err := h.kdfParams().Transform(composite)
	if err != nil {
		return nil, err
	}
	aead, err := h.newAEAD(transformed[:])
	if err != nil {
		return nil, err
	}

	h.raw = encodeHeader(h)
	binary.LittleEndian.PutUint64(h.raw[84:92], uint64(len(payload)+aead.Overhead()))

	sealed := aead.Seal(nil, nonce, payload, h.raw)
	out := make([]byte, 0, len(h.raw)+len(sealed))
	out = append(out, h.raw...)
	out = append(out, sealed...)
	return out, nil
}

// Load decrypts and parses a GoKeep database.
func Load(data []byte, key dbdoc.Key) (*dbdoc.Database, error) {
	h, err := parseHeader(data)
	if err != nil {
		return nil, err
	}
	if len(h.raw)+int(h.payloadLen) != len(data) {
		return nil, errors.New("gkdb: the file length does not match the header (file is truncated or padded)")
	}
	composite, err := key.CompositeKey()
	if err != nil {
		return nil, err
	}
	transformed, err := h.kdfParams().Transform(composite)
	if err != nil {
		return nil, err
	}
	aead, err := h.newAEAD(transformed[:])
	if err != nil {
		return nil, err
	}
	payload, err := aead.Open(nil, h.nonce, data[len(h.raw):], h.raw)
	if err != nil {
		return nil, ErrWrongKey
	}
	if h.flags&CompressionGzip != 0 {
		payload, err = gzipDecompress(payload)
		if err != nil {
			return nil, fmt.Errorf("gkdb: cannot decompress the payload: %w", err)
		}
	}
	var document envelope
	if err := json.Unmarshal(payload, &document); err != nil {
		return nil, fmt.Errorf("gkdb: cannot parse the database document: %w", err)
	}
	if document.Database == nil {
		return nil, errors.New("gkdb: the document does not contain a database")
	}
	if document.Format != "" && document.Format != "gokeep" {
		return nil, fmt.Errorf("gkdb: unexpected document format %q", document.Format)
	}
	if document.Generator != "" {
		document.Database.Meta.Generator = document.Generator
	}
	return document.Database, nil
}

// SaveFile writes a database to disk.
func SaveFile(path string, db *dbdoc.Database, key dbdoc.Key, opts Options) error {
	data, err := Save(db, key, opts)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// LoadFile reads a database from disk.
func LoadFile(path string, key dbdoc.Key) (*dbdoc.Database, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return Load(data, key)
}

func encodeHeader(h *header) []byte {
	out := make([]byte, headerSize)
	copy(out[0:4], Magic[:])
	binary.LittleEndian.PutUint16(out[4:6], h.version)
	binary.LittleEndian.PutUint16(out[6:8], h.flags)
	out[8] = h.kdfID
	out[9] = h.cipherID
	binary.LittleEndian.PutUint64(out[12:20], h.memory)
	binary.LittleEndian.PutUint32(out[20:24], h.iterations)
	binary.LittleEndian.PutUint32(out[24:28], h.parallelism)
	binary.LittleEndian.PutUint64(out[28:36], h.rounds)
	binary.LittleEndian.PutUint32(out[36:40], h.argon2Ver)
	copy(out[40:72], h.salt)
	copy(out[72:84], h.nonce)
	binary.LittleEndian.PutUint64(out[84:92], h.payloadLen)
	return out
}

func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipDecompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
