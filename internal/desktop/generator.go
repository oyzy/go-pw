package desktop

import (
	"os"

	"gokeep/internal/pwgen"
)

// openGenerator opens the password generator, either standalone or so that its result
// can be used in the entry editor.
func (m *Model) openGenerator(target string) {
	opts := m.defaultGenerator()
	state := generatorState{open: true, opts: opts}
	if target == "editor" && m.editor.open {
		state.target = "editor"
	}
	m.gen = state
	m.regenerate()
}

// regenerate builds a new password from the dialog's settings.
func (m *Model) regenerate() {
	password, err := pwgen.Generate(m.gen.opts)
	if err != nil {
		m.gen.message = err.Error()
		m.gen.password = ""
		return
	}
	m.gen.message = ""
	m.gen.password = password
}

func clampLength(length int) int {
	return min(max(length, 1), 256)
}

func clampIcon(id int) int {
	return min(max(id, 0), 68)
}

func clampSeconds(seconds int) int {
	return min(max(seconds, 0), 600)
}

// writeFile writes a small file, used when exporting attachments and backups.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
