package config

import "testing"

// determinismIterations bounds how many times a nondeterminism
// regression test re-runs the function under test. Go randomizes map
// iteration order per run within a single process, so a handful of
// repetitions is enough to surface a source that is not sorting its
// keys before use, without making the test noticeably slow.
const determinismIterations = 30

func TestCheckUnknownKeys_ReportsExtraKeysInSortedOrder(t *testing.T) {
	yamlContent := baseConfig + "\nzzz: 1\naaa: 2\nmmm: 3\n"

	want := "unrecognized key(s): [aaa mmm zzz]"
	for i := 0; i < determinismIterations; i++ {
		issues := mustReject(t, yamlContent)
		found := false
		for _, issue := range issues {
			if issue.Message == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("run %d: issues = %v, want one with message %q", i, issues, want)
		}
	}
}

func TestValidateEnvironments_ReportsIssuesInSortedNameOrder(t *testing.T) {
	yamlContent := `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
environments:
  e5: {}
  e1: {}
  e3: {}
  e2: {}
  e4: {}
`
	for i := 0; i < determinismIterations; i++ {
		issues := mustReject(t, yamlContent)
		if len(issues) == 0 {
			t.Fatalf("run %d: expected validation issues, got none", i)
		}
		if got := pathString(issues[0].Path); got != "environments.e1.app" {
			t.Fatalf("run %d: issues[0].Path = %q, want environments.e1.app", i, got)
		}
	}
}

func TestCheckEnvironmentAppReferences_ReportsIssuesInSortedNameOrder(t *testing.T) {
	yamlContent := `
project: Demo
apps:
  - id: main
    platform: android
    appId: com.example.app
environments:
  e5:
    app: bogus
  e1:
    app: bogus
  e3:
    app: bogus
  e2:
    app: bogus
  e4:
    app: bogus
`
	for i := 0; i < determinismIterations; i++ {
		issues := mustReject(t, yamlContent)
		if len(issues) == 0 {
			t.Fatalf("run %d: expected validation issues, got none", i)
		}
		if got := pathString(issues[0].Path); got != "environments.e1.app" {
			t.Fatalf("run %d: issues[0].Path = %q, want environments.e1.app", i, got)
		}
	}
}

func TestValidateSuites_ReportsIssuesInSortedNameOrder(t *testing.T) {
	yamlContent := baseConfig + `
suites:
  s5: {}
  s1: {}
  s3: {}
  s2: {}
  s4: {}
`
	for i := 0; i < determinismIterations; i++ {
		issues := mustReject(t, yamlContent)
		if len(issues) == 0 {
			t.Fatalf("run %d: expected validation issues, got none", i)
		}
		if got := pathString(issues[0].Path); got != "suites.s1" {
			t.Fatalf("run %d: issues[0].Path = %q, want suites.s1", i, got)
		}
	}
}
