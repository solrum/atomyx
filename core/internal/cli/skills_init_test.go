package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/skills"
)

func TestRunInitWritesTreeAndPrintsSummary(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := RunInit(skillsDeps(&stdout, &stderr), map[string]FlagValue{"--target": dir})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Atomyx skills installed (v"+skills.Version+")") {
		t.Errorf("missing install headline: got %q", out)
	}
	if !strings.Contains(out, "Destination: "+dir) {
		t.Errorf("missing destination line: got %q", out)
	}
	for _, f := range skills.AllFiles() {
		if !strings.Contains(out, "  + "+f) {
			t.Errorf("missing file line for %s: got %q", f, out)
		}
	}

	for _, name := range skills.SkillFiles {
		if _, err := os.Stat(filepath.Join(dir, "skills", name)); err != nil {
			t.Errorf("skills/%s not written: %v", name, err)
		}
	}
	for _, name := range skills.AgentFiles {
		if _, err := os.Stat(filepath.Join(dir, "agents", name)); err != nil {
			t.Errorf("agents/%s not written: %v", name, err)
		}
	}
}

func TestRunInitWithoutForceRefusesExistingInstall(t *testing.T) {
	dir := t.TempDir()
	var stdout1, stderr1 bytes.Buffer
	if code := RunInit(skillsDeps(&stdout1, &stderr1), map[string]FlagValue{"--target": dir}); code != 0 {
		t.Fatalf("first RunInit: exit code %d, stderr: %s", code, stderr1.String())
	}

	var stdout2, stderr2 bytes.Buffer
	code := RunInit(skillsDeps(&stdout2, &stderr2), map[string]FlagValue{"--target": dir})
	if code != 1 {
		t.Fatalf("exit code: got %d, want 1", code)
	}
	if !strings.Contains(stderr2.String(), "files already exist in "+dir) {
		t.Errorf("stderr: got %q", stderr2.String())
	}
	if !strings.Contains(stderr2.String(), "--force") {
		t.Errorf("stderr missing --force guidance: got %q", stderr2.String())
	}
}

func TestRunInitWithForceOverwritesExistingInstall(t *testing.T) {
	dir := t.TempDir()
	var stdout1, stderr1 bytes.Buffer
	if code := RunInit(skillsDeps(&stdout1, &stderr1), map[string]FlagValue{"--target": dir}); code != 0 {
		t.Fatalf("first RunInit: exit code %d, stderr: %s", code, stderr1.String())
	}

	stale := filepath.Join(dir, "skills", skills.SkillFiles[0])
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout2, stderr2 bytes.Buffer
	code := RunInit(skillsDeps(&stdout2, &stderr2), map[string]FlagValue{"--target": dir, "--force": true})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr2.String())
	}
	got, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "stale" {
		t.Error("expected --force to overwrite the stale file")
	}
}

func TestRunInitDefaultsTargetToCwdDotClaude(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	var stdout, stderr bytes.Buffer
	code := RunInit(skillsDeps(&stdout, &stderr), map[string]FlagValue{})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", skills.SkillFiles[0])); err != nil {
		t.Errorf("expected install under <cwd>/.claude: %v", err)
	}
}
