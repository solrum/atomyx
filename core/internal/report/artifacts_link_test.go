package report

import (
	"path/filepath"
	"testing"
)

func TestArtifactsLink(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "base", "run")
	tests := []struct {
		name         string
		reportDir    string
		artifactsDir string
		want         string
	}{
		{"sibling directories", filepath.Join(root, "report"), filepath.Join(root, "artifacts"), "../artifacts/"},
		{"same directory", root, root, ""},
		{"artifacts below the report directory", root, filepath.Join(root, "shots"), "shots/"},
		{"deeper report directory", filepath.Join(root, "a", "b"), filepath.Join(root, "artifacts"), "../../artifacts/"},
		{"segment needing escape", filepath.Join(root, "report"), filepath.Join(root, "my shots"), "../my%20shots/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ArtifactsLink(tc.reportDir, tc.artifactsDir)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("ArtifactsLink = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestArtifactsLinkMixedAbsoluteAndRelativeFails(t *testing.T) {
	if _, err := ArtifactsLink("report", filepath.Join(string(filepath.Separator), "abs")); err == nil {
		t.Error("expected an error for one absolute and one relative directory")
	}
}
