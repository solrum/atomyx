package expression

// TokenKind classifies a lexical token produced by tokenize.
type TokenKind string

const (
	TokenNumber     TokenKind = "number"
	TokenString     TokenKind = "string"
	TokenIdentifier TokenKind = "identifier"
	TokenPunct      TokenKind = "punct"
	TokenEOF        TokenKind = "eof"
)

// Token is one lexical unit of expression source. Position is the
// zero-based UTF-16 code-unit offset of the token's first code unit
// within the source (length for the trailing EOF token).
//
// Value carries different content depending on Kind: for TokenString it
// is the decoded string value with the surrounding quotes stripped and
// escape sequences resolved, so the parser never re-parses escapes; for
// every other kind it is the raw source text of the token.
type Token struct {
	Kind     TokenKind
	Value    string
	Position int
}

// describeToken renders t for embedding in a parser error message. A
// decoded string token therefore appears without its quotes, so the
// message quotes back the token's actual value rather than its raw
// source spelling.
func describeToken(t Token) string {
	if t.Kind == TokenEOF {
		return "end of input"
	}
	return t.Value
}
