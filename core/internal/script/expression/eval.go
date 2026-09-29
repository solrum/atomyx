package expression

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
)

// Lookup resolves a variable name to its current value. It returns
// ok == false when the name is not defined, which Eval turns into an
// EvalError naming the variable rather than treating it as any kind of
// default or zero value.
type Lookup func(name string) (Value, bool)

// Evaluate parses and evaluates source against lookup in one call. A
// caller that evaluates the same source against many lookups should call
// Parse once and pass the resulting Node to Eval directly, so the source
// is tokenized and parsed only once.
//
// It returns a *ParseError for malformed source and a *EvalError for a
// failure while walking an otherwise well-formed AST (an undefined
// variable, a value used at the wrong type, division or modulo by zero,
// or a cast that cannot succeed for the given value).
func Evaluate(source string, lookup Lookup) (Value, error) {
	node, err := Parse(source)
	if err != nil {
		return Value{}, err
	}
	return Eval(node, lookup)
}

// Eval walks node against lookup and returns the resulting value.
//
// A value keeps the type it was authored with or the type lookup
// returned it as. Eval never coerces a string that looks numeric, or a
// number that looks boolean, on its own: guessing intent is how a
// mistyped variable turns into a silently wrong test result instead of
// a loud failure. The only way a value changes type is one of the three
// cast functions the expression source names explicitly, so every type
// change is visible at the call site that wrote it.
//
// Every error Eval returns has concrete type *EvalError.
func Eval(node Node, lookup Lookup) (Value, error) {
	switch n := node.(type) {
	case *NumberLiteral:
		return NumberValue(n.Value), nil
	case *StringLiteral:
		return StringValue(n.Value), nil
	case *BooleanLiteral:
		return BoolValue(n.Value), nil
	case *Identifier:
		return evalIdentifier(n, lookup)
	case *UnaryExpr:
		return evalUnary(n, lookup)
	case *BinaryExpr:
		return evalBinary(n, lookup)
	case *CastExpr:
		return evalCast(n, lookup)
	default:
		panic(fmt.Sprintf("expression: unhandled node type %T", node))
	}
}

func evalIdentifier(n *Identifier, lookup Lookup) (Value, error) {
	value, ok := lookup(n.Name)
	if !ok {
		return Value{}, &EvalError{Message: fmt.Sprintf(`variable "%s" is not defined`, n.Name), Position: n.Start()}
	}
	return value, nil
}

func evalUnary(n *UnaryExpr, lookup Lookup) (Value, error) {
	value, err := Eval(n.Argument, lookup)
	if err != nil {
		return Value{}, err
	}

	if n.Operator == UnaryMinus {
		if value.Kind() != KindNumber {
			return Value{}, &EvalError{Message: describeArithmeticRejection(value), Position: n.Start()}
		}
		return NumberValue(-value.Num()), nil
	}

	if value.Kind() != KindBool {
		return Value{}, &EvalError{Message: describeLogicalRejection(value), Position: n.Start()}
	}
	return BoolValue(!value.Bool()), nil
}

func evalBinary(n *BinaryExpr, lookup Lookup) (Value, error) {
	if n.Operator == BinaryAnd || n.Operator == BinaryOr {
		return evalLogical(n, lookup)
	}

	left, err := Eval(n.Left, lookup)
	if err != nil {
		return Value{}, err
	}
	right, err := Eval(n.Right, lookup)
	if err != nil {
		return Value{}, err
	}

	switch n.Operator {
	case BinaryEq, BinaryNeq:
		return evalEquality(n.Operator, left, right), nil
	case BinaryLt, BinaryLte, BinaryGt, BinaryGte:
		return evalComparison(n.Operator, left, right, n.Start())
	default:
		return evalArithmetic(n.Operator, left, right, n.Left.Start(), n.Right.Start())
	}
}

// evalLogical evaluates left to right and short-circuits, so the right
// operand is only evaluated when it can still change the result. This
// matters because evaluating it can fail, for example on a missing
// variable: "and"/"or" never surface an error from a side they didn't
// need to look at.
func evalLogical(n *BinaryExpr, lookup Lookup) (Value, error) {
	left, err := Eval(n.Left, lookup)
	if err != nil {
		return Value{}, err
	}
	if left.Kind() != KindBool {
		return Value{}, &EvalError{Message: describeLogicalRejection(left), Position: n.Left.Start()}
	}
	if n.Operator == BinaryAnd && !left.Bool() {
		return BoolValue(false), nil
	}
	if n.Operator == BinaryOr && left.Bool() {
		return BoolValue(true), nil
	}

	right, err := Eval(n.Right, lookup)
	if err != nil {
		return Value{}, err
	}
	if right.Kind() != KindBool {
		return Value{}, &EvalError{Message: describeLogicalRejection(right), Position: n.Right.Start()}
	}
	return right, nil
}

// evalEquality never fails: two values of different Kind are always
// unequal, and same-Kind values compare by their own payload (float64
// == for numbers, so NaN != NaN and -0 == 0 hold the way IEEE 754
// already defines them).
func evalEquality(operator BinaryOperator, left, right Value) Value {
	equal := left.Kind() == right.Kind() && valuesEqual(left, right)
	if operator == BinaryEq {
		return BoolValue(equal)
	}
	return BoolValue(!equal)
}

func valuesEqual(left, right Value) bool {
	switch left.Kind() {
	case KindNumber:
		return left.Num() == right.Num()
	case KindString:
		return left.Str() == right.Str()
	default:
		return left.Bool() == right.Bool()
	}
}

