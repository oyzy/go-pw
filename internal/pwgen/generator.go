// Package pwgen implements the password generator of KeePass 2: character set
// selection, excluded look-alike characters, guaranteed character groups and the
// pattern based generator.
package pwgen

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode"

	"gokeep/internal/pwquality"
)

// The character sets KeePass offers for generated passwords.
const (
	Upper    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	Lower    = "abcdefghijklmnopqrstuvwxyz"
	Digits   = "0123456789"
	Minus    = "-"
	Under    = "_"
	Space    = " "
	Special  = "!\"#$%&'()*+,./:;<=>?@[\\]^`{|}~"
	Brackets = "[]{}()<>"
	HexLower = "0123456789abcdef"
	HexUpper = "0123456789ABCDEF"
)

// LookAlikes are the characters KeePass can exclude because they are easy to confuse.
const LookAlikes = "O0Il1|`'\""

// Options configures a generated password.
type Options struct {
	Length           int    `json:"length"`
	Upper            bool   `json:"upper"`
	Lower            bool   `json:"lower"`
	Digits           bool   `json:"digits"`
	Minus            bool   `json:"minus"`
	Under            bool   `json:"under"`
	Space            bool   `json:"space"`
	Special          bool   `json:"special"`
	Brackets         bool   `json:"brackets"`
	Hex              bool   `json:"hex"`
	ExcludeLookAlike bool   `json:"excludeLookAlike"`
	EachGroupAtLeast int    `json:"eachGroupAtLeast"`
	NoRepeating      bool   `json:"noRepeating"`
	CustomSet        string `json:"customSet"`
	Pattern          string `json:"pattern"`
	UsePattern       bool   `json:"usePattern"`
}

// DefaultOptions returns the settings KeePass uses for a new password.
func DefaultOptions() Options {
	return Options{
		Length:           20,
		Upper:            true,
		Lower:            true,
		Digits:           true,
		Special:          true,
		EachGroupAtLeast: 0,
	}
}

// CharSets returns the enabled character sets in the order KeePass uses.
func (o Options) CharSets() []string {
	var sets []string
	if o.Upper {
		sets = append(sets, Upper)
	}
	if o.Lower {
		sets = append(sets, Lower)
	}
	if o.Digits {
		sets = append(sets, Digits)
	}
	if o.Special {
		sets = append(sets, Special)
	}
	if o.Minus {
		sets = append(sets, Minus)
	}
	if o.Under {
		sets = append(sets, Under)
	}
	if o.Space {
		sets = append(sets, Space)
	}
	if o.Brackets {
		sets = append(sets, Brackets)
	}
	if o.Hex && !o.Digits {
		sets = append(sets, HexLower)
	}
	return sets
}

// Alphabet returns the pool of characters a generated password is drawn from.
func (o Options) Alphabet() string {
	if o.CustomSet != "" {
		return o.CustomSet
	}
	sets := o.CharSets()
	if len(sets) == 0 {
		sets = []string{Lower}
	}
	alphabet := strings.Join(sets, "")
	if o.ExcludeLookAlike {
		alphabet = removeAll(alphabet, LookAlikes)
	}
	return alphabet
}

// Generate returns a new password according to the options.
func Generate(o Options) (string, error) {
	if o.UsePattern && strings.TrimSpace(o.Pattern) != "" {
		return generateFromPattern(o.Pattern, o.ExcludeLookAlike)
	}
	if o.Length <= 0 {
		o.Length = 20
	}
	alphabet := o.Alphabet()
	if len([]rune(alphabet)) < 2 {
		return "", errors.New("pwgen: the selected character set is too small")
	}
	if o.EachGroupAtLeast > 0 {
		return generateWithGroups(o, alphabet)
	}
	return generateFixed(o, alphabet)
}

func generateFixed(o Options, alphabet string) (string, error) {
	runes := []rune(alphabet)
	var b strings.Builder
	var previous rune
	for i := 0; i < o.Length; i++ {
		for attempt := 0; attempt < 100; attempt++ {
			r, err := randomRune(runes)
			if err != nil {
				return "", err
			}
			if o.NoRepeating && r == previous {
				continue
			}
			b.WriteRune(r)
			previous = r
			break
		}
	}
	return b.String(), nil
}

