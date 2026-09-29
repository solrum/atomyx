package expression

import "fmt"

// ParseError reports a malformed expression: a lexical error, unbalanced
// parentheses, trailing tokens after a complete expression, empty
// source, or a call to a name outside the number, string, bool
// whitelist. Position is the zero-based UTF-16 code-unit offset into the
// expression source where the problem was found, matching how the
// TypeScript engine (and therefore any script author's mental model of
// "column N") counts positions. Message is a behavioral contract: its
// exact wording is a stable value that a script's assertions or a
// test fixture may depend on directly, so changing it is a breaking
// change.
type ParseError struct {
	Message  string
	Position int
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s at position %d", e.Message, e.Position)
}

// EvalError reports a failure while evaluating an already-parsed
// expression: an undefined variable, a value used at the wrong type, or
// an arithmetic error such as division by zero. Position is the
// zero-based UTF-16 code-unit offset into the expression source of the
// node responsible for the failure (which node depends on the operator;
// see Eval). Message is a behavioral contract: assertExpr and
// branch-condition failure reasons depend on this text matching the
// TypeScript engine's wording exactly.
type EvalError struct {
	Message  string
	Position int
}

func (e *EvalError) Error() string {
	return fmt.Sprintf("%s at position %d", e.Message, e.Position)
}
