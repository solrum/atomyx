package expression

// Node is one syntax node of a parsed expression. Every concrete node
// type in this package is used as a pointer (*NumberLiteral, and so on)
// and implements Node. The exprNode marker method is unexported, so no
// type outside this package can implement Node; Eval's type switch over
// concrete node types is therefore exhaustive.
type Node interface {
	// Start returns the zero-based UTF-16 code-unit offset of the
	// node's own leading token in the original source, independent of
	// any enclosing parentheses. A parenthesized subexpression parses
	// to the same node (and therefore the same Start) as the
	// unparenthesized form, so wrapping an expression in parentheses
	// never changes where an error inside it is reported.
	Start() int
	exprNode()
}

// BinaryOperator names one of the 13 binary operators the parser
// produces, using the same literal spelling as expression source.
type BinaryOperator string

const (
	BinaryOr  BinaryOperator = "or"
	BinaryAnd BinaryOperator = "and"
	BinaryEq  BinaryOperator = "=="
	BinaryNeq BinaryOperator = "!="
	BinaryLt  BinaryOperator = "<"
	BinaryLte BinaryOperator = "<="
	BinaryGt  BinaryOperator = ">"
	BinaryGte BinaryOperator = ">="
	BinaryAdd BinaryOperator = "+"
	BinarySub BinaryOperator = "-"
	BinaryMul BinaryOperator = "*"
	BinaryDiv BinaryOperator = "/"
	BinaryMod BinaryOperator = "%"
)

// UnaryOperator names one of the 2 unary operators the parser produces,
// using the same literal spelling as expression source.
type UnaryOperator string

const (
	UnaryMinus UnaryOperator = "-"
	UnaryNot   UnaryOperator = "not"
)

// CastFunction names one of the 3 callable cast functions, the closed
// whitelist of identifiers that may be followed by a parenthesized
// argument in expression source.
type CastFunction string

const (
	CastNumber CastFunction = "number"
	CastString CastFunction = "string"
	CastBool   CastFunction = "bool"
)

// NumberLiteral is a numeric literal, always non-negative: a leading
// minus sign in source parses as UnaryExpr, not part of the literal.
type NumberLiteral struct {
	StartPos int
	Value    float64
}

func (n *NumberLiteral) Start() int { return n.StartPos }
func (n *NumberLiteral) exprNode()  {}

// StringLiteral is a string literal with quotes stripped and escapes
// already resolved.
type StringLiteral struct {
	StartPos int
	Value    string
}

func (n *StringLiteral) Start() int { return n.StartPos }
func (n *StringLiteral) exprNode()  {}

// BooleanLiteral is the literal true or false keyword.
type BooleanLiteral struct {
	StartPos int
	Value    bool
}

func (n *BooleanLiteral) Start() int { return n.StartPos }
func (n *BooleanLiteral) exprNode()  {}

// Identifier is a bare name resolved against a Lookup at evaluation
// time.
type Identifier struct {
	StartPos int
	Name     string
}

func (n *Identifier) Start() int { return n.StartPos }
func (n *Identifier) exprNode()  {}

// UnaryExpr applies Operator to Argument. StartPos is the position of
// the operator token itself, not of Argument, so a type-rejection error
// on the operand points at the operator that rejected it.
type UnaryExpr struct {
	StartPos int
	Operator UnaryOperator
	Argument Node
}

func (n *UnaryExpr) Start() int { return n.StartPos }
func (n *UnaryExpr) exprNode()  {}

// BinaryExpr applies Operator to Left and Right. StartPos equals
// Left.Start(), so a binary expression's own reported position always
// coincides with where its left operand begins.
type BinaryExpr struct {
	StartPos int
	Operator BinaryOperator
	Left     Node
	Right    Node
}

func (n *BinaryExpr) Start() int { return n.StartPos }
func (n *BinaryExpr) exprNode()  {}

// CastExpr applies Func to Argument. StartPos is the position of the
// function name token, not of Argument.
type CastExpr struct {
	StartPos int
	Func     CastFunction
	Argument Node
}

func (n *CastExpr) Start() int { return n.StartPos }
func (n *CastExpr) exprNode()  {}