// generateWithGroups makes sure every selected character set appears at least n times,
// which is KeePass' "(each group at least n characters)" option.
func generateWithGroups(o Options, alphabet string) (string, error) {
	password, err := generateFixed(o, alphabet)
	if err != nil {
		return "", err
	}
	sets := o.CharSets()
	if o.CustomSet != "" {
		sets = []string{o.CustomSet}
	}
	runes := []rune(password)
	// Replacing a character to satisfy one group can reduce the count of another, so the
	// whole set of groups is checked again until every group is happy.
	for pass := 0; pass < 32; pass++ {
		changed := false
		for _, set := range sets {
			chars := []rune(set)
			if o.ExcludeLookAlike {
				chars = []rune(removeAll(string(chars), LookAlikes))
			}
			if len(chars) == 0 {
				continue
			}
			present := 0
			for _, r := range runes {
				if strings.ContainsRune(string(chars), r) {
					present++
				}
			}
			for present < o.EachGroupAtLeast {
				r, err := randomRune(chars)
				if err != nil {
					return "", err
				}
				pos, err := randomInt(len(runes))
				if err != nil {
					return "", err
				}
				runes[pos] = r
				present++
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return string(runes), nil
}

// generateFromPattern implements the KeePass pattern generator:
// a lower case alphanumeric, A upper case alphanumeric, d digit, s special,
// h hex lower case, H hex upper case, \\ escapes the next character, [..] adds custom
// characters and {n} repeats the previous group n times.
func generateFromPattern(pattern string, excludeLookAlike bool) (string, error) {
	var alphabet strings.Builder
	var output strings.Builder
	runes := []rune(pattern)
	pool := func(chars string) string {
		if excludeLookAlike {
			return removeAll(chars, LookAlikes)
		}
		return chars
	}
	appendGroup := func(chars string, count int) error {
		chars = pool(chars)
		if chars == "" {
			return errors.New("pwgen: pattern uses an empty character group")
		}
		alphabet.WriteString(chars)
		for i := 0; i < count; i++ {
			r, err := randomRune([]rune(chars))
			if err != nil {
				return err
			}
			output.WriteRune(r)
		}
		return nil
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '\\':
			if i+1 < len(runes) {
				i++
				if err := appendGroup(string(runes[i]), 1); err != nil {
					return "", err
				}
			}
		case '[':
			end := i + 1
			var custom strings.Builder
			for end < len(runes) && runes[end] != ']' {
				if runes[end] == '\\' && end+1 < len(runes) {
					end++
				}
				custom.WriteRune(runes[end])
				end++
			}
			if end >= len(runes) {
				return "", errors.New("pwgen: unterminated character group in pattern")
			}
			if err := appendGroup(custom.String(), 1); err != nil {
				return "", err
			}
			i = end
		case 'a':
			if err := appendGroup(Lower+Digits, 1); err != nil {
				return "", err
			}
		case 'A':
			if err := appendGroup(Upper+Digits, 1); err != nil {
				return "", err
			}
		case 'd':
			if err := appendGroup(Digits, 1); err != nil {
				return "", err
			}
		case 's':
			if err := appendGroup(Special, 1); err != nil {
				return "", err
			}
		case 'h':
			if err := appendGroup(HexLower, 1); err != nil {
				return "", err
			}
		case 'H':
			if err := appendGroup(HexUpper, 1); err != nil {
				return "", err
			}
		default:
			if err := appendGroup(string(r), 1); err != nil {
				return "", err
			}
		}
	}
	if output.Len() == 0 {
		return "", errors.New("pwgen: the pattern does not produce any characters")
	}
	return output.String(), nil
}

// Entropy returns the estimated strength of a configuration.
func Entropy(o Options) (bits float64, score int, label string) {
	password, err := Generate(o)
	if err != nil {
		return 0, 0, "invalid"
	}
	bits, score = pwquality.Estimate(password, false)
	return bits, score, pwquality.Label(score)
}

// ValidatePattern checks a pattern without generating a password.
func ValidatePattern(pattern string) error {
	_, err := generateFromPattern(pattern, false)
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	return nil
}

func randomInt(max int) (int, error) {
	if max <= 0 {
		return 0, errors.New("pwgen: invalid range")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

func randomRune(alphabet []rune) (rune, error) {
	if len(alphabet) == 0 {
		return 0, errors.New("pwgen: empty alphabet")
	}
	i, err := randomInt(len(alphabet))
	if err != nil {
		return 0, err
	}
	return alphabet[i], nil
}

func removeAll(s, chars string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(chars, r) {
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
