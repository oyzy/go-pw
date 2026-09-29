package pwgen

import (
	"strings"
	"testing"
)

func TestGenerateRespectsLengthAndAlphabet(t *testing.T) {
	options := DefaultOptions()
	options.Length = 24
	// Every selected group must appear at least once when that option is switched on;
	// without it the generator only guarantees the length and the alphabet.
	options.EachGroupAtLeast = 1
	password, err := Generate(options)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len([]rune(password)) != 24 {
		t.Fatalf("length = %d, want 24", len([]rune(password)))
	}
	if !strings.ContainsAny(password, Lower) || !strings.ContainsAny(password, Upper) ||
		!strings.ContainsAny(password, Digits) || !strings.ContainsAny(password, Special) {
		t.Errorf("password %q does not use every selected group", password)
	}
	// Every character has to come from the selected alphabet.
	alphabet := options.Alphabet()
	for _, r := range password {
		if !strings.ContainsRune(alphabet, r) {
			t.Fatalf("password %q contains %q which is not in the alphabet", password, r)
		}
	}
}

func TestExcludeLookAlikes(t *testing.T) {
	options := DefaultOptions()
	options.Length = 64
	options.ExcludeLookAlike = true
	for i := 0; i < 8; i++ {
		password, err := Generate(options)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range password {
			if strings.ContainsRune(LookAlikes, r) {
				t.Fatalf("password %q contains the look-alike character %q", password, r)
			}
		}
	}
}

func TestEachGroupAtLeast(t *testing.T) {
	options := Options{Length: 12, Upper: true, Lower: true, Digits: true, EachGroupAtLeast: 3}
	for i := 0; i < 20; i++ {
		password, err := Generate(options)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(password, "") == 0 {
			t.Fatal("empty password")
		}
		for _, set := range []string{Upper, Lower, Digits} {
			count := 0
			for _, r := range password {
				if strings.ContainsRune(set, r) {
					count++
				}
			}
			if count < 3 {
				t.Fatalf("password %q has only %d characters of group %q", password, count, set)
			}
		}
	}
}

func TestPatternGenerator(t *testing.T) {
	options := Options{UsePattern: true, Pattern: `Aa\-\d ds`}
	password, err := Generate(options)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// A, a, escaped "-", escaped "d", space, d, s -> seven characters, because a
	// backslash turns the next character into a literal.
	if len([]rune(password)) != 7 {
		t.Fatalf("pattern password %q has %d characters", password, len([]rune(password)))
	}
	// Positions 2, 3 and 4 come from the escaped and literal characters in the pattern,
	// so they must be exactly "-", "d" and a space. (The random groups may well contain a
	// backslash themselves, so the whole string is not checked for one.)
	runes := []rune(password)
	if runes[2] != '-' || runes[3] != 'd' || runes[4] != ' ' {
		t.Errorf("escape sequences were not handled: %q", password)
	}
	if err := ValidatePattern("[abc"); err == nil {
		t.Error("an unterminated class must be rejected")
	}
	if err := ValidatePattern(`\`); err == nil {
		t.Error("a pattern that produces no character must be rejected")
	}
	if err := ValidatePattern("[abc]"); err != nil {
		t.Errorf("a class only pattern must be accepted: %v", err)
	}
}

func TestCustomSet(t *testing.T) {
	options := Options{Length: 30, CustomSet: "ab"}
	password, err := Generate(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range password {
		if r != 'a' && r != 'b' {
			t.Fatalf("password %q contains %q", password, r)
		}
	}
	if _, err := Generate(Options{Length: 10, CustomSet: "x"}); err == nil {
		t.Error("a single character alphabet must be rejected")
	}
}

func TestEntropyEstimate(t *testing.T) {
	bits, score, label := Entropy(DefaultOptions())
	if bits < 80 || score < 60 {
		t.Errorf("a 20 character password scored %d (%s, %.1f bits)", score, label, bits)
	}
}
