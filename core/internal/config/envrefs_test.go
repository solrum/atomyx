package config

import (
	"os"
	"testing"
)

func TestResolveEnvRefs_SubstitutesFromProcessEnvironment(t *testing.T) {
	t.Setenv("ATOMYX_TEST_TOKEN", "secret-value")

	cfg := mustValidate(t, baseConfig+"\n")
	env := cfg.Environments["staging"]
	vars := map[string]string{"token": "${ENV:ATOMYX_TEST_TOKEN}"}
	env.Vars = &vars
	cfg.Environments = map[string]EnvironmentEntry{"staging": env}

	resolved, err := ResolveEnvRefs(*cfg, "/atomyx.config.yaml")
	if err != nil {
		t.Fatalf("ResolveEnvRefs: %v", err)
	}
	got := (*resolved.Environments["staging"].Vars)["token"]
	if got != "secret-value" {
		t.Errorf("token = %q, want secret-value", got)
	}
}

func TestResolveEnvRefs_ErrorsNamingTheMissingVariable(t *testing.T) {
	_ = os.Unsetenv("ATOMYX_TEST_TOKEN_MISSING")

	cfg := mustValidate(t, baseConfig+"\n")
	env := cfg.Environments["staging"]
	vars := map[string]string{"token": "${ENV:ATOMYX_TEST_TOKEN_MISSING}"}
	env.Vars = &vars
	cfg.Environments = map[string]EnvironmentEntry{"staging": env}

	_, err := ResolveEnvRefs(*cfg, "/atomyx.config.yaml")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	want := `/atomyx.config.yaml references environment variable "ATOMYX_TEST_TOKEN_MISSING" in environments.staging.vars.token, but it is not set.`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestResolveEnvRefs_ErrorsOnTheAlphabeticallyFirstMissingVariable(t *testing.T) {
	_ = os.Unsetenv("ATOMYX_TEST_TOKEN_A")
	_ = os.Unsetenv("ATOMYX_TEST_TOKEN_B")
	_ = os.Unsetenv("ATOMYX_TEST_TOKEN_C")

	cfg := mustValidate(t, baseConfig+"\n")
	env := cfg.Environments["staging"]
	vars := map[string]string{
		"z": "${ENV:ATOMYX_TEST_TOKEN_C}",
		"x": "${ENV:ATOMYX_TEST_TOKEN_A}",
		"y": "${ENV:ATOMYX_TEST_TOKEN_B}",
	}
	env.Vars = &vars
	cfg.Environments = map[string]EnvironmentEntry{"staging": env}

	want := `/atomyx.config.yaml references environment variable "ATOMYX_TEST_TOKEN_A" in environments.staging.vars.x, but it is not set.`
	for i := 0; i < determinismIterations; i++ {
		_, err := ResolveEnvRefs(*cfg, "/atomyx.config.yaml")
		if err == nil {
			t.Fatalf("run %d: expected an error, got nil", i)
		}
		if err.Error() != want {
			t.Fatalf("run %d: err = %q, want %q", i, err.Error(), want)
		}
	}
}

func TestResolveEnvRefs_NamesTheVarsThatHeldAReference(t *testing.T) {
	t.Setenv("ATOMYX_TEST_TOKEN", "secret-value")

	cfg := mustValidate(t, baseConfig+"\n")
	env := cfg.Environments["staging"]
	vars := map[string]string{"token": "Bearer ${ENV:ATOMYX_TEST_TOKEN}", "literal": "plain"}
	env.Vars = &vars
	cfg.Environments = map[string]EnvironmentEntry{"staging": env}

	resolved, err := ResolveEnvRefs(*cfg, "/atomyx.config.yaml")
	if err != nil {
		t.Fatalf("ResolveEnvRefs: %v", err)
	}
	fromEnv := resolved.Environments["staging"].EnvRefVars
	if !fromEnv["token"] || fromEnv["literal"] {
		t.Errorf("EnvRefVars = %v, want only token", fromEnv)
	}
}
