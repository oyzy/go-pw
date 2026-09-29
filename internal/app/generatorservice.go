package app

import (
	"fmt"
	"os"

	"gokeep/internal/kdbx"
	"gokeep/internal/pwgen"
	"gokeep/internal/pwquality"
	"gokeep/internal/vault"
)

// GeneratorService exposes the password generator and the strength estimate.
type GeneratorService struct {
	manager *Manager
}

// NewGeneratorService wires the service to the shared state.
func NewGeneratorService(m *Manager) *GeneratorService { return &GeneratorService{manager: m} }

// GenerateOptions is the result of a generator run, including the estimate.
type GenerateOptions struct {
	Password string  `json:"password"`
	Bits     float64 `json:"bits"`
	Score    int     `json:"score"`
	Label    string  `json:"label"`
	Error    string  `json:"error"`
}

// Generate creates a password with the given options.
func (s *GeneratorService) Generate(opts pwgen.Options) GenerateOptions {
	password, err := pwgen.Generate(opts)
	if err != nil {
		return GenerateOptions{Error: err.Error()}
	}
	bits, score := pwquality.Estimate(password, false)
	return GenerateOptions{
		Password: password,
		Bits:     bits,
		Score:    score,
		Label:    pwquality.Label(score),
	}
}

// GenerateAndStore creates a password and writes it into the password field of an entry
// that is already open in the editor, following the KeePass generator dialog.
func (s *GeneratorService) GenerateAndStore(entryID string, opts pwgen.Options, field string) (GenerateOptions, error) {
	result := s.Generate(opts)
	if result.Error != "" {
		return result, fmt.Errorf("%s", result.Error)
	}
	if entryID == "" {
		return result, nil
	}
	v, err := s.manager.RequireVault()
	if err != nil {
		return result, err
	}
	dto, err := v.Entry(entryID)
	if err != nil {
		return result, err
	}
	if field == "" {
		field = kdbx.FieldPassword
	}
	switch field {
	case kdbx.FieldPassword:
		dto.Password = result.Password
	default:
		replaced := false
		for i := range dto.Fields {
			if dto.Fields[i].Key == field {
				dto.Fields[i].Value = result.Password
				replaced = true
			}
		}
		if !replaced {
			dto.Fields = append(dto.Fields, vault.FieldDTO{Key: field, Value: result.Password, Protected: true})
		}
	}
	if err := v.UpdateEntry(dto); err != nil {
		return result, err
	}
	return result, nil
}

// Evaluate returns the strength estimate of a password.
func (s *GeneratorService) Evaluate(password string) GenerateOptions {
	bits, score := pwquality.Estimate(password, false)
	return GenerateOptions{Bits: bits, Score: score, Label: pwquality.Label(score)}
}

// Defaults returns the default generator options.
func (s *GeneratorService) Defaults() pwgen.Options { return pwgen.DefaultOptions() }

// SaveDefaults remembers the generator options in the configuration.
func (s *GeneratorService) SaveDefaults(opts pwgen.Options) error {
	cfg := s.manager.Config().Get()
	cfg.Generator = opts
	return s.manager.Config().Set(cfg)
}

// ValidatePattern checks a generator pattern.
func (s *GeneratorService) ValidatePattern(pattern string) error {
	return pwgen.ValidatePattern(pattern)
}

// WritePasswordFile is a development helper that writes a password to a file, which the
// tools menu uses to export a single password without the clipboard.
func (s *GeneratorService) WritePasswordFile(path, text string) error {
	if path == "" {
		return fmt.Errorf("a file name is required")
	}
	return os.WriteFile(path, []byte(text), 0o600)
}
