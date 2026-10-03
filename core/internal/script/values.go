package script

import (
	"fmt"
	"math"
	"sort"
	"strconv"
)

// The generic YAML decode result carries these dynamic shapes: map[string]any (or,
// for a mapping reachable from a script's step list, an OrderedMap, see
// see decodeGenericValue) for mappings, []any for sequences, and
// string/int/float64/bool/nil for scalars.

// asMap renders v as a plain Go map regardless of which of the two
// mapping shapes decodeGenericValue can produce, for a caller that
// does not need key order.
func asMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case OrderedMap:
		return t.ToMap(), true
	}
	return nil, false
}

// asOrderedMap extracts a mapping's entries in declaration order, for
// the three dot-path-keyed fields that need it: AssertApiStep.Body,
// BranchMatchCondition.Body, and ExtractStep.Values. v is an
// OrderedMap whenever it was decoded from a script's step list, which
// preserves declaration order at every depth via decodeGenericValue.
// A step embedded in a precondition's inline `setup` block instead
// decodes through the script's plain config mapping, which does not
// track key order, so asOrderedMap falls back to that map's keys
// sorted alphabetically, which is deterministic, and no worse than this
// package's behavior before declaration order was tracked, rather
// than silently dropping the field.
func asOrderedMap(v any) (OrderedMap, bool) {
	switch t := v.(type) {
	case OrderedMap:
		return t, true
	case map[string]any:
		om := OrderedMap{}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			om.Set(k, t[k])
		}
		return om, true
	}
	return OrderedMap{}, false
}

func asSlice(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// asInt accepts any of the numeric scalar shapes the YAML decoder can
// produce for a whole-number literal (an int, an int64, or a float64
// with no fractional part) and returns it as an int, so a step field
// written as `5` or as `5.0` in the script normalizes to the same
// value, while a genuinely fractional number is rejected.
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == math.Trunc(n) {
			return int(n), true
		}
		return 0, false
	}
	return 0, false
}

// asFloat accepts any numeric scalar and returns it as a float64.
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// asFiniteFloat is asFloat, further rejecting NaN and infinite
// values. The YAML decoder accepts `.nan` and `.inf` scalars, but
// every script field that carries a physical quantity (a pressure, a
// pixel offset, a duration) is meaningless outside the finite range,
// so a non-finite value is treated the same as a non-numeric one.
func asFiniteFloat(v any) (float64, bool) {
	f, ok := asFloat(v)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// stringOrDefault renders v with JavaScript's `String()` coercion,
// falling back to def when v is nil (the TS normalizer's `?? ""`
// pattern before calling `String()`).
func stringOrDefault(v any, def string) string {
	if v == nil {
		return def
	}
	return jsString(v)
}

// jsString mirrors JavaScript's `String(value)` for the scalar and
// container shapes the YAML decoder can produce.
func jsString(v any) string {
	switch t := v.(type) {
	case nil:
		return "undefined"
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return formatJSNumber(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// formatJSNumber approximates JavaScript's Number.prototype.toString
// for the finite, non-exponential values ordinary script fields carry
// (ports, counts, timeouts): an integral float renders without a
// decimal point, everything else renders via its shortest round-trip
// decimal form.
func formatJSNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func goType(v any) string {
	if v == nil {
		return "undefined"
	}
	switch v.(type) {
	case map[string]any, OrderedMap:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int64, float64:
		return "number"
	default:
		return fmt.Sprintf("%T", v)
	}
}
