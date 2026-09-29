package kdbx

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// File signatures. KeePass 1.x shares the first signature but uses a different second one.
const (
	signature1 uint32 = 0x9AA2D903
	signature2 uint32 = 0xB54BFB67
)

// Supported file versions.
const (
	// FileVersion31 is the legacy KDBX 3.1 format (KeePass 2.09 - 2.34 default).
	FileVersion31 uint32 = 0x00030001
	// FileVersion4 is KDBX 4.0 (KeePass 2.35 - 2.46).
	FileVersion4 uint32 = 0x00040000
	// FileVersion41 is KDBX 4.1 (KeePass 2.47 and newer), the format this package writes.
	FileVersion41 uint32 = 0x00040001
	// FileVersionCriticalMask masks the bits that indicate an incompatible major version.
	FileVersionCriticalMask uint32 = 0xFFFF0000
)

// Outer header field identifiers.
const (
	headerEnd                byte = 0
	headerComment            byte = 1
	headerCipherID           byte = 2
	headerCompressionFlags   byte = 3
	headerMasterSeed         byte = 4
	headerTransformSeed      byte = 5
	headerTransformRounds    byte = 6
	headerEncryptionIV       byte = 7
	headerProtectedStreamKey byte = 8
	headerStreamStartBytes   byte = 9
	headerInnerRandomStream  byte = 10
	headerKdfParameters      byte = 11
	headerPublicCustomData   byte = 12
)

// Inner header field identifiers (KDBX 4 only).
const (
	innerHeaderEnd             byte = 0
	innerHeaderRandomStreamID  byte = 1
	innerHeaderRandomStreamKey byte = 2
	innerHeaderBinary          byte = 3
)

// header is the parsed KDBX file header.
type header struct {
	Version     uint32
	Comment     []byte
	CipherID    UUID
	Compression uint32
	MasterSeed  []byte
	IV          []byte

	// KDBX 3.1 only.
	TransformSeed       []byte
	TransformRounds     uint64
	ProtectedStreamKey  []byte
	StreamStartBytes    []byte
	InnerRandomStreamID uint32

	// KDBX 4 only.
	KDF              *KDFParams
	PublicCustomData *VariantDict

	// raw holds the exact bytes of the header, which are needed for the integrity
	// checks (SHA-256 in 4.x, HeaderHash in 3.1).
	raw []byte
}

// IsV4 reports whether the file uses the KDBX 4 header layout.
func (h *header) IsV4() bool { return h.Version >= FileVersion4 }

