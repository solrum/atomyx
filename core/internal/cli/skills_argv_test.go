package cli

import (
	"errors"
	"testing"
)

func TestParseSkillsArgvInitDefaults(t *testing.T) {
	argv, err := ParseSkillsArgv(nil, SkillsCommandInit)
	if err != nil {
		t.Fatalf("ParseSkillsArgv: %v", err)
	}
	if argv.Help {
		t.Errorf("Help: got true, want false")
	}
	if len(argv.Flags) != 0 {
		t.Errorf("Flags: got %v, want empty", argv.Flags)
	}
}

func TestParseSkillsArgvInitTargetAndForce(t *testing.T) {
	argv, err := ParseSkillsArgv([]string{"--target", "/tmp/dest", "--force"}, SkillsCommandInit)
	if err != nil {
		t.Fatalf("ParseSkillsArgv: %v", err)
	}
	if got, ok := flagString(argv.Flags, "--target"); !ok || got != "/tmp/dest" {
		t.Errorf("--target: got %q, ok=%v", got, ok)
	}
	if !flagBool(argv.Flags, "--force") {
		t.Errorf("--force: got false, want true")
	}
}

func TestParseSkillsArgvInitTargetEquals(t *testing.T) {
	argv, err := ParseSkillsArgv([]string{"--target=/tmp/dest"}, SkillsCommandInit)
	if err != nil {
		t.Fatalf("ParseSkillsArgv: %v", err)
	}
	if got, ok := flagString(argv.Flags, "--target"); !ok || got != "/tmp/dest" {
		t.Errorf("--target: got %q, ok=%v", got, ok)
	}
}

func TestParseSkillsArgvUpdateSkillsRejectsForce(t *testing.T) {
	_, err := ParseSkillsArgv([]string{"--force"}, SkillsCommandUpdateSkills)
	if err == nil {
		t.Fatal("expected an error for --force on update-skills")
	}
	var argvErr *ArgvError
	if !errors.As(err, &argvErr) {
		t.Fatalf("expected *ArgvError, got %T: %v", err, err)
	}
}

func TestParseSkillsArgvHelpShortCircuits(t *testing.T) {
	argv, err := ParseSkillsArgv([]string{"--help"}, SkillsCommandInit)
	if err != nil {
		t.Fatalf("ParseSkillsArgv: %v", err)
	}
	if !argv.Help {
		t.Errorf("Help: got false, want true")
	}
}

func TestParseSkillsArgvEmptyEqualsValueErrors(t *testing.T) {
	_, err := ParseSkillsArgv([]string{"--target="}, SkillsCommandInit)
	if err == nil {
		t.Fatal("expected an error for an empty --target= value")
	}
}

func TestParseSkillsArgvDuplicateFlagErrors(t *testing.T) {
	_, err := ParseSkillsArgv([]string{"--force", "--force"}, SkillsCommandInit)
	if err == nil {
		t.Fatal("expected an error for a duplicate --force")
	}
}

func TestParseSkillsArgvMissingValueErrors(t *testing.T) {
	_, err := ParseSkillsArgv([]string{"--target"}, SkillsCommandInit)
	if err == nil {
		t.Fatal("expected an error for --target with no value")
	}
}
