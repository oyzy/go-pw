// Package config stores the preferences the application keeps between runs, in the
// same file the Wails frontend used, so an existing installation keeps its settings,
// recent files and generator defaults.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"gokeep/internal/pwgen"
)

// Config is the persisted application configuration, mirroring the settings KeePass
// keeps in its own configuration file.
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
		BackupCount:           1,
		ConfirmDelete:         true,
		ShowRecycleBin:        true,
		Theme:                 "dark",
		MaxRecentFiles:        10,
		Generator:             pwgen.DefaultOptions(),
	}
}

// Store loads and saves the configuration file.
type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

// New opens (or creates) the configuration in the user configuration directory.
func New() (*Store, error) {
	if dir, err := os.UserConfigDir(); err == nil {
		appDir := filepath.Join(dir, "GoKeep")
		if err := os.MkdirAll(appDir, 0o700); err == nil {
			store := &Store{path: filepath.Join(appDir, "GoKeep.config.json"), cfg: DefaultConfig()}
			if store.load() {
				return store, nil
			}
			if err := store.save(); err == nil {
				return store, nil
			}
		}
	}
	fallback := filepath.Join(os.TempDir(), "GoKeep")
	if err := os.MkdirAll(fallback, 0o700); err != nil {
		return nil, err
	}
	store := &Store{path: filepath.Join(fallback, "GoKeep.config.json"), cfg: DefaultConfig()}
	store.load()
	return store, nil
}

// NewAt opens a configuration file at an explicit location, which the tests use to
// avoid touching the real user configuration.
func NewAt(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	store := &Store{path: path, cfg: DefaultConfig()}
	store.load()
	return store, nil
}

func (c *Store) load() bool {
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

func (c *Store) save() error {
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
func (c *Store) Get() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

// Set stores a new configuration and writes it to disk.
func (c *Store) Set(cfg Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg.ClipboardClearSeconds < 0 {
		cfg.ClipboardClearSeconds = 0
	}
	c.cfg = cfg
	return c.save()
}

// AddRecent remembers a recently used database file.
func (c *Store) AddRecent(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg := c.cfg
	files := make([]string, 0, len(cfg.RecentFiles)+1)
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

// RemoveRecent forgets one recently used file.
func (c *Store) RemoveRecent(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg := c.cfg
	files := make([]string, 0, len(cfg.RecentFiles))
	removed := false
	for _, f := range cfg.RecentFiles {
		if !sameFile(f, path) {
			files = append(files, f)
		} else {
			removed = true
		}
	}
	cfg.RecentFiles = files
	if removed && sameFile(cfg.LastDatabase, path) {
		cfg.LastDatabase = ""
	}
	c.cfg = cfg
	return c.save()
}

// ClearRecent forgets every recently used file.
func (c *Store) ClearRecent() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg := c.cfg
	cfg.RecentFiles = nil
	c.cfg = cfg
	return c.save()
}

// Path returns the configuration file name.
func (c *Store) Path() string { return c.path }

func sameFile(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return aa == bb
	}
	return a == b
}
