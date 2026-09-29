// Package pwquality estimates how hard a password is to guess. KeePass shows a similar
// number in its "quality" column and its password generator.
package pwquality

import (
	"math"
	"strings"
	"unicode"
)

// Estimate returns the Shannon style entropy in bits and a 0..100 score, the same scale
// KeePass uses for its quality estimate.
func Estimate(password string, ignore bool) (bits float64, score int) {
	if ignore || password == "" {
		return 0, 0
	}
	alphabet := 0
	var hasLower, hasUpper, hasDigit, hasSpecial, hasOther bool
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsSpace(r):
			hasSpecial = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		default:
			hasOther = true
		}
	}
	if hasLower {
		alphabet += 26
	}
	if hasUpper {
		alphabet += 26
	}
	if hasDigit {
		alphabet += 10
	}
	if hasSpecial {
		alphabet += 32
	}
	if hasOther {
		alphabet += 100
	}
	if alphabet == 0 {
		alphabet = 1
	}
	length := len([]rune(password))
	bits = float64(length) * math.Log2(float64(alphabet))

	// Penalise the common patterns that make a password much weaker than its length
	// suggests: repeated characters and runs of sequential characters.
	bits -= repetitionPenalty(password)
	if bits < 0 {
		bits = 0
	}
	return bits, scoreFromBits(bits)
}

func repetitionPenalty(password string) float64 {
	runes := []rune(strings.ToLower(password))
	penalty := 0.0
	same := 0
	for i := 1; i < len(runes); i++ {
		switch {
		case runes[i] == runes[i-1]:
			same++
		case isSequential(runes[i-1], runes[i]):
			penalty += 1.5
		default:
			same = 0
		}
		if same >= 2 {
			penalty += 2
		}
	}
	return penalty
}

func isSequential(a, b rune) bool {
	if b-a == 1 {
		return (a >= 'a' && a < 'z') || (a >= '0' && a < '9') || (a >= 'A' && a < 'Z')
	}
	return false
}

// scoreFromBits converts the entropy into the 0..100 scale used by KeePass.
func scoreFromBits(bits float64) int {
	switch {
	case bits >= 128:
		return 100
	case bits <= 0:
		return 0
	default:
		return int(math.Round(bits * 100 / 128))
	}
}

// Label returns a human readable description of a score.
func Label(score int) string {
	switch {
	case score >= 80:
		return "Excellent"
	case score >= 60:
		return "Good"
	case score >= 40:
		return "Fair"
	case score >= 20:
		return "Weak"
	default:
		return "Very weak"
	}
}
