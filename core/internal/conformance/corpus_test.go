package conformance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCase_MetaHandshake(t *testing.T) {
	c, err := LoadCase(filepath.Join("corpus", "meta-handshake.json"))
	if err != nil {
		t.Fatalf("LoadCase returned error: %v", err)
	}
	if c.Name != "meta-handshake" {
		t.Errorf("Name = %q, want %q", c.Name, "meta-handshake")
	}
	if c.Description == "" {
		t.Error("Description is empty")
	}
	if len(c.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(c.Steps))
	}
	if c.Steps[0].Method != "protocolVersion" {
		t.Errorf("Steps[0].Method = %q, want %q", c.Steps[0].Method, "protocolVersion")
	}
	if c.Steps[1].Method != "listMethods" {
		t.Errorf("Steps[1].Method = %q, want %q", c.Steps[1].Method, "listMethods")
	}
	if len(c.Steps[0].Expect) == 0 {
		t.Error("Steps[0].Expect is empty")
	}
}

func TestLoadCase_MissingFile(t *testing.T) {
	if _, err := LoadCase(filepath.Join("corpus", "does-not-exist.json")); err == nil {
		t.Fatal("expected an error for a missing corpus file")
	}
}

func TestLoadCase_Invalid(t *testing.T) {
	dir := t.TempDir()

	noName := filepath.Join(dir, "no-name.json")
	writeFile(t, noName, `{"description":"x","steps":[{"method":"m","expect":1}]}`)
	if _, err := LoadCase(noName); err == nil {
		t.Error("expected an error for a case with no name")
	}

	noSteps := filepath.Join(dir, "no-steps.json")
	writeFile(t, noSteps, `{"name":"x","description":"x","steps":[]}`)
	if _, err := LoadCase(noSteps); err == nil {
		t.Error("expected an error for a case with no steps")
	}

	noMethod := filepath.Join(dir, "no-method.json")
	writeFile(t, noMethod, `{"name":"x","description":"x","steps":[{"expect":1}]}`)
	if _, err := LoadCase(noMethod); err == nil {
		t.Error("expected an error for a step with no method")
	}
}

func TestLoadCorpus(t *testing.T) {
	cases, err := LoadCorpus("corpus")
	if err != nil {
		t.Fatalf("LoadCorpus returned error: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("expected at least one corpus case")
	}
	found := false
	for _, c := range cases {
		if c.Name == "meta-handshake" {
			found = true
		}
	}
	if !found {
		t.Error("expected the meta-handshake case to be loaded")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing test fixture %s: %v", path, err)
	}
}
