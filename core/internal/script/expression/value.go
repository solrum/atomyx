package expression

// Kind identifies which of the three primitive shapes a Value holds.
// It starts at 1 so a zero-valued Kind (and therefore a zero-valued
// Value) is never mistaken for a valid KindNumber.
type Kind int

const (
	KindNumber Kind = iota + 1
	KindString
	KindBool
)

// Value is a tagged union over the three primitive types the expression
// engine operates on: number, string, and bool. A Value never changes
// the Kind it was constructed with; the engine only ever produces a
// different Kind through one of the explicit cast functions.
//
// Two Value structs must never be compared with the == operator: dispatch
// on Kind() first, then compare the accessor for that kind.
type Value struct {
	kind Kind
	num  float64
	str  string
	b    bool
}

// NumberValue constructs a Value holding f as KindNumber.
func NumberValue(f float64) Value {
	return Value{kind: KindNumber, num: f}
}

// StringValue constructs a Value holding s as KindString.
func StringValue(s string) Value {
	return Value{kind: KindString, str: s}
}

// BoolValue constructs a Value holding b as KindBool.
func BoolValue(b bool) Value {
	return Value{kind: KindBool, b: b}
}

// Kind reports which primitive shape v holds.
func (v Value) Kind() Kind {
	return v.kind
}

// Num returns the numeric payload of v. Its result is meaningful only
// when v.Kind() == KindNumber.
func (v Value) Num() float64 {
	return v.num
}

// Str returns the string payload of v. Its result is meaningful only
// when v.Kind() == KindString.
func (v Value) Str() string {
	return v.str
}

// Bool returns the boolean payload of v. Its result is meaningful only
// when v.Kind() == KindBool.
func (v Value) Bool() bool {
	return v.b
}
