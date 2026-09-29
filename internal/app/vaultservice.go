package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gokeep/internal/kdbx"
	"gokeep/internal/pwquality"
	"gokeep/internal/vault"
)

// VaultService exposes the database operations to the frontend.
type VaultService struct {
	manager *Manager
}

// NewVaultService wires the service to the shared state.
func NewVaultService(m *Manager) *VaultService { return &VaultService{manager: m} }

// OpenDatabase opens a database file. The password may be empty when the file only uses
// a key file.
func (s *VaultService) OpenDatabase(path, password, keyFilePath string) (vault.DatabaseInfo, error) {
	key := kdbx.Key{Password: password}
	if keyFilePath != "" {
		data, err := os.ReadFile(keyFilePath)
		if err != nil {
			return vault.DatabaseInfo{}, Errorf(CodeWrongKeyFile,
				"无法读取密钥文件 %q：%v（如果创建数据库时没有使用密钥文件，请取消勾选“使用密钥文件”）", keyFilePath, err)
		}
		key.KeyFile = data
		key.KeyFileName = keyFilePath
	}
	v, err := vault.Open(path, key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return vault.DatabaseInfo{}, Errorf(CodeNoFile, "数据库文件不存在：%s", path)
		}
		if errors.Is(err, kdbx.ErrWrongKey) {
			return vault.DatabaseInfo{}, Errorf(CodeWrongKey, "主密码或密钥文件不正确，无法解密数据库")
		}
		if strings.Contains(err.Error(), "not a KeePass 2 database file") {
			return vault.DatabaseInfo{}, Errorf(CodeNoFile, "这不是一个 KeePass 数据库文件：%s", filepath.Base(path))
		}
		if strings.Contains(err.Error(), "newer than this application supports") {
			return vault.DatabaseInfo{}, Errorf(CodeNoFile, "该数据库使用了更新的 KDBX 版本，当前版本无法打开")
		}
		return vault.DatabaseInfo{}, err
	}
	s.manager.SetVault(v)
	_ = s.manager.Config().AddRecent(path)
	return v.Info(), nil
}

// CreateDatabase creates a new database file.
func (s *VaultService) CreateDatabase(path, name, password, keyFilePath string, settings vault.DatabaseSettings) (vault.DatabaseInfo, error) {
	if strings.TrimSpace(path) == "" {
		return vault.DatabaseInfo{}, Errorf(CodeEmptyField, "请先选择数据库文件")
	}
	if password == "" && keyFilePath == "" {
		return vault.DatabaseInfo{}, Errorf(CodeEmptyField, "请设置主密码，或选择一个密钥文件")
	}
	if _, err := os.Stat(path); err == nil {
		return vault.DatabaseInfo{}, Errorf(CodeConflict, "文件已存在：%s", path)
	}
	key := kdbx.Key{Password: password}
	if keyFilePath != "" {
		data, err := os.ReadFile(keyFilePath)
		if err != nil {
			return vault.DatabaseInfo{}, Errorf(CodeWrongKeyFile, "无法读取密钥文件 %q：%v", keyFilePath, err)
		}
		key.KeyFile = data
		key.KeyFileName = keyFilePath
	}
	save := vault.DefaultSaveSettings()
	if settings.FileVersion != 0 {
		save.Version = settings.FileVersion
	}
	if settings.Name == "" {
		settings.Name = name
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	v, err := vault.Create(path, key, name, save)
	if err != nil {
		return vault.DatabaseInfo{}, err
	}
	if settings.HistoryMaxItems != 0 || settings.HistoryMaxSize != 0 || settings.ProtectPassword {
		// ApplyCreateOptions never creates groups, so a new database starts empty.
		if err := v.ApplyCreateOptions(settings); err != nil {
			return vault.DatabaseInfo{}, err
		}
		// Persist the settings right away so that a freshly created database is not
		// reported as modified.
		if err := v.Save(); err != nil {
			return vault.DatabaseInfo{}, err
		}
	}
	s.manager.SetVault(v)
	_ = s.manager.Config().AddRecent(path)
	return v.Info(), nil
}

// CloseDatabase closes the open database, saving first when requested.
func (s *VaultService) CloseDatabase(saveFirst bool) error {
	v := s.manager.Vault()
	if v == nil {
		return nil
	}
	if saveFirst && v.Modified() {
		if err := v.Save(); err != nil {
			return err
		}
	}
	s.manager.Lock()
	return nil
}

// SaveDatabase writes the open database back to its file.
func (s *VaultService) SaveDatabase() error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.Save()
}

