package kdbx

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrWrongKey is returned when a database cannot be decrypted with the supplied
// credentials.
var ErrWrongKey = errors.New("kdbx: could not open the database: the master key is wrong or the file is corrupted")

// SaveOptions controls how a database is written.
type SaveOptions struct {
	// Version selects the KDBX file version; defaults to KDBX 4.1 like KeePass 2.60.
	Version uint32
	// Cipher defaults to AES-256.
	Cipher UUID
	// Compression is CompressionGzip (default) or CompressionNone.
	Compression *uint32
	// KDF defaults to Argon2d with KeePass defaults for new databases.
	KDF *KDFParams
	// RandomStreamID defaults to ChaCha20, the algorithm KeePass writes for KDBX 4.
	RandomStreamID *uint32
}

// LoadFile opens a database file.
func LoadFile(path string, key Key) (*Database, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Load(data, key)
}

// Load decrypts and parses a database from memory.
func Load(data []byte, key Key) (*Database, error) {
	reader := bytes.NewReader(data)
	h, err := readHeader(reader)
	if err != nil {
		return nil, err
	}
	composite, err := key.CompositeKey()
	if err != nil {
		return nil, err
	}
	transformedKey, err := transformKey(h, composite)
	transformed := transformedKey[:]
	if err != nil {
		return nil, err
	}
	cipherKey, nonce := deriveCipherKey(h.CipherID, h.MasterSeed, transformed)
	if h.CipherID == CipherChaCha20 {
		// The ChaCha20 nonce is the encryption IV stored in the header.
		nonce = h.IV
	}

	var (
		payload         []byte
		randomStreamID  uint32
		randomStreamKey []byte
		innerBinaries   []Binary
	)

	if h.IsV4() {
		payload, randomStreamID, randomStreamKey, innerBinaries, err = readV4(reader, h, transformed, cipherKey, nonce)
	} else {
		payload, randomStreamID, randomStreamKey, err = readV3(reader, h, cipherKey, nonce)
	}
	if err != nil {
		return nil, err
	}

	stream, err := newRandomStream(randomStreamID, randomStreamKey)
	if err != nil {
		return nil, err
	}
	db, err := parseDatabaseXML(payload, stream)
	if err != nil {
		return nil, err
	}

	// KDBX 3.1 stores the header digest inside the encrypted XML.
	if !h.IsV4() && len(db.Meta.HeaderHash) > 0 {
		if sum := h.sha256Hash(); !bytes.Equal(sum, db.Meta.HeaderHash) {
			return nil, errors.New("kdbx: the header was modified (HeaderHash mismatch)")
		}
	}
	db.Meta.HeaderHash = nil
	if len(innerBinaries) > 0 {
		db.Binaries = innerBinaries
	}
	if db.Meta.Generator == "" {
		db.Meta.Generator = "GoKeep"
	}
	return db, nil
}

// readV4 verifies the header integrity information and decrypts a KDBX 4 payload.
func readV4(reader *bytes.Reader, h *header, transformed, cipherKey, nonce []byte) ([]byte, uint32, []byte, []Binary, error) {
	var storedHash [32]byte
	if _, err := reader.Read(storedHash[:]); err != nil {
		return nil, 0, nil, nil, fmt.Errorf("kdbx: cannot read header hash: %w", err)
	}
	if sum := h.sha256Hash(); !bytes.Equal(sum, storedHash[:]) {
		return nil, 0, nil, nil, errors.New("kdbx: the header was modified (SHA-256 mismatch)")
	}
	var storedHMAC [32]byte
	if _, err := reader.Read(storedHMAC[:]); err != nil {
		return nil, 0, nil, nil, fmt.Errorf("kdbx: cannot read header HMAC: %w", err)
	}
	mac := hmac.New(sha256.New, indexedHMACKey(deriveHMACBaseKey(h.MasterSeed, transformed), hmacIndexMagic))
	mac.Write(h.raw)
	if !hmac.Equal(mac.Sum(nil), storedHMAC[:]) {
		return nil, 0, nil, nil, ErrWrongKey
	}

	encrypted, err := readHMACBlockStream(reader, deriveHMACBaseKey(h.MasterSeed, transformed))
	if err != nil {
		return nil, 0, nil, nil, err
	}
	payload, err := decryptPayload(h.CipherID, cipherKey, nonce, h.IV, encrypted)
	if err != nil {
		return nil, 0, nil, nil, ErrWrongKey
	}
	if h.Compression == CompressionGzip {
		payload, err = gzipDecompress(payload)
		if err != nil {
			return nil, 0, nil, nil, ErrWrongKey
		}
	}
	ih, rest, err := splitInnerHeader(payload)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	return rest, ih.RandomStreamID, ih.RandomStreamKey, ih.Binaries, nil
}

