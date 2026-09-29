package config

// ref returns a JSON Schema `$ref` pointing at the named entry under
// this document's own "definitions" map.
func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/definitions/" + name}
}

func withDescription(m map[string]any, description string) map[string]any {
	if description != "" {
		m["description"] = description
	}
	return m
}

func stringSchema(description string) map[string]any {
	return withDescription(map[string]any{"type": "string"}, description)
}

func stringMinLen(minLen int, description string) map[string]any {
	m := stringSchema(description)
	m["minLength"] = minLen
	return m
}

func stringEnum(values []string, description string) map[string]any {
	m := stringSchema(description)
	m["enum"] = values
	return m
}

func booleanSchema(description string) map[string]any {
	return withDescription(map[string]any{"type": "boolean"}, description)
}

func arraySchema(items any, minItems int, description string) map[string]any {
	m := withDescription(map[string]any{"type": "array", "items": items}, description)
	if minItems > 0 {
		m["minItems"] = minItems
	}
	return m
}

// objectSchema builds a closed object schema (`additionalProperties:
// false`), matching every AtomyxConfigSchema object schema, which is
// built from a zod `.strict()` object.
func objectSchema(properties map[string]any, required []string, description string) map[string]any {
	m := withDescription(map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}, description)
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

// stringMapSchema is a record keyed by string with string values,
// matching zod's `z.record(z.string(), z.string())`.
func stringMapSchema(description string) map[string]any {
	return withDescription(map[string]any{
		"type":                 "object",
		"additionalProperties": map[string]any{"type": "string"},
	}, description)
}

// recordSchema is a record keyed by string whose values follow
// valueSchema, matching zod's `z.record(z.string(), X)`.
func recordSchema(valueSchema any, description string) map[string]any {
	return withDescription(map[string]any{
		"type":                 "object",
		"additionalProperties": valueSchema,
	}, description)
}
