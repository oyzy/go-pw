// Package desktop is the Viva user interface of GoKeep: a page that draws the whole
// application, from the lock screen to the entry editor. It owns no cryptography: the
// database itself lives in internal/vault, and this package only calls it.
package desktop

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/app"

	"gokeep/internal/config"
	"gokeep/internal/dbdoc"
	"gokeep/internal/gkdb"
	"gokeep/internal/vault"
)

// Error carries a code the user interface translates, the values the translated
// sentence fills in, and a message for the log.
type Error struct {
	Code    string
	Message string
	// Values replace the {placeholders} of the sentence the pack holds for Code. A
	// sentence shown with a placeholder still in it is worse than no sentence at all,
	// so every error that carries a path, a name or a count fills them here.
	Values map[string]string
}

func (e *Error) Error() string { return e.Message }

// Error codes. The user interface maps each to a localized sentence.
const (
	CodeWrongKey     = "wrong-key"
	CodeWrongKeyFile = "wrong-key-file"
	CodeNoFile       = "file-not-found"
	CodeNotADatabase = "not-a-database"
	CodeVersionNew   = "version-too-new"
	CodeConflict     = "conflict"
	CodeEmptyField   = "empty-field"
	CodeNoDatabase   = "no-database"
	// CodeDamaged, CodeCreateFailed and CodeSaveFailed cover what the storage layer
	// reports without a sentence of its own: the user reads an explanation in the
	// active language and the raw message goes to the log.
	CodeDamaged      = "damaged"
	CodeCreateFailed = "create-failed"
	CodeSaveFailed   = "save-failed"
)

// errorf builds an error the interface shows in the user's language: the sentence the
// pack holds for code, with values filling its placeholders. message is the same
// sentence in the language of the log, for a bug report.
func errorf(code string, values map[string]string, message string, args ...any) error {
	return &Error{Code: code, Values: values, Message: fmt.Sprintf(message, args...)}
}

// translate turns an error from the storage layer into one the interface can show in the
// user's language. The raw message still goes to the log, where it is useful: a damaged
// file reports block digests and byte offsets, which a bug report needs and the user
// should not have to read.
func translate(code string, err error) error {
	if err == nil {
		return nil
	}
	var coded *Error
	if errors.As(err, &coded) {
		return coded
	}
	slog.Error("database access failed", "code", code, "error", err)
	return errorf(code, nil, "%s", err)
}

// Session is the application state a page reads: the open database, the settings
// store, the language packs and the clipboard policy.
type Session struct {
	version string
	cfg     *config.Store
	tr      *i18n

	mu          sync.Mutex
	v           *vault.Vault
	clipSeq     int
	clipClearAt time.Time
	clipTimer   *time.Timer
	quitting    bool
	invalidator *app.Invalidator

	// Auto lock is driven by a deadline rather than a fixed tick: every user
	// activity pushes autoLockAt forward, and the timer that is already running
	// only compares the clock when it fires. The window thread performs the lock
	// itself in OnWake, so the model can be cleared on the thread that owns it.
	now             func() time.Time
	autoLockAt      time.Time
	autoLockTimer   *time.Timer
	autoLockSeq     int
	autoLockPending bool
}

// NewSession loads the configuration and language packs.
func NewSession(version string, store *config.Store) *Session {
	s := &Session{version: version, cfg: store, now: time.Now}
	s.tr = newI18n(store.Get().Language)
	return s
}

// Version is the application version.
func (s *Session) Version() string { return s.version }

// Config returns the settings store.
func (s *Session) Config() *config.Store { return s.cfg }

// Tr returns the language packs.
func (s *Session) Tr() *i18n { return s.tr }

// SetLanguage switches the interface language and remembers it.
func (s *Session) SetLanguage(code string) error {
	s.tr.SetLanguage(code)
	cfg := s.cfg.Get()
	cfg.Language = code
	return s.cfg.Set(cfg)
}

// SetInvalidator registers the window's repaint request, which the clipboard timer
// and anything else off the window thread use to wake the page.
func (s *Session) SetInvalidator(invalidator *app.Invalidator) {
	s.mu.Lock()
	s.invalidator = invalidator
	s.mu.Unlock()
}

