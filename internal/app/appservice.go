package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gokeep/internal/kdbx"
	"gokeep/internal/pwgen"
	"gokeep/internal/vault"
)

// AppService exposes application level functionality: configuration, file dialogs,
// recent files and window handling.
type AppService struct {
	manager *Manager
}

// NewAppService wires the service to the shared state.
func NewAppService(m *Manager) *AppService { return &AppService{manager: m} }

// Config returns the persisted configuration.
func (s *AppService) Config() Config { return s.manager.Config().Get() }

// UpdateConfig stores a new configuration.
func (s *AppService) UpdateConfig(cfg Config) error {
	return s.manager.Config().Set(cfg)
}

// ConfigPath returns the file the configuration is stored in.
func (s *AppService) ConfigPath() string { return s.manager.Config().Path() }

// Version returns the application version.
func (s *AppService) Version() string { return s.manager.Version }

// Platform returns the operating system the application runs on.
func (s *AppService) Platform() string { return runtime.GOOS }

// RecentFiles returns the list of recently opened databases that still exist.
func (s *AppService) RecentFiles() []string {
	cfg := s.manager.Config().Get()
	var out []string
	for _, f := range cfg.RecentFiles {
		if _, err := os.Stat(f); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// RemoveRecent forgets one recent file.
func (s *AppService) RemoveRecent(path string) error {
	cfg := s.manager.Config().Get()
	var files []string
	for _, f := range cfg.RecentFiles {
		if !strings.EqualFold(f, path) {
			files = append(files, f)
		}
	}
	cfg.RecentFiles = files
	if strings.EqualFold(cfg.LastDatabase, path) {
		cfg.LastDatabase = ""
	}
	return s.manager.Config().Set(cfg)
}

// ClearRecent forgets all recent files.
func (s *AppService) ClearRecent() error {
	cfg := s.manager.Config().Get()
	cfg.RecentFiles = nil
	cfg.LastDatabase = ""
	return s.manager.Config().Set(cfg)
}

// OpenFileDialog shows the system open dialog and returns the selected path.
func (s *AppService) OpenFileDialog(title, pattern, defaultDirectory string) (string, error) {
	app := s.manager.Application()
	if app == nil {
		return "", errors.New("the dialog is not available")
	}
	dialog := app.Dialog.OpenFile().SetTitle(title)
	if pattern != "" {
		dialog = dialog.AddFilter("KeePass databases", pattern)
	}
	if defaultDirectory != "" {
		dialog = dialog.SetDirectory(defaultDirectory)
	}
	if window := s.manager.Window(); window != nil {
		dialog = dialog.AttachToWindow(window)
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil
}

// SaveFileDialog shows the system save dialog and returns the selected path.
func (s *AppService) SaveFileDialog(title, defaultName, pattern string) (string, error) {
	app := s.manager.Application()
	if app == nil {
		return "", errors.New("the dialog is not available")
	}
	dialog := app.Dialog.SaveFile()
	if title != "" {
		dialog = dialog.SetMessage(title)
	}
	if defaultName != "" {
		dialog = dialog.SetFilename(defaultName)
	}
	if pattern != "" {
		dialog = dialog.AddFilter("Supported files", pattern)
	}
	if window := s.manager.Window(); window != nil {
		dialog = dialog.AttachToWindow(window)
	}
	return dialog.PromptForSingleSelection()
}

// OpenFolderDialog shows the system folder dialog.
func (s *AppService) OpenFolderDialog(title string) (string, error) {
	app := s.manager.Application()
	if app == nil {
		return "", errors.New("the dialog is not available")
	}
	dialog := app.Dialog.OpenFile().SetTitle(title).CanChooseDirectories(true).CanChooseFiles(false)
	if window := s.manager.Window(); window != nil {
		dialog = dialog.AttachToWindow(window)
	}
	return dialog.PromptForSingleSelection()
}

// InfoDialog shows a message box.
func (s *AppService) InfoDialog(title, message string) {
	app := s.manager.Application()
	if app == nil {
		return
	}
	dialog := app.Dialog.Info().SetTitle(title).SetMessage(message)
	if window := s.manager.Window(); window != nil {
		dialog = dialog.AttachToWindow(window)
	}
	dialog.Show()
}

// QuestionDialog asks a yes/no question and returns the answer.
func (s *AppService) QuestionDialog(title, message string) (bool, error) {
	app := s.manager.Application()
	if app == nil {
		return false, errors.New("the dialog is not available")
	}
	answer := make(chan bool, 1)
	dialog := app.Dialog.Question().SetTitle(title).SetMessage(message)
	if window := s.manager.Window(); window != nil {
		dialog = dialog.AttachToWindow(window)
	}
	dialog.AddButton("Yes").OnClick(func() { answer <- true })
	dialog.AddButton("No").SetAsCancel().OnClick(func() { answer <- false })
	dialog.Show()
	select {
	case value := <-answer:
		return value, nil
	case <-time.After(10 * time.Minute):
		return false, errors.New("the dialog was not answered")
	}
}

// ReadFileBase64 reads a file for the frontend, used for attachments and key files.
func (s *AppService) ReadFileBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// WriteFileBase64 writes data that the frontend provides.
func (s *AppService) WriteFileBase64(path, data string) error {
	if path == "" {
		return errors.New("a file name is required")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("invalid data: %w", err)
	}
	return os.WriteFile(path, raw, 0o600)
}

// DefaultDatabasePath suggests a file name for a new database.
func (s *AppService) DefaultDatabasePath(name string) string {
	dir, err := os.UserHomeDir()
	if err != nil {
		dir = "."
	}
	if name == "" {
		name = "Database"
	}
	return filepath.Join(dir, name+".kdbx")
}

// Quit closes the application, applying the configured security policy.
func (s *AppService) Quit(saveFirst bool) error {
	if saveFirst {
		if v := s.manager.Vault(); v != nil && v.Modified() {
			if err := v.Save(); err != nil {
				return err
			}
		}
	}
	cfg := s.manager.Config().Get()
	if cfg.ClipboardClearOnExit {
		s.manager.ClearClipboard()
	}
	s.manager.Lock()
	s.manager.SetQuitting()
	if app := s.manager.Application(); app != nil {
		app.Quit()
	}
	return nil
}

// Minimize hides the window, used by the "minimize to tray" behaviour.
func (s *AppService) Minimize() {
	if window := s.manager.Window(); window != nil {
		window.Minimise()
	}
	if cfg := s.manager.Config().Get(); cfg.LockOnMinimize {
		s.manager.Lock()
	}
}

// IconNames returns the built-in KeePass icon identifiers with readable names.
func (s *AppService) IconNames() map[string]string {
	out := map[string]string{}
	for id, name := range kdbx.IconNames {
		out[fmt.Sprintf("%d", id)] = name
	}
	return out
}

// GeneratorDefaults returns the password generator defaults of KeePass.
func (s *AppService) GeneratorDefaults() pwgen.Options { return pwgen.DefaultOptions() }

// VaultSettingsPresets returns ready-made security settings.
func (s *AppService) VaultSettingsPresets() map[string]vault.DatabaseSettings {
	argonDefaults := vault.DatabaseSettings{
		Format:          "kdbx",
		Cipher:          "AES-256",
		Compression:     "GZip",
		FileVersion:     0x00040001,
		KDFName:         "Argon2d",
		KDFMemory:       64 * 1024 * 1024,
		KDFIterations:   2,
		KDFParallelism:  2,
		HistoryMaxItems: 10,
		HistoryMaxSize:  6 * 1024 * 1024,
	}
	paranoid := argonDefaults
	// The strongest settings are available for both formats.
	paranoid.Format = ""
	paranoid.KDFMemory = 512 * 1024 * 1024
	paranoid.KDFIterations = 8
	paranoid.KDFParallelism = 4
	legacy := argonDefaults
	legacy.Format = "kdbx"
	legacy.FileVersion = 0x00030001
	legacy.KDFName = "AES-KDF"
	legacy.KDFRounds = 60000
	legacy.Cipher = "AES-256"
	// The native format keeps the same key derivation options but uses an authenticated
	// cipher; the KDBX file version does not apply.
	nativeDefaults := argonDefaults
	nativeDefaults.Format = "gokeep"
	nativeDefaults.FileVersion = 0

	return map[string]vault.DatabaseSettings{
		"keepass-defaults": argonDefaults,
		"gokeep-defaults":  nativeDefaults,
		"paranoid":         paranoid,
		"legacy-3.1":       legacy,
	}
}

// ForegroundWindow opens a URL or file with the system handler.
func (s *AppService) OpenExternal(target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("nothing to open")
	}
	app := s.manager.Application()
	if app == nil {
		return errors.New("not available")
	}
	return app.Browser.OpenURL(target)
}

// SetWindowTitle updates the title bar.
func (s *AppService) SetWindowTitle(title string) {
	if window := s.manager.Window(); window != nil {
		window.SetTitle(title)
	}
}

// ApplyLanguage stores the interface language, applies the localised recycle bin name
// and rebuilds the native menu bar with the labels the frontend provides. Keeping the
// translations in the frontend language packs means there is only one place to edit.
func (s *AppService) ApplyLanguage(language string, labels map[string]string) error {
	if language != "" {
		cfg := s.manager.Config().Get()
		if cfg.Language != language {
			cfg.Language = language
			if err := s.manager.Config().Set(cfg); err != nil {
				return err
			}
		}
	}
	if name := labels["recycleBin"]; name != "" {
		s.manager.SetRecycleBinName(name)
	}
	s.manager.RebuildMenu(labels)
	return nil
}

// ApplyTheme stores the theme and updates the native window decoration (the title bar),
// so the whole window follows the chosen theme instead of the Windows setting.
func (s *AppService) ApplyTheme(theme string) error {
	if theme != "light" && theme != "dark" {
		return Errorf(CodeEmptyField, "未知的主题：%q", theme)
	}
	cfg := s.manager.Config().Get()
	if cfg.Theme != theme {
		cfg.Theme = theme
		if err := s.manager.Config().Set(cfg); err != nil {
			return err
		}
	}
	s.manager.ApplyTheme(theme)
	return nil
}
