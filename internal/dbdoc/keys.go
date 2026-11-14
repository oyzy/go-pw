package dbdoc

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Key holds the credentials needed to open a database. KeePass calls the combination a
// "composite key"; the same file can require a password, a key file, or both.
type Key struct {
	Password    string
	KeyFile     []byte // raw content of the key file
	KeyFileName string // only used in error messages
}

// IsEmpty reports whether no credentials are configured.
func (k Key) IsEmpty() bool { return k.Password == "" && len(k.KeyFile) == 0 }

// Describe returns a short human readable description of the key sources.
func (k Key) Describe() string {
	var parts []string
	if k.Password != "" {
		parts = append(parts, "password")
	}
	if len(k.KeyFile) > 0 {
		name := k.KeyFileName
		if name == "" {
			name = "key file"
		}
		parts = append(parts, name)
	}
	if len(parts) == 0 {
		return "no credentials"
	}
	return strings.Join(parts, " + ")
}

// LoadKeyFile reads a key file from disk.
func LoadKeyFile(path string) (Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Key{}, err
	}
	return Key{KeyFile: data, KeyFileName: path}, nil
}

// CompositeKey converts the credentials into the 32 byte composite key, applying the
// exact rules KeePass 2 uses.
func (k Key) CompositeKey() ([32]byte, error) {
	var parts [][]byte
	if k.Password != "" {
		sum := sha256.Sum256([]byte(k.Password))
		parts = append(parts, sum[:])
	}
	if len(k.KeyFile) > 0 {
		keyData, err := KeyFileData(k.KeyFile)
		if err != nil {
			return [32]byte{}, err
		}
		// The 32 bytes returned by KeyFileData are exactly what KeePass calls the
		// "key data" of the key file, and the composite key hashes the concatenation
		// of the key data of all user keys; it is not hashed again here.
		parts = append(parts, keyData)
	}
	if len(parts) == 0 {
		return [32]byte{}, errors.New("gokeep: no credentials supplied")
	}
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// KeyFileData extracts the 32 byte key material from a KeePass key file. KeePass
// supports four representations and this function follows the same order: an XML key
// file (version 1.0 with hex data, or version 2.0 with base64 data and a hash), exactly
// 32 raw bytes, exactly 64 hexadecimal characters, or in every other case the SHA-256
// hash of the whole file.
func KeyFileData(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("gokeep: key file is empty")
	}

	head := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))
	if bytes.HasPrefix(head, []byte("<?xml")) || bytes.HasPrefix(head, []byte("<KeyFile")) {
		return parseXMLKeyFile(head)
	}

	if len(raw) == 32 {
		return append([]byte(nil), raw...), nil
	}

	if len(raw) == 64 {
		text := strings.TrimSpace(string(raw))
		if data, err := hex.DecodeString(text); err == nil && len(data) == 32 {
			return data, nil
		}
	}

	sum := sha256.Sum256(raw)
	return sum[:], nil
}

type xmlKeyFile struct {
	XMLName xml.Name `xml:"KeyFile"`
	Meta    struct {
		Version string `xml:"Version"`
	} `xml:"Meta"`
	Key struct {
		Data struct {
			Hash string `xml:"Hash,attr"`
			Text string `xml:",chardata"`
		} `xml:"Data"`
	} `xml:"Key"`
}

func parseXMLKeyFile(data []byte) ([]byte, error) {
	var kf xmlKeyFile
	if err := xml.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("gokeep: invalid XML key file: %w", err)
	}
	version := strings.TrimSpace(kf.Meta.Version)
	text := strings.Join(strings.Fields(kf.Key.Data.Text), "")
	switch {
	case strings.HasPrefix(version, "2"):
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("gokeep: key file (v2) has invalid base64 data: %w", err)
		}
		if len(decoded) != 32 {
			return nil, fmt.Errorf("gokeep: key file (v2) must contain 32 bytes, got %d", len(decoded))
		}
		if hash := strings.TrimSpace(kf.Key.Data.Hash); hash != "" {
			want, err := hex.DecodeString(hash)
			if err == nil && len(want) > 0 {
				got := sha256.Sum256(decoded)
				if !bytes.Equal(want, got[:]) {
					return nil, errors.New("gokeep: key file (v2) hash mismatch")
				}
			}
		}
		return decoded, nil
	case strings.HasPrefix(version, "1"), version == "":
		decoded, err := hex.DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("gokeep: key file (v1) has invalid hex data: %w", err)
		}
		if len(decoded) != 32 {
			return nil, fmt.Errorf("gokeep: key file (v1) must contain 32 bytes, got %d", len(decoded))
		}
		return decoded, nil
	}
	return nil, fmt.Errorf("gokeep: unsupported key file version %q", version)
}

// CreateKeyFile generates a new key file with 32 random bytes. This is the binary key
// file variant that every KeePass version (and KeePassXC) understands; KeePass 2 also
// writes an XML variant, which is read but deliberately not produced here.
func CreateKeyFile() ([]byte, error) {
	key := make([]byte, 32)
	mustRand(key)
	return key, nil
}

// WriteKeyFile writes a freshly generated key file to disk. Existing files are never
// overwritten unless overwrite is true.
func WriteKeyFile(path string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("gokeep: %s already exists", path)
		}
	}
	data, err := CreateKeyFile()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
