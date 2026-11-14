package dbdoc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRotationKeepsRecentRevisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.gkdb")
	writeRevision := func(value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	readRevision := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	writeRevision("v0")
	if err := RotateBackups(path, 3); err != nil {
		t.Fatal(err)
	}
	if err := WriteBackup(path); err != nil {
		t.Fatal(err)
	}
	writeRevision("v1")

	if err := RotateBackups(path, 3); err != nil {
		t.Fatal(err)
	}
	if err := WriteBackup(path); err != nil {
		t.Fatal(err)
	}
	writeRevision("v2")

	if err := RotateBackups(path, 3); err != nil {
		t.Fatal(err)
	}
	if err := WriteBackup(path); err != nil {
		t.Fatal(err)
	}
	writeRevision("v3")

	for backupPath, want := range map[string]string{
		BackupPath(path):        "v2",
		BackupPath(path) + ".1": "v1",
		BackupPath(path) + ".2": "v0",
	} {
		if got := readRevision(backupPath); got != want {
			t.Errorf("%s = %q, want %q", backupPath, got, want)
		}
	}
}

func TestWriteBackupIgnoresAMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.gkdb")
	if err := WriteBackup(path); err != nil {
		t.Fatalf("WriteBackup: %v", err)
	}
	if _, err := os.Stat(BackupPath(path)); !os.IsNotExist(err) {
		t.Errorf("a backup was created for a missing database: %v", err)
	}
}
