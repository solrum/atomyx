package cli

import (
	"os"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/script"
)

func TestFlowLoaderLeavesPlaceholdersToTheEnvironmentItWasGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow.yaml")
	if err := os.WriteFile(path, []byte("- type: \"${late}\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	def, err := fsFlowLoader{}.LoadFlow(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if script.NeedsResolution(def.Steps[0]) {
		t.Fatal("a step of a loaded flow takes run variables a second time")
	}
}
