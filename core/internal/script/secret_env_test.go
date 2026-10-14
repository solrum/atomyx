package script

import "testing"

func TestParseScriptMarksOnlyExternalEntriesSecret(t *testing.T) {
	yaml := "env:\n  LITERAL: written\n  SHADOWED: written\n---\n- sleep: 1\n"
	def, err := ParseScript(yaml, map[string]string{"SHADOWED": "external", "TOKEN": "external"})
	if err != nil {
		t.Fatal(err)
	}
	if def.SecretEnv["LITERAL"] {
		t.Errorf("script literal is secret: %v", def.SecretEnv)
	}
	if !def.SecretEnv["SHADOWED"] || !def.SecretEnv["TOKEN"] {
		t.Errorf("external entries are not secret: %v", def.SecretEnv)
	}
}

func TestParseScriptHonoursCommittedNames(t *testing.T) {
	def, err := ParseScript("- sleep: 1\n", map[string]string{"appId": "com.example", "TOKEN": "external"},
		WithCommittedNames(map[string]bool{"appId": true}))
	if err != nil {
		t.Fatal(err)
	}
	if def.SecretEnv["appId"] || !def.SecretEnv["TOKEN"] {
		t.Errorf("secret env = %v, want only TOKEN", def.SecretEnv)
	}
}
