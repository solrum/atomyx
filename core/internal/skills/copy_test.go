package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyToWritesExpectedTree(t *testing.T) {
	dir := t.TempDir()

	result, err := CopyTo(dir, false)
	if err != nil {
		t.Fatalf("CopyTo: %v", err)
	}
	if len(result.Written) != len(SkillFiles)+len(AgentFiles) {
		t.Fatalf("written count: got %d, want %d", len(result.Written), len(SkillFiles)+len(AgentFiles))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("skipped: got %v, want none", result.Skipped)
	}

	for _, name := range SkillFiles {
		assertWrittenMatchesEmbedded(t, filepath.Join(dir, "skills", name), filepath.Join(contentRoot, "skills", name))
	}
	for _, name := range AgentFiles {
		assertWrittenMatchesEmbedded(t, filepath.Join(dir, "agents", name), filepath.Join(contentRoot, "agents", name))
	}

	if _, err := os.Stat(filepath.Join(dir, versionStampFile)); err != nil {
		t.Errorf("version stamp not written: %v", err)
	}

	staging := filepath.Join(dir, fmt.Sprintf(".claude-skills.tmp-%d", os.Getpid()))
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("staging directory left behind at %s", staging)
	}
}

func TestCopyToWithoutOverwriteFailsOnConflict(t *testing.T) {
	dir := t.TempDir()
	if _, err := CopyTo(dir, false); err != nil {
		t.Fatalf("first CopyTo: %v", err)
	}

	_, err := CopyTo(dir, false)
	if err == nil {
		t.Fatal("expected an error on the second CopyTo without overwrite")
	}
	var exist *ErrAlreadyExists
	if !errors.As(err, &exist) {
		t.Fatalf("expected *ErrAlreadyExists, got %T: %v", err, err)
	}
	if len(exist.Conflicts) != len(SkillFiles)+len(AgentFiles) {
		t.Errorf("conflicts: got %d, want %d", len(exist.Conflicts), len(SkillFiles)+len(AgentFiles))
	}
}

func TestCopyToWithOverwriteReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := CopyTo(dir, false); err != nil {
		t.Fatalf("first CopyTo: %v", err)
	}

	stale := filepath.Join(dir, "skills", SkillFiles[0])
	if err := os.WriteFile(stale, []byte("stale content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := CopyTo(dir, true); err != nil {
		t.Fatalf("second CopyTo with overwrite: %v", err)
	}

	got, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "stale content" {
		t.Error("expected overwrite to replace the stale file")
	}
}

func TestGetInstalledVersionReportsAbsentThenUpToDate(t *testing.T) {
	dir := t.TempDir()

	before := GetInstalledVersion(dir)
	if before.Version != "" || before.UpToDate {
		t.Fatalf("expected no version installed, got %+v", before)
	}
	if before.Current != Version {
		t.Errorf("current: got %q, want %q", before.Current, Version)
	}

	if _, err := CopyTo(dir, false); err != nil {
		t.Fatalf("CopyTo: %v", err)
	}

	after := GetInstalledVersion(dir)
	if after.Version != Version || !after.UpToDate {
		t.Fatalf("expected up to date at %s, got %+v", Version, after)
	}
}

func assertWrittenMatchesEmbedded(t *testing.T, writtenPath, embeddedPath string) {
	t.Helper()
	want, err := contentFS.ReadFile(embeddedPath)
	if err != nil {
		t.Fatalf("reading embedded %s: %v", embeddedPath, err)
	}
	got, err := os.ReadFile(writtenPath)
	if err != nil {
		t.Fatalf("reading written %s: %v", writtenPath, err)
	}
	if string(got) != string(want) {
		t.Errorf("content mismatch for %s", writtenPath)
	}
}