// readV3 decrypts a KDBX 3.1 payload, which has no block integrity information.
func readV3(reader *bytes.Reader, h *header, cipherKey, nonce []byte) ([]byte, uint32, []byte, error) {
	encrypted := make([]byte, reader.Len())
	if _, err := reader.Read(encrypted); err != nil {
		return nil, 0, nil, err
	}
	plain, err := decryptPayload(h.CipherID, cipherKey, nonce, h.IV, encrypted)
	if err != nil {
		// Some producers omit the PKCS#7 padding for legacy payloads.
		plain, err = decryptPayloadNoPadding(h.CipherID, cipherKey, nonce, h.IV, encrypted)
		if err != nil {
			return nil, 0, nil, ErrWrongKey
		}
	}
	// KDBX 3.1 starts the encrypted payload with 32 random "stream start bytes"; they
	// act as an integrity check for the master key.
	if len(plain) < len(h.StreamStartBytes) {
		return nil, 0, nil, ErrWrongKey
	}
	if !bytes.Equal(plain[:len(h.StreamStartBytes)], h.StreamStartBytes) {
		return nil, 0, nil, ErrWrongKey
	}
	plain = plain[len(h.StreamStartBytes):]
	plain, err = readHashedBlockStream(plain)
	if err != nil {
		return nil, 0, nil, err
	}
	if h.Compression == CompressionGzip {
		decompressed, err := gzipDecompress(plain)
		if err != nil {
			return nil, 0, nil, ErrWrongKey
		}
		plain = decompressed
	}
	return plain, h.InnerRandomStreamID, h.ProtectedStreamKey, nil
}

// splitInnerHeader separates the KDBX 4 inner header from the encrypted XML payload.
func splitInnerHeader(payload []byte) (*innerHeader, []byte, error) {
	pos := 0
	for {
		if pos+5 > len(payload) {
			return nil, nil, errors.New("kdbx: truncated inner header")
		}
		id := payload[pos]
		size := int(uint32(payload[pos+1]) | uint32(payload[pos+2])<<8 |
			uint32(payload[pos+3])<<16 | uint32(payload[pos+4])<<24)
		pos += 5
		if pos+size > len(payload) {
			return nil, nil, errors.New("kdbx: truncated inner header field")
		}
		pos += size
		if id == innerHeaderEnd {
			break
		}
	}
	ih, err := readInnerHeader(payload[:pos])
	if err != nil {
		return nil, nil, err
	}
	return ih, payload[pos:], nil
}

func transformKey(h *header, composite [32]byte) ([32]byte, error) {
	if h.IsV4() {
		if h.KDF == nil {
			return [32]byte{}, errors.New("kdbx: the file has no key derivation parameters")
		}
		return h.KDF.Transform(composite)
	}
	if len(h.TransformSeed) != 32 {
		return [32]byte{}, errors.New("kdbx: invalid transform seed in header")
	}
	return aesTransformKey(h.TransformSeed, h.TransformRounds, composite), nil
}

// decryptPayloadNoPadding decrypts legacy payloads that were written without padding.
func decryptPayloadNoPadding(cipherUUID UUID, key, nonce, iv, encrypted []byte) ([]byte, error) {
	if cipherUUID == CipherChaCha20 {
		return chachaXOR(key, nonce, encrypted)
	}
	return aesCBCDecryptNoPadding(key, iv, encrypted)
}

