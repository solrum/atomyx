package config

import "testing"

func fixtureLoaded(environments map[string]EnvironmentEntry) *LoadedConfig {
	artifact := "/builds/app.apk"
	return &LoadedConfig{
		Path: "/fake/atomyx.config.yaml",
		Config: AtomyxConfig{
			Project: "Demo",
			Apps: []AppEntry{
				{ID: "main", Platform: PlatformAndroid, AppID: "com.example.app", Artifact: &artifact},
				{ID: "alt", Platform: PlatformIOS, AppID: "com.example.alt"},
			},
			Environments: environments,
		},
	}
}

func TestSelectEnvironment_ReturnsTheNamedEnvironment(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{
		"staging": {App: "main"},
		"prod":    {App: "alt"},
	})
	result, err := SelectEnvironment(loaded, "prod", true)
	if err != nil {
		t.Fatalf("SelectEnvironment: %v", err)
	}
	if result.Name != "prod" || result.AppID != "com.example.alt" {
		t.Errorf("result = %+v", result)
	}
}

func TestSelectEnvironment_ErrorsNamingAvailableEnvironmentsWhenRequestedOneIsAbsent(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{"staging": {App: "main"}})
	loaded.Config.EnvironmentOrder = []string{"staging"}
	_, err := SelectEnvironment(loaded, "missing", true)
	if err == nil {
		t.Fatal("expected an error")
	}
	want := `Environment "missing" not found in /fake/atomyx.config.yaml. Available environments: staging.`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestSelectEnvironment_ErrorsTellingTheCallerVarReplacesTheOldEnvInjectionSyntax(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{"staging": {App: "main"}})
	_, err := SelectEnvironment(loaded, "FOO=bar", true)
	if err == nil {
		t.Fatal("expected an error")
	}
	want := "--env now selects a named environment from the project config, and no longer injects a variable. " +
		"Use --var FOO=bar to inject a variable."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestSelectEnvironment_ErrorsWhenEnvGivenButNoConfigWasFound(t *testing.T) {
	_, err := SelectEnvironment(nil, "staging", true)
	if err == nil {
		t.Fatal("expected an error")
	}
	want := `--env "staging" was given, but no atomyx.config.yaml or atomyx.config.yml was found.`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

func TestSelectEnvironment_AppliesNoEnvironmentWhenNoConfigWasFoundAndEnvIsOmitted(t *testing.T) {
	result, err := SelectEnvironment(nil, "", false)
	if err != nil || result != nil {
		t.Errorf("result = %+v, err = %v, want (nil, nil)", result, err)
	}
}

func TestSelectEnvironment_ReturnsTheOnlyEnvironmentWhenNameIsOmittedAndExactlyOneExists(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{"staging": {App: "main"}})
	result, err := SelectEnvironment(loaded, "", false)
	if err != nil {
		t.Fatalf("SelectEnvironment: %v", err)
	}
	if result.Name != "staging" || result.AppID != "com.example.app" {
		t.Errorf("result = %+v", result)
	}
}

func TestSelectEnvironment_ResolvesPlatformFromTheSelectedAppNotFromADeviceField(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{"prod": {App: "alt"}})
	result, err := SelectEnvironment(loaded, "", false)
	if err != nil {
		t.Fatalf("SelectEnvironment: %v", err)
	}
	if result.Platform != PlatformIOS || result.DeviceID != nil {
		t.Errorf("result = %+v", result)
	}
}

func TestSelectEnvironment_ExposesTheEnvironmentsDeviceAsAnExactDeviceID(t *testing.T) {
	deviceID := "emulator-5554"
	loaded := fixtureLoaded(map[string]EnvironmentEntry{"staging": {App: "main", Device: &deviceID}})
	result, err := SelectEnvironment(loaded, "", false)
	if err != nil {
		t.Fatalf("SelectEnvironment: %v", err)
	}
	if result.DeviceID == nil || *result.DeviceID != deviceID || result.Platform != PlatformAndroid {
		t.Errorf("result = %+v", result)
	}
}

func TestSelectEnvironment_AppliesNoEnvironmentWhenZeroEnvironmentsExistAndNameIsOmitted(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{})
	result, err := SelectEnvironment(loaded, "", false)
	if err != nil || result != nil {
		t.Errorf("result = %+v, err = %v, want (nil, nil)", result, err)
	}
}

func TestSelectEnvironment_ExposesTheSelectedAppsArtifactPath(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{"staging": {App: "main"}})
	result, err := SelectEnvironment(loaded, "", false)
	if err != nil {
		t.Fatalf("SelectEnvironment: %v", err)
	}
	if result.Artifact == nil || *result.Artifact != "/builds/app.apk" {
		t.Errorf("Artifact = %v, want /builds/app.apk", result.Artifact)
	}
}

func TestSelectEnvironment_LeavesArtifactNilWhenTheSelectedAppDeclaresNone(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{"prod": {App: "alt"}})
	result, err := SelectEnvironment(loaded, "", false)
	if err != nil {
		t.Fatalf("SelectEnvironment: %v", err)
	}
	if result.Artifact != nil {
		t.Errorf("Artifact = %v, want nil", result.Artifact)
	}
}

func TestSelectEnvironment_AppliesNoEnvironmentWhenSeveralEnvironmentsExistAndNameIsOmitted(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{
		"staging": {App: "main"},
		"prod":    {App: "alt"},
	})
	result, err := SelectEnvironment(loaded, "", false)
	if err != nil || result != nil {
		t.Errorf("result = %+v, err = %v, want (nil, nil)", result, err)
	}
}

func TestSelectEnvironment_DefaultsVarsToAnEmptyMapWhenAbsent(t *testing.T) {
	loaded := fixtureLoaded(map[string]EnvironmentEntry{"staging": {App: "main"}})
	result, err := SelectEnvironment(loaded, "", false)
	if err != nil {
		t.Fatalf("SelectEnvironment: %v", err)
	}
	if result.Vars == nil || len(result.Vars) != 0 {
		t.Errorf("Vars = %v, want an empty, non-nil map", result.Vars)
	}
}