func evalComparison(operator BinaryOperator, left, right Value, position int) (Value, error) {
	if left.Kind() != KindNumber || right.Kind() != KindNumber {
		return Value{}, &EvalError{
			Message: fmt.Sprintf("cannot compare %s and %s, both sides must be numbers",
				describeValue(left), describeValue(right)),
			Position: position,
		}
	}
	l, r := left.Num(), right.Num()
	switch operator {
	case BinaryLt:
		return BoolValue(l < r), nil
	case BinaryLte:
		return BoolValue(l <= r), nil
	case BinaryGt:
		return BoolValue(l > r), nil
	default:
		return BoolValue(l >= r), nil
	}
}

func evalArithmetic(operator BinaryOperator, left, right Value, leftStart, rightStart int) (Value, error) {
	if left.Kind() != KindNumber || right.Kind() != KindNumber {
		leftIsOffending := left.Kind() != KindNumber
		offending, offendingStart := right, rightStart
		if leftIsOffending {
			offending, offendingStart = left, leftStart
		}
		return Value{}, &EvalError{Message: describeArithmeticRejection(offending), Position: offendingStart}
	}

	l, r := left.Num(), right.Num()
	switch operator {
	case BinaryAdd:
		return NumberValue(l + r), nil
	case BinarySub:
		return NumberValue(l - r), nil
	case BinaryMul:
		return NumberValue(l * r), nil
	case BinaryDiv:
		if r == 0 {
			return Value{}, &EvalError{Message: "division by zero", Position: rightStart}
		}
		return NumberValue(l / r), nil
	default: // BinaryMod
		if r == 0 {
			return Value{}, &EvalError{Message: "modulo by zero", Position: rightStart}
		}
		return NumberValue(math.Mod(l, r)), nil
	}
}

func evalCast(n *CastExpr, lookup Lookup) (Value, error) {
	value, err := Eval(n.Argument, lookup)
	if err != nil {
		return Value{}, err
	}
	switch n.Func {
	case CastNumber:
		return castToNumber(value, n.Argument.Start())
	case CastString:
		return castToString(value), nil
	default: // CastBool
		return castToBool(value, n.Argument.Start())
	}
}

// strictNumberPattern is the whole grammar number(...) accepts from a
// string: an optional sign, at least one digit, and an optional decimal
// fraction. Unlike the tokenizer's number literal, a leading sign is
// allowed here because the string being converted was not itself parsed
// as expression syntax.
var strictNumberPattern = regexp.MustCompile(`^[+-]?\d+(\.\d+)?$`)

func castToNumber(value Value, position int) (Value, error) {
	switch value.Kind() {
	case KindNumber:
		return value, nil
	case KindString:
		s := value.Str()
		if !strictNumberPattern.MatchString(s) {
			return Value{}, &EvalError{Message: fmt.Sprintf(`cannot convert "%s" to a number`, s), Position: position}
		}
		// strictNumberPattern guarantees a form strconv.ParseFloat
		// accepts. A magnitude beyond a 64-bit float's range still
		// parses successfully, rounding to positive or negative
		// infinity instead of producing an error.
		f, _ := strconv.ParseFloat(s, 64)
		return NumberValue(f), nil
	default: // KindBool
		return Value{}, &EvalError{Message: "cannot convert a boolean to a number, compare it directly instead", Position: position}
	}
}

func castToString(value Value) Value {
	switch value.Kind() {
	case KindString:
		return value
	case KindNumber:
		return StringValue(FormatNumber(value.Num()))
	default: // KindBool
		if value.Bool() {
			return StringValue("true")
		}
		return StringValue("false")
	}
}

func castToBool(value Value, position int) (Value, error) {
	switch value.Kind() {
	case KindBool:
		return value, nil
	case KindString:
		s := value.Str()
		if s == "true" {
			return BoolValue(true), nil
		}
		if s == "false" {
			return BoolValue(false), nil
		}
		return Value{}, &EvalError{
			Message:  fmt.Sprintf(`cannot convert "%s" to a boolean, expected "true" or "false"`, s),
			Position: position,
		}
	default: // KindNumber
		return Value{}, &EvalError{Message: "cannot convert a number to a boolean, compare it directly instead", Position: position}
	}
}

// describeArithmeticRejection explains why value cannot participate in
// a numeric operator. A boolean has no valid numeric conversion, so
// unlike a numeric string it must not suggest number(...): that call
// would itself be rejected by castToNumber. A plain string keeps the
// number(...) suggestion because that conversion genuinely works for a
// numeric string.
func describeArithmeticRejection(value Value) string {
	if value.Kind() == KindBool {
		return fmt.Sprintf("cannot use %s in arithmetic, a boolean has no valid numeric conversion", describeValue(value))
	}
	return fmt.Sprintf("cannot use %s in arithmetic, convert it with number(...)", describeValue(value))
}

// describeLogicalRejection explains why value cannot participate in a
// logical operator. A number has no valid boolean conversion, so unlike
// a string it must not suggest bool(...): that call would itself be
// rejected by castToBool. A plain string keeps the bool(...) suggestion
// because that conversion genuinely works for the strings "true" and
// "false".
func describeLogicalRejection(value Value) string {
	if value.Kind() == KindNumber {
		return fmt.Sprintf("cannot use %s in a logical operation, a number cannot be used as a boolean", describeValue(value))
	}
	return fmt.Sprintf("cannot use %s in a logical operation, convert it with bool(...)", describeValue(value))
}

func describeValue(value Value) string {
	switch value.Kind() {
	case KindString:
		return fmt.Sprintf(`string value "%s"`, value.Str())
	case KindBool:
		if value.Bool() {
			return "boolean value true"
		}
		return "boolean value false"
	default: // KindNumber
		return fmt.Sprintf("number value %s", FormatNumber(value.Num()))
	}
}
