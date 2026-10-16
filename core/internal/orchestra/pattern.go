package orchestra

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Pattern is a selector field value: either an exact-match literal
// string or a compiled regular expression. On the wire (JSON) a
// Pattern decodes from either a plain string or an object shaped
// {"regex": string, "flags"?: string}. UnmarshalJSON and MarshalJSON
// must keep accepting and producing exactly those two shapes, since
// external tool input is validated against this same shape before it
// reaches Selector.
type Pattern struct {
	literal string
	regex   *regexp.Regexp
}

// LiteralPattern builds a Pattern that matches a value by exact
// string equality.
func LiteralPattern(s string) Pattern {
	return Pattern{literal: s}
}

// RegexPattern builds a Pattern that matches a value against a
// compiled regular expression.
func RegexPattern(re *regexp.Regexp) Pattern {
	return Pattern{regex: re}
}

// Matches reports whether value satisfies the pattern: exact string
// equality for a literal pattern, or an unanchored search for a
// regex pattern, matching JavaScript's RegExp.prototype.test.
func (p Pattern) Matches(value string) bool {
	if p.regex != nil {
		return p.regex.MatchString(value)
	}
	return value == p.literal
}

// segmentSeparator joins the parts of a value that a cross-platform
// semantics tree merged into one attribute, such as a title and its
// subtitle.
const segmentSeparator = "\n"

// MatchesSegment reports whether value holds newline-separated
// segments and one of them equals the literal exactly. It is always
// false for a regex pattern, an empty literal, and a value without a
// newline, so the common single-line value costs one byte scan and no
// allocation. Segments are compared with the same exact equality
// Matches uses for a whole value.
func (p Pattern) MatchesSegment(value string) bool {
	if p.regex != nil || p.literal == "" || !strings.Contains(value, segmentSeparator) {
		return false
	}
	rest := value
	for {
		segment, tail, more := strings.Cut(rest, segmentSeparator)
		if segment == p.literal {
			return true
		}
		if !more {
			return false
		}
		rest = tail
	}
}

// segmentCapable reports whether the pattern can ever match a
// segment: a non-empty literal.
func (p Pattern) segmentCapable() bool {
	return p.regex == nil && p.literal != ""
}

