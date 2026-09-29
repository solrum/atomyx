package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func skillsDeps(stdout, stderr *bytes.Buffer) Deps {
	return Deps{Stdout: stdout, Stderr: stderr}
}

func TestExecuteSkillsHelpPrintsModuleHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := ExecuteSkills(context.Background(), skillsDeps(&stdout, &stderr), nil)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "atomyx skills") {
		t.Errorf("stdout: got %q", stdout.String())
	}
}

func TestExecuteSkillsUnknownCommandExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := ExecuteSkills(context.Background(), skillsDeps(&stdout, &stderr), []string{"bogus"})
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `unknown skills command "bogus"`) {
		t.Errorf("stderr: got %q", stderr.String())
	}
}

func TestExecuteSkillsInitHelpFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := ExecuteSkills(context.Background(), skillsDeps(&stdout, &stderr), []string{"init", "--help"})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "atomyx init") {
		t.Errorf("stdout: got %q", stdout.String())
	}
}

func TestExecuteSkillsInitBadFlagPrintsCommandHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := ExecuteSkills(context.Background(), skillsDeps(&stdout, &stderr), []string{"init", "--bogus"})
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), `does not accept "--bogus"`) {
		t.Errorf("stderr: got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "atomyx init") {
		t.Errorf("stderr missing command help: got %q", stderr.String())
	}
}

func TestExecuteSkillsInitRunsAgainstTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".claude")

	var stdout, stderr bytes.Buffer
	code := ExecuteSkills(context.Background(), skillsDeps(&stdout, &stderr), []string{"init", "--target", target})
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Atomyx skills installed") {
		t.Errorf("stdout: got %q", stdout.String())
	}
}
