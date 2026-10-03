package expression

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func lookupFrom(vars map[string]Value) Lookup {
	return func(name string) (Value, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

func mustEval(t *testing.T, source string, lookup Lookup) Value {
	t.Helper()
	v, err := Evaluate(source, lookup)
	if err != nil {
		t.Fatalf("Evaluate(%q) unexpected error: %v", source, err)
	}
	return v
}

func expectEvalError(t *testing.T, source string, lookup Lookup, wantMessage string, wantPos int) {
	t.Helper()
	_, err := Evaluate(source, lookup)
	if err == nil {
		t.Fatalf("Evaluate(%q) expected error, got none", source)
	}
	var evalErr *EvalError
	if !errors.As(err, &evalErr) {
		t.Fatalf("Evaluate(%q) error type = %T, want *EvalError", source, err)
	}
	if evalErr.Message != wantMessage {
		t.Errorf("Evaluate(%q) message = %q, want %q", source, evalErr.Message, wantMessage)
	}
	if evalErr.Position != wantPos {
		t.Errorf("Evaluate(%q) position = %d, want %d", source, evalErr.Position, wantPos)
	}
}

func assertNumber(t *testing.T, v Value, want float64) {
	t.Helper()
	if v.Kind() != KindNumber {
		t.Fatalf("Kind() = %v, want KindNumber", v.Kind())
	}
	if v.Num() != want {
		t.Errorf("Num() = %v, want %v", v.Num(), want)
	}
}

func assertBool(t *testing.T, v Value, want bool) {
	t.Helper()
	if v.Kind() != KindBool {
		t.Fatalf("Kind() = %v, want KindBool", v.Kind())
	}
	if v.Bool() != want {
		t.Errorf("Bool() = %v, want %v", v.Bool(), want)
	}
}

func assertString(t *testing.T, v Value, want string) {
	t.Helper()
	if v.Kind() != KindString {
		t.Fatalf("Kind() = %v, want KindString", v.Kind())
	}
	if v.Str() != want {
		t.Errorf("Str() = %q, want %q", v.Str(), want)
	}
}

// --- literals and identifiers ---

func TestEvalLiterals(t *testing.T) {
	assertNumber(t, mustEval(t, "42", nil), 42)
	assertString(t, mustEval(t, `"hi"`, nil), "hi")
	assertBool(t, mustEval(t, "true", nil), true)
	assertBool(t, mustEval(t, "false", nil), false)
}

func TestEvalIdentifier(t *testing.T) {
	lookup := lookupFrom(map[string]Value{"x": NumberValue(5)})
	assertNumber(t, mustEval(t, "x", lookup), 5)

	expectEvalError(t, "y", lookup, `variable "y" is not defined`, 0)
	expectEvalError(t, "1 + y", lookup, `variable "y" is not defined`, 4)
}

// --- unary operators ---

func TestEvalUnaryMinus(t *testing.T) {
	lookup := lookupFrom(map[string]Value{"x": NumberValue(5)})
	assertNumber(t, mustEval(t, "-x", lookup), -5)

	stringLookup := lookupFrom(map[string]Value{"x": StringValue("a")})
	expectEvalError(t, "-x", stringLookup,
		"cannot use a string in arithmetic, convert it with number(...)", 0)

	boolLookup := lookupFrom(map[string]Value{"x": BoolValue(true)})
	expectEvalError(t, "-x", boolLookup,
		"cannot use a boolean in arithmetic, a boolean has no valid numeric conversion", 0)
}

func TestEvalUnaryNot(t *testing.T) {
	lookup := lookupFrom(map[string]Value{"x": BoolValue(false)})
	assertBool(t, mustEval(t, "not x", lookup), true)

	numberLookup := lookupFrom(map[string]Value{"x": NumberValue(5)})
	expectEvalError(t, "not x", numberLookup,
		"cannot use a number in a logical operation, a number cannot be used as a boolean", 0)

	stringLookup := lookupFrom(map[string]Value{"x": StringValue("a")})
	expectEvalError(t, "not x", stringLookup,
		"cannot use a string in a logical operation, convert it with bool(...)", 0)
}

// --- and / or short-circuiting ---

func TestEvalLogicalShortCircuit(t *testing.T) {
	t.Run("and short-circuits on a false left without evaluating right", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": BoolValue(false)})
		assertBool(t, mustEval(t, "x and y", lookup), false)
	})

	t.Run("or short-circuits on a true left without evaluating right", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": BoolValue(true)})
		assertBool(t, mustEval(t, "x or y", lookup), true)
	})

	t.Run("and evaluates the right side when left is true", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": BoolValue(true), "y": BoolValue(true)})
		assertBool(t, mustEval(t, "x and y", lookup), true)
	})

	t.Run("or evaluates the right side when left is false", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": BoolValue(false), "y": BoolValue(false)})
		assertBool(t, mustEval(t, "x or y", lookup), false)
	})

	t.Run("and reports a wrong-type left at the left operand's position", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(1)})
		expectEvalError(t, "x and y", lookup,
			"cannot use a number in a logical operation, a number cannot be used as a boolean", 0)
	})

	t.Run("or reports a wrong-type left at the left operand's position", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(1)})
		expectEvalError(t, "x or y", lookup,
			"cannot use a number in a logical operation, a number cannot be used as a boolean", 0)
	})

	t.Run("and reports a wrong-type right at the right operand's position", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": BoolValue(true), "y": NumberValue(1)})
		expectEvalError(t, "x and y", lookup,
			"cannot use a number in a logical operation, a number cannot be used as a boolean", 6)
	})

	t.Run("or reports a wrong-type right at the right operand's position", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": BoolValue(false), "y": NumberValue(1)})
		expectEvalError(t, "x or y", lookup,
			"cannot use a number in a logical operation, a number cannot be used as a boolean", 5)
	})
}

