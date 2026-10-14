package runner

import (
	"sort"
	"strings"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/redact"
)

// Redactor masks secret values and token-shaped strings in any text a
// host is about to display. Every match becomes orchestra.SecureMask,
// so the output never reveals the length of what it hid. It is
// immutable and safe for concurrent use.
type Redactor struct {
	inner *redact.Redactor
	// short holds secret values below redact.MinSecretLen. They are masked
	// only as whole words, because a substring match would also hit
	// ordinary text.
	short []string
}

// String returns s with every secret value and token-shaped string
// replaced by orchestra.SecureMask.
func (r *Redactor) String(s string) string {
	out := strings.ReplaceAll(r.inner.String(s), redact.Mask, orchestra.SecureMask)
	for _, value := range r.short {
		out = maskWord(out, value)
	}
	return out
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// indexWord returns the index of the first occurrence of word in s that
// is not part of a longer run of letters, digits or underscores, or -1.
func indexWord(s, word string) int {
	for from := 0; from <= len(s)-len(word); {
		i := strings.Index(s[from:], word)
		if i < 0 {
			return -1
		}
		i += from
		end := i + len(word)
		if (i == 0 || !isWordByte(s[i-1])) && (end == len(s) || !isWordByte(s[end])) {
			return i
		}
		from = i + 1
	}
	return -1
}

func maskWord(s, word string) string {
	var b strings.Builder
	for {
		i := indexWord(s, word)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(orchestra.SecureMask)
		s = s[i+len(word):]
	}
}

// MentionsSecret reports whether text names a secret variable or holds a
// secret value as a whole word, however short the value is. A host uses it
// to withhold free text that a value-based mask cannot be trusted to clean.
func (s VariableSnapshot) MentionsSecret(text string) bool {
	for name, secret := range s.Secret {
		if !secret {
			continue
		}
		if indexWord(text, name) >= 0 {
			return true
		}
		if value, ok := s.Values[name]; ok {
			if v := scriptValueToEnvString(value); v != "" && indexWord(text, v) >= 0 {
				return true
			}
		}
	}
	return false
}

// Redactor builds a Redactor over the snapshot's secret values. A
// value shorter than redact.MinSecretLen is masked only where it stands
// as a whole word, because a substring match would also hit ordinary
// words. Redacted also hides such a value by name.
func (s VariableSnapshot) Redactor() *Redactor {
	names := make([]string, 0, len(s.Secret))
	for name, secret := range s.Secret {
		if secret {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	texts := make([]string, 0, len(names))
	for _, name := range names {
		if value, ok := s.Values[name]; ok {
			texts = append(texts, scriptValueToEnvString(value))
		}
	}
	return newRedactor(texts)
}

// newRedactor builds a Redactor over secret value texts. Empty texts are
// ignored.
func newRedactor(texts []string) *Redactor {
	values := make([]string, 0, len(texts))
	var short []string
	for _, text := range texts {
		switch {
		case len(text) >= redact.MinSecretLen:
			values = append(values, text)
		case text != "":
			short = append(short, text)
		}
	}
	return &Redactor{inner: redact.New(values), short: short}
}

// Redacted renders every variable for display. A secret variable shows
// as orchestra.SecureMask whatever its value, and any other value
// passes through the snapshot's Redactor.
func (s VariableSnapshot) Redacted() map[string]string {
	redactor := s.Redactor()
	out := make(map[string]string, len(s.Values))
	for name, value := range s.Values {
		if s.Secret[name] {
			out[name] = orchestra.SecureMask
			continue
		}
		out[name] = redactor.String(scriptValueToEnvString(value))
	}
	return out
}

// Environment renders the variables to the string form `${var}`
// placeholders resolve against, so a host can parse a step written
// while the run is paused against the run's current values.
func (s VariableSnapshot) Environment() map[string]string {
	return environmentFromVariables(s.Values)
}
