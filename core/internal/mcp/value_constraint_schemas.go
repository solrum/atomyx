package mcp

import (
	"encoding/json"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Value-constraint schemas enforce numeric and string bounds (integer
// -ness, positivity, non-negativity, maximum, minimum string length)
// that the published tool schemas in schema_helpers.go deliberately
// omit. The published schemas emitted by "tools/list" stay
// structurally loose on purpose: they describe field names, types,
// and which fields are required, without embedding bound constraints
// that would make the published surface longer and harder to read.
// The bounds are enforced separately, as a validation pass applied
// after published-schema validation succeeds. A schema here only
// lists the properties that carry an extra constraint. Any property
// it omits is left to the tool's own (loose) published schema and Go
// struct decoding.

// jsIntPositive is a JSON Schema integer greater than zero, matching
// Zod's `z.number().int().positive()`.
func jsIntPositive() map[string]any {
	return map[string]any{"type": "integer", "exclusiveMinimum": 0}
}

// jsIntNonnegative is a JSON Schema integer of zero or more, matching
// Zod's `z.number().int().nonnegative()`.
func jsIntNonnegative() map[string]any {
	return map[string]any{"type": "integer", "minimum": 0}
}

// jsStringMinLen is a JSON Schema string with a minimum length,
// matching Zod's `z.string().min(n)`.
func jsStringMinLen(n int) map[string]any {
	return map[string]any{"type": "string", "minLength": n}
}

// jsFractionUpToOne is a JSON Schema number greater than zero and at
// most one, matching Zod's `z.number().positive().max(1)` (the swipe
// tool's `fraction` argument).
func jsFractionUpToOne() map[string]any {
	return map[string]any{"type": "number", "exclusiveMinimum": 0, "maximum": 1}
}

// nthConstraint is the shared `Selector.nth` bound
// (`z.number().int().nonnegative()` in selector-schema.ts) embedded
// under the given property name wherever a tool accepts a selector.
func nthConstraint(selectorField string) map[string]any {
	return map[string]any{selectorField: jsObject(map[string]any{"nth": jsIntNonnegative()})}
}

// compileConstraintSchema compiles a value-constraint schema for
// toolName, distinct from the published input schema compiled by
// compileToolSchema for the same tool.
func compileConstraintSchema(toolName string, schema map[string]any) *jsonschema.Schema {
	return compileToolSchema(toolName+"-value-constraints", schema)
}

// toolValueConstraintSchemas maps tool name to the schema that
// enforces its Zod value constraints. A tool absent from this map has
// no constraint beyond what its published schema and Go struct
// decoding already check.
var toolValueConstraintSchemas = map[string]*jsonschema.Schema{
	"get_ui_tree": compileConstraintSchema("get_ui_tree", jsObject(map[string]any{
		"limit": jsIntPositive(),
	})),
	"find_element": compileConstraintSchema("find_element", jsObject(map[string]any{
		"nth": jsIntNonnegative(),
	})),
	"tap": compileConstraintSchema("tap", jsObject(
		nthConstraint("selector"),
	)),
	"input_text": compileConstraintSchema("input_text", jsObject(
		nthConstraint("selector"),
	)),
	"wait_for_element": compileConstraintSchema("wait_for_element", jsObject(mergeProps(
		nthConstraint("selector"),
		map[string]any{
			"timeoutMs":      jsIntPositive(),
			"pollIntervalMs": jsIntPositive(),
		},
	))),
	"tap_and_wait_transition": compileConstraintSchema("tap_and_wait_transition", jsObject(mergeProps(
		nthConstraint("selector"),
		nthConstraint("waitForAbsent"),
		nthConstraint("waitForAppear"),
		map[string]any{
			"timeoutMs":    jsIntPositive(),
			"maxTimeoutMs": jsIntPositive(),
			"intervalMs":   jsIntPositive(),
		},
	))),
	"swipe": compileConstraintSchema("swipe", jsObject(map[string]any{
		"fraction":   jsFractionUpToOne(),
		"durationMs": jsIntPositive(),
	})),
	"select_device": compileConstraintSchema("select_device", jsObject(map[string]any{
		"id":   jsStringMinLen(1),
		"port": jsIntPositive(),
	})),
	"list_runs": compileConstraintSchema("list_runs", jsObject(map[string]any{
		"limit":  jsIntPositive(),
		"offset": jsIntNonnegative(),
	})),
	"get_test_stats": compileConstraintSchema("get_test_stats", jsObject(map[string]any{
		"test": jsStringMinLen(1),
		"last": jsIntPositive(),
	})),
	"get_run":            compileConstraintSchema("get_run", jsObject(map[string]any{"id": jsStringMinLen(1)})),
	"delete_run":         compileConstraintSchema("delete_run", jsObject(map[string]any{"id": jsStringMinLen(1)})),
	"update_run_summary": compileConstraintSchema("update_run_summary", jsObject(map[string]any{"id": jsStringMinLen(1)})),
	"list_bugs": compileConstraintSchema("list_bugs", jsObject(map[string]any{
		"limit":  jsIntPositive(),
		"offset": jsIntNonnegative(),
	})),
	"get_bug":    compileConstraintSchema("get_bug", jsObject(map[string]any{"id": jsStringMinLen(1)})),
	"delete_bug": compileConstraintSchema("delete_bug", jsObject(map[string]any{"id": jsStringMinLen(1)})),
	"start_run":  compileConstraintSchema("start_run", jsObject(map[string]any{"name": jsStringMinLen(1)})),
	"report_bug": compileConstraintSchema("report_bug", jsObject(map[string]any{
		"title":       jsStringMinLen(1),
		"description": jsStringMinLen(1),
	})),
	"add_case_study": compileConstraintSchema("add_case_study", jsObject(map[string]any{
		"title":    jsStringMinLen(1),
		"trigger":  jsStringMinLen(1),
		"solution": jsStringMinLen(1),
	})),
	"get_case_studies": compileConstraintSchema("get_case_studies", jsObject(map[string]any{
		"limit":  jsIntPositive(),
		"offset": jsIntNonnegative(),
	})),
}

// mergeProps combines several property maps into one, for tools whose
// constraint schema embeds more than one selector field or mixes
// selector and non-selector constraints.
func mergeProps(maps ...map[string]any) map[string]any {
	out := make(map[string]any, len(maps))
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// validateToolValueConstraints checks rawArgs against toolName's value
// -constraint schema, if it has one. It returns nil for a tool with no
// registered constraints, or when rawArgs satisfies every constraint.
func validateToolValueConstraints(toolName string, rawArgs json.RawMessage) error {
	schema, ok := toolValueConstraintSchemas[toolName]
	if !ok {
		return nil
	}
	return validateToolArguments(schema, rawArgs)
}
