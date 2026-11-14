package desktop

import (
	"embed"
	"encoding/json"
	"strings"

	"github.com/vivaui/viva/app"
)

// The language packs are plain JSON files embedded in the binary, so every
// translation that existed keeps working.
//
//go:embed locales/*.json
var localeFS embed.FS

// Languages lists the packs in the order the language menu shows them.
var Languages = []struct{ Code, Label string }{
	{"zh-CN", "简体中文"},
	{"zh-TW", "繁體中文"},
	{"en", "English"},
	{"ja", "日本語"},
	{"ko", "한국어"},
}

const defaultLanguage = "en"

// bundle is a flattened language pack: "toolbar.save" -> "保存".
type bundle map[string]string

// i18n holds every loaded pack and the language in use. The keys are plain strings;
// a missing key falls back to English and then to the key itself, so a gap shows up
// as the key instead of as nothing.
type i18n struct {
	language string
	packs    map[string]bundle
}

func newI18n(language string) *i18n {
	i := &i18n{packs: map[string]bundle{}}
	for _, lang := range Languages {
		data, err := localeFS.ReadFile("locales/" + lang.Code + ".json")
		if err != nil {
			continue
		}
		var nested map[string]any
		if err := json.Unmarshal(data, &nested); err != nil {
			continue
		}
		flat := bundle{}
		flatten("", nested, flat)
		i.packs[lang.Code] = flat
	}
	i.language = normalizeLanguage(language)
	if language == "" {
		i.language = SystemLanguage()
	}
	return i
}

func flatten(prefix string, value map[string]any, out bundle) {
	for key, raw := range value {
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		switch typed := raw.(type) {
		case string:
			out[name] = typed
		case map[string]any:
			flatten(name, typed, out)
		}
	}
}

// T returns the translation of a dotted key.
func (i *i18n) T(key string) string {
	if i == nil {
		return key
	}
	if text, ok := i.packs[i.language][key]; ok {
		return text
	}
	if text, ok := i.packs[defaultLanguage][key]; ok {
		return text
	}
	return key
}

// Has reports whether the active pack defines a key.
func (i *i18n) Has(key string) bool {
	if i == nil {
		return false
	}
	_, ok := i.packs[i.language][key]
	return ok
}

// Language returns the active language code.
func (i *i18n) Language() string { return i.language }

// SetLanguage switches packs.
func (i *i18n) SetLanguage(language string) { i.language = normalizeLanguage(language) }

// Missing lists the keys the active pack does not define, which the check tool uses.
func (i *i18n) Missing() []string {
	var missing []string
	for key := range i.packs[defaultLanguage] {
		if _, ok := i.packs[i.language][key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

// SystemLanguage picks the pack closest to the operating system's preference.
func SystemLanguage() string {
	for _, tag := range app.PreferredLanguages() {
		if code := normalizeLanguage(tag); code != "" {
			return code
		}
	}
	return defaultLanguage
}

// normalizeLanguage maps a tag such as "zh-Hant-TW" or "ja-JP" onto a pack.
func normalizeLanguage(tag string) string {
	lower := strings.ToLower(strings.ReplaceAll(tag, "_", "-"))
	switch {
	case lower == "":
		return ""
	case strings.HasPrefix(lower, "zh"):
		if strings.Contains(lower, "tw") || strings.Contains(lower, "hk") ||
			strings.Contains(lower, "hant") || strings.Contains(lower, "mo") {
			return "zh-TW"
		}
		return "zh-CN"
	case strings.HasPrefix(lower, "ja"):
		return "ja"
	case strings.HasPrefix(lower, "ko"):
		return "ko"
	case strings.HasPrefix(lower, "en"):
		return "en"
	}
	return defaultLanguage
}
