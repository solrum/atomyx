package config

import (
	"encoding/json"
	"testing"
)

func TestGenerateJSONSchema_ProducesValidJSON(t *testing.T) {
	data, err := json.Marshal(GenerateJSONSchema())
	if err != nil {
		t.Fatalf("marshaling generated schema: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("generated schema is not valid JSON: %s", data)
	}
}

// TestJSONSchemaEmbedIsFresh guards against internal/config/
// atomyx-config.schema.json drifting from GenerateJSONSchema: run
// `go run ./cmd/gen-config-schema -out internal/config/atomyx-config.schema.json`
// after any change to a config type or to jsonschema.go / jsonschema_builders.go.
func TestJSONSchemaEmbedIsFresh(t *testing.T) {
	generated, err := json.MarshalIndent(GenerateJSONSchema(), "", "  ")
	if err != nil {
		t.Fatalf("marshaling generated schema: %v", err)
	}
	generated = append(generated, '\n')

	if string(generated) != string(JSONSchemaBytes) {
		t.Fatal("internal/config/atomyx-config.schema.json is stale; regenerate it with " +
			"`go run ./cmd/gen-config-schema -out internal/config/atomyx-config.schema.json`")
	}
}