// SaveDatabaseAs writes the database to a new file and switches to it.
func (s *VaultService) SaveDatabaseAs(path string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	if err := v.SaveAs(path, kdbx.Key{}); err != nil {
		return err
	}
	_ = s.manager.Config().AddRecent(path)
	return nil
}

// LockDatabase closes the database without saving and clears the clipboard.
func (s *VaultService) LockDatabase() {
	s.manager.Lock()
}

// ChangeMasterKey re-encrypts the open database with new credentials.
func (s *VaultService) ChangeMasterKey(newPassword, newKeyFilePath string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	key := kdbx.Key{Password: newPassword}
	if newKeyFilePath != "" {
		data, err := os.ReadFile(newKeyFilePath)
		if err != nil {
			return fmt.Errorf("key file: %w", err)
		}
		key.KeyFile = data
		key.KeyFileName = newKeyFilePath
	}
	if key.IsEmpty() {
		return errors.New("a master password or a key file is required")
	}
	return v.ChangeKey(key)
}

// CreateKeyFile writes a new key file and returns its path.
func (s *VaultService) CreateKeyFile(path string) error {
	if path == "" {
		return errors.New("a file name is required")
	}
	return kdbx.WriteKeyFile(path, false)
}

// Info returns information about the open database.
func (s *VaultService) Info() (vault.DatabaseInfo, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return vault.DatabaseInfo{}, err
	}
	return v.Info(), nil
}

// IsOpen reports whether a database is currently unlocked.
func (s *VaultService) IsOpen() bool {
	_, err := s.manager.RequireVault()
	return err == nil
}

// Tree returns the group tree without the root group itself.
func (s *VaultService) Tree() ([]vault.GroupDTO, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return nil, err
	}
	return v.Tree(), nil
}

// Groups returns every group, used by the move menus.
func (s *VaultService) Groups(includeRecycled bool) ([]vault.GroupDTO, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return nil, err
	}
	return v.Groups(includeRecycled)
}

// RootID returns the identifier of the root group.
func (s *VaultService) RootID() (string, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return "", err
	}
	return v.RootID(), nil
}

// Entries lists the entries of a group.
func (s *VaultService) Entries(groupID string, recursive, includeRecycled bool) ([]vault.EntrySummary, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return nil, err
	}
	return v.Entries(groupID, recursive, includeRecycled)
}

// AllEntries lists every entry of the database.
func (s *VaultService) AllEntries(includeRecycled bool) ([]vault.EntrySummary, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return nil, err
	}
	return v.AllEntries(includeRecycled)
}

// Entry returns one entry in full.
func (s *VaultService) Entry(id string) (vault.EntryDTO, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return vault.EntryDTO{}, err
	}
	return v.Entry(id)
}

// CreateEntry adds an entry to a group and returns its identifier.
func (s *VaultService) CreateEntry(groupID string, dto vault.EntryDTO) (string, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return "", err
	}
	return v.CreateEntry(groupID, dto)
}

// UpdateEntry stores changes to an entry.
func (s *VaultService) UpdateEntry(dto vault.EntryDTO) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.UpdateEntry(dto)
}

// DeleteEntry deletes an entry.
func (s *VaultService) DeleteEntry(id string, permanently bool) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.DeleteEntry(id, permanently)
}

// RestoreEntry restores an entry from the recycle bin.
func (s *VaultService) RestoreEntry(id, groupID string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.RestoreEntry(id, groupID)
}

// RestoreToPreviousGroup restores an entry into the group it was deleted from.
func (s *VaultService) RestoreToPreviousGroup(id string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.RestoreToPreviousGroup(id)
}

// MoveEntry moves an entry to another group.
func (s *VaultService) MoveEntry(id, groupID string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.MoveEntry(id, groupID)
}

// DuplicateEntry copies an entry inside its group.
func (s *VaultService) DuplicateEntry(id string) (string, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return "", err
	}
	return v.DuplicateEntry(id)
}

// CreateGroup adds a subgroup.
func (s *VaultService) CreateGroup(parentID, name string, iconID int) (string, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return "", err
	}
	return v.CreateGroup(parentID, name, iconID)
}

