package suite

import (
	"regexp"
	"strings"
)

// globSpecialChars are the regular expression metacharacters
// CompileGlob escapes before handing a literal run to regexp, so a
// literal character in the glob pattern never gets interpreted as
// regexp syntax.
const globSpecialChars = `.+^${}()|[]\`

// CompileGlob compiles pattern into a regular expression matched
// against a forward-slash relative path.
//
// Supported syntax:
//   - "*" matches any run of characters within one path segment.
//   - "**" matches any run of characters across segments, including
//     zero segments, so "**/*.yml" also matches a file directly under
//     the search root.
//   - "?" matches exactly one character within one path segment.
//   - Any other character is matched literally.
func CompileGlob(pattern string) *regexp.Regexp {
	normalized := strings.ReplaceAll(pattern, `\`, "/")
	runes := []rune(normalized)
	var source strings.Builder
	for i := 0; i < len(runes); {
		ch := runes[i]

		if ch == '*' && i+1 < len(runes) && runes[i+1] == '*' {
			followedBySlash := i+2 < len(runes) && runes[i+2] == '/'
			// A "**/" prefix may also match nothing, so "**/*.yml"
			// reaches files at the search root, not only nested ones.
			if followedBySlash {
				source.WriteString("(?:.*/)?")
				i += 3
			} else {
				source.WriteString(".*")
				i += 2
			}
			continue
		}

		if ch == '*' {
			source.WriteString("[^/]*")
			i++
			continue
		}

		if ch == '?' {
			source.WriteString("[^/]")
			i++
			continue
		}

		if strings.ContainsRune(globSpecialChars, ch) {
			source.WriteByte('\\')
		}
		source.WriteRune(ch)
		i++
	}
	return regexp.MustCompile("^" + source.String() + "$")
}

// MatchesGlob tests one forward-slash relative path against one glob
// pattern.
func MatchesGlob(relativePath, pattern string) bool {
	return CompileGlob(pattern).MatchString(relativePath)
}

// compileGlobPattern is the compilation step GlobMatcher uses to turn
// one pattern into a regular expression at construction time. Tests
// substitute this var to count compilations without touching any
// production call site.
var compileGlobPattern = CompileGlob

// GlobMatcher matches a relative path against a fixed set of glob
// patterns, compiling every pattern once at construction time. A
// caller that matches many paths against the same pattern set (a
// directory walk, for example) should build one GlobMatcher and reuse
// it, rather than calling MatchesGlob per path, so the regexp
// compilation cost is paid once per pattern instead of once per path.
type GlobMatcher struct {
	compiled []*regexp.Regexp
}

// NewGlobMatcher compiles every pattern in patterns once.
func NewGlobMatcher(patterns []string) *GlobMatcher {
	compiled := make([]*regexp.Regexp, len(patterns))
	for i, pattern := range patterns {
		compiled[i] = compileGlobPattern(pattern)
	}
	return &GlobMatcher{compiled: compiled}
}

// MatchAny reports whether relativePath matches at least one of the
// matcher's patterns.
func (m *GlobMatcher) MatchAny(relativePath string) bool {
	for _, re := range m.compiled {
		if re.MatchString(relativePath) {
			return true
		}
	}
	return false
}
