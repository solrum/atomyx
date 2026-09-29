package conformance

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"dev.atomyx/internal/config"
)

// resolveConfigSchema compiles the Go-generated config JSON Schema
// that every test in this file validates corpus and constraint
// documents against.
func resolveConfigSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	raw, err := json.Marshal(config.GenerateJSONSchema())
	if err != nil {
		t.Fatalf("marshaling Go config schema: %v", err)
	}
	compiled, err := compileJSONSchema("go-config-schema.json", raw)
	if err != nil {
		t.Fatalf("compiling Go config schema: %v", err)
	}
	return compiled
}

// TestConfigJSONSchemaBehavior_Corpus asserts the Go-generated schema
// accepts every corpus config the Go validator itself accepts. A
// config the validator treats as valid must never be flagged invalid
// by the editor-facing schema.
func TestConfigJSONSchemaBehavior_Corpus(t *testing.T) {
	schema := resolveConfigSchema(t)

	cases, err := LoadConfigCorpus("corpus/config")
	if err != nil {
		t.Fatalf("loading config corpus: %v", err)
	}

	ran := 0
	for _, c := range cases {
		c := c
		if c.NoConfig {
			continue
		}
		cfg, issues := config.ParseAndValidate([]byte(c.Yaml))
		if len(issues) > 0 {
			continue
		}
		ran++
		t.Run(c.Name, func(t *testing.T) {
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatalf("marshaling parsed config: %v", err)
			}
			doc, err := decodeJSONDoc(raw)
			if err != nil {
				t.Fatalf("decoding parsed config: %v", err)
			}

			if !acceptsDoc(schema, doc) {
				t.Errorf("%s: schema unexpectedly rejects a config the validator accepted", c.Description)
			}
		})
	}
	if ran == 0 {
		t.Fatal("no corpus case parsed successfully; the accept-side check exercised nothing")
	}
}

// configSchemaFieldConstraintCase is one canonical document that
// violates a single field-level constraint AtomyxConfigSchema
// enforces. Every one of these constraints is representable in JSON
// Schema (enum, minLength, minItems, or a closed object), so the
// schema must reject it.
type configSchemaFieldConstraintCase struct {
	name string
	doc  string
}

func configSchemaFieldConstraintCases() []configSchemaFieldConstraintCase {
	return []configSchemaFieldConstraintCase{
		{
			name: "missing-project",
			doc:  `{"apps":[{"id":"main","platform":"android","appId":"com.example.app"}],"environments":{}}`,
		},
		{
			name: "empty-apps",
			doc:  `{"project":"Demo","apps":[],"environments":{}}`,
		},
		{
			name: "invalid-platform-enum",
			doc:  `{"project":"Demo","apps":[{"id":"main","platform":"windows","appId":"com.example.app"}],"environments":{}}`,
		},
		{
			name: "empty-app-id",
			doc:  `{"project":"Demo","apps":[{"id":"","platform":"android","appId":"com.example.app"}],"environments":{}}`,
		},
		{
			name: "top-level-unknown-key",
			doc: `{"project":"Demo","apps":[{"id":"main","platform":"android","appId":"com.example.app"}],` +
				`"environments":{},"bogusTopLevelField":"nope"}`,
		},
		{
			name: "app-entry-unknown-key",
			doc: `{"project":"Demo","apps":[{"id":"main","platform":"android","appId":"com.example.app",` +
				`"bogus":"nope"}],"environments":{}}`,
		},
		{
			name: "invalid-preconditions-enum",
			doc: `{"project":"Demo","apps":[{"id":"main","platform":"android","appId":"com.example.app"}],` +
				`"environments":{},"preconditions":"always"}`,
		},
		{
			name: "report-with-empty-formats",
			doc: `{"project":"Demo","apps":[{"id":"main","platform":"android","appId":"com.example.app"}],` +
				`"environments":{},"report":{"formats":[],"outDir":"reports"}}`,
		},
		{
			name: "report-with-invalid-format-enum",
			doc: `{"project":"Demo","apps":[{"id":"main","platform":"android","appId":"com.example.app"}],` +
				`"environments":{},"report":{"formats":["pdf"],"outDir":"reports"}}`,
		},
		{
			name: "environment-device-given-as-an-object",
			doc: `{"project":"Demo","apps":[{"id":"main","platform":"android","appId":"com.example.app"}],` +
				`"environments":{"staging":{"app":"main","device":{"platform":"android"}}}}`,
		},
	}
}

// TestConfigJSONSchemaBehavior_FieldConstraints asserts the
// Go-generated schema rejects a document violating a schema-
// expressible field constraint.
func TestConfigJSONSchemaBehavior_FieldConstraints(t *testing.T) {
	schema := resolveConfigSchema(t)

	for _, c := range configSchemaFieldConstraintCases() {
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

// configSchemaGapCase is one canonical document that violates ONLY a
// cross-field rule enforced by config.ValidateDocument, never a
// field-level schema constraint. See the GenerateJSONSchema doc
// comment in internal/config/jsonschema.go for the set of rules the
// schema cannot express, so the schema must ACCEPT these.
type configSchemaGapCase struct {
	name string
	doc  string
}

func configSchemaGapCases() []configSchemaGapCase {
	return []configSchemaGapCase{
		{
			name: "environment-references-unknown-app-id",
			doc: `{"project":"Demo","apps":[{"id":"main","platform":"android","appId":"com.example.app"}],` +
				`"environments":{"staging":{"app":"does-not-exist"}}}`,
		},
		{
			name: "suite-with-neither-tags-nor-globs",
			doc: `{"project":"Demo","apps":[{"id":"main","platform":"android","appId":"com.example.app"}],` +
				`"environments":{},"suites":{"smoke":{}}}`,
		},
	}
}

// TestConfigJSONSchemaBehavior_DocumentedGaps asserts the
// Go-generated schema ACCEPTS a document that only
// config.ValidateDocument (not the schema) can reject, confirming the
// schema stays as permissive as its cross-field validation gap
// requires rather than silently tightening.
func TestConfigJSONSchemaBehavior_DocumentedGaps(t *testing.T) {
	schema := resolveConfigSchema(t)

	for _, c := range configSchemaGapCases() {
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