// Invalidate asks the window for one repaint from any goroutine.
func (s *Session) Invalidate() {
	s.mu.Lock()
	invalidator := s.invalidator
	s.mu.Unlock()
	if invalidator != nil {
		invalidator.Invalidate()
	}
}

// TouchActivity records that the user did something. It moves the auto-lock
// deadline forward without replacing the running timer, so pointer moves do not
// allocate a new timer for every event.
func (s *Session) TouchActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touchActivityLocked()
}

func (s *Session) touchActivityLocked() {
	// A click that arrives after the timer requested a lock but before OnWake
	// ran means the user is still there.
	s.autoLockPending = false
	if s.v == nil {
		s.stopAutoLockLocked()
		return
	}
	idle, enabled := s.autoLockIdleLocked()
	if !enabled {
		s.stopAutoLockLocked()
		return
	}
	s.autoLockAt = s.clock().Add(idle)
	if s.autoLockTimer == nil {
		s.scheduleAutoLockLocked(idle)
	}
}

func (s *Session) autoLockIdleLocked() (time.Duration, bool) {
	seconds := clampAutoLockSeconds(s.cfg.Get().AutoLockSeconds)
	if seconds <= 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// ResetAutoLock restarts the idle timer after the setting or the open database
// changed. Unlike TouchActivity it replaces the timer, so a newly shorter
// interval takes effect immediately.
func (s *Session) ResetAutoLock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopAutoLockLocked()
	if s.v == nil {
		return
	}
	s.startAutoLockLocked()
}

func (s *Session) startAutoLockLocked() {
	idle, enabled := s.autoLockIdleLocked()
	if !enabled {
		return
	}
	s.autoLockAt = s.clock().Add(idle)
	s.scheduleAutoLockLocked(idle)
}

// scheduleAutoLockLocked starts the one timer that checks the deadline. The
// caller holds the session lock.
func (s *Session) scheduleAutoLockLocked(delay time.Duration) {
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	if s.autoLockTimer != nil {
		s.autoLockTimer.Stop()
	}
	s.autoLockSeq++
	seq := s.autoLockSeq
	s.autoLockTimer = time.AfterFunc(delay, func() { s.checkAutoLock(seq) })
}

// stopAutoLockLocked cancels the timer and invalidates a callback that has
// already started. The caller holds the session lock.
func (s *Session) stopAutoLockLocked() {
	s.autoLockSeq++
	if s.autoLockTimer != nil {
		s.autoLockTimer.Stop()
		s.autoLockTimer = nil
	}
	s.autoLockAt = time.Time{}
	s.autoLockPending = false
}

func (s *Session) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// checkAutoLock runs on the timer goroutine. It never touches the model: it
// only requests a lock, and OnWake performs it on the window thread.
func (s *Session) checkAutoLock(seq int) {
	s.mu.Lock()
	if seq != s.autoLockSeq || s.v == nil {
		s.mu.Unlock()
		return
	}
	now := s.clock()
	if now.Before(s.autoLockAt) {
		s.scheduleAutoLockLocked(s.autoLockAt.Sub(now))
		s.mu.Unlock()
		return
	}
	s.autoLockSeq++
	if s.autoLockTimer != nil {
		s.autoLockTimer.Stop()
	}
	s.autoLockTimer = nil
	s.autoLockAt = time.Time{}
	s.autoLockPending = true
	s.mu.Unlock()
	s.Invalidate()
}

// TakeAutoLockRequest asks whether the idle timer wants the workspace locked.
// The window thread calls it from OnWake, before the next paint.
func (s *Session) TakeAutoLockRequest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.autoLockPending
	s.autoLockPending = false
	return pending
}

// Vault returns the open database, or nil when the workspace is locked.
func (s *Session) Vault() *vault.Vault {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v
}

// Unlocked reports whether a database is open and readable.
func (s *Session) Unlocked() bool {
	v := s.Vault()
	return v != nil && !v.Locked()
}

// SetQuitting records that the unsaved-changes question has been answered.
func (s *Session) SetQuitting() {
	s.mu.Lock()
	s.quitting = true
	s.mu.Unlock()
}

