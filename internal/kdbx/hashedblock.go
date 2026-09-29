package kdbx

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// KDBX 3.1 wraps the compressed XML in a "hashed block stream": a chain of blocks that
// each carry their index and a SHA-256 digest, terminated by an empty block. The format
// predates the authenticated HMAC block stream of KDBX 4 but serves the same purpose.

// writeHashedBlockStream splits data into blocks of at most blockSize bytes.
func writeHashedBlockStream(data []byte, blockSize int) ([]byte, error) {
	if blockSize <= 0 {
		blockSize = 1024 * 1024
	}
	var out bytes.Buffer
	var index uint32
	for len(data) > 0 {
		n := len(data)
		if n > blockSize {
			n = blockSize
		}
		chunk := data[:n]
		if err := writeHashedBlock(&out, index, chunk); err != nil {
			return nil, err
		}
		index++
		data = data[n:]
	}
	// The terminating block has no data and an all zero digest.
	if err := writeHashedBlock(&out, index, nil); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeHashedBlock(w io.Writer, index uint32, data []byte) error {
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], index)
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	digest := make([]byte, 32)
	if len(data) > 0 {
		sum := sha256.Sum256(data)
		digest = sum[:]
	}
	if _, err := w.Write(digest); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	_, err := w.Write(data)
	return err
}

// readHashedBlockStream reads and verifies a hashed block stream.
func readHashedBlockStream(data []byte) ([]byte, error) {
	var out bytes.Buffer
	pos := 0
	var expectedIndex uint32
	for {
		if pos+40 > len(data) {
			return nil, errors.New("kdbx: truncated hashed block stream")
		}
		index := binary.LittleEndian.Uint32(data[pos : pos+4])
		if index != expectedIndex {
			return nil, fmt.Errorf("kdbx: unexpected block index %d (want %d)", index, expectedIndex)
		}
		expectedIndex++
		digest := data[pos+4 : pos+36]
		size := int(binary.LittleEndian.Uint32(data[pos+36 : pos+40]))
		pos += 40
		if size < 0 || pos+size > len(data) {
			return nil, errors.New("kdbx: truncated hashed block data")
		}
		block := data[pos : pos+size]
		pos += size
		if size == 0 {
			return out.Bytes(), nil
		}
		sum := sha256.Sum256(block)
		if !bytes.Equal(sum[:], digest) {
			return nil, errors.New("kdbx: hashed block does not match its digest")
		}
		out.Write(block)
	}
}
