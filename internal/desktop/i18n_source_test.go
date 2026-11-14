package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The language packs are the only place a sentence the user reads lives, and m.tf fills
// its {placeholders} by name. Two mistakes are silent and painful: a sentence that asks
// for a name nobody passes shows the raw {name}, and a call that passes a name the
// sentence does not use drops the value. Both are checked here against the calls in this
// package, so "wrong password" can never come out as "错误的 {name}".
func TestEverySentenceMatchesTheCallThatShowsIt(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot locate the package directory: %v", err)
	}
	sentences := loadReferencePack(t, filepath.Join(dir, "locales", "zh-CN.json"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("cannot read the package directory: %v", err)
	}
	files := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("cannot read %s: %v", name, err)
		}
		files++
		for _, problem := range checkTranslatedCalls(string(source), sentences) {
			t.Errorf("%s: %s", name, problem)
		}
	}
	t.Logf("checked %d language pack sentences against the m.t/m.tf calls of %d files",
		len(sentences), files)
}

// loadReferencePack flattens a pack into "section.key" -> sentence. The Simplified
// Chinese pack is the reference: tools/i18n-check.mjs keeps every pack at that key set.
func loadReferencePack(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	var nested map[string]any
	if err := json.Unmarshal(data, &nested); err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
	out := map[string]string{}
	var walk func(prefix string, value map[string]any)
	walk = func(prefix string, value map[string]any) {
		for key, raw := range value {
			name := key
			if prefix != "" {
				name = prefix + "." + key
			}
			switch typed := raw.(type) {
			case string:
				out[name] = typed
			case map[string]any:
				walk(name, typed)
			}
		}
	}
	walk("", nested)
	return out
}

// placeholderPattern matches the {name} a sentence fills in.
var placeholderPattern = regexp.MustCompile(`\{(\w+)\}`)

// checkTranslatedCalls reports every m.t/m.tf call whose key or placeholder names do not
// line up with the sentence. The source is read the way the compiler reads it: comments
// and string literals are skipped while the calls are found, and a call's arguments run
// up to the parenthesis that closes it, not to the first one.
func checkTranslatedCalls(source string, sentences map[string]string) []string {
	var problems []string
	for i := 0; i < len(source); {
		switch {
		case source[i] == '"' || source[i] == '\'' || source[i] == '`':
			i = skipLiteral(source, i, source[i])
		case strings.HasPrefix(source[i:], "//"):
			for i < len(source) && source[i] != '\n' {
				i++
			}
		case strings.HasPrefix(source[i:], "/*"):
			end := strings.Index(source[i+2:], "*/")
			if end < 0 {
				i = len(source)
			} else {
				i += 2 + end + 2
			}
		case (i == 0 || !isIdentifierByte(source[i-1])) && strings.HasPrefix(source[i:], "m.t"):
			open := i + len("m.t")
			format := open < len(source) && source[open] == 'f'
			if format {
				open++
			}
			if open >= len(source) || source[open] != '(' {
				i++
				continue
			}
			args, end := readCallArguments(source, open)
			line := 1 + strings.Count(source[:i], "\n")
			problems = append(problems, checkOneCall(line, args, format, sentences)...)
			i = end
		default:
			i++
		}
	}
	return problems
}

// checkOneCall compares one call with the sentence its key names.
func checkOneCall(line int, args []string, format bool, sentences map[string]string) []string {
	if len(args) == 0 {
		return []string{fmt.Sprintf("%d: the call passes no key", line)}
	}
	key, ok := stringLiteral(args[0])
	if !ok {
		// A key built at runtime cannot be checked against a pack.
		return nil
	}
	sentence, known := sentences[key]
	if !known {
		return []string{fmt.Sprintf("%d: %s is not in the language packs", line, key)}
	}
	wanted := placeholdersOf(sentence)
	where := fmt.Sprintf("%d: %s", line, key)
	if !format {
		if len(wanted) > 0 {
			return []string{fmt.Sprintf("%s wants %v but m.t passes no value", where, wanted)}
		}
		return nil
	}
	rest := args[1:]
	if len(rest)%2 != 0 {
		return []string{fmt.Sprintf("%s has an odd number of m.tf arguments", where)}
	}
	passed := make([]string, 0, len(rest)/2)
	for i := 0; i < len(rest); i += 2 {
		name, ok := stringLiteral(rest[i])
		if !ok {
			// The name is computed at runtime, so no pack can be checked against it.
			return nil
		}
		passed = append(passed, name)
	}
	sort.Strings(passed)
	if !equalStrings(wanted, passed) {
		return []string{fmt.Sprintf("%s wants %v but the call passes %v", where, wanted, passed)}
	}
	return nil
}

// placeholdersOf lists the {names} a sentence uses, sorted.
func placeholdersOf(sentence string) []string {
	names := []string{}
	for _, match := range placeholderPattern.FindAllStringSubmatch(sentence, -1) {
		names = append(names, match[1])
	}
	sort.Strings(names)
	return names
}

// stringLiteral reads a plain Go string literal, reporting false for every other
// expression.
func stringLiteral(argument string) (string, bool) {
	argument = strings.TrimSpace(argument)
	if len(argument) < 2 || argument[0] != '"' {
		return "", false
	}
	value, err := strconv.Unquote(argument)
	if err != nil {
		return "", false
	}
	return value, true
}

// readCallArguments returns the arguments of the call whose "(" sits at open, and the
// index just past the ")" that closes it.
func readCallArguments(source string, open int) ([]string, int) {
	depth := 0
	for i := open; i < len(source); {
		switch {
		case source[i] == '"' || source[i] == '\'' || source[i] == '`':
			i = skipLiteral(source, i, source[i])
		case source[i] == '(':
			depth++
			i++
		case source[i] == ')':
			depth--
			i++
			if depth == 0 {
				return splitArguments(source[open+1 : i-1]), i
			}
		default:
			i++
		}
	}
	return nil, len(source)
}

// splitArguments splits a call's arguments at the commas that are not inside a nested
// expression or a literal.
func splitArguments(text string) []string {
	args := []string{}
	depth := 0
	start := 0
	for i := 0; i < len(text); {
		switch {
		case text[i] == '"' || text[i] == '\'' || text[i] == '`':
			i = skipLiteral(text, i, text[i])
		case text[i] == '(' || text[i] == '{' || text[i] == '[':
			depth++
			i++
		case text[i] == ')' || text[i] == '}' || text[i] == ']':
			depth--
			i++
		case text[i] == ',' && depth == 0:
			args = append(args, strings.TrimSpace(text[start:i]))
			i++
			start = i
		default:
			i++
		}
	}
	if last := strings.TrimSpace(text[start:]); last != "" || len(args) > 0 {
		args = append(args, last)
	}
	return args
}

// skipLiteral returns the index just past the literal that starts at start. A backtick
// string has no escapes; a quoted one skips the character after a backslash.
func skipLiteral(source string, start int, quote byte) int {
	for i := start + 1; i < len(source); i++ {
		switch {
		case source[i] == '\\' && quote != '`':
			i++
		case source[i] == quote:
			return i + 1
		case source[i] == '\n' && quote != '`':
			return i
		}
	}
	return len(source)
}

func isIdentifierByte(ch byte) bool {
	return ch == '_' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
