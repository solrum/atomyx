package config

import "testing"

func mustValidate(t *testing.T, yamlContent string) *AtomyxConfig {
	t.Helper()
	cfg, issues := ParseAndValidate([]byte(yamlContent))
	if len(issues) > 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
	return cfg
}

func mustReject(t *testing.T, yamlContent string) []ValidationIssue {
	t.Helper()
	cfg, issues := ParseAndValidate([]byte(yamlContent))
	if len(issues) == 0 {
		t.Fatalf("expected validation issues, got a valid config: %+v", cfg)
	}
	return issues
}

const baseConfig = `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
environments:
  staging:
    app: main
`

func TestParseAndValidate_AcceptsValidMinimalConfig(t *testing.T) {
	cfg := mustValidate(t, baseConfig)
	if cfg.Project != "Demo" {
		t.Errorf("Project = %q, want Demo", cfg.Project)
	}
	if len(cfg.Apps) != 1 || cfg.Apps[0].ID != "main" {
		t.Errorf("Apps = %+v", cfg.Apps)
	}
}

func TestParseAndValidate_RejectsEnvironmentReferencingUnknownAppID(t *testing.T) {
	issues := mustReject(t, `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
environments:
  staging:
    app: does-not-exist
`)
	found := false
	for _, issue := range issues {
		if pathString(issue.Path) == "environments.staging.app" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an issue at environments.staging.app, got %v", issues)
	}
}

func TestParseAndValidate_RejectsSuiteWithNeitherTagsNorGlobs(t *testing.T) {
	mustReject(t, baseConfig+"suites:\n  smoke: {}\n")
}

func TestParseAndValidate_AcceptsSuiteWithOnlyTags(t *testing.T) {
	mustValidate(t, baseConfig+"suites:\n  smoke:\n    tags: [smoke]\n")
}

func TestParseAndValidate_AcceptsEachPreconditionsOverrideValue(t *testing.T) {
	for _, value := range []string{PreconditionsSkip, PreconditionsForce} {
		mustValidate(t, baseConfig+"preconditions: "+value+"\n")
	}
}

func TestParseAndValidate_RejectsUnknownPreconditionsOverrideValue(t *testing.T) {
	mustReject(t, baseConfig+"preconditions: always\n")
}

func TestParseAndValidate_RejectsReportWithEmptyFormats(t *testing.T) {
	mustReject(t, baseConfig+"report:\n  formats: []\n  outDir: reports\n")
}

func TestParseAndValidate_AcceptsEnvironmentWithExactDeviceID(t *testing.T) {
	cfg := mustValidate(t, `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
environments:
  staging:
    app: main
    device: emulator-5554
`)
	env := cfg.Environments["staging"]
	if env.Device == nil || *env.Device != "emulator-5554" {
		t.Errorf("Device = %v, want emulator-5554", env.Device)
	}
}

func TestParseAndValidate_RejectsEnvironmentDeviceGivenAsAnObject(t *testing.T) {
	mustReject(t, `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
environments:
  staging:
    app: main
    device:
      platform: android
      match: Pixel
`)
}

func TestParseAndValidate_AcceptsConfigWithZeroEnvironments(t *testing.T) {
	cfg := mustValidate(t, `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
environments: {}
`)
	if len(cfg.Environments) != 0 {
		t.Errorf("Environments = %+v, want empty", cfg.Environments)
	}
}

func TestParseAndValidate_LeavesLaunchModeNilWhenLaunchSectionOmitted(t *testing.T) {
	cfg := mustValidate(t, baseConfig)
	if cfg.Launch != nil {
		t.Errorf("Launch = %+v, want nil", cfg.Launch)
	}
}

func TestParseAndValidate_LeavesLaunchModeNilWhenOmittedFromExplicitLaunchSection(t *testing.T) {
	cfg := mustValidate(t, baseConfig+"launch: {}\n")
	if cfg.Launch == nil || cfg.Launch.Mode != nil {
		t.Errorf("Launch = %+v, want non-nil with nil Mode", cfg.Launch)
	}
}

func TestParseAndValidate_AcceptsExplicitLaunchMode(t *testing.T) {
	cfg := mustValidate(t, baseConfig+"launch:\n  mode: fresh\n")
	if cfg.Launch == nil || cfg.Launch.Mode == nil || *cfg.Launch.Mode != LaunchModeFresh {
		t.Errorf("Launch.Mode = %v, want fresh", cfg.Launch)
	}
}

