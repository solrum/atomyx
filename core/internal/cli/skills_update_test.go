package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/skills"
)

func TestRunUpdateSkillsInstallsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := RunUpdateSkills(skillsDeps(&stdout, &stderr), map[string]FlagValue{"--target": dir})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "Atomyx skills updated: v(none) to v"+skills.Version) {
		t.Errorf("missing update headline: got %q", out)
	}
	for _, f := range skills.AllFiles() {
		if !strings.Contains(out, "  ↺ "+f) {
			t.Errorf("missing file line for %s: got %q", f, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", skills.SkillFiles[0])); err != nil {
		t.Errorf("expected skills to be written: %v", err)
	}
}

func TestRunUpdateSkillsNoOpWhenAlreadyUpToDate(t *testing.T) {
	dir := t.TempDir()
	var stdoutInit, stderrInit bytes.Buffer
	if code := RunInit(skillsDeps(&stdoutInit, &stderrInit), map[string]FlagValue{"--target": dir}); code != 0 {
		t.Fatalf("RunInit: exit code %d, stderr: %s", code, stderrInit.String())
	}

	var stdout, stderr bytes.Buffer
	code := RunUpdateSkills(skillsDeps(&stdout, &stderr), map[string]FlagValue{"--target": dir})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	want := "Atomyx skills are already up to date (v" + skills.Version + ")\n"
	if stdout.String() != want {
		t.Errorf("stdout: got %q, want %q", stdout.String(), want)
	}
}

func TestRunUpdateSkillsOverwritesStaleFile(t *testing.T) {
	dir := t.TempDir()
	var stdoutInit, stderrInit bytes.Buffer
	if code := RunInit(skillsDeps(&stdoutInit, &stderrInit), map[string]FlagValue{"--target": dir}); code != 0 {
		t.Fatalf("RunInit: exit code %d, stderr: %s", code, stderrInit.String())
	}

	stamp := filepath.Join(dir, "atomyx-skills.version.json")
	if err := os.WriteFile(stamp, []byte(`{"version":"0.0.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "skills", skills.SkillFiles[0])
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunUpdateSkills(skillsDeps(&stdout, &stderr), map[string]FlagValue{"--target": dir})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Atomyx skills updated: v0.0.1 to v"+skills.Version) {
		t.Errorf("stdout: got %q", stdout.String())
	}
	got, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "stale" {
		t.Error("expected update-skills to overwrite the stale file")
	}
}
