package expression

import (
	"errors"
	"testing"
)

func mustParse(t *testing.T, source string) Node {
	t.Helper()
	node, err := Parse(source)
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", source, err)
	}
	return node
}

func expectParseError(t *testing.T, source, wantMessage string, wantPos int) {
	t.Helper()
	_, err := Parse(source)
	if err == nil {
		t.Fatalf("Parse(%q) expected error, got none", source)
	}
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("Parse(%q) error type = %T, want *ParseError", source, err)
	}
	if parseErr.Message != wantMessage {
		t.Errorf("Parse(%q) message = %q, want %q", source, parseErr.Message, wantMessage)
	}
	if parseErr.Position != wantPos {
		t.Errorf("Parse(%q) position = %d, want %d", source, parseErr.Position, wantPos)
	}
}

func asBinary(t *testing.T, node Node) *BinaryExpr {
	t.Helper()
	b, ok := node.(*BinaryExpr)
	if !ok {
		t.Fatalf("node type = %T, want *BinaryExpr", node)
	}
	return b
}

func asUnary(t *testing.T, node Node) *UnaryExpr {
	t.Helper()
	u, ok := node.(*UnaryExpr)
	if !ok {
		t.Fatalf("node type = %T, want *UnaryExpr", node)
	}
	return u
}

func asCast(t *testing.T, node Node) *CastExpr {
	t.Helper()
	c, ok := node.(*CastExpr)
	if !ok {
		t.Fatalf("node type = %T, want *CastExpr", node)
	}
	return c
}

func asNumber(t *testing.T, node Node) *NumberLiteral {
	t.Helper()
	n, ok := node.(*NumberLiteral)
	if !ok {
		t.Fatalf("node type = %T, want *NumberLiteral", node)
	}
	return n
}

func asBool(t *testing.T, node Node) *BooleanLiteral {
	t.Helper()
	b, ok := node.(*BooleanLiteral)
	if !ok {
		t.Fatalf("node type = %T, want *BooleanLiteral", node)
	}
	return b
}

func asIdentifier(t *testing.T, node Node) *Identifier {
	t.Helper()
	id, ok := node.(*Identifier)
	if !ok {
		t.Fatalf("node type = %T, want *Identifier", node)
	}
	return id
}