// --- equality ---

func TestEvalEquality(t *testing.T) {
	t.Run("different kinds are always unequal", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(5), "y": StringValue("5")})
		assertBool(t, mustEval(t, "x == y", lookup), false)
		assertBool(t, mustEval(t, "x != y", lookup), true)
	})

	t.Run("same kind same value is equal", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(5), "y": NumberValue(5)})
		assertBool(t, mustEval(t, "x == y", lookup), true)
		assertBool(t, mustEval(t, "x != y", lookup), false)
	})

	t.Run("NaN is never equal to itself", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(math.NaN())})
		assertBool(t, mustEval(t, "x == x", lookup), false)
		assertBool(t, mustEval(t, "x != x", lookup), true)
	})

	t.Run("positive and negative zero are equal", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{
			"x": NumberValue(0),
			"y": NumberValue(math.Copysign(0, -1)),
		})
		assertBool(t, mustEval(t, "x == y", lookup), true)
	})

	t.Run("equality never fails even for mismatched types", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": BoolValue(true), "y": StringValue("true")})
		assertBool(t, mustEval(t, "x == y", lookup), false)
	})
}

// --- comparison ---

func TestEvalComparison(t *testing.T) {
	lookup := lookupFrom(map[string]Value{"x": NumberValue(1), "y": NumberValue(2)})
	assertBool(t, mustEval(t, "x < y", lookup), true)
	assertBool(t, mustEval(t, "x <= y", lookup), true)
	assertBool(t, mustEval(t, "x > y", lookup), false)
	assertBool(t, mustEval(t, "x >= y", lookup), false)

	t.Run("both sides evaluated before the type check, left invalid", func(t *testing.T) {
		badLeft := lookupFrom(map[string]Value{"x": StringValue("a"), "y": NumberValue(2)})
		expectEvalError(t, "x < y", badLeft,
			"cannot compare a string and a number, both sides must be numbers", 0)
	})

	t.Run("right invalid still reports the binary node's own start, not the right operand's", func(t *testing.T) {
		badRight := lookupFrom(map[string]Value{"x": NumberValue(1), "y": StringValue("a")})
		expectEvalError(t, "x < y", badRight,
			"cannot compare a number and a string, both sides must be numbers", 0)
	})
}

// --- arithmetic ---

func TestEvalArithmetic(t *testing.T) {
	lookup := lookupFrom(map[string]Value{"x": NumberValue(4), "y": NumberValue(2)})
	assertNumber(t, mustEval(t, "x + y", lookup), 6)
	assertNumber(t, mustEval(t, "x - y", lookup), 2)
	assertNumber(t, mustEval(t, "x * y", lookup), 8)
	assertNumber(t, mustEval(t, "x / y", lookup), 2)

	t.Run("left checked first when both operands are invalid", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": BoolValue(true), "y": BoolValue(true)})
		expectEvalError(t, "x + y", lookup,
			"cannot use a boolean in arithmetic, a boolean has no valid numeric conversion", 0)
	})

	t.Run("left invalid reports the left operand's own start", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": BoolValue(true), "y": NumberValue(1)})
		expectEvalError(t, "x + y", lookup,
			"cannot use a boolean in arithmetic, a boolean has no valid numeric conversion", 0)
	})

	t.Run("right invalid reports the right operand's own start", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(1), "y": BoolValue(true)})
		expectEvalError(t, "x + y", lookup,
			"cannot use a boolean in arithmetic, a boolean has no valid numeric conversion", 4)
	})

	t.Run("division by zero", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(1), "y": NumberValue(0)})
		expectEvalError(t, "x / y", lookup, "division by zero", 4)
	})

	t.Run("modulo by zero", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(1), "y": NumberValue(0)})
		expectEvalError(t, "x % y", lookup, "modulo by zero", 4)
	})

	t.Run("modulo sign parity matches JavaScript's %", func(t *testing.T) {
		cases := []struct {
			source string
			want   float64
		}{
			{"5 % 3", 2},
			{"-5 % 3", -2},
			{"5 % -3", 2},
			{"-5 % -3", -2},
		}
		for _, c := range cases {
			assertNumber(t, mustEval(t, c.source, nil), c.want)
		}
	})

	t.Run("overflow produces infinity rather than an error", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(math.MaxFloat64), "y": NumberValue(math.MaxFloat64)})
		result := mustEval(t, "x * y", lookup)
		if result.Kind() != KindNumber || !math.IsInf(result.Num(), 1) {
			t.Errorf("x * y = %+v, want +Inf", result)
		}
	})

	t.Run("NaN propagates through non-division arithmetic without an error", func(t *testing.T) {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(math.NaN())})
		result := mustEval(t, "x + 1", lookup)
		if result.Kind() != KindNumber || !math.IsNaN(result.Num()) {
			t.Errorf("x + 1 = %+v, want NaN", result)
		}
	})
}

