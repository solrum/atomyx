package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_FindsConfigByWalkingUpFromANestedCwd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "atomyx.config.yaml"), baseConfig)
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(LoadOptions{Cwd: nested})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil, want a LoadedConfig")
	}
	want := filepath.Join(root, "atomyx.config.yaml")
	if loaded.Path != want {
		t.Errorf("Path = %q, want %q", loaded.Path, want)
	}
	if loaded.Config.Project != "Demo" {
		t.Errorf("Project = %q, want Demo", loaded.Config.Project)
	}
}

func TestLoad_ReturnsNilWhenNoConfigFileExistsAnywhereAboveCwd(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(LoadOptions{Cwd: nested})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded != nil {
		t.Errorf("Load = %+v, want nil", loaded)
	}
}

func TestLoad_ExplicitConfigPathMissing_ReportsNotFound(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "does-not-exist.yaml")

	_, err := Load(LoadOptions{Cwd: root, ConfigPath: missing})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("err = %v, want a not found message", err)
	}
}

func TestLoad_GenuineReadFailure_IsSurfacedInsteadOfReportedAsNotFound(t *testing.T) {
	root := t.TempDir()
	// A directory in place of the config file passes discovery's own
	// os.Stat existence check but fails on the subsequent read, the
	// same shape a permission-denied file produces.
	dirAsConfig := filepath.Join(root, "atomyx.config.yaml")
	if err := os.Mkdir(dirAsConfig, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Load(LoadOptions{Cwd: root, ConfigPath: dirAsConfig})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want the real read error surfaced instead of a not-found message", err)
	}
	if !strings.Contains(err.Error(), dirAsConfig) {
		t.Fatalf("err = %v, want it to name the offending path %q", err, dirAsConfig)
	}
}

func TestLoad_UsesAnExplicitConfigPathInsteadOfDiscovery(t *testing.T) {
	root := t.TempDir()
	explicitDir := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(explicitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	explicitPath := filepath.Join(explicitDir, "custom.yaml")
	writeFile(t, explicitPath, baseConfig)

	loaded, err := Load(LoadOptions{Cwd: root, ConfigPath: explicitPath})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || loaded.Path != explicitPath {
		t.Errorf("Load = %+v, want Path %q", loaded, explicitPath)
	}
}

func TestLoad_NamesTheOffendingFieldPathWhenTheConfigIsInvalid(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "atomyx.config.yaml"), `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
environments:
  staging:
    app: does-not-exist
`)

	_, err := Load(LoadOptions{Cwd: root})
	if err == nil {
		t.Fatal("expected an error")
	}
	var validationErr *ValidationError
	if !asValidationError(err, &validationErr) {
		t.Fatalf("err = %v, want a *ValidationError", err)
	}
	found := false
	for _, issue := range validationErr.Issues {
		if pathString(issue.Path) == "environments.staging.app" {
			found = true
		}
	}
	if !found {
		t.Errorf("Issues = %v, want one at environments.staging.app", validationErr.Issues)
	}
}

func TestLoad_SubstitutesEnvRefsFromTheProcessEnvironment(t *testing.T) {
	t.Setenv("ATOMYX_TEST_TOKEN", "secret-value")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "atomyx.config.yaml"), baseConfig+`    vars:
      token: ${ENV:ATOMYX_TEST_TOKEN}
`)

	loaded, err := Load(LoadOptions{Cwd: root})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := (*loaded.Config.Environments["staging"].Vars)["token"]
	if got != "secret-value" {
		t.Errorf("token = %q, want secret-value", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func asValidationError(err error, target **ValidationError) bool {
	return errors.As(err, target)
}