// Quitting reports whether the application is on its way out.
func (s *Session) Quitting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quitting
}

// keyFor builds the composite key a database file is opened with.
func keyFor(password, keyFilePath string) (dbdoc.Key, error) {
	key := dbdoc.Key{Password: password}
	if keyFilePath == "" {
		return key, nil
	}
	data, err := os.ReadFile(keyFilePath)
	if err != nil {
		slog.Error("the key file could not be read", "path", keyFilePath, "error", err)
		return key, errorf(CodeWrongKeyFile, map[string]string{"path": keyFilePath},
			"the key file %q could not be read: %v", keyFilePath, err)
	}
	key.KeyFile = data
	key.KeyFileName = keyFilePath
	return key, nil
}

// Open unlocks an existing database file.
func (s *Session) Open(path, password, keyFilePath string) (vault.DatabaseInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return vault.DatabaseInfo{}, errorf(CodeEmptyField, nil, "no database file was chosen")
	}
	key, err := keyFor(password, keyFilePath)
	if err != nil {
		return vault.DatabaseInfo{}, err
	}
	v, err := vault.Open(path, key)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			return vault.DatabaseInfo{}, errorf(CodeNoFile, map[string]string{"path": path},
				"the database file does not exist: %s", path)
		case errors.Is(err, gkdb.ErrWrongKey):
			return vault.DatabaseInfo{}, errorf(CodeWrongKey, nil, "the master key is wrong")
		case errors.Is(err, gkdb.ErrNotDatabase):
			return vault.DatabaseInfo{}, errorf(CodeNotADatabase, map[string]string{"name": filepath.Base(path)},
				"not a GoKeep database file: %s", filepath.Base(path))
		case strings.Contains(err.Error(), "newer than this application supports"):
			return vault.DatabaseInfo{}, errorf(CodeVersionNew, nil,
				"the database was written by a newer version of GoKeep")
		}
		return vault.DatabaseInfo{}, translate(CodeDamaged, err)
	}
	s.setVault(v)
	_ = s.cfg.AddRecent(path)
	return v.Info(), nil
}

// Create makes a new database file and opens it.
func (s *Session) Create(path, name, password, keyFilePath string, settings vault.DatabaseSettings) (vault.DatabaseInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return vault.DatabaseInfo{}, errorf(CodeEmptyField, nil, "no database file was chosen")
	}
	if password == "" && keyFilePath == "" {
		return vault.DatabaseInfo{}, errorf(CodeEmptyField, nil, "a master password or a key file is required")
	}
	if _, err := os.Stat(path); err == nil {
		return vault.DatabaseInfo{}, errorf(CodeConflict, map[string]string{"path": path},
			"the file already exists: %s", path)
	}
	key, err := keyFor(password, keyFilePath)
	if err != nil {
		return vault.DatabaseInfo{}, err
	}
	save := vault.DefaultSaveSettings()
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	v, err := vault.Create(path, key, name, save)
	if err != nil {
		return vault.DatabaseInfo{}, translate(CodeCreateFailed, err)
	}
	if settings.HistoryMaxItems != 0 || settings.HistoryMaxSize != 0 || settings.ProtectPassword {
		if err := v.ApplyCreateOptions(settings); err != nil {
			return vault.DatabaseInfo{}, translate(CodeCreateFailed, err)
		}
		if err := v.Save(); err != nil {
			return vault.DatabaseInfo{}, translate(CodeSaveFailed, err)
		}
	}
	s.setVault(v)
	_ = s.cfg.AddRecent(path)
	return v.Info(), nil
}

func (s *Session) setVault(v *vault.Vault) {
	s.mu.Lock()
	previous := s.v
	s.v = v
	s.stopAutoLockLocked()
	if v != nil {
		s.startAutoLockLocked()
	}
	s.mu.Unlock()
	if previous != nil && previous != v {
		previous.Close()
	}
}

// Save writes the open database in place.
func (s *Session) Save() error {
	v := s.Vault()
	if v == nil {
		return errorf(CodeNoDatabase, nil, "no database is open")
	}
	return translate(CodeSaveFailed, v.Save())
}

