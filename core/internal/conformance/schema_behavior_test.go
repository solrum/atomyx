package conformance

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"dev.atomyx/internal/script"
)

// resolveScriptSchema compiles the Go-generated script JSON Schema
// that every test in this file validates corpus and constraint
// documents against.
func resolveScriptSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	raw, err := json.Marshal(script.GenerateJSONSchema())
	if err != nil {
		t.Fatalf("marshaling Go schema: %v", err)
	}
	compiled, err := compileJSONSchema("go-script-schema.json", raw)
	if err != nil {
		t.Fatalf("compiling Go schema: %v", err)
	}
	return compiled
}

// TestScriptJSONSchemaBehavior_Corpus asserts the Go-generated schema
// accepts every corpus script the Go parser itself accepts. A script
// the parser can run must never be flagged invalid by the
// editor-facing schema, or an editor validating against this schema
// would reject scripts the runtime treats as valid.
//
// The canonical document validated here is Go's own parsed and
// normalized ScriptDefinition. script_differential_test.go already
// proves, over this same corpus, that Go's canonical output matches
// its frozen golden byte for byte for every case the parser accepts,
// so validating that document against the schema is equivalent to
// validating the parser's own canonical output against it.
func TestScriptJSONSchemaBehavior_Corpus(t *testing.T) {
	schema := resolveScriptSchema(t)

	cases, err := LoadScriptCorpus("corpus/script")
	if err != nil {
		t.Fatalf("loading script corpus: %v", err)
	}

	ran := 0
	for _, c := range cases {
		c := c
		def, parseErr := script.ParseScript(c.Yaml, c.ExternalEnv)
		if parseErr != nil {
			continue
		}
		ran++
		t.Run(c.Name, func(t *testing.T) {
			raw, err := json.Marshal(def)
			if err != nil {
				t.Fatalf("marshaling parsed script: %v", err)
			}
			doc, err := decodeJSONDoc(raw)
			if err != nil {
				t.Fatalf("decoding parsed script: %v", err)
			}

			if !acceptsDoc(schema, doc) {
				t.Errorf("%s: schema unexpectedly rejects a script the parser accepted", c.Description)
			}
		})
	}
	if ran == 0 {
		t.Fatal("no corpus case parsed successfully; the accept-side check exercised nothing")
	}
}

// schemaFieldConstraintCase is one canonical (already shorthand-
// expanded) document that violates a single field-level constraint
// the schema generator enforces, paired with the corpus case it
// mirrors. Every one of these constraints is representable in JSON
// Schema (pattern, enum, minimum/maximum, minItems, or a closed
// oneOf), so the schema must reject it.
type schemaFieldConstraintCase struct {
	name string
	doc  string
}

func schemaFieldConstraintCases() []schemaFieldConstraintCase {
	cases := []schemaFieldConstraintCase{
		{
			name: "invalid-format-regex",
			doc:  `{"format":"v1","appId":"com.example.app","name":"Smoke","env":{},"steps":[{"command":"launchApp"}]}`,
		},
		{
			name: "invalid-capture-var-name",
			doc:  `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[{"command":"capture","pattern":"POST /api/login","as":"1bad"}]}`,
		},
		{
			name: "invalid-assert-api-status-out-of-range",
			doc:  `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[{"command":"assertApi","from":"loginRsp","status":999}]}`,
		},
		{
			name: "invalid-branch-empty-cases",
			doc:  `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[{"command":"branch","from":"resp","on":[]}]}`,
		},
		{
			name: "invalid-handle-empty-branches",
			doc:  `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[{"command":"handle","branches":[]}]}`,
		},
		{
			name: "invalid-unknown-step-command",
			doc:  `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[{"command":"bogusCommand"}]}`,
		},
		{
			name: "invalid-top-level-unknown-key",
			doc: `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[{"command":"launchApp"}],` +
				`"bogusTopLevelField":"nope"}`,
		},
		{
			name: "invalid-pointer-target-both-selector-and-point",
			doc: `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[` +
				`{"command":"pointer","touches":[{"actions":[` +
				`{"type":"down","target":{"selector":{"text":"Item A"},"point":{"x":1,"y":2}}},` +
				`{"type":"up"}]}]}]}`,
		},
		{
			name: "invalid-pointer-target-neither-selector-nor-point",
			doc: `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[` +
				`{"command":"pointer","touches":[{"actions":[{"type":"down","target":{}},{"type":"up"}]}]}]}`,
		},
	}
	return append(cases, nestedHandleRejectCases()...)
}

