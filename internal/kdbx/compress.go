package kdbx

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

// gzipCompress compresses data using the same settings KeePass uses for its payload.
func gzipCompress(data []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(data)
	w.Close()
	return buf.Bytes()
}

// gzipDecompress inflates a gzip payload.
func gzipDecompress(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("kdbx: invalid gzip data: %w", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("kdbx: cannot inflate gzip data: %w", err)
	}
	return out, nil
}
