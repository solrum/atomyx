package config

import (
	"math"
	"sort"
)

// Generic schema-checking primitives shared by every entity validator:
// type checks, required and optional field helpers, and enum
// validation.

// checkUnknownKeys mirrors zod's `.strict()`: an object carrying any
// key outside allowed is one issue at the object's own path (not one
// issue per extra key), matching zod's single "unrecognized_keys"
// issue per offending object.
func checkUnknownKeys(obj map[string]any, allowed map[string]bool, path []any, c *issueCollector) {
	var extra []string
	for key := range obj {
		if !allowed[key] {
			extra = append(extra, key)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		c.add(path, "unrecognized key(s): %v", extra)
	}
}

func asObject(raw any, path []any, c *issueCollector) (map[string]any, bool) {
	obj, ok := raw.(map[string]any)
	if !ok {
		c.add(path, "expected an object")
		return nil, false
	}
	return obj, true
}

func requiredString(obj map[string]any, key string, minLen int, path []any, c *issueCollector) (string, bool) {
	fieldPath := withSegment(path, key)
	raw, present := obj[key]
	if !present {
		c.add(fieldPath, "required")
		return "", false
	}
	s, isStr := raw.(string)
	if !isStr {
		c.add(fieldPath, "expected a string")
		return "", false
	}
	if len(s) < minLen {
		c.add(fieldPath, "string too short, expected at least %d character(s)", minLen)
		return "", false
	}
	return s, true
}

func optionalString(obj map[string]any, key string, minLen int, path []any, c *issueCollector) (*string, bool) {
	raw, present := obj[key]
	if !present || raw == nil {
		return nil, true
	}
	fieldPath := withSegment(path, key)
	s, isStr := raw.(string)
	if !isStr {
		c.add(fieldPath, "expected a string")
		return nil, false
	}
	if len(s) < minLen {
		c.add(fieldPath, "string too short, expected at least %d character(s)", minLen)
		return nil, false
	}
	return &s, true
}

func optionalBool(obj map[string]any, key string, path []any, c *issueCollector) (*bool, bool) {
	raw, present := obj[key]
	if !present || raw == nil {
		return nil, true
	}
	b, isBool := raw.(bool)
	if !isBool {
		c.add(withSegment(path, key), "expected a boolean")
		return nil, false
	}
	return &b, true
}

// optionalPositiveInt reads an optional integer field that must be at
// least 1. A YAML number with a fractional part is rejected.
func optionalPositiveInt(obj map[string]any, key string, path []any, c *issueCollector) (*int64, bool) {
	raw, present := obj[key]
	if !present || raw == nil {
		return nil, true
	}
	var n int64
	switch v := raw.(type) {
	case int:
		n = int64(v)
	case int64:
		n = v
	case uint64:
		if v > math.MaxInt64 {
			c.add(withSegment(path, key), "expected a positive integer")
			return nil, false
		}
		n = int64(v)
	case float64:
		if v != math.Trunc(v) || math.IsInf(v, 0) || v > math.MaxInt64 {
			c.add(withSegment(path, key), "expected an integer")
			return nil, false
		}
		n = int64(v)
	default:
		c.add(withSegment(path, key), "expected an integer")
		return nil, false
	}
	if n < 1 {
		c.add(withSegment(path, key), "expected a positive integer")
		return nil, false
	}
	return &n, true
}

func optionalStringArray(obj map[string]any, key string, minItemLen int, path []any, c *issueCollector) *[]string {
	raw, present := obj[key]
	if !present || raw == nil {
		return nil
	}
	fieldPath := withSegment(path, key)
	list, isArray := raw.([]any)
	if !isArray {
		c.add(fieldPath, "expected an array")
		return nil
	}
	failed := false
	out := make([]string, len(list))
	for i, item := range list {
		s, isStr := item.(string)
		if !isStr || len(s) < minItemLen {
			c.add(withSegment(fieldPath, i), "expected a non-empty string")
			failed = true
			continue
		}
		out[i] = s
	}
	if failed {
		return nil
	}
	return &out
}

func optionalStringMap(obj map[string]any, key string, path []any, c *issueCollector) (*map[string]string, bool) {
	raw, present := obj[key]
	if !present || raw == nil {
		return nil, true
	}
	fieldPath := withSegment(path, key)
	src, isObject := raw.(map[string]any)
	if !isObject {
		c.add(fieldPath, "expected an object")
		return nil, false
	}
	failed := false
	out := make(map[string]string, len(src))
	for k, v := range src {
		s, isStr := v.(string)
		if !isStr {
			c.add(withSegment(fieldPath, k), "expected a string")
			failed = true
			continue
		}
		out[k] = s
	}
	if failed {
		return nil, false
	}
	return &out, true
}

// validateEnum validates an optional string field against values,
// returning nil when the field is absent.
func validateEnum(obj map[string]any, key string, values []string, path []any, c *issueCollector) *string {
	raw, present := obj[key]
	if !present || raw == nil {
		return nil
	}
	fieldPath := withSegment(path, key)
	s, isStr := raw.(string)
	if !isStr || !contains(values, s) {
		c.add(fieldPath, "expected one of %v", values)
		return nil
	}
	return &s
}

// validateEnumRequired validates a required string field against
// values, returning nil when the field is absent, of the wrong type,
// or not one of values.
func validateEnumRequired(obj map[string]any, key string, values []string, path []any, c *issueCollector) *string {
	fieldPath := withSegment(path, key)
	raw, present := obj[key]
	if !present {
		c.add(fieldPath, "required")
		return nil
	}
	s, isStr := raw.(string)
	if !isStr || !contains(values, s) {
		c.add(fieldPath, "expected one of %v", values)
		return nil
	}
	return &s
}

func contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
