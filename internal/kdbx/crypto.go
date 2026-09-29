package kdbx

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20"
)

// Cipher UUIDs supported by KeePass 2.
var (
	CipherAES256   = MustParseUUID("31C1F2E6BF714350BE5805216AFC5AFF")
	CipherChaCha20 = MustParseUUID("D6038A2B8B6F4CB5A524339A31DBB59A")
	CipherTwoFish  = MustParseUUID("AD68F29F576F4BB9A36AD47AF965346C")
)

// Compression algorithms.
const (
	CompressionNone = 0
	CompressionGzip = 1
)

// deriveCipherKey computes the block cipher key. KeePass concatenates the master seed
// and the transformed key and reduces the resulting 64 bytes to the required key size
// with SHA-256; AES-256 and ChaCha20 share the same key material, and ChaCha20 takes its
// twelve byte nonce from the encryption IV stored in the header.
func deriveCipherKey(cipherUUID UUID, masterSeed, transformedKey []byte) (key, nonce []byte) {
	h := sha256.New()
	h.Write(masterSeed)
	h.Write(transformedKey)
	return h.Sum(nil), nil
}

// deriveHMACBaseKey returns the 64 byte key that KDBX 4 uses as the basis for the
// header HMAC and for every block HMAC: SHA-512(masterSeed || transformedKey || 0x01).
func deriveHMACBaseKey(masterSeed, transformedKey []byte) []byte {
	h := sha512.New()
	h.Write(masterSeed)
	h.Write(transformedKey)
	h.Write([]byte{0x01})
	return h.Sum(nil)
}

// hmacIndexMagic is the pseudo index KeePass uses for the header HMAC.
const hmacIndexMagic = ^uint64(0)

// indexedHMACKey derives the HMAC key of one block (or of the header):
// SHA-512(little endian index || base key). KeePass derives a fresh key per block
// instead of using one static key for the whole stream.
func indexedHMACKey(baseKey []byte, index uint64) []byte {
	var indexBytes [8]byte
	binary.LittleEndian.PutUint64(indexBytes[:], index)
	h := sha512.New()
	h.Write(indexBytes[:])
	h.Write(baseKey)
	return h.Sum(nil)
}

// encryptPayload encrypts a payload with the configured cipher.
func encryptPayload(cipherUUID UUID, key, nonce, iv, plain []byte) ([]byte, error) {
	switch cipherUUID {
	case CipherAES256:
		return aesCBCEncrypt(key, iv, plain)
	case CipherChaCha20:
		return chachaXOR(key, nonce, plain)
	}
	return nil, fmt.Errorf("kdbx: unsupported cipher %s", cipherUUID)
}

// decryptPayload reverses encryptPayload.
func decryptPayload(cipherUUID UUID, key, nonce, iv, encrypted []byte) ([]byte, error) {
	switch cipherUUID {
	case CipherAES256:
		return aesCBCDecrypt(key, iv, encrypted)
	case CipherChaCha20:
		return chachaXOR(key, nonce, encrypted)
	}
	return nil, fmt.Errorf("kdbx: unsupported cipher %s", cipherUUID)
}

func aesCBCEncrypt(key, iv, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != block.BlockSize() {
		return nil, fmt.Errorf("kdbx: AES IV must be %d bytes, got %d", block.BlockSize(), len(iv))
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out, nil
}

func aesCBCDecrypt(key, iv, encrypted []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != block.BlockSize() {
		return nil, fmt.Errorf("kdbx: AES IV must be %d bytes, got %d", block.BlockSize(), len(iv))
	}
	if len(encrypted) == 0 || len(encrypted)%block.BlockSize() != 0 {
		return nil, errors.New("kdbx: ciphertext length is not a multiple of the AES block size")
	}
	out := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, encrypted)
	return pkcs7Unpad(out, block.BlockSize())
}

// aesCBCDecryptNoPadding decrypts data that was written without PKCS#7 padding.
func aesCBCDecryptNoPadding(key, iv, encrypted []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != block.BlockSize() {
		return nil, fmt.Errorf("kdbx: AES IV must be %d bytes, got %d", block.BlockSize(), len(iv))
	}
	if len(encrypted) == 0 || len(encrypted)%block.BlockSize() != 0 {
		return nil, errors.New("kdbx: ciphertext length is not a multiple of the AES block size")
	}
	out := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, encrypted)
	return out, nil
}

