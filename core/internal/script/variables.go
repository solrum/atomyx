package script

import (
	"net/url"
	"regexp"
	"strings"
)

// variablePlaceholder matches a `${varName}` interpolation site.
var variablePlaceholder = regexp.MustCompile(`\$\{(\w+)\}`)

// resolveVariables resolves `${varName}` placeholders in value
// against env, recursing into sequences and mappings. Every raw
// script value reaching this function is the output of
// decodeGenericValue, so a mapping is always an OrderedMap, never a
// plain Go map. An unresolved variable is left as its literal
// `${name}` text, not an error. The runner catches a missing
// variable at execution time if it matters.
func resolveVariables(value any, env map[string]string) any {
	return expandPlaceholders(value, func(name string) (string, bool) {
		val, ok := env[name]
		return val, ok
	})
}

// expandPlaceholders is resolveVariables with the value of each name
// supplied by lookup, which reports false to leave a placeholder as
// written.
func expandPlaceholders(value any, lookup func(name string) (string, bool)) any {
	switch v := value.(type) {
	case string:
		return variablePlaceholder.ReplaceAllStringFunc(v, func(match string) string {
			key := variablePlaceholder.FindStringSubmatch(match)[1]
			if val, ok := lookup(key); ok {
				return val
			}
			return match
		})
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = expandPlaceholders(item, lookup)
		}
		return out
	case OrderedMap:
		var out OrderedMap
		for _, k := range v.Keys() {
			val, _ := v.Get(k)
			if k == CommandHTTPRequest {
				out.Set(k, expandHTTPRequest(val, lookup))
				continue
			}
			out.Set(k, expandPlaceholders(val, lookup))
		}
		return out
	default:
		return value
	}
}

// coerceStringRecord renders every value in a raw YAML mapping to its
// string form via jsString, matching the parser's coercion of the
// script's own `env:` block before it is merged with external env.
func coerceStringRecord(raw any) map[string]string {
	out := map[string]string{}
	m, ok := asMap(raw)
	if !ok {
		return out
	}
	for k, v := range m {
		out[k] = jsString(v)
	}
	return out
}

// expandHTTPRequest expands the placeholders of an httpRequest step's
// fields. A value substituted into the query part of `url` is
// query-escaped, so a variable holding `a&b=c` stays one parameter
// value. Every other field takes the value as written.
func expandHTTPRequest(value any, lookup func(name string) (string, bool)) any {
	fields, ok := value.(OrderedMap)
	if !ok {
		return expandPlaceholders(value, lookup)
	}
	var out OrderedMap
	for _, k := range fields.Keys() {
		val, _ := fields.Get(k)
		if raw, isString := val.(string); isString && k == "url" {
			out.Set(k, expandURLPlaceholders(raw, lookup))
			continue
		}
		out.Set(k, expandPlaceholders(val, lookup))
	}
	return out
}

// expandURLPlaceholders expands the placeholders of a URL, escaping the
// substituted values that land after the first `?`.
func expandURLPlaceholders(raw string, lookup func(name string) (string, bool)) string {
	head, query, hasQuery := strings.Cut(raw, "?")
	expandedHead, _ := expandPlaceholders(head, lookup).(string)
	if !hasQuery {
		return expandedHead
	}
	expandedQuery, _ := expandPlaceholders(query, func(name string) (string, bool) {
		val, ok := lookup(name)
		return url.QueryEscape(val), ok
	}).(string)
	return expandedHead + "?" + expandedQuery
}
