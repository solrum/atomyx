package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTestKeyOfUsesTheNearestConfigDirectory(t *testing.T) {
	root := t.TempDir()
	flows := filepath.Join(root, "flows")
	if err := os.MkdirAll(flows, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "atomyx.config.yml"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := testKeyOf(filepath.Join(flows, "a.yml")), "flows/a.yml"; got != want {
		t.Fatalf("testKeyOf = %q, want %q", got, want)
	}
}

func TestTestKeyOfFallsBackToTheGitRoot(t *testing.T) {
	root := t.TempDir()
	tests := filepath.Join(root, "tests")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tests, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := testKeyOf(filepath.Join(tests, "a.yml")), "tests/a.yml"; got != want {
		t.Fatalf("testKeyOf = %q, want %q", got, want)
	}
}