func chachaXOR(key, nonce, data []byte) ([]byte, error) {
	c, err := chacha20.NewUnauthenticatedCipher(key, nonce)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out, nil
}

func pkcs7Pad(data []byte, size int) []byte {
	pad := size - len(data)%size
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func pkcs7Unpad(data []byte, size int) ([]byte, error) {
	if len(data) == 0 || len(data)%size != 0 {
		return nil, errors.New("kdbx: invalid padded data length")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > size || pad > len(data) {
		return nil, errors.New("kdbx: invalid padding")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, errors.New("kdbx: invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}

// --- HMAC block stream (KDBX 4) ----------------------------------------------

// hmacBlockWriter writes the HMAC block stream that wraps the encrypted payload of a
// KDBX 4 file: for every block the HMAC of index, size and data is written first.
type hmacBlockWriter struct {
	w      io.Writer
	base   []byte
	index  uint64
	maxLen int
}

const defaultBlockChunk = 1024 * 1024

func newHMACBlockWriter(w io.Writer, baseKey []byte, maxBlock int) *hmacBlockWriter {
	if maxBlock <= 0 {
		maxBlock = defaultBlockChunk
	}
	return &hmacBlockWriter{w: w, base: baseKey, maxLen: maxBlock}
}

func (b *hmacBlockWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n := len(p)
		if n > b.maxLen {
			n = b.maxLen
		}
		if err := b.writeBlock(p[:n]); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

func (b *hmacBlockWriter) writeBlock(data []byte) error {
	header := make([]byte, 12)
	binary.LittleEndian.PutUint64(header[0:8], b.index)
	binary.LittleEndian.PutUint32(header[8:12], uint32(len(data)))

	mac := hmac.New(sha256.New, indexedHMACKey(b.base, b.index))
	mac.Write(header)
	mac.Write(data)
	if _, err := b.w.Write(mac.Sum(nil)); err != nil {
		return err
	}
	if _, err := b.w.Write(header[8:12]); err != nil {
		return err
	}
	if _, err := b.w.Write(data); err != nil {
		return err
	}
	b.index++
	return nil
}

// Close terminates the stream with an empty block.
func (b *hmacBlockWriter) Close() error {
	header := make([]byte, 12)
	binary.LittleEndian.PutUint64(header[0:8], b.index)
	mac := hmac.New(sha256.New, indexedHMACKey(b.base, b.index))
	mac.Write(header)
	if _, err := b.w.Write(mac.Sum(nil)); err != nil {
		return err
	}
	_, err := b.w.Write(header[8:12])
	return err
}

// readHMACBlockStream reads and verifies the whole block stream.
func readHMACBlockStream(r io.Reader, baseKey []byte) ([]byte, error) {
	var (
		index uint64
		out   bytes.Buffer
	)
	for {
		var mac [32]byte
		if _, err := io.ReadFull(r, mac[:]); err != nil {
			return nil, fmt.Errorf("kdbx: cannot read block %d HMAC: %w", index, err)
		}
		var sizeBuf [4]byte
		if _, err := io.ReadFull(r, sizeBuf[:]); err != nil {
			return nil, fmt.Errorf("kdbx: cannot read block %d size: %w", index, err)
		}
		size := binary.LittleEndian.Uint32(sizeBuf[:])

		header := make([]byte, 12)
		binary.LittleEndian.PutUint64(header[0:8], index)
		binary.LittleEndian.PutUint32(header[8:12], size)

		key := indexedHMACKey(baseKey, index)
		if size == 0 {
			expected := hmac.New(sha256.New, key)
			expected.Write(header)
			if !hmac.Equal(mac[:], expected.Sum(nil)) {
				return nil, errors.New("kdbx: final block HMAC mismatch (wrong credentials or corrupted file)")
			}
			return out.Bytes(), nil
		}

		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, fmt.Errorf("kdbx: cannot read block %d data: %w", index, err)
		}
		expected := hmac.New(sha256.New, key)
		expected.Write(header)
		expected.Write(data)
		if !hmac.Equal(mac[:], expected.Sum(nil)) {
			return nil, fmt.Errorf("kdbx: block %d HMAC mismatch (wrong credentials or corrupted file)", index)
		}
		out.Write(data)
		index++
	}
}
