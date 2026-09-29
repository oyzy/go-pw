package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gokeep/internal/kdbx"
)

// ExportCSV writes every entry of the open database to a CSV file.
func (s *VaultService) ExportCSV(path string) (int, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return 0, err
	}
	if path == "" {
		return 0, fmt.Errorf("a file name is required")
	}
	file, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	if err := v.ExportCSV(file); err != nil {
		return 0, err
	}
	count, _ := v.AllEntries(true)
	return len(count), nil
}

// ImportCSV adds entries from a CSV file.
func (s *VaultService) ImportCSV(path, targetGroupID string) (int, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	return v.ImportCSV(file, targetGroupID)
}

// ImportKeyFileFromDatabase is a convenience helper: KeePass allows a database file to
// be used as a key file, and so does this application.
func (s *VaultService) IsValidKeyFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_, err = kdbx.KeyFileData(data)
	return err == nil
}

// CopyDatabaseFile copies a database file, which the "backup" command uses.
func (s *VaultService) CopyDatabaseFile(target string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	source := v.Path()
	if source == "" {
		return fmt.Errorf("the database has no file name yet")
	}
	if target == "" {
		ext := filepath.Ext(source)
		target = strings.TrimSuffix(source, ext) + "-backup" + ext
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o600)
}