func TestParsePrecedenceAndAssociativity(t *testing.T) {
	t.Run("multiplicative binds tighter than additive on the right", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "1 + 2 * 3"))
		if top.Operator != BinaryAdd {
			t.Fatalf("operator = %v, want %v", top.Operator, BinaryAdd)
		}
		if asNumber(t, top.Left).Value != 1 {
			t.Errorf("left = %v, want 1", top.Left)
		}
		right := asBinary(t, top.Right)
		if right.Operator != BinaryMul || asNumber(t, right.Left).Value != 2 || asNumber(t, right.Right).Value != 3 {
			t.Errorf("right = %+v, want 2 * 3", right)
		}
	})

	t.Run("multiplicative binds tighter than additive on the left", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "1 * 2 + 3"))
		if top.Operator != BinaryAdd {
			t.Fatalf("operator = %v, want %v", top.Operator, BinaryAdd)
		}
		left := asBinary(t, top.Left)
		if left.Operator != BinaryMul || asNumber(t, left.Left).Value != 1 || asNumber(t, left.Right).Value != 2 {
			t.Errorf("left = %+v, want 1 * 2", left)
		}
		if asNumber(t, top.Right).Value != 3 {
			t.Errorf("right = %v, want 3", top.Right)
		}
	})

	t.Run("subtraction is left associative", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "2 - 3 - 4"))
		if top.Operator != BinarySub || asNumber(t, top.Right).Value != 4 {
			t.Fatalf("top = %+v, want (2 - 3) - 4", top)
		}
		left := asBinary(t, top.Left)
		if left.Operator != BinarySub || asNumber(t, left.Left).Value != 2 || asNumber(t, left.Right).Value != 3 {
			t.Errorf("left = %+v, want 2 - 3", left)
		}
	})

	t.Run("division is left associative", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "8 / 4 / 2"))
		if top.Operator != BinaryDiv || asNumber(t, top.Right).Value != 2 {
			t.Fatalf("top = %+v, want (8 / 4) / 2", top)
		}
		left := asBinary(t, top.Left)
		if left.Operator != BinaryDiv || asNumber(t, left.Left).Value != 8 || asNumber(t, left.Right).Value != 4 {
			t.Errorf("left = %+v, want 8 / 4", left)
		}
	})

	t.Run("comparison binds looser than additive", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "1 + 2 < 3 + 4"))
		if top.Operator != BinaryLt {
			t.Fatalf("operator = %v, want %v", top.Operator, BinaryLt)
		}
		left := asBinary(t, top.Left)
		if left.Operator != BinaryAdd || asNumber(t, left.Left).Value != 1 || asNumber(t, left.Right).Value != 2 {
			t.Errorf("left = %+v, want 1 + 2", left)
		}
		right := asBinary(t, top.Right)
		if right.Operator != BinaryAdd || asNumber(t, right.Left).Value != 3 || asNumber(t, right.Right).Value != 4 {
			t.Errorf("right = %+v, want 3 + 4", right)
		}
	})

	t.Run("equality binds looser than comparison", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "1 < 2 == true"))
		if top.Operator != BinaryEq {
			t.Fatalf("operator = %v, want %v", top.Operator, BinaryEq)
		}
		left := asBinary(t, top.Left)
		if left.Operator != BinaryLt {
			t.Errorf("left = %+v, want 1 < 2", left)
		}
		if !asBool(t, top.Right).Value {
			t.Errorf("right = %+v, want true", top.Right)
		}
	})

	t.Run("and binds tighter than or", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "true or false and true"))
		if top.Operator != BinaryOr {
			t.Fatalf("operator = %v, want %v", top.Operator, BinaryOr)
		}
		if !asBool(t, top.Left).Value {
			t.Errorf("left = %+v, want true", top.Left)
		}
		right := asBinary(t, top.Right)
		if right.Operator != BinaryAnd {
			t.Errorf("right = %+v, want false and true", right)
		}
	})

	t.Run("not binds tighter than and", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "not true and false"))
		if top.Operator != BinaryAnd {
			t.Fatalf("operator = %v, want %v", top.Operator, BinaryAnd)
		}
		left := asUnary(t, top.Left)
		if left.Operator != UnaryNot {
			t.Errorf("left = %+v, want not true", left)
		}
	})

	t.Run("not binds looser than equality", func(t *testing.T) {
		top := asUnary(t, mustParse(t, "not 1 == 1"))
		if top.Operator != UnaryNot {
			t.Fatalf("operator = %v, want %v", top.Operator, UnaryNot)
		}
		arg := asBinary(t, top.Argument)
		if arg.Operator != BinaryEq {
			t.Errorf("argument = %+v, want 1 == 1", arg)
		}
	})

	t.Run("not stacks right associatively", func(t *testing.T) {
		top := asUnary(t, mustParse(t, "not not true"))
		inner := asUnary(t, top.Argument)
		if top.Operator != UnaryNot || inner.Operator != UnaryNot {
			t.Fatalf("top = %+v, inner = %+v, want nested not", top, inner)
		}
		if !asBool(t, inner.Argument).Value {
			t.Errorf("innermost = %+v, want true", inner.Argument)
		}
	})

	t.Run("unary minus stacks right associatively", func(t *testing.T) {
		top := asUnary(t, mustParse(t, "- - 5"))
		inner := asUnary(t, top.Argument)
		if top.Operator != UnaryMinus || inner.Operator != UnaryMinus {
			t.Fatalf("top = %+v, inner = %+v, want nested unary minus", top, inner)
		}
		if asNumber(t, inner.Argument).Value != 5 {
			t.Errorf("innermost = %+v, want 5", inner.Argument)
		}
	})

	t.Run("unary minus binds tighter than multiplicative", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "-2 * 3"))
		if top.Operator != BinaryMul {
			t.Fatalf("operator = %v, want %v", top.Operator, BinaryMul)
		}
		left := asUnary(t, top.Left)
		if left.Operator != UnaryMinus || asNumber(t, left.Argument).Value != 2 {
			t.Errorf("left = %+v, want -2", left)
		}
	})

	t.Run("a cast whitelist name not followed by a call parses as a plain identifier", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "number + 1"))
		if top.Operator != BinaryAdd {
			t.Fatalf("operator = %v, want %v", top.Operator, BinaryAdd)
		}
		if asIdentifier(t, top.Left).Name != "number" {
			t.Errorf("left = %+v, want identifier number", top.Left)
		}
	})

	t.Run("cast expression structure", func(t *testing.T) {
		top := asCast(t, mustParse(t, "string(x)"))
		if top.Func != CastString {
			t.Fatalf("func = %v, want %v", top.Func, CastString)
		}
		if asIdentifier(t, top.Argument).Name != "x" {
			t.Errorf("argument = %+v, want identifier x", top.Argument)
		}
	})
}