// UnmarshalJSON decodes either a JSON string (a literal pattern) or
// an object {"regex": string, "flags"?: string} (a regex pattern
// compiled through compileJSRegex).
func (p *Pattern) UnmarshalJSON(data []byte) error {
	var literal string
	if err := json.Unmarshal(data, &literal); err == nil {
		*p = Pattern{literal: literal}
		return nil
	}

	var wire struct {
		Regex string `json:"regex"`
		Flags string `json:"flags"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("pattern must be a string or {regex, flags} object: %w", err)
	}
	compiled, err := compileJSRegex(wire.Regex, wire.Flags)
	if err != nil {
		return fmt.Errorf("compiling pattern regex %q with flags %q: %w", wire.Regex, wire.Flags, err)
	}
	*p = Pattern{regex: compiled}
	return nil
}

// MarshalJSON encodes a literal pattern as a plain JSON string and a
// regex pattern as {"regex": string}, the inverse of UnmarshalJSON.
func (p Pattern) MarshalJSON() ([]byte, error) {
	if p.regex != nil {
		return json.Marshal(struct {
			Regex string `json:"regex"`
		}{Regex: p.regex.String()})
	}
	return json.Marshal(p.literal)
}

// compileJSRegex translates a JavaScript regular expression source
// and flag string into a Go regexp.Regexp. Supported flags: "i"
// (case-insensitive), "m" (multiline anchors), "s" (dot matches
// newline). "g", "y", "u", and "d" are accepted but have no effect:
// every Pattern match is a single unanchored MatchString call, the
// same operation JavaScript's RegExp.prototype.test performs on a
// regex with none of those flags set.
//
// Go's regexp package runs on RE2, which guarantees linear-time
// matching by construction and therefore cannot express backtracking
// constructs: lookahead, lookbehind, and backreferences. A JS source
// pattern using any of them would either fail to compile with a
// confusing RE2 syntax error or, worse, compile to a Go pattern that
// matches something silently different from what the same source
// matches in JavaScript. rejectUnsupportedJSRegexSyntax detects these
// constructs up front and fails with an explicit, actionable error
// instead of risking that divergence.
func compileJSRegex(source, flags string) (*regexp.Regexp, error) {
	if err := rejectUnsupportedJSRegexSyntax(source); err != nil {
		return nil, err
	}

	var goFlags strings.Builder
	for _, f := range flags {
		switch f {
		case 'i', 'm', 's':
			goFlags.WriteRune(f)
		case 'g', 'y', 'u', 'd':
			// No stateful or per-call effect on a single MatchString.
		default:
			return nil, fmt.Errorf("unsupported regex flag %q", string(f))
		}
	}
	pattern := source
	if goFlags.Len() > 0 {
		pattern = "(?" + goFlags.String() + ")" + source
	}
	return regexp.Compile(pattern)
}

// rejectUnsupportedJSRegexSyntax scans source for JavaScript regex
// constructs Go's RE2-based regexp package cannot express: lookahead
// ("(?=", "(?!"), lookbehind ("(?<=", "(?<!"), and backreferences
// ("\1" through "\9"). It returns a descriptive error naming the
// construct and its position on the first one found, or nil when
// source contains none.
//
// The scan tracks character-class state (`[...]`) because a
// backslash-digit or a literal "(?=" inside a class is not the
// backtracking construct it would be outside one; it does not
// attempt to detect JS-only named-group syntax ("(?<name>"), which
// is out of scope for this check.
func rejectUnsupportedJSRegexSyntax(source string) error {
	inClass := false
	for i := 0; i < len(source); i++ {
		c := source[i]
		switch {
		case c == '\\':
			if !inClass && i+1 < len(source) && source[i+1] >= '1' && source[i+1] <= '9' {
				return fmt.Errorf(
					"regex %q uses backreference \\%c at position %d, which Go's RE2 regex engine cannot express (no backtracking support)",
					source, source[i+1], i,
				)
			}
			i++ // the escaped character is never itself a class delimiter or group opener.
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case inClass:
			// Parens inside a character class are literal, not a group.
		case c == '(' && i+2 < len(source) && source[i+1] == '?':
			switch {
			case source[i+2] == '=':
				return fmt.Errorf("regex %q uses lookahead \"(?=\" at position %d, which Go's RE2 regex engine cannot express (no backtracking support)", source, i)
			case source[i+2] == '!':
				return fmt.Errorf("regex %q uses negative lookahead \"(?!\" at position %d, which Go's RE2 regex engine cannot express (no backtracking support)", source, i)
			case i+3 < len(source) && source[i+2] == '<' && source[i+3] == '=':
				return fmt.Errorf("regex %q uses lookbehind \"(?<=\" at position %d, which Go's RE2 regex engine cannot express (no backtracking support)", source, i)
			case i+3 < len(source) && source[i+2] == '<' && source[i+3] == '!':
				return fmt.Errorf("regex %q uses negative lookbehind \"(?<!\" at position %d, which Go's RE2 regex engine cannot express (no backtracking support)", source, i)
			}
		}
	}
	return nil
}

// Literal returns the exact string of a literal pattern. The second
// result is false for a regex pattern, which has no single string.
func (p Pattern) Literal() (string, bool) {
	if p.regex != nil {
		return "", false
	}
	return p.literal, true
}

// NewRegexPattern compiles a JavaScript-syntax regular expression
// source and flag string into a regex Pattern, applying the same
// translation, flag set, and unsupported-construct rejection the JSON
// {"regex", "flags"} form goes through. It returns the compile error
// unchanged so a caller can report it at the point the pattern is
// authored.
func NewRegexPattern(source, flags string) (Pattern, error) {
	re, err := compileJSRegex(source, flags)
	if err != nil {
		return Pattern{}, err
	}
	return RegexPattern(re), nil
}