// SaveAs writes the open database to a new path, re-encrypted with the given key.
func (s *Session) SaveAs(path, password, keyFilePath string) error {
	v := s.Vault()
	if v == nil {
		return errorf(CodeNoDatabase, nil, "no database is open")
	}
	key, err := keyFor(password, keyFilePath)
	if err != nil {
		return err
	}
	if err := v.SaveAs(path, key); err != nil {
		return translate(CodeSaveFailed, err)
	}
	_ = s.cfg.AddRecent(path)
	return nil
}

// ChangeKey re-encrypts the open database with a new master key.
func (s *Session) ChangeKey(password, keyFilePath string) error {
	v := s.Vault()
	if v == nil {
		return errorf(CodeNoDatabase, nil, "no database is open")
	}
	if password == "" && keyFilePath == "" {
		return errorf(CodeEmptyField, nil, "a master password or a key file is required")
	}
	key, err := keyFor(password, keyFilePath)
	if err != nil {
		return err
	}
	if err := v.ChangeKey(key); err != nil {
		return translate(CodeSaveFailed, err)
	}
	return nil
}

// CloseDatabase saves when asked and forgets the open database.
func (s *Session) CloseDatabase(saveFirst bool) error {
	v := s.Vault()
	if v == nil {
		return nil
	}
	if saveFirst && v.Modified() {
		if err := v.Save(); err != nil {
			return translate(CodeSaveFailed, err)
		}
	}
	s.Lock()
	return nil
}

// Lock closes the open database and empties the clipboard.
func (s *Session) Lock() {
	s.mu.Lock()
	v := s.v
	s.v = nil
	s.stopAutoLockLocked()
	timer := s.clipTimer
	s.clipTimer = nil
	s.clipClearAt = time.Time{}
	s.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if v != nil {
		v.Close()
	}
	s.ClearClipboard()
}

// CopyToClipboard puts text on the clipboard and, for sensitive values, empties the
// clipboard again after the configured number of seconds, exactly like KeePass does
// for a copied password.
func (s *Session) CopyToClipboard(text string, sensitive bool) error {
	if err := app.SetClipboardText(text); err != nil {
		return err
	}
	cfg := s.cfg.Get()

	s.mu.Lock()
	if s.clipTimer != nil {
		s.clipTimer.Stop()
		s.clipTimer = nil
	}
	s.clipSeq++
	seq := s.clipSeq
	s.clipClearAt = time.Time{}
	if sensitive && cfg.ClipboardClearSeconds > 0 {
		delay := time.Duration(cfg.ClipboardClearSeconds) * time.Second
		s.clipClearAt = time.Now().Add(delay)
		s.clipTimer = time.AfterFunc(delay, func() { s.clearClipboardIfCurrent(seq, text) })
	}
	s.mu.Unlock()
	return nil
}

func (s *Session) clearClipboardIfCurrent(seq int, text string) {
	s.mu.Lock()
	current := s.clipSeq == seq
	s.clipTimer = nil
	s.clipClearAt = time.Time{}
	s.mu.Unlock()
	if !current {
		return
	}
	if value, err := app.ClipboardText(); err == nil && value == text {
		_ = app.SetClipboardText("")
	}
	s.Invalidate()
}

// ClipboardState reports whether the clipboard is still due to be cleared, and in how
// many seconds, for the status bar.
func (s *Session) ClipboardState() (bool, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clipClearAt.IsZero() {
		return false, 0
	}
	remaining := int(time.Until(s.clipClearAt).Seconds())
	if remaining < 0 {
		return false, 0
	}
	return true, remaining
}

// ClearClipboard empties the clipboard immediately.
func (s *Session) ClearClipboard() {
	s.mu.Lock()
	if s.clipTimer != nil {
		s.clipTimer.Stop()
		s.clipTimer = nil
	}
	s.clipClearAt = time.Time{}
	s.clipSeq++
	s.mu.Unlock()
	_ = app.SetClipboardText("")
}

// ReadFile reads a small file, used for key files and CSV import.
func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
