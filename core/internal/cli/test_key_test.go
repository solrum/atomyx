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

func TestTestKeyOfResolvesASymlinkedScript(t *testing.T) {
	root := t.TempDir()
	flows := filepath.Join(root, "flows")
	elsewhere := t.TempDir()
	if err := os.MkdirAll(flows, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "atomyx.config.yml"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(flows, "a.yml")
	if err := os.WriteFile(target, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(elsewhere, "alias.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got, want := testKeyOf(link), "flows/a.yml"; got != want {
		t.Fatalf("testKeyOf(link) = %q, want %q", got, want)
	}
}

func TestTestKeyOfIsTheSameFromTwoWorkingDirectories(t *testing.T) {
	root := t.TempDir()
	flows := filepath.Join(root, "flows")
	if err := os.MkdirAll(flows, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "atomyx.config.yml"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flows, "a.yml"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	fromRoot := testKeyOf(filepath.Join("flows", "a.yml"))
	t.Chdir(flows)
	fromFlows := testKeyOf("a.yml")
	if fromRoot != "flows/a.yml" || fromFlows != fromRoot {
		t.Fatalf("keys = %q from the root and %q from flows, want both flows/a.yml", fromRoot, fromFlows)
	}
}
