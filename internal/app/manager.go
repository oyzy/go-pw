package app

import (
	"errors"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"gokeep/internal/kdbx"
	"gokeep/internal/vault"
)

// Manager owns the state shared by all services: the open database, the configuration
// and the clipboard policy.
type Manager struct {
	mu             sync.Mutex
	vault          *vault.Vault
	config         *ConfigStore
	app            *application.App
	window         *application.WebviewWindow
	clearAt        time.Time
	clipSeq        int
	Version        string
	menuUpdater    func(labels map[string]string)
	recycleBinName string
	themeUpdater   func(theme string)
	quitting       bool
}

// NewManager creates the shared state.
func NewManager(version string) (*Manager, error) {
	store, err := NewConfigStore()
	if err != nil {
		return nil, err
	}
	return &Manager{config: store, Version: version}, nil
}

// Attach stores the Wails application handles so services can show dialogs and use the
// system clipboard.
func (m *Manager) Attach(app *application.App, window *application.WebviewWindow) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.app = app
	m.window = window
}

// Application returns the Wails application.
func (m *Manager) Application() *application.App {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.app
}

// Window returns the main window.
func (m *Manager) Window() *application.WebviewWindow {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.window
}

// Config returns the configuration store.
func (m *Manager) Config() *ConfigStore { return m.config }

// SetMenuUpdater registers the callback that rebuilds the native menu bar with the
// labels coming from the frontend language pack.
func (m *Manager) SetMenuUpdater(updater func(labels map[string]string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.menuUpdater = updater
}

// RebuildMenu applies new menu labels.
func (m *Manager) RebuildMenu(labels map[string]string) {
	m.mu.Lock()
	updater := m.menuUpdater
	m.mu.Unlock()
	if updater != nil {
		updater(labels)
	}
}

// SetRecycleBinName remembers the localised name of the recycle bin group and applies it
// to the open database.
func (m *Manager) SetRecycleBinName(name string) {
	if name == "" {
		return
	}
	m.mu.Lock()
	m.recycleBinName = name
	v := m.vault
	m.mu.Unlock()
	if v != nil {
		v.SetRecycleBinName(name)
	}
}

// RecycleBinName returns the localised name of the recycle bin group.
func (m *Manager) RecycleBinName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recycleBinName == "" {
		return kdbx.RecycleBinName
	}
	return m.recycleBinName
}

// SetThemeUpdater registers the callback that applies a theme to the native window
// decorations.
func (m *Manager) SetThemeUpdater(updater func(theme string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.themeUpdater = updater
}

// ApplyTheme tells the platform layer about the current theme.
func (m *Manager) ApplyTheme(theme string) {
	m.mu.Lock()
	updater := m.themeUpdater
	m.mu.Unlock()
	if updater != nil {
		updater(theme)
	}
}

// SetQuitting marks the application as shutting down, which lets the window close
// handler know that the "unsaved changes" question has already been answered.
func (m *Manager) SetQuitting() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quitting = true
}

// IsQuitting reports whether the application is shutting down.
func (m *Manager) IsQuitting() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.quitting
}

// Vault returns the open database, or nil.
func (m *Manager) Vault() *vault.Vault {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.vault
}

// SetVault replaces the open database.
func (m *Manager) SetVault(v *vault.Vault) {
	m.mu.Lock()
	m.vault = v
	name := m.recycleBinName
	m.mu.Unlock()
	if v != nil && name != "" {
		// A database opened after the language was chosen uses the localised name for
		// its recycle bin as well.
		v.SetRecycleBinName(name)
	}
}

// RequireVault returns the open database or an error.
func (m *Manager) RequireVault() (*vault.Vault, error) {
	v := m.Vault()
	if v == nil || v.Locked() {
		return nil, errors.New("no database is open")
	}
	return v, nil
}

// CopyToClipboard puts text on the clipboard. When the configuration asks for it, the
// clipboard is cleared again after the configured number of seconds, exactly like
// KeePass does for copied passwords.
func (m *Manager) CopyToClipboard(text string, sensitive bool) error {
	m.mu.Lock()
	app := m.app
	cfg := m.config.Get()
	m.clipSeq++
	seq := m.clipSeq
	m.mu.Unlock()

	if app == nil {
		return errors.New("clipboard is not available")
	}
	if !app.Clipboard.SetText(text) {
		return errors.New("the clipboard could not be written")
	}
	if !sensitive || cfg.ClipboardClearSeconds <= 0 {
		return nil
	}
	go func() {
		time.Sleep(time.Duration(cfg.ClipboardClearSeconds) * time.Second)
		m.mu.Lock()
		stillCurrent := m.clipSeq == seq
		m.mu.Unlock()
		if !stillCurrent {
			return
		}
		// Only clear when the clipboard still holds the value we put there.
		if current, ok := app.Clipboard.Text(); ok && current == text {
			app.Clipboard.SetText("")
		}
		m.mu.Lock()
		m.clearAt = time.Time{}
		m.mu.Unlock()
	}()
	m.mu.Lock()
	m.clearAt = time.Now().Add(time.Duration(cfg.ClipboardClearSeconds) * time.Second)
	m.mu.Unlock()
	return nil
}

// ClearClipboard empties the clipboard immediately.
func (m *Manager) ClearClipboard() {
	app := m.Application()
	if app == nil {
		return
	}
	app.Clipboard.SetText("")
}

// ClipboardState reports when the clipboard will be cleared, for the status bar.
func (m *Manager) ClipboardState() (bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.clearAt.IsZero() {
		return false, 0
	}
	remaining := int(time.Until(m.clearAt).Seconds())
	if remaining < 0 {
		return false, 0
	}
	return true, remaining
}

// SecurityPolicy describes what happens when the application or the window is closed.
func (m *Manager) SecurityPolicy() (clipboardOnExit, lockOnMinimize bool, autoLockSeconds int) {
	cfg := m.config.Get()
	return cfg.ClipboardClearOnExit, cfg.LockOnMinimize, cfg.AutoLockSeconds
}

// Lock closes the open database and clears the clipboard.
func (m *Manager) Lock() {
	m.mu.Lock()
	v := m.vault
	m.vault = nil
	m.mu.Unlock()
	if v != nil {
		v.Close()
	}
	m.ClearClipboard()
}