// readHeader parses the outer header and returns the number of bytes it occupies.
func readHeader(r *bytes.Reader) (*header, error) {
	h := &header{}
	start := int64(0)
	var sig1, sig2 uint32
	if err := binary.Read(r, binary.LittleEndian, &sig1); err != nil {
		return nil, fmt.Errorf("kdbx: cannot read signature: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &sig2); err != nil {
		return nil, fmt.Errorf("kdbx: cannot read second signature: %w", err)
	}
	if sig1 != signature1 || sig2 != signature2 {
		return nil, errors.New("kdbx: not a KeePass 2 database file (bad signature)")
	}
	if err := binary.Read(r, binary.LittleEndian, &h.Version); err != nil {
		return nil, fmt.Errorf("kdbx: cannot read version: %w", err)
	}
	switch h.Version {
	case FileVersion31, FileVersion4, FileVersion41:
	default:
		if h.Version&FileVersionCriticalMask > FileVersion41 {
			return nil, fmt.Errorf("kdbx: file version 0x%08X is newer than this application supports", h.Version)
		}
	}

	for {
		id, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("kdbx: truncated header: %w", err)
		}
		var size uint32
		if h.IsV4() {
			var s uint32
			if err := binary.Read(r, binary.LittleEndian, &s); err != nil {
				return nil, fmt.Errorf("kdbx: truncated header field %d: %w", id, err)
			}
			size = s
		} else {
			var s uint16
			if err := binary.Read(r, binary.LittleEndian, &s); err != nil {
				return nil, fmt.Errorf("kdbx: truncated header field %d: %w", id, err)
			}
			size = uint32(s)
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("kdbx: truncated header field %d data: %w", id, err)
		}
		switch id {
		case headerEnd:
			h.raw = make([]byte, r.Size()-start-int64(r.Len())+start)
			// Everything up to and including this field forms the header bytes.
			pos := r.Size() - int64(r.Len())
			h.raw = make([]byte, pos)
			if _, err := r.ReadAt(h.raw, 0); err != nil {
				return nil, err
			}
			return h, nil
		case headerComment:
			h.Comment = data
		case headerCipherID:
			u, err := UUIDFromBytes(data)
			if err != nil {
				return nil, err
			}
			h.CipherID = u
		case headerCompressionFlags:
			if len(data) != 4 {
				return nil, fmt.Errorf("kdbx: compression field must be 4 bytes, got %d", len(data))
			}
			h.Compression = binary.LittleEndian.Uint32(data)
		case headerMasterSeed:
			h.MasterSeed = data
		case headerTransformSeed:
			h.TransformSeed = data
		case headerTransformRounds:
			if len(data) != 8 {
				return nil, fmt.Errorf("kdbx: transform rounds must be 8 bytes, got %d", len(data))
			}
			h.TransformRounds = binary.LittleEndian.Uint64(data)
		case headerEncryptionIV:
			h.IV = data
		case headerProtectedStreamKey:
			h.ProtectedStreamKey = data
		case headerStreamStartBytes:
			h.StreamStartBytes = data
		case headerInnerRandomStream:
			if len(data) != 4 {
				return nil, fmt.Errorf("kdbx: inner random stream id must be 4 bytes, got %d", len(data))
			}
			h.InnerRandomStreamID = binary.LittleEndian.Uint32(data)
		case headerKdfParameters:
			params, err := ParseVariantDict(data)
			if err != nil {
				return nil, fmt.Errorf("kdbx: invalid KDF parameters: %w", err)
			}
			h.KDF = &KDFParams{Dict: params}
		case headerPublicCustomData:
			params, err := ParseVariantDict(data)
			if err != nil {
				return nil, fmt.Errorf("kdbx: invalid public custom data: %w", err)
			}
			h.PublicCustomData = params
		}
	}
}

// write builds the serialised header.
func (h *header) write() ([]byte, error) {
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, signature1); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, signature2); err != nil {
		return nil, err
	}
	version := h.Version
	if version == 0 {
		version = FileVersion41
	}
	if err := binary.Write(&buf, binary.LittleEndian, version); err != nil {
		return nil, err
	}

	writeField := func(id byte, data []byte) error {
		if err := buf.WriteByte(id); err != nil {
			return err
		}
		if version >= FileVersion4 {
			if err := binary.Write(&buf, binary.LittleEndian, uint32(len(data))); err != nil {
				return err
			}
		} else {
			if err := binary.Write(&buf, binary.LittleEndian, uint16(len(data))); err != nil {
				return err
			}
		}
		_, err := buf.Write(data)
		return err
	}

	cipher := h.CipherID
	if cipher.IsZero() {
		cipher = CipherAES256
	}
	if err := writeField(headerCipherID, cipher[:]); err != nil {
		return nil, err
	}
	compression := make([]byte, 4)
	binary.LittleEndian.PutUint32(compression, h.Compression)
	if err := writeField(headerCompressionFlags, compression); err != nil {
		return nil, err
	}
	if err := writeField(headerMasterSeed, h.MasterSeed); err != nil {
		return nil, err
	}
	if err := writeField(headerEncryptionIV, h.IV); err != nil {
		return nil, err
	}

	if version >= FileVersion4 {
		if h.KDF == nil {
			return nil, errors.New("kdbx: missing key derivation parameters")
		}
		if err := writeField(headerKdfParameters, h.KDF.Dict.Encode()); err != nil {
			return nil, err
		}
		if h.PublicCustomData != nil {
			if err := writeField(headerPublicCustomData, h.PublicCustomData.Encode()); err != nil {
				return nil, err
			}
		}
	} else {
		if err := writeField(headerTransformSeed, h.TransformSeed); err != nil {
			return nil, err
		}
		rounds := make([]byte, 8)
		binary.LittleEndian.PutUint64(rounds, h.TransformRounds)
		if err := writeField(headerTransformRounds, rounds); err != nil {
			return nil, err
		}
		if err := writeField(headerProtectedStreamKey, h.ProtectedStreamKey); err != nil {
			return nil, err
		}
		if err := writeField(headerStreamStartBytes, h.StreamStartBytes); err != nil {
			return nil, err
		}
		id := make([]byte, 4)
		binary.LittleEndian.PutUint32(id, h.InnerRandomStreamID)
		if err := writeField(headerInnerRandomStream, id); err != nil {
			return nil, err
		}
	}
	if err := writeField(headerEnd, []byte{0x0D, 0x0A, 0x0D, 0x0A}); err != nil {
		return nil, err
	}
	h.raw = buf.Bytes()
	return h.raw, nil
}

