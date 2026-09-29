package mcp

// Draft-07 JSON Schema builders for the tool input schemas below. Each
// builder returns a plain map describing one JSON Schema fragment: an
// object's "properties" carries its field list, and "required" lists
// only the property names a caller must supply. A field-level
// "description" is attached only where the schema author calls
// jsStringDesc explicitly, so a client reading a compiled schema sees
// documentation on exactly the fields the author chose to annotate.
//
// Go's encoding/json sorts map keys alphabetically when marshaling a
// map, so the "properties" object in a compiled schema does not
// preserve the order these builders declare it in. "required" is a
// plain slice, so its order is preserved. A "tools/list" response
// built from these schemas is therefore stable in its set of
// properties, types, and required fields, but not in property byte
// order. Only Go struct-based tool RESULTS, whose declared field
// order encoding/json preserves, produce byte-for-byte reproducible
// output.

func jsString() map[string]any { return map[string]any{"type": "string"} }

func jsStringDesc(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func jsNumber() map[string]any { return map[string]any{"type": "number"} }

func jsBoolean() map[string]any { return map[string]any{"type": "boolean"} }

func jsArray(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func jsConst(value string) map[string]any { return map[string]any{"const": value} }

func jsAnyOf(options ...map[string]any) map[string]any {
	anyOf := make([]any, len(options))
	for i, o := range options {
		anyOf[i] = o
	}
	return map[string]any{"anyOf": anyOf}
}

func jsStringRecord() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": jsString()}
}

// jsObject builds an object schema. required lists the property names
// that must be present, in the order the source Zod object declares
// its shape (Go's []string preserves this order even though the
// sibling "properties" map does not).
func jsObject(properties map[string]any, required ...string) map[string]any {
	out := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// jsEmptyObject is the schema for a tool that takes no arguments: an
// object with no declared properties and none required. It does not
// forbid unknown properties, so a call that passes extra arguments
// still validates against it, and the tool's handler simply ignores
// them.
func jsEmptyObject() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// patternSchema is the JSON Schema for one SelectorSchema pattern
// field: either a literal string or a {regex, flags?} object.
func patternSchema() map[string]any {
	return jsAnyOf(
		jsString(),
		jsObject(map[string]any{
			"regex": jsString(),
			"flags": jsString(),
		}, "regex"),
	)
}

// selectorInputSchema is the shared JSON Schema for the Selector wire
// shape, factored once so every tool that accepts a selector embeds
// the identical schema packages/mcp/src/selector-schema.ts defines.
func selectorInputSchema() map[string]any {
	return jsObject(map[string]any{
		"id":        patternSchema(),
		"text":      patternSchema(),
		"label":     patternSchema(),
		"hint":      patternSchema(),
		"value":     patternSchema(),
		"role":      jsString(),
		"enabled":   jsBoolean(),
		"clickable": jsBoolean(),
		"focused":   jsBoolean(),
		"nth":       jsNumber(),
	})
}
