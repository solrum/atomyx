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
}

// String returns s with every secret value and token-shaped string
// replaced by orchestra.SecureMask.
func (r *Redactor) String(s string) string {
	return strings.ReplaceAll(r.inner.String(s), redact.Mask, orchestra.SecureMask)
}

// Redactor builds a Redactor over the snapshot's secret values. A
// value shorter than redact.MinSecretLen is not searched for in free
// text, because it would also match ordinary words. Redacted still
// hides such a value by name.
func (s VariableSnapshot) Redactor() *Redactor {
	names := make([]string, 0, len(s.Secret))
	for name, secret := range s.Secret {
		if secret {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	values := make([]string, 0, len(names))
	for _, name := range names {
		value, ok := s.Values[name]
		if !ok {
			continue
		}
		if text := scriptValueToEnvString(value); len(text) >= redact.MinSecretLen {
			values = append(values, text)
		}
	}
	return &Redactor{inner: redact.New(values)}
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