// SaveFile writes a database to disk using KeePass defaults for the target version.
func SaveFile(path string, db *Database, key Key, opts SaveOptions) error {
	data, err := Save(db, key, opts)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// Save serialises and encrypts a database.
func Save(db *Database, key Key, opts SaveOptions) ([]byte, error) {
	version := opts.Version
	if version == 0 {
		version = FileVersion41
	}
	cipher := opts.Cipher
	if cipher.IsZero() {
		cipher = CipherAES256
	}
	compression := uint32(CompressionGzip)
	if opts.Compression != nil {
		compression = *opts.Compression
	}
	if version < FileVersion4 {
		return saveV3(db, key, version, cipher, compression, opts.KDF)
	}
	kdf := opts.KDF
	if kdf == nil {
		kdf = DefaultKDFParams()
	}
	if err := kdf.Validate(); err != nil {
		return nil, err
	}
	streamID := uint32(InnerStreamChaCha20)
	if opts.RandomStreamID != nil {
		streamID = *opts.RandomStreamID
	}

	composite, err := key.CompositeKey()
	if err != nil {
		return nil, err
	}

	// KeePass removes unreferenced attachments before saving.
	db.CompactBinaries()

	masterSeed := make([]byte, 32)
	mustRand(masterSeed)
	ivLen := 16
	if cipher == CipherChaCha20 {
		ivLen = 12
	}
	encryptionIV := make([]byte, ivLen)
	mustRand(encryptionIV)
	streamKey := make([]byte, 64)
	mustRand(streamKey)

	h := &header{
		Version:     version,
		CipherID:    cipher,
		Compression: compression,
		MasterSeed:  masterSeed,
		IV:          encryptionIV,
		KDF:         kdf,
	}

	transformedKey, err := kdf.Transform(composite)
	if err != nil {
		return nil, err
	}
	transformed := transformedKey[:]
	cipherKey, nonce := deriveCipherKey(cipher, masterSeed, transformed)
	if cipher == CipherChaCha20 {
		nonce = encryptionIV
	}

	stream, err := newRandomStream(streamID, streamKey)
	if err != nil {
		return nil, err
	}
	writer := newXMLWriter(version, stream, db.Binaries)
	xmlData := writer.writeDocument(db)

	inner := &innerHeader{RandomStreamID: streamID, RandomStreamKey: streamKey, Binaries: db.Binaries}
	payload := append(inner.write(), xmlData...)
	if compression == CompressionGzip {
		payload = gzipCompress(payload)
	}
	encrypted, err := encryptPayload(cipher, cipherKey, nonce, encryptionIV, payload)
	if err != nil {
		return nil, err
	}

	headerBytes, err := h.write()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.Write(headerBytes)
	out.Write(h.sha256Hash())
	hmacBaseKey := deriveHMACBaseKey(masterSeed, transformed)
	headerMAC := hmac.New(sha256.New, indexedHMACKey(hmacBaseKey, hmacIndexMagic))
	headerMAC.Write(headerBytes)
	out.Write(headerMAC.Sum(nil))
	blockWriter := newHMACBlockWriter(&out, hmacBaseKey, 0)
	if _, err := blockWriter.Write(encrypted); err != nil {
		return nil, err
	}
	if err := blockWriter.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// saveV3 writes a legacy KDBX 3.1 file: AES-256-CBC with PKCS#7 padding, a 32 byte
// stream start marker inside the encrypted payload, a hashed block stream and the
// Salsa20 inner random stream for protected values. KeePass 2 can still write this
// format for applications that predate KDBX 4.
func saveV3(db *Database, key Key, version uint32, cipher UUID, compression uint32, kdf *KDFParams) ([]byte, error) {
	if cipher != CipherAES256 {
		return nil, errors.New("kdbx: KDBX 3.1 only supports the AES-256 cipher")
	}
	if kdf == nil {
		kdf = DefaultAESKDFParams()
	}
	if kdf.UUID() != KDFAES {
		return nil, errors.New("kdbx: KDBX 3.1 only supports the AES key derivation function")
	}
	seed := kdf.Dict.GetBytes(KdfParamSeed)
	rounds := kdf.Dict.GetUint64(KdfParamRounds, 60000)
	if len(seed) != 32 {
		return nil, errors.New("kdbx: AES-KDF seed must be 32 bytes")
	}

	composite, err := key.CompositeKey()
	if err != nil {
		return nil, err
	}
	db.CompactBinaries()

	h := &header{
		Version:             version,
		CipherID:            cipher,
		Compression:         compression,
		MasterSeed:          make([]byte, 32),
		TransformSeed:       append([]byte(nil), seed...),
		TransformRounds:     rounds,
		IV:                  make([]byte, 16),
		ProtectedStreamKey:  make([]byte, 32),
		StreamStartBytes:    make([]byte, 32),
		InnerRandomStreamID: InnerStreamSalsa20,
	}
	for _, field := range [][]byte{h.MasterSeed, h.IV, h.ProtectedStreamKey, h.StreamStartBytes} {
		mustRand(field)
	}
	headerBytes, err := h.write()
	if err != nil {
		return nil, err
	}

	transformed := aesTransformKey(h.TransformSeed, h.TransformRounds, composite)
	cipherKey, nonce := deriveCipherKey(cipher, h.MasterSeed, transformed[:])
	stream, err := newRandomStream(h.InnerRandomStreamID, h.ProtectedStreamKey)
	if err != nil {
		return nil, err
	}
	// KDBX 3.1 stores the header digest inside the encrypted XML instead of a header
	// HMAC.
	db.Meta.HeaderHash = h.sha256Hash()
	xmlData := newXMLWriter(version, stream, db.Binaries).writeDocument(db)
	db.Meta.HeaderHash = nil

	payload := xmlData
	if compression == CompressionGzip {
		payload = gzipCompress(payload)
	}
	blocks, err := writeHashedBlockStream(payload, 0)
	if err != nil {
		return nil, err
	}
	plain := append(append([]byte(nil), h.StreamStartBytes...), blocks...)
	encrypted, err := encryptPayload(cipher, cipherKey, nonce, h.IV, plain)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(headerBytes)+len(encrypted))
	out = append(out, headerBytes...)
	out = append(out, encrypted...)
	return out, nil
}

// writeFileAtomic writes data through a temporary file so that an interrupted save
// never destroys the original database.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gokeep-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// BackupPath returns the file name KeePass uses for its automatic backup copy.
func BackupPath(path string) string { return path + ".bak" }

// WriteBackup copies the current database file next to itself, the way KeePass does
// before overwriting a file.
func WriteBackup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return writeFileAtomic(BackupPath(path), data)
}