func TestParseAndValidate_AcceptsExplicitLaunchClearData(t *testing.T) {
	cfg := mustValidate(t, baseConfig+"launch:\n  mode: fresh\n  clearData: true\n")
	if cfg.Launch == nil || cfg.Launch.ClearData == nil || !*cfg.Launch.ClearData {
		t.Errorf("Launch.ClearData = %v, want true", cfg.Launch)
	}
}

func TestParseAndValidate_RejectsTopLevelUnknownKey(t *testing.T) {
	mustReject(t, baseConfig+"bogusTopLevelField: nope\n")
}

func TestParseAndValidate_RejectsAppEntryUnknownKey(t *testing.T) {
	mustReject(t, `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
    bogus: nope
environments:
  staging:
    app: main
`)
}

func TestParseAndValidate_RejectsMissingRequiredField(t *testing.T) {
	issues := mustReject(t, `
apps:
  - id: main
    platform: android
    appId: com.example.app
environments:
  staging:
    app: main
`)
	found := false
	for _, issue := range issues {
		if pathString(issue.Path) == "project" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an issue at project, got %v", issues)
	}
}

func TestParseAndValidate_RejectsEmptyApps(t *testing.T) {
	mustReject(t, `
project: Demo
apps: []
environments: {}
`)
}

func TestParseAndValidate_RejectsMalformedYAML(t *testing.T) {
	issues := mustReject(t, "project: [unterminated\n")
	if len(issues) != 1 || pathString(issues[0].Path) != "(root)" {
		t.Errorf("issues = %v, want a single root-level issue", issues)
	}
}

func TestParseAndValidate_RejectsNonMappingRoot(t *testing.T) {
	mustReject(t, "- just\n- a\n- list\n")
}

func TestParseAndValidate_PreservesEnvironmentDeclarationOrder(t *testing.T) {
	cfg := mustValidate(t, `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
  - id: alt
    platform: ios
    appId: com.example.alt
environments:
  zeta:
    app: main
  alpha:
    app: alt
`)
	want := []string{"zeta", "alpha"}
	if len(cfg.EnvironmentOrder) != len(want) || cfg.EnvironmentOrder[0] != want[0] || cfg.EnvironmentOrder[1] != want[1] {
		t.Errorf("EnvironmentOrder = %v, want %v", cfg.EnvironmentOrder, want)
	}
}

func TestParseAndValidate_KeepsPresentEmptySuiteContainerFields(t *testing.T) {
	cfg := mustValidate(t, baseConfig+"suites:\n  smoke:\n    tags: []\n    globs: [x]\n")
	entry := cfg.Suites
	if entry == nil {
		t.Fatal("Suites = nil, want non-nil")
	}
	smoke := (*entry)["smoke"]
	if smoke.Tags == nil || len(*smoke.Tags) != 0 {
		t.Errorf("Tags = %v, want a present, empty slice", smoke.Tags)
	}
}

func TestParseAndValidate_ReadsTheSessionTimeouts(t *testing.T) {
	cfg := mustValidate(t, baseConfig+`
session:
  mcpIdleTimeoutMs: 20000
  openRunIdleTimeoutMs: 30000
  deviceAcquireTimeoutMs: 90000
  debugPauseTimeoutMs: 120000
`)
	s := cfg.Session
	if s == nil || *s.McpIdleTimeoutMs != 20000 || *s.OpenRunIdleTimeoutMs != 30000 || *s.DeviceAcquireTimeoutMs != 90000 || *s.DebugPauseTimeoutMs != 120000 {
		t.Fatalf("Session = %+v, want all four timeouts read", s)
	}
}

func TestParseAndValidate_AConfigWithoutASessionBlockHasNoSession(t *testing.T) {
	if cfg := mustValidate(t, baseConfig); cfg.Session != nil {
		t.Fatalf("Session = %+v, want nil", cfg.Session)
	}
}

func TestParseAndValidate_RejectsInvalidSessionTimeouts(t *testing.T) {
	cases := map[string]string{
		"zero":     "session:\n  mcpIdleTimeoutMs: 0\n",
		"negative": "session:\n  openRunIdleTimeoutMs: -1\n",
		"fraction": "session:\n  deviceAcquireTimeoutMs: 1.5\n",
		"string":   "session:\n  mcpIdleTimeoutMs: soon\n",
		"pause":    "session:\n  debugPauseTimeoutMs: 0\n",
		"unknown":  "session:\n  idleMs: 5\n",
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) { mustReject(t, baseConfig+block) })
	}
}
