package expression

import (
	"fmt"
	"unicode/utf16"
)

var twoCharPunct = map[string]bool{
	"==": true,
	"!=": true,
	"<=": true,
	">=": true,
}

var oneCharPunct = map[uint16]bool{
	'(': true,
	')': true,
	'+': true,
	'-': true,
	'*': true,
	'/': true,
	'%': true,
	'<': true,
	'>': true,
}

// tokenize splits expression source into tokens for the parser. Every
// position it produces is a UTF-16 code-unit offset rather than a byte
// or rune offset, so a position lines up with the character counting
// the Monaco editor uses when it turns a Position into a column to
// underline. A character outside the Basic Multilingual Plane
// therefore advances the position by 2, one for each surrogate half.
//
// A number token is unsigned digits with an optional single decimal
// point, because a leading sign is grammar for unary minus, not part of
// the literal. String tokens resolve backslash escapes inline so the
// parser sees the final string value directly.
func tokenize(source string) ([]Token, error) {
	units := utf16.Encode([]rune(source))
	length := len(units)
	var tokens []Token
	i := 0

	for i < length {
		ch := units[i]

		if isSpace(ch) {
			i++
			continue
		}

		if isDigit(ch) {
			start := i
			i = consumeDigits(units, i)
			if i < length && units[i] == '.' && i+1 < length && isDigit(units[i+1]) {
				i = consumeDigits(units, i+1)
			}
			tokens = append(tokens, Token{Kind: TokenNumber, Value: decodeUnits(units[start:i]), Position: start})
			continue
		}

		if ch == '"' || ch == '\'' {
			start := i
			value, endIndex, err := readStringLiteral(units, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, Token{Kind: TokenString, Value: value, Position: start})
			i = endIndex
			continue
		}

		if isIdentifierStart(ch) {
			start := i
			i++
			for i < length && isIdentifierPart(units[i]) {
				i++
			}
			tokens = append(tokens, Token{Kind: TokenIdentifier, Value: decodeUnits(units[start:i]), Position: start})
			continue
		}

		if i+1 < length {
			two := decodeUnits(units[i : i+2])
			if twoCharPunct[two] {
				tokens = append(tokens, Token{Kind: TokenPunct, Value: two, Position: i})
				i += 2
				continue
			}
		}

		if oneCharPunct[ch] {
			tokens = append(tokens, Token{Kind: TokenPunct, Value: decodeUnits(units[i : i+1]), Position: i})
			i++
			continue
		}

		return nil, &ParseError{
			Message:  fmt.Sprintf(`unexpected character "%s" in expression`, decodeUnits(units[i:i+1])),
			Position: i,
		}
	}

	tokens = append(tokens, Token{Kind: TokenEOF, Value: "", Position: length})
	return tokens, nil
}

func isDigit(ch uint16) bool {
	return ch >= '0' && ch <= '9'
}

func isIdentifierStart(ch uint16) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentifierPart(ch uint16) bool {
	return isIdentifierStart(ch) || isDigit(ch)
}

func isSpace(ch uint16) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func consumeDigits(units []uint16, start int) int {
	i := start
	for i < len(units) && isDigit(units[i]) {
		i++
	}
	return i
}

func decodeUnits(units []uint16) string {
	return string(utf16.Decode(units))
}

// readStringLiteral decodes the string literal beginning at start (the
// index of the opening quote) and returns its resolved value plus the
// index just past the closing quote. It returns a ParseError positioned
// at start when the literal is unterminated: either the source ends
// before a matching quote, or a trailing backslash has nothing after it
// to escape.
func readStringLiteral(units []uint16, start int) (string, int, error) {
	quote := units[start]
	i := start + 1
	var value []uint16

	for i < len(units) {
		ch := units[i]
		if ch == quote {
			return decodeUnits(value), i + 1, nil
		}
		if ch == '\\' {
			if i+1 >= len(units) {
				break
			}
			escaped := units[i+1]
			replacement, err := unescape(escaped, i)
			if err != nil {
				return "", 0, err
			}
			value = append(value, replacement)
			i += 2
			continue
		}
		value = append(value, ch)
		i++
	}

	return "", 0, &ParseError{Message: "unterminated string literal", Position: start}
}

func unescape(escaped uint16, backslashPosition int) (uint16, error) {
	switch escaped {
	case '\\':
		return '\\', nil
	case '"':
		return '"', nil
	case '\'':
		return '\'', nil
	case 'n':
		return '\n', nil
	case 't':
		return '\t', nil
	default:
		return 0, &ParseError{
			Message:  fmt.Sprintf(`unknown escape sequence "\%s" in string literal`, decodeUnits([]uint16{escaped})),
			Position: backslashPosition,
		}
	}
}
