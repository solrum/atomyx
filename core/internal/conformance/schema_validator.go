package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// compileJSONSchema compiles a JSON Schema document from raw bytes,
// resolving every internal "$ref" against its own "definitions"
// section. name is only the resource identifier the compiler reports
// in error messages; it need not be a real file path.
func compileJSONSchema(name string, schemaJSON json.RawMessage) (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, bytes.NewReader(schemaJSON)); err != nil {
		return nil, fmt.Errorf("adding schema resource %s: %w", name, err)
	}
	return c.Compile(name)
}

// decodeJSONDoc unmarshals raw JSON bytes into the any-typed value
// tree jsonschema.Schema.Validate expects.
func decodeJSONDoc(raw json.RawMessage) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decoding JSON document: %w", err)
	}
	return v, nil
}

// acceptsDoc reports whether schema validates doc without error.
func acceptsDoc(schema *jsonschema.Schema, doc any) bool {
	return schema.Validate(doc) == nil
}
