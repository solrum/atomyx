package script

// GenerateJSONSchema builds a JSON Schema (draft-07) describing every
// document ParseScript accepts before Validate runs its cross-field
// checks. The schema is the source of truth Monaco and the MCP
// surface consume for completion, hover, and structural validation.
//
// Every field-level constraint zod enforces on ScriptDefinitionSchema
// (required properties, enums, numeric ranges, regex-constrained
// strings, closed objects) is represented here, at every nesting
// depth, because every object schema below is built through
// objectSchema, which always sets `additionalProperties: false`. That
// matches every object schema in ScriptDefinitionSchema, since all of
// them are declared with zod's `.strict()`. Two categories of
// constraint are NOT representable in JSON Schema, because a
// zod-to-json-schema conversion drops a schema's `.refine` and
// `.superRefine` callbacks:
//
//   - Cross-field rules enforced by Validate instead: a precondition
//     check's `visible` versus `notVisible` (exactly one), and a
//     pointer touch's `id` required and unique only once the step
//     carries more than one touch. Each field is independently
//     optional or count-conditional here, so Validate is what rejects
//     a document that leaves both or neither of the precondition
//     alternatives set, or a multi-touch pointer step whose touches
//     lack ids.
//   - A handle branch's `when.visible` versus `when.notVisible` is
//     not enforced anywhere, by design: HandleConditionSchema carries
//     no zod `.refine`, unlike PreconditionCheckSchema, so a handle
//     branch with both or neither set is accepted by the runtime.
//     Neither this schema nor Validate rejects it. See
//     HandleCondition's doc comment in types.go.
//   - The parser-level rule that a script needs either a non-empty
//     `appId` or at least one step. This rule was never part of the
//     zod schema either, because ParseScript enforces it directly
//     before Validate runs, so neither generated schema expresses it.
//
// Unknown (misspelled or extraneous) object keys are rejected by this
// schema at every depth, matching zod's `.strict()`. That rejection
// is never exercised while parsing YAML shorthand, in either
// language, because every per-command normalizer (here and in
// step-normalizer.ts and yaml-parser.ts) builds its canonical step or
// config object field by field from a fixed list of known keys, so an
// unrecognized key is silently absent from the object that reaches
// the schema or Validate and is never there for either to reject.
// The `.strict()` and `additionalProperties: false` rejection instead
// applies to a caller that validates an already-canonical object
// directly: `validateDefinition` and `ScriptDefinitionSchema.safeParse`,
// and this generated schema in Monaco or the MCP
// surface.
//
// A document nested inside a `handle` branch, a `branch` case, or a
// precondition's inline `setup` is validated against the full
// ScriptStep union via `$ref`, at any depth. A schema generator that
// inlines every definition instead of emitting `$ref` cannot express
// a cycle, so it would have to emit an unconstrained schema the
// second time it re-enters ScriptStepSchema along the same path (for
// example steps[].handle.branches[].do). The generated schema below
// has no such gap, because it uses `$ref` for every recursive
// occurrence of ScriptStep. It therefore rejects a document two or
// more levels deep with a missing required field or an unknown key
// inside a nested handle or branch, because
// every such document is one Validate and the zod runtime both
// already reject. This closes a gap rather than opening one: the
// script conformance corpus carries a valid, deeply nested handle and
// branch case precisely to prove that no document the runtime accepts
// is newly rejected here.
func GenerateJSONSchema() map[string]any {
	return map[string]any{
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"$id":         "https://schemas.atomyx.dev/script.schema.json",
		"$ref":        "#/definitions/ScriptDefinition",
		"definitions": jsonSchemaDefinitions(),
	}
}

func jsonSchemaDefinitions() map[string]any {
	defs := map[string]any{
		"ScriptSelector":       scriptSelectorSchema(),
		"StringOrSelector":     stringOrSelectorSchema(),
		"PointerAnchor":        pointerAnchorSchema(),
		"PointerTarget":        pointerTargetSchema(),
		"PointerAction":        pointerActionSchema(),
		"PointerTouch":         pointerTouchSchema(),
		"HandleCondition":      handleConditionSchema(),
		"HandleBranch":         handleBranchSchema(),
		"RepeatCondition":      repeatConditionSchema(),
		"PreconditionCheck":    preconditionCheckSchema(),
		"Precondition":         preconditionSchema(),
		"BranchMatchCondition": branchMatchConditionSchema(),
		"BranchCase":           branchCaseSchema(),
		"ScriptStep":           scriptStepSchema(),
		"ScriptDefinition":     scriptDefinitionSchema(),
	}
	for name, s := range stepJSONSchemaDefinitions() {
		defs[name] = s
	}
	for name, s := range inputStepSchemaDefinitions() {
		defs[name] = s
	}
	return defs
}

func scriptDefinitionSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"format": stringWithPattern(
				formatRegex.String(),
				"Script format version. Format: 'atomyx/v<n>'. Defaults to 'atomyx/v1' when omitted.",
			),
			"appId": stringSchema(
				"Bundle id (iOS) or package name (Android) of the app under test. " +
					"An empty string is reserved for flow fragments that run in a parent script's context.",
			),
			"name": stringMinLen(1, "Human-readable script name."),
			"description": stringSchema(
				"Longer description of what this script verifies.",
			),
			"precondition": stringSchema(
				"Human-readable precondition text. Informational only, not enforced by the runner.",
			),
			"tags": arraySchema(stringMinLen(1, ""), 0, "Tags for filtering and organizing runs."),
			"env": stringMapSchema(
				"Variables available via `${varName}` in every step value.",
			),
			"proxy": stringEnum(
				[]string{"required", "optional"},
				"Network-capture proxy policy. Defaults to 'optional'.",
			),
			"requires": arraySchema(
				stringMinLen(1, ""), 0,
				"Flow files to run before this script, relative to the current working directory.",
			),
			"preconditions": arraySchema(
				ref("Precondition"), 0,
				"Preconditions checked, and established via `setup` when unmet, before this script's steps run.",
			),
			"stepDelay": numberMin(0, "Delay in ms inserted between each step. Defaults to 500 ms."),
			"steps": arraySchema(
				ref("ScriptStep"), 0,
				"Ordered list of steps to execute.",
			),
			"_stepLines": arraySchema(
				integerMin(0, ""), 0,
				"Parser metadata: 1-indexed source line for each entry in `steps`.",
			),
		},
		[]string{"appId", "name", "env", "steps"},
		"An Atomyx YAML test script: an entry script (full config plus steps) or a flow fragment (steps only).",
	)
}

func scriptSelectorSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"text":  patternFieldSchema("Match element by its visible text."),
			"id":    patternFieldSchema("Match element by its stable resource id or accessibility id."),
			"label": patternFieldSchema("Match element by its accessibility label."),
			"hint":  patternFieldSchema("Match element by its input hint or placeholder."),
			"role":  stringSchema("Constrain the match to a semantic role, for example 'button'."),
			"nth":   integerMin(0, "When multiple elements match, pick the nth one (0-indexed)."),
		},
		nil,
		"Element selector. Every field is optional, but at least one should be set for the selector to be useful.",
	)
}

// patternFieldSchema describes a selector content field: an exact
// string, or a {regex, flags} object for a regular expression match.
func patternFieldSchema(description string) map[string]any {
	return anyOfSchema(
		description+" A plain string matches exactly. A {regex, flags} object matches by regular expression, unanchored unless the source starts with ^ and ends with $.",
		stringSchema(""),
		objectSchema(
			map[string]any{
				"regex": stringSchema("JavaScript-syntax regular expression source. Lookahead, lookbehind and backreferences are not supported."),
				"flags": stringSchema("Optional flags from i, m, s, g, y, u, d."),
			},
			[]string{"regex"},
			"Regular expression pattern.",
		),
	)
}

func stringOrSelectorSchema() map[string]any {
	return anyOfSchema(
		"Either a bare visible-text string or a full element selector.",
		stringSchema(""),
		ref("ScriptSelector"),
	)
}

func pointerAnchorSchema() map[string]any {
	return stringEnum(
		PointerAnchorValues,
		"Which point on the matched element's rect the target resolves to before `offset` is applied. Defaults to `center`.",
	)
}

func pointerTargetSchema() map[string]any {
	return anyOfSchema(
		"Target of a pointer down or move action: a selector-relative point, or an absolute coordinate.",
		objectSchema(
			map[string]any{
				"selector": ref("ScriptSelector"),
				"anchor":   ref("PointerAnchor"),
				"offset": objectSchema(
					map[string]any{
						"dx": numberSchema("Horizontal offset in points from the anchor, positive rightward."),
						"dy": numberSchema("Vertical offset in points from the anchor, positive downward."),
					},
					nil,
					"Point offset applied after resolving `anchor`.",
				),
			},
			[]string{"selector"},
			"Resolve to a point on the matched element's rect.",
		),
		objectSchema(
			map[string]any{
				"point": objectSchema(
					map[string]any{
						"x": numberSchema("Screen x in points."),
						"y": numberSchema("Screen y in points."),
					},
					[]string{"x", "y"},
					"",
				),
			},
			[]string{"point"},
			"Absolute screen coordinates in points.",
		),
	)
}

