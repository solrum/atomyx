package redact

import (
	"regexp"
	"sort"
	"strings"
)

// Mask replaces every secret found in a scrubbed value.
const Mask = "[REDACTED]"

// MinSecretLen is the shortest environment value treated as a secret.
// Shorter values such as "1", "dev" or "en" appear in ordinary text, so
// masking them would damage the output without protecting anything.
const MinSecretLen = 6

// MinTokenLen is the shortest unbroken run of hex digits or base64url
// characters scrubbed as a token.
const MinTokenLen = 32

var (
	bearerPattern = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]{8,}`)

	headerPattern = regexp.MustCompile(`(?i)(x-atomyx-token["']?\s*[:=]\s*["']?)[^\s"',;}]+`)

	hexPattern = regexp.MustCompile(`\b[0-9A-Fa-f]{32,}\b`)

	base64URLPattern = regexp.MustCompile(`[A-Za-z0-9_-]{32,}`)

	uuidPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
)

// nonSecretNames are environment variable names whose values describe the
// host rather than guard anything, so they stay readable.
var nonSecretNames = map[string]bool{
	"USER": true, "LOGNAME": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
	"TERM": true, "TERM_PROGRAM": true, "TERM_PROGRAM_VERSION": true, "COLORTERM": true,
	"SHELL": true, "TMPDIR": true, "PWD": true, "OLDPWD": true, "HOME": true,
	"EDITOR": true, "VISUAL": true, "PAGER": true, "HOSTNAME": true, "SHLVL": true,
	"GOOS": true, "GOARCH": true, "TZ": true,
}

// Redactor masks a fixed set of secret values and token-shaped strings.
// It is immutable after New and safe for concurrent use.
type Redactor struct {
	replacer *strings.Replacer
}

// New returns a Redactor that masks every occurrence of each value in
// values. Empty values are ignored. A value that contains another is
// masked whole, because replacement tries longer values first.
func New(values []string) *Redactor {
	seen := make(map[string]bool, len(values))
	unique := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		unique = append(unique, v)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		if len(unique[i]) != len(unique[j]) {
			return len(unique[i]) > len(unique[j])
		}
		return unique[i] < unique[j]
	})
	pairs := make([]string, 0, 2*len(unique))
	for _, v := range unique {
		pairs = append(pairs, v, Mask)
	}
	return &Redactor{replacer: strings.NewReplacer(pairs...)}
}

// String returns s with every known secret value and every token-shaped
// string replaced by Mask.
func (r *Redactor) String(s string) string {
	s = r.replacer.Replace(s)
	s = bearerPattern.ReplaceAllString(s, "${1}"+Mask)
	s = headerPattern.ReplaceAllString(s, "${1}"+Mask)
	s = hexPattern.ReplaceAllString(s, Mask)
	return base64URLPattern.ReplaceAllStringFunc(s, maskBase64URL)
}

// Bytes is String over a byte slice.
func (r *Redactor) Bytes(b []byte) []byte {
	return []byte(r.String(string(b)))
}

// maskBase64URL masks a long run only when it looks random: it must mix
// letters and digits, and a UUID is left alone because it identifies a
// device rather than guarding access.
func maskBase64URL(run string) string {
	if uuidPattern.MatchString(run) || strings.Contains(run, Mask) {
		return run
	}
	var letter, digit bool
	for _, c := range run {
		switch {
		case c >= '0' && c <= '9':
			digit = true
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			letter = true
		}
	}
	if letter && digit {
		return Mask
	}
	return run
}

// EnvValues returns the values of environ ("NAME=value" entries) that
// are worth masking. It drops a value shorter than MinSecretLen, the
// words true and false, a plain number, an absolute path, and the value
// of a host-description variable such as USER or HOME.
func EnvValues(environ []string) []string {
	var out []string
	for _, entry := range environ {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || nonSecretNames[name] || !secretCandidate(value) {
			continue
		}
		out = append(out, value)
	}
	return out
}

// EnvFileValues returns the values of a dotenv-style file, one
// "NAME=value" per line. Blank lines and lines starting with "#" are
// skipped, an "export " prefix is accepted, and one pair of matching
// quotes around a value is removed. The same filters as EnvValues apply.
func EnvFileValues(content []byte) []string {
	var out []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		if !ok || nonSecretNames[strings.TrimSpace(name)] {
			continue
		}
		value = unquote(strings.TrimSpace(value))
		if secretCandidate(value) {
			out = append(out, value)
		}
	}
	return out
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

func secretCandidate(v string) bool {
	if len(v) < MinSecretLen || strings.HasPrefix(v, "/") {
		return false
	}
	switch strings.ToLower(v) {
	case "true", "false":
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return true
		}
	}
	return false
}
