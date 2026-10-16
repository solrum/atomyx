package runner

import (
	"strings"

	"dev.atomyx/internal/script"
)

const (
	lowercaseLetters = "abcdefghijklmnopqrstuvwxyz"
	decimalDigits    = "0123456789"
	randomEmailHost  = "example.com"
)

var (
	randomFirstNames = []string{
		"Alex", "Bao", "Chloe", "Dmitri", "Elena", "Farid", "Grace", "Hiro", "Isabel", "Jamal",
		"Kavya", "Liam", "Mei", "Noah", "Olivia", "Priya", "Quinn", "Rosa", "Sven", "Tara",
	}
	randomLastNames = []string{
		"Adams", "Bennett", "Chen", "Dubois", "Evans", "Fischer", "Garcia", "Haddad", "Ito", "Jensen",
		"Khan", "Lopez", "Moreau", "Nguyen", "Okafor", "Petrov", "Rossi", "Silva", "Tanaka", "Walker",
	}
)

// generateRandomValue builds the text an inputRandom step types. length
// sizes the letters, digits or email name, and personName ignores it.
func generateRandomValue(kind string, length int, src RandomSource) string {
	switch kind {
	case script.RandomKindNumber:
		return randomString(decimalDigits, length, src)
	case script.RandomKindEmail:
		return randomString(lowercaseLetters, length, src) + "@" + randomEmailHost
	case script.RandomKindPersonName:
		first := randomFirstNames[src.IntN(len(randomFirstNames))]
		last := randomLastNames[src.IntN(len(randomLastNames))]
		return first + " " + last
	default:
		return randomString(lowercaseLetters, length, src)
	}
}

func randomString(alphabet string, length int, src RandomSource) string {
	var b strings.Builder
	b.Grow(length)
	for range length {
		b.WriteByte(alphabet[src.IntN(len(alphabet))])
	}
	return b.String()
}