// sha256Hash returns the header digest used by the KDBX 4 integrity check.
func (h *header) sha256Hash() []byte {
	sum := sha256.Sum256(h.raw)
	return sum[:]
}

// innerHeader holds the KDBX 4 "inner header" that precedes the encrypted XML.
type innerHeader struct {
	RandomStreamID  uint32
	RandomStreamKey []byte
	Binaries        []Binary
}

// readInnerHeader parses the inner header from the decrypted payload.
func readInnerHeader(data []byte) (*innerHeader, error) {
	ih := &innerHeader{}
	pos := 0
	for {
		if pos >= len(data) {
			return nil, errors.New("kdbx: truncated inner header")
		}
		id := data[pos]
		pos++
		if pos+4 > len(data) {
			return nil, errors.New("kdbx: truncated inner header field size")
		}
		size := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
		pos += 4
		if size < 0 || pos+size > len(data) {
			return nil, errors.New("kdbx: truncated inner header field data")
		}
		value := data[pos : pos+size]
		pos += size

		switch id {
		case innerHeaderEnd:
			return ih, nil
		case innerHeaderRandomStreamID:
			if len(value) != 4 {
				return nil, fmt.Errorf("kdbx: inner random stream id must be 4 bytes, got %d", len(value))
			}
			ih.RandomStreamID = binary.LittleEndian.Uint32(value)
		case innerHeaderRandomStreamKey:
			ih.RandomStreamKey = append([]byte(nil), value...)
		case innerHeaderBinary:
			bin := Binary{}
			if len(value) > 0 && value[0] <= 1 {
				bin.Protected = value[0]&1 != 0
				value = value[1:]
			}
			bin.Data = append([]byte(nil), value...)
			ih.Binaries = append(ih.Binaries, bin)
		}
	}
}

// write serialises the inner header.
func (ih *innerHeader) write() []byte {
	var buf bytes.Buffer
	writeField := func(id byte, data []byte) {
		buf.WriteByte(id)
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(len(data)))
		buf.Write(size[:])
		buf.Write(data)
	}
	var id [4]byte
	binary.LittleEndian.PutUint32(id[:], ih.RandomStreamID)
	writeField(innerHeaderRandomStreamID, id[:])
	writeField(innerHeaderRandomStreamKey, ih.RandomStreamKey)
	for _, bin := range ih.Binaries {
		flags := byte(0)
		if bin.Protected {
			flags = 1
		}
		payload := make([]byte, 0, len(bin.Data)+1)
		payload = append(payload, flags)
		payload = append(payload, bin.Data...)
		writeField(innerHeaderBinary, payload)
	}
	writeField(innerHeaderEnd, nil)
	return buf.Bytes()
}
