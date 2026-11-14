// Package dbdoc is the in-memory document model of a GoKeep database: the group tree,
// the entries with their history and attachments, the metadata and the recycle bin.
//
// The model carries no file format of its own. internal/gkdb serialises it into the
// sealed GoKeep container, and internal/vault is the only layer above that touches it.
package dbdoc

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// NewDatabase creates an empty database: only the root group exists, so the user starts
// with a clean tree.
func NewDatabase(name string, defaultUserName string) *Database {
	db := New(name)
	db.Meta.DefaultUserName = defaultUserName
	db.Meta.SettingsChanged = time.Now().UTC().Truncate(time.Second)
	return db
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

// BackupPath returns the file name of the automatic backup copy kept next to a
// database.
func BackupPath(path string) string { return path + ".bak" }

// WriteBackup copies the current database file next to itself. It is written before a
// database is overwritten, so an interrupted save leaves the previous revision behind.
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

// RotateBackups keeps at most max numbered backups, named <file>.bak.<n>.
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