func TestParseStartPositions(t *testing.T) {
	t.Run("binary expression start equals left operand start", func(t *testing.T) {
		top := asBinary(t, mustParse(t, "1 + 2"))
		if top.Start() != 0 || top.Left.Start() != 0 {
			t.Errorf("top.Start() = %d, left.Start() = %d, want both 0", top.Start(), top.Left.Start())
		}
	})

	t.Run("parenthesization returns the inner node unchanged", func(t *testing.T) {
		top := asBinary(t, mustParse(t, " (1 + 2)"))
		if top.Start() != 2 {
			t.Errorf("Start() = %d, want 2 (position of the inner literal, not of the opening paren at 1)", top.Start())
		}
	})

	t.Run("unary node start is the operator position, not the operand's", func(t *testing.T) {
		top := asUnary(t, mustParse(t, "- x"))
		if top.Start() != 0 {
			t.Errorf("UnaryExpr.Start() = %d, want 0 (position of '-')", top.Start())
		}
		if top.Argument.Start() != 2 {
			t.Errorf("Argument.Start() = %d, want 2 (position of 'x')", top.Argument.Start())
		}
	})

	t.Run("cast argument start is distinct from the cast keyword position", func(t *testing.T) {
		top := asCast(t, mustParse(t, "number(x)"))
		if top.Start() != 0 {
			t.Errorf("CastExpr.Start() = %d, want 0", top.Start())
		}
		if top.Argument.Start() != 7 {
			t.Errorf("Argument.Start() = %d, want 7 (position of 'x')", top.Argument.Start())
		}
	})

	t.Run("parenthesis transparency survives nesting under a unary operator", func(t *testing.T) {
		top := asUnary(t, mustParse(t, "-(1 + 2)"))
		if top.Start() != 0 {
			t.Errorf("UnaryExpr.Start() = %d, want 0", top.Start())
		}
		inner := asBinary(t, top.Argument)
		if inner.Start() != 2 {
			t.Errorf("inner BinaryExpr.Start() = %d, want 2 (position of '1', not of the paren at 1)", inner.Start())
		}
	})
}

func TestParseErrors(t *testing.T) {
	t.Run("empty source", func(t *testing.T) {
		expectParseError(t, "", "expression source is empty", 0)
	})

	t.Run("whitespace-only source is still empty", func(t *testing.T) {
		expectParseError(t, "   ", "expression source is empty", 0)
	})

	t.Run("trailing input after a complete expression", func(t *testing.T) {
		expectParseError(t, "1 2", `unexpected trailing input "2"`, 2)
	})

	t.Run("malformed primary", func(t *testing.T) {
		expectParseError(t, "+1", `expected an expression but found "+"`, 0)
	})

	t.Run("unbalanced opening paren reports end of input", func(t *testing.T) {
		expectParseError(t, "(1 + 2", `expected ")" but found "end of input"`, 6)
	})

	t.Run("unbalanced paren mid expression", func(t *testing.T) {
		expectParseError(t, "(1 + 2 3", `expected ")" but found "3"`, 7)
	})

	t.Run("call to a name outside the cast whitelist", func(t *testing.T) {
		expectParseError(t, "foo(1)", `unknown function "foo", only number, string, and bool are callable`, 0)
	})

	t.Run("empty call argument list", func(t *testing.T) {
		expectParseError(t, "number()", `expected an expression but found ")"`, 7)
	})

	t.Run("lexical error propagates as a parse error", func(t *testing.T) {
		expectParseError(t, "1 + #", `unexpected character "#" in expression`, 4)
	})
}
