package expression

import (
	"errors"
	"reflect"
	"testing"
)

func TestTokenizeSuccess(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   []Token
	}{
		{
			name:   "skips whitespace",
			source: " 1 + 2 ",
			want: []Token{
				{Kind: TokenNumber, Value: "1", Position: 1},
				{Kind: TokenPunct, Value: "+", Position: 3},
				{Kind: TokenNumber, Value: "2", Position: 5},
				{Kind: TokenEOF, Value: "", Position: 7},
			},
		},
		{
			name:   "tabs newlines and carriage returns are whitespace",
			source: "1\t+\n2\r",
			want: []Token{
				{Kind: TokenNumber, Value: "1", Position: 0},
				{Kind: TokenPunct, Value: "+", Position: 2},
				{Kind: TokenNumber, Value: "2", Position: 4},
				{Kind: TokenEOF, Value: "", Position: 6},
			},
		},
		{
			name:   "integer literal",
			source: "5",
			want: []Token{
				{Kind: TokenNumber, Value: "5", Position: 0},
				{Kind: TokenEOF, Value: "", Position: 1},
			},
		},
		{
			name:   "decimal literal",
			source: "12.34",
			want: []Token{
				{Kind: TokenNumber, Value: "12.34", Position: 0},
				{Kind: TokenEOF, Value: "", Position: 5},
			},
		},
		{
			name:   "identifier",
			source: "abc_123",
			want: []Token{
				{Kind: TokenIdentifier, Value: "abc_123", Position: 0},
				{Kind: TokenEOF, Value: "", Position: 7},
			},
		},
		{
			name:   "identifier starting with underscore",
			source: "_x1",
			want: []Token{
				{Kind: TokenIdentifier, Value: "_x1", Position: 0},
				{Kind: TokenEOF, Value: "", Position: 3},
			},
		},
		{
			name:   "simple string literal",
			source: `"hello"`,
			want: []Token{
				{Kind: TokenString, Value: "hello", Position: 0},
				{Kind: TokenEOF, Value: "", Position: 7},
			},
		},
		{
			name:   "single quoted string literal",
			source: `'hello'`,
			want: []Token{
				{Kind: TokenString, Value: "hello", Position: 0},
				{Kind: TokenEOF, Value: "", Position: 7},
			},
		},
		{
			name:   "string literal with newline and tab escapes",
			source: `"a\nb\tc"`,
			want: []Token{
				{Kind: TokenString, Value: "a\nb\tc", Position: 0},
				{Kind: TokenEOF, Value: "", Position: 9},
			},
		},
		{
			name:   "string literal with escaped quote and backslash",
			source: `"say \"hi\" \\ ok"`,
			want: []Token{
				{Kind: TokenString, Value: `say "hi" \ ok`, Position: 0},
				{Kind: TokenEOF, Value: "", Position: 18},
			},
		},
		{
			name:   "string literal with escaped single quote inside double quotes",
			source: `"it\'s"`,
			want: []Token{
				{Kind: TokenString, Value: "it's", Position: 0},
				{Kind: TokenEOF, Value: "", Position: 7},
			},
		},
		{
			name:   "empty string literal",
			source: `""`,
			want: []Token{
				{Kind: TokenString, Value: "", Position: 0},
				{Kind: TokenEOF, Value: "", Position: 2},
			},
		},
		{
			name:   "non-ASCII BMP character in string literal advances by one",
			source: `"héllo" x`,
			want: []Token{
				{Kind: TokenString, Value: "héllo", Position: 0},
				{Kind: TokenIdentifier, Value: "x", Position: 8},
				{Kind: TokenEOF, Value: "", Position: 9},
			},
		},
		{
			name:   "astral character in string literal advances position by two",
			source: `"a😀b" x`,
			want: []Token{
				{Kind: TokenString, Value: "a😀b", Position: 0},
				{Kind: TokenIdentifier, Value: "x", Position: 7},
				{Kind: TokenEOF, Value: "", Position: 8},
			},
		},
		{
			name:   "two char punct beats one char punct",
			source: "== != <= >=",
			want: []Token{
				{Kind: TokenPunct, Value: "==", Position: 0},
				{Kind: TokenPunct, Value: "!=", Position: 3},
				{Kind: TokenPunct, Value: "<=", Position: 6},
				{Kind: TokenPunct, Value: ">=", Position: 9},
				{Kind: TokenEOF, Value: "", Position: 11},
			},
		},
		{
			name:   "one char punct",
			source: "( ) + - * / % < >",
			want: []Token{
				{Kind: TokenPunct, Value: "(", Position: 0},
				{Kind: TokenPunct, Value: ")", Position: 2},
				{Kind: TokenPunct, Value: "+", Position: 4},
				{Kind: TokenPunct, Value: "-", Position: 6},
				{Kind: TokenPunct, Value: "*", Position: 8},
				{Kind: TokenPunct, Value: "/", Position: 10},
				{Kind: TokenPunct, Value: "%", Position: 12},
				{Kind: TokenPunct, Value: "<", Position: 14},
				{Kind: TokenPunct, Value: ">", Position: 16},
				{Kind: TokenEOF, Value: "", Position: 17},
			},
		},
		{
			name:   "empty source is only EOF",
			source: "",
			want: []Token{
				{Kind: TokenEOF, Value: "", Position: 0},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := tokenize(c.source)
			if err != nil {
				t.Fatalf("tokenize(%q) returned error: %v", c.source, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("tokenize(%q) = %+v, want %+v", c.source, got, c.want)
			}
		})
	}
}

func TestTokenizeErrors(t *testing.T) {
	cases := []struct {
		name        string
		source      string
		wantMessage string
		wantPos     int
	}{
		{
			name:        "leading dot is not a valid number",
			source:      ".5",
			wantMessage: `unexpected character "." in expression`,
			wantPos:     0,
		},
		{
			name:        "trailing dot is not part of the number and is not punctuation either",
			source:      "5.",
			wantMessage: `unexpected character "." in expression`,
			wantPos:     1,
		},
		{
			name:        "bare equals sign is invalid",
			source:      "=",
			wantMessage: `unexpected character "=" in expression`,
			wantPos:     0,
		},
		{
			name:        "unexpected character after valid tokens",
			source:      "1 + #",
			wantMessage: `unexpected character "#" in expression`,
			wantPos:     4,
		},
		{
			name:        "unknown escape sequence",
			source:      `"a\xb"`,
			wantMessage: `unknown escape sequence "\x" in string literal`,
			wantPos:     2,
		},
		{
			name:        "unterminated string literal at EOF",
			source:      `"abc`,
			wantMessage: "unterminated string literal",
			wantPos:     0,
		},
		{
			name:        "unterminated string literal with trailing backslash",
			source:      `"abc\`,
			wantMessage: "unterminated string literal",
			wantPos:     0,
		},
		{
			name:        "unterminated string literal starting mid source",
			source:      `1 + "abc`,
			wantMessage: "unterminated string literal",
			wantPos:     4,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := tokenize(c.source)
			if err == nil {
				t.Fatalf("tokenize(%q) expected error, got none", c.source)
			}
			var parseErr *ParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("tokenize(%q) error type = %T, want *ParseError", c.source, err)
			}
			if parseErr.Message != c.wantMessage {
				t.Errorf("tokenize(%q) message = %q, want %q", c.source, parseErr.Message, c.wantMessage)
			}
			if parseErr.Position != c.wantPos {
				t.Errorf("tokenize(%q) position = %d, want %d", c.source, parseErr.Position, c.wantPos)
			}
		})
	}
}
