// Package app exposes the password manager to the user interface through Wails
// services. It owns the open vault, the persisted configuration and the clipboard
// policy.
package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"gokeep/internal/pwgen"
)

// Config is the persisted application configuration. It mirrors the settings KeePass
// stores in its own configuration file.
type Config struct {
	Version               int           `json:"version"`
	RecentFiles           []string      `json:"recentFiles"`
	LastDatabase          string        `json:"lastDatabase"`
	ClipboardClearSeconds int           `json:"clipboardClearSeconds"`
	ClipboardClearOnExit  bool          `json:"clipboardClearOnExit"`
	AutoLockSeconds       int           `json:"autoLockSeconds"`
	LockOnMinimize        bool          `json:"lockOnMinimize"`
	BackupCount           int           `json:"backupCount"`
	ConfirmDelete         bool          `json:"confirmDelete"`
	ShowPasswordsInList   bool          `json:"showPasswordsInList"`
	ShowRecycleBin        bool          `json:"showRecycleBin"`
	Theme                 string        `json:"theme"`
	Language              string        `json:"language"`
	StartMinimized        bool          `json:"startMinimized"`
	MaxRecentFiles        int           `json:"maxRecentFiles"`
	Generator             pwgen.Options `json:"generator"`
	LastGroupSelected     string        `json:"lastGroupSelected"`
}

// DefaultConfig returns the defaults KeePass uses.
func DefaultConfig() Config {
	return Config{
		Version:               1,
		ClipboardClearSeconds: 12,
		ClipboardClearOnExit:  true,
		AutoLockSeconds:       0,
		LockOnMinimize:        false,
		BackupCount:           1,
		ConfirmDelete:         true,
		ShowRecycleBin:        true,
		Theme:                 "dark",
		// An empty language means "follow the operating system"; the frontend detects it
		// on first start and stores the result.
		Language:       "",
		MaxRecentFiles: 10,
		Generator:      pwgen.DefaultOptions(),
	}
}

// ConfigStore loads and saves the configuration file.
type ConfigStore struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

// NewConfigStore opens (or creates) the configuration in the user configuration
// directory.
func NewConfigStore() (*ConfigStore, error) {
	dir, err := os.UserConfigDir()
	if err == nil {
		appDir := filepath.Join(dir, "GoKeep")
		if err := os.MkdirAll(appDir, 0o700); err == nil {
			store := &ConfigStore{path: filepath.Join(appDir, "GoKeep.config.json"), cfg: DefaultConfig()}
			if store.load() {
				return store, nil
			}
			// Only use this directory when it is writable as well.
			if err := store.save(); err == nil {
				return store, nil
			}
		}
	}
	// Fall back to the temporary directory so the application still starts when the
	// user configuration directory cannot be written.
	fallback := filepath.Join(os.TempDir(), "GoKeep")
	if err := os.MkdirAll(fallback, 0o700); err != nil {
		return nil, err
	}
	store := &ConfigStore{path: filepath.Join(fallback, "GoKeep.config.json"), cfg: DefaultConfig()}
	store.load()
	return store, nil
}

// NewConfigStoreAt opens a configuration file at an explicit location, which the tests
// use to avoid touching the real user configuration.
func NewConfigStoreAt(path string) (*ConfigStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	store := &ConfigStore{path: path, cfg: DefaultConfig()}
	store.load()
	return store, nil
}

// load reads the configuration file, keeping the defaults when it is missing or broken.
func (c *ConfigStore) load() bool {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return false
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	if cfg.ClipboardClearSeconds == 0 {
		cfg.ClipboardClearSeconds = 12
	}
	if cfg.Generator.Length == 0 {
		cfg.Generator = pwgen.DefaultOptions()
	}
	c.cfg = cfg
	return true
}

// save writes the configuration without taking the lock.
func (c *ConfigStore) save() error {
	data, err := json.MarshalIndent(c.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Get returns a copy of the configuration.
func (c *ConfigStore) Get() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

// Set stores a new configuration and writes it to disk.
func (c *ConfigStore) Set(cfg Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg.ClipboardClearSeconds < 0 {
		cfg.ClipboardClearSeconds = 0
	}
	c.cfg = cfg
	return c.save()
}

// AddRecent remembers a recently used database file.
func (c *ConfigStore) AddRecent(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg := c.cfg
	var files []string
	for _, f := range cfg.RecentFiles {
		if !sameFile(f, path) {
			files = append(files, f)
		}
	}
	files = append([]string{path}, files...)
	if cfg.MaxRecentFiles > 0 && len(files) > cfg.MaxRecentFiles {
		files = files[:cfg.MaxRecentFiles]
	}
	cfg.RecentFiles = files
	cfg.LastDatabase = path
	c.cfg = cfg
	return c.save()
}

// Path returns the configuration file name.
func (c *ConfigStore) Path() string { return c.path }

func sameFile(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return aa == bb
	}
	return a == b
}

var errNoVault = errors.New("no database is open")