// nestedHandleDocTemplate wraps one step, two `handle` levels deep
// inside a `do` array, into a full canonical ScriptDefinition
// document: steps[0] is a handle whose branch's `do` holds a second
// handle, whose own branch's `do` holds the step under test. It
// exercises the schema's ScriptStep recursion at depth two rather
// than only at the top level.
func nestedHandleDocTemplate(stepAtDepthTwo string) string {
	return `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[` +
		`{"command":"handle","branches":[{"when":{"visible":"Outer"},"do":[` +
		`{"command":"handle","branches":[{"when":{"visible":"Inner"},"do":[` +
		stepAtDepthTwo +
		`]}]}` +
		`]}]}` +
		`]}`
}

// nestedHandleRejectCases returns documents invalid two `handle`
// levels deep, mirroring the same violations schemaFieldConstraintCases
// checks at the top level. The schema resolves ScriptStep recursion
// through "$ref" at every depth, so a step nested inside two `handle`
// branches must be rejected exactly like its depth-zero equivalent.
func nestedHandleRejectCases() []schemaFieldConstraintCase {
	return []schemaFieldConstraintCase{
		{
			name: "nested-handle-do-unknown-key-at-depth-two",
			doc:  nestedHandleDocTemplate(`{"command":"tap","selector":{"text":"Confirm"},"bogusField":"nope"}`),
		},
		{
			name: "nested-handle-do-missing-required-field-at-depth-two",
			doc:  nestedHandleDocTemplate(`{"command":"tap"}`),
		},
	}
}

// TestScriptJSONSchemaBehavior_FieldConstraints asserts the
// Go-generated schema rejects a document violating a schema-
// expressible field constraint. Each case names the corpus/script/*.json
// case under go/internal/conformance/corpus/script it mirrors; the
// document here is the canonical (post-normalization) shape the
// corpus's shorthand YAML would parse into if the violated field
// alone were valid, because the JSON Schema, unlike ParseScript,
// validates the canonical shape directly and has no shorthand
// normalizer in front of it.
func TestScriptJSONSchemaBehavior_FieldConstraints(t *testing.T) {
	schema := resolveScriptSchema(t)

	for _, c := range schemaFieldConstraintCases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			doc, err := decodeJSONDoc(json.RawMessage(c.doc))
			if err != nil {
				t.Fatalf("decoding case document: %v", err)
			}

			if acceptsDoc(schema, doc) {
				t.Errorf("schema unexpectedly accepts %s", c.doc)
			}
		})
	}
}

// schemaGapCase is one canonical document that violates ONLY a
// cross-field rule enforced by script.Validate or ParseScript itself,
// never a field-level constraint the generated schema can express.
// See the GenerateJSONSchema doc comment in
// internal/script/jsonschema.go for the set of rules the schema
// cannot express, so the schema must ACCEPT these.
type schemaGapCase struct {
	name string
	doc  string
}

func schemaGapCases() []schemaGapCase {
	return []schemaGapCase{
		{
			// A multi-touch pointer step requires a non-empty id on each
			// touch, but that requirement is conditional on the touch
			// count, which no field-level JSON Schema construct the
			// generator emits can express; the schema accepts it and
			// script.Validate rejects it.
			name: "pointer-multi-touch-missing-id-invalid",
			doc: `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[` +
				`{"command":"pointer","touches":[` +
				`{"actions":[{"type":"down","target":{"point":{"x":1,"y":2}}},{"type":"up"}]},` +
				`{"actions":[{"type":"down","target":{"point":{"x":3,"y":4}}},{"type":"up"}]}]}` +
				`]}`,
		},
		{
			name: "precondition-check-both-visible-and-not-visible-invalid",
			doc: `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[{"command":"launchApp"}],` +
				`"preconditions":[{"check":{"visible":"Home","notVisible":"Onboarding"},"setup":"flows/setup.yml"}]}`,
		},
		{
			name: "precondition-check-neither-visible-nor-not-visible-invalid",
			doc: `{"appId":"com.example.app","name":"Smoke","env":{},"steps":[{"command":"launchApp"}],` +
				`"preconditions":[{"check":{},"setup":"flows/setup.yml"}]}`,
		},
		{
			name: "invalid-no-steps-no-app-id",
			doc:  `{"appId":"","name":"Empty","env":{},"steps":[]}`,
		},
	}
}

// TestScriptJSONSchemaBehavior_DocumentedGaps asserts the
// Go-generated schema ACCEPTS a document that only the full validator
// (script.Validate, not the schema) can reject. Neither JSON Schema
// construct can express a cross-field rule like these, so the schema
// is necessarily permissive here; script.Validate closes the gap at
// runtime.
func TestScriptJSONSchemaBehavior_DocumentedGaps(t *testing.T) {
	schema := resolveScriptSchema(t)

	for _, c := range schemaGapCases() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			doc, err := decodeJSONDoc(json.RawMessage(c.doc))
			if err != nil {
				t.Fatalf("decoding case document: %v", err)
			}

			if !acceptsDoc(schema, doc) {
				t.Errorf("schema unexpectedly rejects %s; the documented gap may have closed", c.doc)
			}
		})
	}
}
