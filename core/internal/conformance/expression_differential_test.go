package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/script/expression"
)

// expressionComparable is the shape the Go engine's result is
// marshaled through before diffing against the frozen golden, so no
// field can differ in presence from one case to the next. Unlike
// scriptComparable, message text and position ARE part of the diff:
// exact error strings and position attribution are a behavioral
// contract this test exists to verify.
type expressionComparable struct {
	Kind         string `json:"kind"`
	ValueType    string `json:"valueType"`
	NumberText   string `json:"numberText"`
	NumberBits   string `json:"numberBits"`
	StringValue  string `json:"stringValue"`
	BooleanValue bool   `json:"booleanValue"`
	ErrorKind    string `json:"errorKind"`
	Message      string `json:"message"`
	Position     int    `json:"position"`
}

func goResultComparable(value expression.Value, err error) (expressionComparable, error) {
	if err != nil {
		var parseErr *expression.ParseError
		var evalErr *expression.EvalError
		switch {
		case errors.As(err, &parseErr):
			return expressionComparable{Kind: "error", ErrorKind: "parse", Message: parseErr.Error(), Position: parseErr.Position}, nil
		case errors.As(err, &evalErr):
			return expressionComparable{Kind: "error", ErrorKind: "eval", Message: evalErr.Error(), Position: evalErr.Position}, nil
		default:
			return expressionComparable{}, fmt.Errorf("expression engine returned an error of unexpected type %T: %w", err, err)
		}
	}

	switch value.Kind() {
	case expression.KindNumber:
		n := value.Num()
		bits := math.Float64bits(n)
		if math.IsNaN(n) {
			bits = 0x7ff8000000000000
		}
		return expressionComparable{
			Kind:       "value",
			ValueType:  "number",
			NumberText: expression.FormatNumber(n),
			NumberBits: fmt.Sprintf("%016x", bits),
		}, nil
	case expression.KindString:
		return expressionComparable{Kind: "value", ValueType: "string", StringValue: value.Str()}, nil
	default: // KindBool
		return expressionComparable{Kind: "value", ValueType: "boolean", BooleanValue: value.Bool()}, nil
	}
}

// TestExpressionCorpusShape proves the corpus loads cleanly and is
// non-empty. LoadExpressionCorpus itself already rejects a case whose
// name is empty or does not match its filename stem, so this test
// only needs to confirm the load succeeds and yields cases.
func TestExpressionCorpusShape(t *testing.T) {
	cases, err := LoadExpressionCorpus("corpus/expression")
	if err != nil {
		t.Fatalf("loading expression corpus: %v", err)
	}
	if len(cases) == 0 {
		t.Fatalf("expression corpus is empty")
	}

	seen := make(map[string]bool, len(cases))
	for _, c := range cases {
		if seen[c.Name] {
			t.Fatalf("duplicate expression corpus case name %q", c.Name)
		}
		seen[c.Name] = true
	}
}

func TestExpressionDifferential(t *testing.T) {
	cases, err := LoadExpressionCorpus("corpus/expression")
	if err != nil {
		t.Fatalf("loading expression corpus: %v", err)
	}
	if len(cases) == 0 {
		t.Fatalf("expression corpus is empty")
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			value, evalErr := expression.Evaluate(c.Expr, c.Lookup())
			goComparable, err := goResultComparable(value, evalErr)
			if err != nil {
				t.Fatalf("classifying Go expression result: %v", err)
			}

			goJSON, err := json.Marshal(goComparable)
			if err != nil {
				t.Fatalf("marshaling Go comparable: %v", err)
			}

			checkGolden(t, filepath.Join("expression", c.Name+".json"), goJSON, DiffJSON)
		})
	}
}
