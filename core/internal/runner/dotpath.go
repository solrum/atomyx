package runner

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
)

// jsUndefined is the sentinel resolveDotPath returns for a path
// segment that names a map key or array index absent from the
// traversed value. It is distinct from a Go nil interface, which
// represents an explicit JSON null: assertApi's `$exists` operator
// and extract's "path resolved to nothing" check both depend on
// telling an absent path apart from one that resolved to a real
// null value.
var jsUndefined = &struct{}{}

// isJSUndefined reports whether v is the resolveDotPath sentinel.
func isJSUndefined(v any) bool {
	return v == jsUndefined
}

var fromKeyWrapperPattern = regexp.MustCompile(`^\$\{(.+)\}$`)

// unwrapFromKey strips a `${varName}` wrapper a script author wrote
// around a `from:` value instead of the bare variable name, matching
// every command that reads a captured request by name.
func unwrapFromKey(from string) string {
	if m := fromKeyWrapperPattern.FindStringSubmatch(from); m != nil {
		return m[1]
	}
	return from
}

var dotPathIndexSegmentPattern = regexp.MustCompile(`^(\w+)((?:\[\d+\])+)$`)
var dotPathIndexPattern = regexp.MustCompile(`\[(\d+)\]`)

// resolveDotPath resolves a dot-path like "$.data.items[0].name" or
// "$.data.matrix[0][1]" against a JSON-decoded value (nil, bool,
// float64, string, map[string]any, or []any), matching how the same
// path indexes into the equivalent JavaScript value: a missing map
// key or an out-of-range array index yields jsUndefined, and once the
// current value is nil or jsUndefined every remaining segment also
// yields jsUndefined rather than panicking on a nil map or slice
// access. A path segment may carry more than one trailing "[N]"
// index, applied left to right, so "matrix[0][1]" first indexes into
// matrix[0] and then indexes [1] into that result.
func resolveDotPath(obj any, path string) any {
	normalized := strings.TrimPrefix(path, "$.")
	if normalized == path {
		normalized = strings.TrimPrefix(path, "$")
	}
	if normalized == "" {
		return obj
	}

	var segments []string
	for _, seg := range strings.Split(normalized, ".") {
		if m := dotPathIndexSegmentPattern.FindStringSubmatch(seg); m != nil {
			segments = append(segments, m[1])
			for _, idx := range dotPathIndexPattern.FindAllStringSubmatch(m[2], -1) {
				segments = append(segments, idx[1])
			}
		} else {
			segments = append(segments, seg)
		}
	}

	current := obj
	for _, segment := range segments {
		if current == nil || isJSUndefined(current) {
			return jsUndefined
		}
		switch c := current.(type) {
		case []any:
			idx, err := strconv.Atoi(segment)
			if err != nil || idx < 0 || idx >= len(c) {
				current = jsUndefined
				continue
			}
			current = c[idx]
		case map[string]any:
			v, ok := c[segment]
			if !ok {
				current = jsUndefined
				continue
			}
			current = v
		default:
			return jsUndefined
		}
	}
	return current
}

// asComparable renders a JSON-decoded map, a parsed script.OrderedMap,
// or a slice as a string-keyed map so jsDeepEqual can walk every
// container kind through one path, mirroring how JavaScript's
// Object.keys treats an array's indices as string keys. An expected
// value written inline in a script (an assertApi or branch body
// assertion) decodes as script.OrderedMap even though the actual
// value captured from a JSON response decodes as map[string]any, so
// both must render into the same comparable form for the two sides to
// match. It reports ok=false for anything else (a primitive, or
// jsUndefined), since only a container value can be compared key by
// key.
func asComparable(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case script.OrderedMap:
		return t.ToMap(), true
	case []any:
		out := make(map[string]any, len(t))
		for i, item := range t {
			out[strconv.Itoa(i)] = item
		}
		return out, true
	default:
		return nil, false
	}
}

// jsDeepEqual reports whether a and b, both JSON-decoded values, are
// structurally equal: primitive equality for scalars, recursive
// same-key comparison for maps and arrays (array indices compared as
// string keys), and false whenever exactly one side is a container.
func jsDeepEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	aObj, aIsContainer := asComparable(a)
	bObj, bIsContainer := asComparable(b)
	if aIsContainer || bIsContainer {
		if !aIsContainer || !bIsContainer {
			return false
		}
		if len(aObj) != len(bObj) {
			return false
		}
		for k, av := range aObj {
			bv, ok := bObj[k]
			if !ok || !jsDeepEqual(av, bv) {
				return false
			}
		}
		return true
	}
	// A number reaches this package through two different decode
	// paths that give it two different Go types for the one
	// JavaScript `number`: a captured JSON response body always
	// decodes a number as float64, while a value written inline in
	// a script (an assertApi or branch body assertion) decodes
	// through the YAML parser as int, int64, or float64 depending on
	// its literal form. JavaScript's `===` never sees this split, so
	// jsDeepEqual normalizes both sides to float64 before comparing
	// whenever both are one of those Go number types.
	if an, aIsNum := jsComparableNumber(a); aIsNum {
		if bn, bIsNum := jsComparableNumber(b); bIsNum {
			return an == bn
		}
	}

	// Both sides are primitives (string, float64, bool) or the
	// jsUndefined sentinel at this point, all of which are safe to
	// compare with == directly: Go panics comparing two interface
	// values only when their shared dynamic type is itself
	// non-comparable (a map or slice), which the container branch
	// above already excluded.
	return a == b
}

// jsComparableNumber reports v's value as a float64 when v is one of
// the Go numeric types the YAML and JSON decoders this package reads
// from can produce (int, int64, float64), so jsDeepEqual can compare
// a script-authored number against a captured one regardless of which
// of those two decoders produced each side.
func jsComparableNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

// isEmptyArray reports whether v is a JSON array with no elements,
// one of the conditions assertApi's `$not_empty` operator checks.
func isEmptyArray(v any) bool {
	arr, ok := v.([]any)
	return ok && len(arr) == 0
}

// toJSNumber converts v to a float64 the way JavaScript's Number()
// would for the scalar shapes a captured JSON body can hold. It
// reports an error where JavaScript's Number() would produce NaN,
// since assertApi's numeric operators treat NaN as "cannot compare".
func toJSNumber(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", t)
		}
		return f, nil
	case bool:
		if t {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("value is not a number")
	}
}

// jsonStringifyJS renders v the way JavaScript's JSON.stringify
// would render it when interpolated into a template string:
// jsUndefined (a value JSON.stringify itself returns `undefined`,
// not a string, for) renders as the literal text "undefined", nil
// renders as "null", and every other shape renders as its JSON text.
func jsonStringifyJS(v any) string {
	if v == nil {
		return "null"
	}
	if isJSUndefined(v) {
		return "undefined"
	}
	switch t := v.(type) {
	case string:
		b, err := json.Marshal(t)
		if err != nil {
			return `""`
		}
		return string(b)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return expression.FormatNumber(t)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

// jsStringOf renders v the way JavaScript's String(value) would for
// the shapes a captured JSON body can hold: extract's variable
// assignment depends on this exact coercion, since a script's later
// `${varName}` interpolation only ever sees the string form.
func jsStringOf(v any) string {
	if v == nil {
		return "null"
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return expression.FormatNumber(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case []any:
		parts := make([]string, len(t))
		for i, item := range t {
			if item == nil {
				parts[i] = ""
			} else {
				parts[i] = jsStringOf(item)
			}
		}
		return strings.Join(parts, ",")
	case map[string]any:
		return "[object Object]"
	default:
		return fmt.Sprintf("%v", t)
	}
}