// RotateBackups keeps at most max numbered backups, named <file>.bak.<n>, exactly like
// the "keep several backup files" setting of KeePass.
func RotateBackups(path string, max int) error {
	if max <= 1 {
		return nil
	}
	base := BackupPath(path)
	for i := max - 1; i >= 1; i-- {
		from := base
		if i > 1 {
			from = fmt.Sprintf("%s.%d", base, i-1)
		}
		to := fmt.Sprintf("%s.%d", base, i)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	return nil
}

// Info summarises the on-disk format of a database file without decrypting it.
type Info struct {
	Version       uint32
	CipherUUID    UUID
	CipherName    string
	Compression   uint32
	KDFName       string
	KDFUUID       UUID
	KDFRounds     uint64
	KDFMemory     uint64
	KDFIterations uint64
	KDFParallel   uint32
	HasHMAC       bool
	KDFSalt       []byte
}

// InspectFile reads only the header of a database file.
func InspectFile(path string) (*Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	h, err := readHeader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	info := &Info{
		Version:     h.Version,
		CipherUUID:  h.CipherID,
		Compression: h.Compression,
		HasHMAC:     h.IsV4(),
	}
	switch h.CipherID {
	case CipherAES256:
		info.CipherName = "AES-256"
	case CipherChaCha20:
		info.CipherName = "ChaCha20"
	case CipherTwoFish:
		info.CipherName = "Twofish"
	default:
		info.CipherName = "Unknown"
	}
	if h.IsV4() && h.KDF != nil {
		info.KDFName = h.KDF.Name()
		info.KDFUUID = h.KDF.UUID()
		info.KDFRounds = h.KDF.Dict.GetUint64(KdfParamRounds, 0)
		info.KDFMemory = h.KDF.Dict.GetUint64(KdfParamMemory, 0)
		info.KDFIterations = h.KDF.Dict.GetUint64(KdfParamIterations, 0)
		info.KDFParallel = h.KDF.Dict.GetUint32(KdfParamParallelism, 0)
		info.KDFSalt = h.KDF.Dict.GetBytes(KdfParamSeed)
	} else {
		info.KDFName = "AES-KDF"
		info.KDFUUID = KDFAES
		info.KDFRounds = h.TransformRounds
		info.KDFSalt = h.TransformSeed
	}
	return info, nil
}

// NewDatabase creates an empty database: only the root group exists, so the user starts
// with a clean tree.
func NewDatabase(name string, defaultUserName string) *Database {
	db := New(name)
	db.Meta.DefaultUserName = defaultUserName
	db.Meta.SettingsChanged = time.Now().UTC().Truncate(time.Second)
	return db
}
