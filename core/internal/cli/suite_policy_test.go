package cli

import (
	"path/filepath"
	"testing"

	"dev.atomyx/internal/config"
)

func configWithSuite(retries int64, quarantine ...string) *config.LoadedConfig {
	entry := config.SuiteEntry{Retries: &retries}
	if len(quarantine) > 0 {
		entry.Quarantine = &quarantine
	}
	return &config.LoadedConfig{Config: config.AtomyxConfig{Suites: &map[string]config.SuiteEntry{"smoke": entry}}}
}

func TestResolveSuitePolicyTakesTheConfigValuesAndLetsTheFlagOverrideRetries(t *testing.T) {
	name := "smoke"
	loaded := configWithSuite(2, "flaky/**")

	fromConfig, err := resolveSuitePolicy(&name, map[string]FlagValue{}, loaded)
	if err != nil || fromConfig.Retries != 2 || len(fromConfig.Quarantine) != 1 {
		t.Fatalf("config policy = %+v, %v, want 2 retries and one glob", fromConfig, err)
	}
	overridden, err := resolveSuitePolicy(&name, map[string]FlagValue{"--retries": "1"}, loaded)
	if err != nil || overridden.Retries != 1 || len(overridden.Quarantine) != 1 {
		t.Fatalf("overridden policy = %+v, %v, want 1 retry and the config quarantine", overridden, err)
	}
	zero, err := resolveSuitePolicy(&name, map[string]FlagValue{"--retries": "0"}, loaded)
	if err != nil || zero.Retries != 0 {
		t.Fatalf("--retries 0 = %+v, %v, want the flag to switch retries off", zero, err)
	}
}

func TestResolveSuitePolicyForAnAdHocSuiteUsesOnlyTheFlag(t *testing.T) {
	none, err := resolveSuitePolicy(nil, map[string]FlagValue{}, configWithSuite(3, "x"))
	if err != nil || none.Retries != 0 || len(none.Quarantine) != 0 {
		t.Fatalf("ad-hoc policy = %+v, %v, want none", none, err)
	}
	flagged, err := resolveSuitePolicy(nil, map[string]FlagValue{"--retries": "3"}, nil)
	if err != nil || flagged.Retries != 3 {
		t.Fatalf("ad-hoc --retries = %+v, %v, want 3", flagged, err)
	}
}

func TestResolveSuitePolicyRejectsABadRetriesFlag(t *testing.T) {
	for _, bad := range []string{"4", "-1", "two", "1.5", ""} {
		if _, err := resolveSuitePolicy(nil, map[string]FlagValue{"--retries": bad}, nil); err == nil {
			t.Errorf("--retries %q accepted", bad)
		}
	}
}

func TestQuarantineMatcherMatchesRelativeToTheSearchRoots(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator), "work")
	match := quarantineMatcher([]string{"flaky/**"}, cwd, []string{"tests"})

	if !match(filepath.Join(cwd, "tests", "flaky", "a.yml")) {
		t.Error("an entry under the test root's flaky directory is not quarantined")
	}
	if match(filepath.Join(cwd, "tests", "stable", "a.yml")) {
		t.Error("an unrelated entry is quarantined")
	}
	if match(filepath.Join(cwd, "other", "flaky", "a.yml")) {
		t.Error("an entry outside every test root is quarantined")
	}
	if quarantineMatcher(nil, cwd, nil) != nil {
		t.Error("an empty quarantine list yields a matcher")
	}
	atCwd := quarantineMatcher([]string{"flaky/**"}, cwd, nil)
	if !atCwd(filepath.Join(cwd, "flaky", "a.yml")) {
		t.Error("without test roots the globs match relative to the working directory")
	}
}