// --- casts ---

func TestEvalCastToNumber(t *testing.T) {
	assertNumber(t, mustEval(t, `number("123")`, nil), 123)
	assertNumber(t, mustEval(t, `number("-123")`, nil), -123)
	assertNumber(t, mustEval(t, `number("+123")`, nil), 123)
	assertNumber(t, mustEval(t, `number("12.34")`, nil), 12.34)

	lookup := lookupFrom(map[string]Value{"x": NumberValue(5)})
	assertNumber(t, mustEval(t, "number(x)", lookup), 5)

	badStrings := []string{"abc", "", ".5", "5.", "1e5"}
	for _, s := range badStrings {
		source := `number("` + s + `")`
		expectEvalError(t, source, nil, fmt.Sprintf("cannot convert a string of length %d to a number", len(s)), 7)
	}

	trueLookup := lookupFrom(map[string]Value{"x": BoolValue(true)})
	expectEvalError(t, "number(x)", trueLookup,
		"cannot convert a boolean to a number, compare it directly instead", 7)

	falseLookup := lookupFrom(map[string]Value{"x": BoolValue(false)})
	expectEvalError(t, "number(x)", falseLookup,
		"cannot convert a boolean to a number, compare it directly instead", 7)
}

func TestEvalCastToString(t *testing.T) {
	assertString(t, mustEval(t, "string(true)", nil), "true")
	assertString(t, mustEval(t, "string(false)", nil), "false")
	assertString(t, mustEval(t, `string("already")`, nil), "already")

	cases := []struct {
		num  float64
		want string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "0"},
		{0.1, "0.1"},
		{1e21, "1e+21"},
		{-5, "-5"},
	}
	for _, c := range cases {
		lookup := lookupFrom(map[string]Value{"x": NumberValue(c.num)})
		assertString(t, mustEval(t, "string(x)", lookup), c.want)
	}
}

func TestEvalCastToBool(t *testing.T) {
	assertBool(t, mustEval(t, `bool("true")`, nil), true)
	assertBool(t, mustEval(t, `bool("false")`, nil), false)
	assertBool(t, mustEval(t, "bool(true)", nil), true)
	assertBool(t, mustEval(t, "bool(false)", nil), false)

	badStrings := []string{"True", "1", ""}
	for _, s := range badStrings {
		source := `bool("` + s + `")`
		expectEvalError(t, source, nil, fmt.Sprintf(`cannot convert a string of length %d to a boolean, expected "true" or "false"`, len(s)), 5)
	}

	zeroLookup := lookupFrom(map[string]Value{"x": NumberValue(0)})
	expectEvalError(t, "bool(x)", zeroLookup,
		"cannot convert a number to a boolean, compare it directly instead", 5)

	oneLookup := lookupFrom(map[string]Value{"x": NumberValue(1)})
	expectEvalError(t, "bool(x)", oneLookup,
		"cannot convert a number to a boolean, compare it directly instead", 5)
}

func TestEvalErrorsNeverCarryARuntimeValue(t *testing.T) {
	const secret = "s3cr3t-token"
	secretString := lookupFrom(map[string]Value{"x": StringValue(secret), "y": NumberValue(48151623)})
	sources := []string{"number(x)", "bool(x)", "x + 1", "-x", "not x", "x < 1", "1 < x", "x and true", "y and true", "1 + true"}
	for _, source := range sources {
		_, err := Evaluate(source, secretString)
		if err == nil {
			t.Fatalf("Evaluate(%q) succeeded, want an error", source)
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "48151623") {
			t.Errorf("Evaluate(%q) error %q carries a runtime value", source, err.Error())
		}
	}
}
