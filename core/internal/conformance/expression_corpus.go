package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dev.atomyx/internal/script/expression"
)

// ExpressionVariable is one typed variable value a corpus case binds
// for its lookup, matching ExpressionOracleVariable in
// packages/script/src/conformance-oracle.ts. Number is transmitted as
// text (see LoadExpressionCorpus) so the corpus and the request wire
// format can represent every special value (Infinity, -Infinity, NaN)
// that a JSON number literal cannot. Boolean has no omitempty: a
// boolean variable whose value is false must still round-trip through
// the oracle request as an explicit "boolean": false, not as an
// absent field, since the oracle interprets an absent field as no
// value bound for that variable at all.
type ExpressionVariable struct {
	Type    string `json:"type"`
	Number  string `json:"number,omitempty"`
	String  string `json:"string,omitempty"`
	Boolean bool   `json:"boolean"`
}

// ExpressionCase is one differential expression evaluation case read
// from a corpus file under corpus/expression/. The corpus file format
// is documented in corpus/expression/format.md.
type ExpressionCase struct {
	Name        string                        `json:"name"`
	Description string                        `json:"description"`
	Expr        string                        `json:"expr"`
	Variables   map[string]ExpressionVariable `json:"variables,omitempty"`
}

// Lookup builds an expression.Lookup closure from c.Variables, ready
// to pass to expression.Evaluate or expression.Eval. Number text is
// decoded with strconv.ParseFloat; LoadExpressionCorpus already
// validated that every variable's Type is one of number, string,
// boolean and that every number's text parses, so the parse error is
// ignored here.
func (c *ExpressionCase) Lookup() expression.Lookup {
	return func(name string) (expression.Value, bool) {
		variable, ok := c.Variables[name]
		if !ok {
			return expression.Value{}, false
		}
		switch variable.Type {
		case "number":
			f, _ := strconv.ParseFloat(variable.Number, 64)
			return expression.NumberValue(f), true
		case "string":
			return expression.StringValue(variable.String), true
		default: // boolean
			return expression.BoolValue(variable.Boolean), true
		}
	}
}

func loadExpressionCase(path string) (*ExpressionCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading expression corpus case %s: %w", path, err)
	}
	var c ExpressionCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing expression corpus case %s: %w", path, err)
	}

	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if c.Name == "" {
		return nil, fmt.Errorf("expression corpus case %s: missing name", path)
	}
	if c.Name != stem {
		return nil, fmt.Errorf("expression corpus case %s: name %q does not match filename stem %q", path, c.Name, stem)
	}
	if c.Description == "" {
		return nil, fmt.Errorf("expression corpus case %s: missing description", path)
	}

	for varName, variable := range c.Variables {
		switch variable.Type {
		case "number":
			if variable.Number == "Inf" {
				return nil, fmt.Errorf(
					"expression corpus case %s: variable %q uses literal \"Inf\", which JavaScript's Number(...) parses as NaN; write \"Infinity\" instead",
					path, varName)
			}
			if _, err := strconv.ParseFloat(variable.Number, 64); err != nil {
				return nil, fmt.Errorf("expression corpus case %s: variable %q has unparseable number text %q: %w",
					path, varName, variable.Number, err)
			}
		case "string", "boolean":
			// No text grammar to validate: every Go string value and
			// every JSON boolean literal is already well-formed.
		default:
			return nil, fmt.Errorf("expression corpus case %s: variable %q has unknown type %q", path, varName, variable.Type)
		}
	}

	return &c, nil
}

// LoadExpressionCorpus reads every *.json case file directly under
// dir, sorted by filename for deterministic execution order. It
// returns an error, rather than skipping, on any malformed case file
// so a bad corpus entry fails loudly instead of silently shrinking
// coverage.
func LoadExpressionCorpus(dir string) ([]*ExpressionCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading expression corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*ExpressionCase, 0, len(names))
	for _, name := range names {
		c, err := loadExpressionCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
