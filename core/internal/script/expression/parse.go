package expression

import (
	"fmt"
	"strconv"
)

// Parse parses expression source into an AST, following precedence from
// lowest to highest: or, and, not, equality, comparison, additive,
// multiplicative, unary, primary. Every node carries a start position,
// the character index of its own leading token, so a downstream
// evaluation error can name where in the source it happened.
//
// It returns a *ParseError on any malformed input, including a lexical
// error from the tokenizer, unbalanced parentheses, trailing tokens
// after a complete expression, empty source, or a call to a name outside
// the number, string, bool whitelist.
func Parse(source string) (Node, error) {
	tokens, err := tokenize(source)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}

	if p.current().Kind == TokenEOF {
		return nil, &ParseError{Message: "expression source is empty", Position: 0}
	}

	result, err := p.parseOr()
	if err != nil {
		return nil, err
	}

	if p.current().Kind != TokenEOF {
		return nil, &ParseError{
			Message:  fmt.Sprintf(`unexpected trailing input "%s"`, describeToken(p.current())),
			Position: p.current().Position,
		}
	}

	return result, nil
}

type parser struct {
	tokens []Token
	pos    int
}

func (p *parser) current() Token {
	return p.tokens[p.pos]
}

func (p *parser) advance() Token {
	token := p.tokens[p.pos]
	if token.Kind != TokenEOF {
		p.pos++
	}
	return token
}

func (p *parser) isPunct(value string) bool {
	token := p.current()
	return token.Kind == TokenPunct && token.Value == value
}

func (p *parser) isKeyword(value string) bool {
	token := p.current()
	return token.Kind == TokenIdentifier && token.Value == value
}

func (p *parser) expectPunct(value string) error {
	if !p.isPunct(value) {
		return &ParseError{
			Message:  fmt.Sprintf(`expected "%s" but found "%s"`, value, describeToken(p.current())),
			Position: p.current().Position,
		}
	}
	p.advance()
	return nil
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("or") {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{StartPos: left.Start(), Operator: BinaryOr, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("and") {
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{StartPos: left.Start(), Operator: BinaryAnd, Left: left, Right: right}
	}
	return left, nil
}

// parseNot binds looser than equality and comparison, so "not a == b"
// groups as "not (a == b)" and "not 1 < 2" evaluates the comparison
// instead of erroring on a non-boolean unary operand. It binds tighter
// than "and" and "or", so "not x and y" groups as "(not x) and y".
// Recursing into itself, not parsePrimary, lets repeated "not" keywords
// stack right-associatively.
func (p *parser) parseNot() (Node, error) {
	if p.isKeyword("not") {
		token := p.advance()
		argument, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{StartPos: token.Position, Operator: UnaryNot, Argument: argument}, nil
	}
	return p.parseEquality()
}

func (p *parser) parseEquality() (Node, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.isPunct("==") || p.isPunct("!=") {
		operator := BinaryOperator(p.advance().Value)
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{StartPos: left.Start(), Operator: operator, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseComparison() (Node, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for p.isPunct("<") || p.isPunct("<=") || p.isPunct(">") || p.isPunct(">=") {
		operator := BinaryOperator(p.advance().Value)
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{StartPos: left.Start(), Operator: operator, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseAdditive() (Node, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.isPunct("+") || p.isPunct("-") {
		operator := BinaryOperator(p.advance().Value)
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{StartPos: left.Start(), Operator: operator, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseMultiplicative() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.isPunct("*") || p.isPunct("/") || p.isPunct("%") {
		operator := BinaryOperator(p.advance().Value)
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{StartPos: left.Start(), Operator: operator, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (Node, error) {
	if p.isPunct("-") {
		token := p.advance()
		argument, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{StartPos: token.Position, Operator: UnaryMinus, Argument: argument}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Node, error) {
	token := p.current()

	switch token.Kind {
	case TokenNumber:
		p.advance()
		// tokenize only ever produces digits with at most one decimal
		// point for a number token, which strconv.ParseFloat always
		// accepts syntactically. The only error it can still report is
		// ErrRange for a magnitude too large to represent, and in that
		// case it already returns the correctly rounded ±Inf, so a
		// numeric literal whose magnitude overflows a 64-bit float
		// evaluates to infinity instead of failing to parse.
		value, _ := strconv.ParseFloat(token.Value, 64)
		return &NumberLiteral{StartPos: token.Position, Value: value}, nil
	case TokenString:
		p.advance()
		return &StringLiteral{StartPos: token.Position, Value: token.Value}, nil
	case TokenIdentifier:
		return p.parseIdentifierLike(token)
	}

	if p.isPunct("(") {
		p.advance()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		return inner, nil
	}

	return nil, &ParseError{
		Message:  fmt.Sprintf(`expected an expression but found "%s"`, describeToken(token)),
		Position: token.Position,
	}
}

func (p *parser) parseIdentifierLike(token Token) (Node, error) {
	if token.Value == "true" || token.Value == "false" {
		p.advance()
		return &BooleanLiteral{StartPos: token.Position, Value: token.Value == "true"}, nil
	}

	followedByCall := p.pos+1 < len(p.tokens) &&
		p.tokens[p.pos+1].Kind == TokenPunct &&
		p.tokens[p.pos+1].Value == "("

	if isCastFunctionName(token.Value) && followedByCall {
		function := CastFunction(token.Value)
		p.advance()
		p.advance()
		argument, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		return &CastExpr{StartPos: token.Position, Func: function, Argument: argument}, nil
	}

	if followedByCall {
		return nil, &ParseError{
			Message:  fmt.Sprintf(`unknown function "%s", only number, string, and bool are callable`, token.Value),
			Position: token.Position,
		}
	}

	p.advance()
	return &Identifier{StartPos: token.Position, Name: token.Value}, nil
}

func isCastFunctionName(name string) bool {
	return name == string(CastNumber) || name == string(CastString) || name == string(CastBool)
}
