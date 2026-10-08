package runfolder

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// compileEntrySchema compiles the committed run schema and returns the
// definition that describes one line of interventions.jsonl.
func compileEntrySchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile("atomyx-run.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("run.schema.json", bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("run.schema.json#/definitions/InterventionEntry")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func validateLine(t *testing.T, schema *jsonschema.Schema, line string) error {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		t.Fatalf("line is not JSON: %q", line)
	}
	return schema.Validate(doc)
}

func TestWrittenInterventionsMatchTheGoldenFileAndTheSchema(t *testing.T) {
	w := startOne(t, t.TempDir())
	w.maxInterventionBytes = 400
	lines := []InterventionLine{
		{Kind: "viewerAttached", AtMs: 1000, Client: "atomyx inspect"},
		{
			Kind: "humanAction", AtMs: 2000, Client: "atomyx inspect",
			Step: "- type: '********'", Command: "type", Outcome: "failed",
			Message: "no field was ready for input", DurationMs: 1500,
			Artifacts: []string{"artifacts/root-01-shot.png"},
		},
		{Kind: "viewerDetached", AtMs: 3000, Client: "atomyx inspect"},
		{Kind: "viewerDetached", AtMs: 4000, Client: "past the bound"},
	}
	for _, line := range lines {
		if err := w.AppendIntervention(line); err != nil {
			t.Fatal(err)
		}
	}

	written, err := os.ReadFile(filepath.Join(w.Dir(), InterventionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "interventions.golden.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(golden) {
		t.Fatalf("interventions.jsonl differs from the golden file:\n%s", written)
	}
	schema := compileEntrySchema(t)
	for _, line := range strings.Split(strings.TrimSuffix(string(written), "\n"), "\n") {
		if err := validateLine(t, schema, line); err != nil {
			t.Errorf("line %q does not match the schema: %v", line, err)
		}
	}
}

func TestInterventionSchemaRejectsALineWithoutItsRequiredFields(t *testing.T) {
	schema := compileEntrySchema(t)
	for _, bad := range []string{`{"atMs":1,"client":"c"}`, `{"kind":"humanAction","atMs":"soon","client":"c"}`, `{"kind":"humanAction","atMs":1,"client":"c","artifacts":"a.png"}`} {
		if err := validateLine(t, schema, bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}