// UpdateGroup stores changes to a group.
func (s *VaultService) UpdateGroup(dto vault.GroupDTO) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.UpdateGroup(dto)
}

// DeleteGroup removes a group.
func (s *VaultService) DeleteGroup(id string, permanently bool) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.DeleteGroup(id, permanently)
}

// MoveGroup moves a group.
func (s *VaultService) MoveGroup(id, parentID string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.MoveGroup(id, parentID)
}

// EmptyRecycleBin deletes everything inside the recycle bin.
func (s *VaultService) EmptyRecycleBin() error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.EmptyRecycleBin()
}

// RestoreHistory restores a stored revision of an entry.
func (s *VaultService) RestoreHistory(entryID string, index int) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.RestoreHistory(entryID, index)
}

// DeleteHistory removes a stored revision.
func (s *VaultService) DeleteHistory(entryID string, index int) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.DeleteHistory(entryID, index)
}

// AddAttachment adds a file to an entry. The data is passed base64 encoded.
func (s *VaultService) AddAttachment(entryID, name, base64Data string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return fmt.Errorf("invalid attachment data: %w", err)
	}
	return v.AddAttachment(entryID, name, data)
}

// RemoveAttachment removes an attachment from an entry.
func (s *VaultService) RemoveAttachment(entryID, name string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.RemoveAttachment(entryID, name)
}

// ExportAttachment writes an attachment to disk.
func (s *VaultService) ExportAttachment(entryID, name, targetPath string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	data, err := v.Attachment(entryID, name)
	if err != nil {
		return err
	}
	if targetPath == "" {
		return errors.New("a target file is required")
	}
	return os.WriteFile(targetPath, data, 0o600)
}

// Search runs a query over the database.
func (s *VaultService) Search(opts vault.SearchOptions) ([]vault.EntrySummary, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return nil, err
	}
	return v.Search(opts)
}

// FindDuplicates returns entries that share a password.
func (s *VaultService) FindDuplicates() ([][]vault.EntrySummary, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return nil, err
	}
	return v.FindDuplicates()
}

// ExpiredEntries returns the entries whose expiry date has passed.
func (s *VaultService) ExpiredEntries() ([]vault.EntrySummary, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return nil, err
	}
	return v.ExpiredEntries()
}

// TagList returns every tag in use.
func (s *VaultService) TagList() ([]string, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return nil, err
	}
	return v.TagList(), nil
}

// DatabaseSettings returns the editable database metadata.
func (s *VaultService) DatabaseSettings() (vault.DatabaseSettings, error) {
	v, err := s.manager.RequireVault()
	if err != nil {
		return vault.DatabaseSettings{}, err
	}
	return v.DatabaseSettings()
}

// ApplyDatabaseSettings stores the database settings dialog.
func (s *VaultService) ApplyDatabaseSettings(settings vault.DatabaseSettings) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.ApplyDatabaseSettings(settings)
}

// TouchEntryAccess updates the usage counter of an entry.
func (s *VaultService) TouchEntryAccess(id string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	return v.TouchEntryAccess(id)
}

// CopyField copies a field value to the clipboard. Protected values are cleared again
// after the configured time.
func (s *VaultService) CopyField(entryID, field string) error {
	v, err := s.manager.RequireVault()
	if err != nil {
		return err
	}
	entry, err := v.Entry(entryID)
	if err != nil {
		return err
	}
	value, sensitive := entry.FieldValue(field)
	if value == "" {
		return fmt.Errorf("the field %q is empty", field)
	}
	if err := v.TouchEntryAccess(entryID); err != nil {
		return err
	}
	return s.manager.CopyToClipboard(value, sensitive)
}

// ClearClipboard empties the system clipboard.
func (s *VaultService) ClearClipboard() {
	s.manager.ClearClipboard()
}

// ClipboardStatus reports whether the clipboard will be cleared automatically.
func (s *VaultService) ClipboardStatus() (bool, int) {
	return s.manager.ClipboardState()
}

// EvaluatePassword returns the estimated strength of a password.
func (s *VaultService) EvaluatePassword(password string) (bits float64, score int, label string) {
	bits, score = pwquality.Estimate(password, false)
	return bits, score, pwquality.Label(score)
}
