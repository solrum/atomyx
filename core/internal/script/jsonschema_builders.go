package script

import "dev.atomyx/internal/driver"

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

func stringWithPattern(pattern string, description string) map[string]any {
	m := stringSchema(description)
	m["pattern"] = pattern
	return m
}

func stringMinLenPattern(minLen int, pattern string, description string) map[string]any {
	m := stringMinLen(minLen, description)
	m["pattern"] = pattern
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

func constString(value string) map[string]any {
	return map[string]any{"type": "string", "const": value}
}

func integerMin(min int, description string) map[string]any {
	return withDescription(map[string]any{"type": "integer", "minimum": min}, description)
}

func integerMinMax(min, max int, description string) map[string]any {
	m := integerMin(min, description)
	m["maximum"] = max
	return m
}

func numberSchema(description string) map[string]any {
	return withDescription(map[string]any{"type": "number"}, description)
}

func numberMin(min float64, description string) map[string]any {
	m := numberSchema(description)
	m["minimum"] = min
	return m
}

func numberMinMax(min, max float64, description string) map[string]any {
	m := numberMin(min, description)
	m["maximum"] = max
	return m
}

func arraySchema(items any, minItems int, description string) map[string]any {
	m := withDescription(map[string]any{"type": "array", "items": items}, description)
	if minItems > 0 {
		m["minItems"] = minItems
	}
	return m
}

// objectSchema builds a closed object schema (`additionalProperties:
// false`), matching every ScriptDefinition object schema, which is
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

// anyMapSchema is a record keyed by string with unconstrained values,
// matching zod's `z.record(z.string(), z.unknown())`.
func anyMapSchema(description string) map[string]any {
	return withDescription(map[string]any{
		"type":                 "object",
		"additionalProperties": map[string]any{},
	}, description)
}

func anyOfSchema(description string, schemas ...any) map[string]any {
	return withDescription(map[string]any{"anyOf": schemas}, description)
}

func oneOfSchema(description string, schemas ...any) map[string]any {
	return withDescription(map[string]any{"oneOf": schemas}, description)
}

// scalarMapSchema is a record keyed by string whose values are strings,
// numbers or booleans.
func scalarMapSchema(description string) map[string]any {
	return withDescription(map[string]any{
		"type": "object",
		"additionalProperties": map[string]any{
			"anyOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"type": "number"},
				map[string]any{"type": "boolean"},
			},
		},
	}, description)
}

// permissionsMapSchema is a record from a known permission name to a
// permission state.
func permissionsMapSchema(description string) map[string]any {
	names := make([]string, 0, len(driver.AllPermissions()))
	for _, p := range driver.AllPermissions() {
		names = append(names, string(p))
	}
	states := make([]string, 0, len(driver.AllPermissionStates()))
	for _, s := range driver.AllPermissionStates() {
		states = append(states, string(s))
	}
	return withDescription(map[string]any{
		"type":                 "object",
		"propertyNames":        map[string]any{"enum": names},
		"additionalProperties": map[string]any{"enum": states},
	}, description)
}