func pointerActionSchema() map[string]any {
	pressure := numberMinMax(0, 1, "Pressure applied at this action, in the range [0.0, 1.0].")
	return oneOfSchema(
		"One primitive in a pointer sequence, following W3C Actions semantics.",
		objectSchema(
			map[string]any{
				"type":     constString("down"),
				"target":   ref("PointerTarget"),
				"pressure": pressure,
			},
			[]string{"type", "target"},
			"Touch down at the resolved target. A pointer sequence must open with `down`.",
		),
		objectSchema(
			map[string]any{
				"type":     constString("move"),
				"target":   ref("PointerTarget"),
				"pressure": pressure,
			},
			[]string{"type", "target"},
			"Drag the active pointer to a new target.",
		),
		objectSchema(
			map[string]any{
				"type": constString("wait"),
				"ms":   numberMin(0, "Hold duration in milliseconds."),
			},
			[]string{"type", "ms"},
			"Hold the current pointer position.",
		),
		objectSchema(
			map[string]any{"type": constString("up")},
			[]string{"type"},
			"Release the pointer. A pointer sequence must close with `up`.",
		),
		objectSchema(
			map[string]any{
				"type":  constString("capture"),
				"label": stringSchema("Filename-safe label attached to the captured image in the run artifacts."),
			},
			[]string{"type"},
			"Capture a screenshot while the pointer is held down, mid-gesture. Valid only strictly between the sequence's opening `down` and its closing `up`.",
		),
	)
}

func pointerTouchSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"id":      stringMinLen(1, "Author-chosen pointer id. Required and unique across touches when the step carries more than one touch (a multi-pointer gesture); optional and unused for a single-touch gesture."),
			"actions": arraySchema(ref("PointerAction"), 1, "This pointer's time-ordered action list."),
		},
		[]string{"actions"},
		"One pointer's path in a pointer step: an optional id and a required time-ordered action list.",
	)
}

func handleConditionSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"visible":    ref("StringOrSelector"),
			"notVisible": ref("StringOrSelector"),
		},
		nil,
		"Condition a handle branch matches on. Exactly one of `visible` or `notVisible` should be set.",
	)
}

func handleBranchSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"when": ref("HandleCondition"),
			"do":   stepsOrFileSchema(),
		},
		[]string{"when", "do"},
		"One branch of a handle step: a condition plus the steps to run when it matches.",
	)
}

func repeatConditionSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"visible":    ref("StringOrSelector"),
			"notVisible": ref("StringOrSelector"),
			"expr":       stringMinLen(1, "Boolean expression over the script's runtime variables."),
		},
		nil,
		"Condition a repeat loop checks before each iteration. Exactly one of `visible`, `notVisible` or `expr` must be set.",
	)
}

func preconditionCheckSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"visible":    ref("StringOrSelector"),
			"notVisible": ref("StringOrSelector"),
		},
		nil,
		"Condition a precondition checks before its flow runs. Exactly one of `visible` or `notVisible` must be set.",
	)
}

func preconditionSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"check": ref("PreconditionCheck"),
			"setup": stepsOrFileSchema(),
			"policy": stringEnum(
				[]string{"ensure", "always", "skip"},
				"How the runner enforces this precondition. Defaults to 'ensure'.",
			),
		},
		[]string{"check", "setup"},
		"A run-once setup gate a flow's steps require before they run.",
	)
}

func branchMatchConditionSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"status": integerMinMax(100, 599, "Match when response status equals this code."),
			"body":   anyMapSchema("Match when every dot-path in this map equals its expected value in the response body."),
		},
		nil,
		"Case condition for an API-based branch. Status and body are ANDed together when both are set.",
	)
}

func branchCaseSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"match": ref("BranchMatchCondition"),
			"do":    stepsOrFileSchema(),
		},
		[]string{"match", "do"},
		"One case of a branch step: a match condition plus the steps to run when it matches.",
	)
}

func scriptStepSchema() map[string]any {
	refs := make([]any, 0, len(stepCommandOrder))
	for _, name := range stepCommandOrder {
		refs = append(refs, ref(name))
	}
	return oneOfSchema("One canonical step the runner accepts, discriminated by `command`.", refs...)
}

// stepsOrFileSchema is the `do` / `setup` / `default` shape every
// branching construct (handle, branch, precondition) accepts: either
// an inline step list or a path to a flow fragment file.
func stepsOrFileSchema() map[string]any {
	return anyOfSchema(
		"Steps to run: an inline list, or a string path to a flow fragment file.",
		arraySchema(ref("ScriptStep"), 0, ""),
		stringSchema(""),
	)
}
