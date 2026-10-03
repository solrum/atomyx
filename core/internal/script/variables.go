package script

import "regexp"

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
